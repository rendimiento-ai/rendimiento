package platform

import (
	"context"
	"time"

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
