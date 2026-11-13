package platform

import (
	"context"
	"sync"
	"time"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people

	"github.com/rendimiento-ai/rendimiento/internal/mesa"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// mesaRuns is how far back the work board looks at runs.
const mesaRuns = 14 * 24 * time.Hour

// Mesa is the app's work board: each branch with its pull request, its last
// run and the encargos about it, and who it waits for.
func (p *Platform) Mesa(ctx context.Context, a *store.App) (mesa.Board, error) {
	now := time.Now()
	history, err := p.Store.RunHistory(ctx, a.ID, now.Add(-mesaRuns))
	if err != nil {
		return mesa.Board{}, err
	}
	runs := make([]mesa.Run, len(history))
	for i, h := range history {
		at := h.CreatedAt
		if h.FinishedAt != nil {
			at = *h.FinishedAt
		}
		runs[i] = mesa.Run{ID: h.ID, Branch: h.Branch, SHA: h.SHA, Status: h.Status, Merge: h.Merge,
			Failed: h.Failed, Release: h.Release, At: at}
	}
	open, err := p.Store.ListEncargos(ctx, a.OrgID, true, 200)
	if err != nil {
		return mesa.Board{}, err
	}
	var encargos []mesa.Encargo
	for _, e := range open {
		if e.App != a.Name {
			continue
		}
		var run int64
		if e.RunID != nil {
			run = *e.RunID
		}
		encargos = append(encargos, mesa.Encargo{ID: e.ID, Title: e.Title, Status: e.Status, Agent: e.KeyName,
			RunID: run, Links: e.Links, At: e.UpdatedAt})
	}
	got, ok := p.recentPulls(ctx, a)
	pulls := make([]mesa.Pull, 0, len(got.prs))
	for _, pr := range got.prs {
		if pr.Head.Repo.FullName != "" && pr.Head.Repo.FullName != a.Repo {
			continue // a fork's branch: its runs are not here
		}
		pulls = append(pulls, mesa.Pull{Number: pr.Number, Title: pr.Title, Branch: pr.Head.Ref, SHA: pr.Head.SHA,
			Open: pr.State == "open", MergedAt: pr.MergedAt})
	}
	return mesa.Build(a.DefaultBranch, pulls, ok, p.heads(ctx, a, history), runs, encargos, now), nil
}

// mesaChecks is how many commits one look at the board asks GitHub about
// at most; the rest are asked on the next look.
const mesaChecks = 30

// heads asks GitHub which branches exist and, for each with a run on the
// board, whether its commit is already in the default branch. A commit in
// it stays in it, so that answer is kept for good; GitHub is asked again
// about the others after branchPullsTTL.
func (p *Platform) heads(ctx context.Context, a *store.App, history []store.HistoryRun) mesa.Heads {
	if p.GitHub == nil {
		return mesa.Heads{}
	}
	list, err := p.GitHub.Branches(ctx, a.InstallationID, a.Repo)
	if err != nil || len(list) >= 100 { // more than one page: some branches would look gone
		return mesa.Heads{}
	}
	h := mesa.Heads{Known: true, SHA: map[string]string{}, InMain: map[string]bool{}}
	for _, b := range list {
		h.SHA[b.Name] = b.Commit.SHA
	}
	seen := map[string]bool{}
	asked := 0
	c := &p.mesaInMain
	for _, r := range history {
		sha, ok := h.SHA[r.Branch]
		if !ok || r.Branch == a.DefaultBranch || seen[sha] {
			continue
		}
		seen[sha] = true
		c.mu.Lock()
		e, known := c.bySHA[sha]
		c.mu.Unlock()
		if known && (e.in || time.Since(e.at) < branchPullsTTL) {
			h.InMain[sha] = e.in
			continue
		}
		if asked >= mesaChecks {
			continue
		}
		asked++
		base, _, err := p.GitHub.MergeBase(ctx, a.InstallationID, a.Repo, a.DefaultBranch, sha)
		if err != nil {
			continue
		}
		in := base == sha // the branch's commit is an ancestor of the default branch
		h.InMain[sha] = in
		c.mu.Lock()
		if c.bySHA == nil || len(c.bySHA) > 20000 {
			c.bySHA = map[string]inMainEntry{}
		}
		c.bySHA[sha] = inMainEntry{in: in, at: time.Now()}
		c.mu.Unlock()
	}
	return h
}

type inMainEntry struct {
	in bool
	at time.Time
}

// inMainCache keeps whether each commit is in its app's default branch.
type inMainCache struct {
	mu    sync.Mutex
	bySHA map[string]inMainEntry
}

// CloseSpare closes a line the work board calls spare: its open pull
// request, closed without merging, and its branch, deleted. It is a
// person's choice, as merging is: rendimiento never does it by itself.
func (p *Platform) CloseSpare(ctx context.Context, a *store.App, branch, who string) error {
	if branch == "" || branch == a.DefaultBranch {
		return &NotApprovable{Why: M("the main branch is never closed")}
	}
	b, err := p.Mesa(ctx, a)
	if err != nil {
		return err
	}
	for _, l := range b.Lines {
		if l.Branch != branch {
			continue
		}
		if l.Group != mesa.GroupSpare {
			break
		}
		if p.GitHub == nil {
			return &NotApprovable{Why: M("GitHub is not connected")}
		}
		if l.Pull != 0 && l.PullOpen {
			if err := p.GitHub.ClosePull(ctx, a.InstallationID, a.Repo, l.Pull); err != nil {
				return &NotApprovable{Why: M("GitHub did not close the pull request: %s", githubSays(err))}
			}
		}
		if err := p.GitHub.DeleteBranch(ctx, a.InstallationID, a.Repo, branch); err != nil {
			return &NotApprovable{Why: M("GitHub did not delete the branch: %s", githubSays(err))}
		}
		p.branchPullsCache.mu.Lock()
		delete(p.branchPullsCache.byApp, a.ID) // the board asks GitHub again
		p.branchPullsCache.mu.Unlock()
		p.Log.Info("work board: closed a spare branch", "app", a.Name, "branch", branch, "pull", l.Pull, "by", who)
		return nil
	}
	return &NotApprovable{Why: M("%s is not spare on the work board: only a copy or a forgotten branch is closed here", branch)}
}
