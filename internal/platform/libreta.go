package platform

import (
	"context"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/libreta"
	"github.com/rendimiento-ai/rendimiento/internal/mesa"
	"github.com/rendimiento-ai/rendimiento/internal/pruebas"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// libretaRuns is how far back the libreta looks at runs: a branch merged
// before it is still known as merged from its pull request, but which
// release it went out in is not looked for.
const libretaRuns = 60 * 24 * time.Hour

// libretaReleases is how many of the latest releases it reads.
const libretaReleases = 100

// Libreta is the app's notebook (Ruta 150): what was asked for it in the
// Ruta and in encargos, and Renovate's pull requests, each with its state
// computed from its branches' runs, pull requests and releases. It asks
// GitHub what the work board asks (kept as long), and works without it.
func (p *Platform) Libreta(ctx context.Context, a *store.App) (libreta.Libreta, error) {
	now := time.Now()
	entries, err := p.Store.RutaOfApp(ctx, a.OrgID, a.Name)
	if err != nil {
		return libreta.Libreta{}, err
	}
	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	ramas, err := p.Store.RutaBranches(ctx, ids)
	if err != nil {
		return libreta.Libreta{}, err
	}
	encargos, err := p.Store.EncargosOfApp(ctx, a.OrgID, a.Name)
	if err != nil {
		return libreta.Libreta{}, err
	}
	history, err := p.Store.RunHistory(ctx, a.ID, now.Add(-libretaRuns))
	if err != nil {
		return libreta.Libreta{}, err
	}
	rels, err := p.Store.ListReleases(ctx, a.ID, libretaReleases)
	if err != nil {
		return libreta.Libreta{}, err
	}

	in := libreta.Input{DefaultBranch: a.DefaultBranch, InMain: map[string]bool{}}
	for _, e := range entries {
		var cargo int64
		if e.CargoID != nil {
			cargo = *e.CargoID
		}
		in.Entries = append(in.Entries, libreta.Entry{ID: e.ID, Kind: e.Kind, Title: e.Title, Author: e.Author, ByAgent: e.ByAgent,
			Done: e.Done, Discarded: e.Discarded, Cargo: cargo, Branches: ramas[e.ID], CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt})
	}
	for _, e := range encargos {
		branches := []string{store.RamaDeEncargo(e.Encargo)}
		if e.RunBranch != "" {
			branches = append(branches, e.RunBranch)
		}
		var pulls []int
		for _, link := range e.Links {
			if b := mesa.LinkBranch(link); b != "" {
				branches = append(branches, b)
			}
			if n := mesa.LinkPull(link); n != 0 {
				pulls = append(pulls, n)
			}
		}
		in.Encargos = append(in.Encargos, libreta.Encargo{ID: e.ID, Title: e.Title, Status: e.Status, CreatedBy: e.CreatedBy, ByAgent: e.ByAgent,
			Agent: e.KeyName, Branches: branches, Pulls: pulls, CancelNote: e.CancelNote, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt})
	}
	for _, h := range history {
		in.Runs = append(in.Runs, libreta.Run{ID: h.ID, Branch: h.Branch, SHA: h.SHA, Status: h.Status, Merge: h.Merge,
			Failed: h.Failed, Release: h.Release, CreatedAt: h.CreatedAt})
	}
	for _, r := range rels {
		var run int64
		if r.RunID != nil {
			run = *r.RunID
		}
		in.Releases = append(in.Releases, libreta.Release{Number: r.Number, RunID: run, Verify: r.VerifyStatus, Rollback: r.RollbackOf != nil})
	}

	got, ok := p.recentPulls(ctx, a)
	in.GitHub = ok
	for _, pr := range got.prs {
		if pr.Head.Repo.FullName != "" && pr.Head.Repo.FullName != a.Repo {
			continue // a fork's branch: its runs are not here
		}
		labels := make([]string, len(pr.Labels))
		for i, l := range pr.Labels {
			labels[i] = l.Name
		}
		in.Pulls = append(in.Pulls, libreta.Pull{Number: pr.Number, Title: pr.Title, Branch: pr.Head.Ref, Open: pr.State == "open",
			Deps: kindOf(pr.Head.Ref, a.DefaultBranch, labels) == KindDeps, Author: pr.User.Login,
			MergedAt: pr.MergedAt, ClosedAt: pr.ClosedAt, CreatedAt: pr.CreatedAt})
	}
	if heads := p.heads(ctx, a, history); heads.Known {
		for b, sha := range heads.SHA {
			if heads.InMain[sha] && b != a.DefaultBranch {
				in.InMain[b] = true
			}
		}
	}

	if a.Spec.Tests != nil {
		tests, err := p.Store.AppTests(ctx, a.ID)
		if err != nil {
			return libreta.Libreta{}, err
		}
		t := &libreta.Tests{BlockAfter: a.Spec.Tests.BlockAfter(), Advisory: map[string]int{}}
		for name, st := range tests {
			if st.State == pruebas.Advisory {
				t.Advisory[name] = st.Streak
			}
		}
		in.Tests = t
	}
	return libreta.Build(in), nil
}
