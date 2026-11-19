package platform

import (
	"context"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
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
	if p.Forja == nil {
		return mesa.Heads{}
	}
	list, err := p.repos(a).Branches(ctx, a.InstallationID, a.Repo)
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
		base, _, err := p.repos(a).MergeBase(ctx, a.InstallationID, a.Repo, a.DefaultBranch, sha)
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
		if p.Forja == nil {
			return &NotApprovable{Why: M("GitHub is not connected")}
		}
		if l.Pull != 0 && l.PullOpen {
			if err := p.repos(a).ClosePull(ctx, a.InstallationID, a.Repo, l.Pull); err != nil {
				return &NotApprovable{Why: M("GitHub did not close the pull request: %s", forja.Says(err))}
			}
		}
		if err := p.repos(a).DeleteBranch(ctx, a.InstallationID, a.Repo, branch); err != nil {
			return &NotApprovable{Why: M("GitHub did not delete the branch: %s", forja.Says(err))}
		}
		p.branchPullsCache.mu.Lock()
		delete(p.branchPullsCache.byApp, a.ID) // the board asks GitHub again
		p.branchPullsCache.mu.Unlock()
		p.Log.Info("work board: closed a spare branch", "app", a.Name, "branch", branch, "pull", l.Pull, "by", who)
		return nil
	}
	return &NotApprovable{Why: M("%s is not spare on the work board: only a copy or a forgotten branch is closed here", branch)}
}

// mesaCountTTL is how long the home page's count of an app's board is new.
// An older count is still the answer, while a new one is made behind it:
// the board asks GitHub about each app, and the home page must not wait
// for all of them each time it looks (it waited up to 17 s, and a page that
// waits that long looks offline).
const mesaCountTTL = 45 * time.Second

// mesaCountWait is how long the home page waits for the counts it never
// made; those that take longer come on its next look.
const mesaCountWait = 4 * time.Second

// mesaCountBuild is how long one count may take, behind the page.
const mesaCountBuild = 2 * time.Minute

// mesaCountParallel is how many boards are made at once.
const mesaCountParallel = 4

type mesaCountEntry struct {
	n  int
	at time.Time
}

// mesaCountCache keeps each app's count and the counts being made.
type mesaCountCache struct {
	mu     sync.Mutex
	byApp  map[int64]mesaCountEntry
	making map[int64]chan struct{} // closed when the count is made (or failed)
	slots  chan struct{}
	// build makes an app's count; nil is the board's (tests replace it).
	build func(context.Context, *store.App) (int, error)
}

// MesaWaiting is, for the home page, how many lines of each app's board
// wait for a person, by app ID (an app whose count is not known yet is
// left out). A count younger than mesaCountTTL is answered as it is; an
// older one too, while a new one is made in the background; one never made
// is made now, all at once, and waited for at most mesaCountWait.
func (p *Platform) MesaWaiting(ctx context.Context, apps []*store.App) map[int64]int {
	c := &p.mesaCounts
	now := time.Now()
	out := map[int64]int{}
	var wait []chan struct{}
	c.mu.Lock()
	if c.byApp == nil {
		c.byApp, c.making = map[int64]mesaCountEntry{}, map[int64]chan struct{}{}
		c.slots = make(chan struct{}, mesaCountParallel)
	}
	for _, a := range apps {
		e, known := c.byApp[a.ID]
		if known {
			out[a.ID] = e.n
			if now.Sub(e.at) < mesaCountTTL {
				continue
			}
		}
		done, busy := c.making[a.ID]
		if !busy {
			done = make(chan struct{})
			c.making[a.ID] = done
			go p.makeMesaCount(context.WithoutCancel(ctx), a, done)
		}
		if !known {
			wait = append(wait, done)
		}
	}
	c.mu.Unlock()
	if len(wait) == 0 {
		return out
	}
	timer := time.NewTimer(mesaCountWait)
	defer timer.Stop()
waiting:
	for _, done := range wait {
		select {
		case <-done:
		case <-timer.C:
			break waiting
		case <-ctx.Done():
			break waiting
		}
	}
	c.mu.Lock()
	for _, a := range apps {
		if e, ok := c.byApp[a.ID]; ok {
			out[a.ID] = e.n
		}
	}
	c.mu.Unlock()
	return out
}

// makeMesaCount makes one app's count and keeps it; on a failure the old
// count (if any) stays, and the next look tries again.
func (p *Platform) makeMesaCount(ctx context.Context, a *store.App, done chan struct{}) {
	c := &p.mesaCounts
	defer func() {
		c.mu.Lock()
		delete(c.making, a.ID)
		c.mu.Unlock()
		close(done)
	}()
	c.slots <- struct{}{}
	defer func() { <-c.slots }()
	ctx, cancel := context.WithTimeout(ctx, mesaCountBuild)
	defer cancel()
	build := c.build
	if build == nil {
		build = func(ctx context.Context, a *store.App) (int, error) {
			b, err := p.Mesa(ctx, a)
			return b.You(), err
		}
	}
	n, err := build(ctx, a)
	if err != nil {
		p.Log.Warn("work board", "app", a.Name, "err", err) // the others still count
		return
	}
	c.mu.Lock()
	c.byApp[a.ID] = mesaCountEntry{n: n, at: time.Now()}
	c.mu.Unlock()
}
