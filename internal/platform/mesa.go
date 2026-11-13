package platform

import (
	"context"
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
	return mesa.Build(a.DefaultBranch, pulls, ok, runs, encargos, now), nil
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
