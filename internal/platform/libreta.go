package platform

import (
	"context"
	"regexp"
	"strconv"
	"strings"
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
	kept, err := p.Store.RutaBranchesKept(ctx, ids)
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
		var k map[string]libreta.Kept
		for b, kb := range kept[e.ID] {
			if k == nil {
				k = map[string]libreta.Kept{}
			}
			k[b] = libreta.Kept{MergedAt: kb.MergedAt, Release: kb.Release}
		}
		in.Entries = append(in.Entries, libreta.Entry{ID: e.ID, Kind: e.Kind, Title: e.Title, Author: e.Author, ByAgent: e.ByAgent,
			Done: e.Done, Discarded: e.Discarded, Cargo: cargo, Branches: ramas[e.ID], Kept: k, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt})
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
	l := libreta.Build(in)
	p.keepLibreta(ctx, l, kept, rels)
	return l, nil
}

// keepLibreta writes down what the libreta worked out of a Ruta entry's
// branch and was not kept yet (merged by hand, say, while rendimiento was
// not looking): its merge, and the release that carried it. So it is still
// known once its runs are past the window the libreta reads.
func (p *Platform) keepLibreta(ctx context.Context, l libreta.Libreta, kept map[int64]map[string]store.KeptBranch, rels []*store.Release) {
	for _, it := range l.Items {
		if it.Source != libreta.FromRuta || it.Branch == "" || it.DoneAt == nil || (it.Status != libreta.Integrado && it.Status != libreta.Publicado) {
			continue
		}
		k := kept[it.ID][it.Branch]
		var release int64
		var releasedAt *time.Time
		if it.Status == libreta.Publicado && k.Release == 0 {
			release = it.Release
			for _, r := range rels {
				if r.Number == release {
					at := r.CreatedAt
					if r.VerifiedAt != nil {
						at = *r.VerifiedAt
					}
					releasedAt = &at
				}
			}
		}
		if k.MergedAt != nil && release == 0 {
			continue
		}
		if err := p.Store.KeepRutaBranch(ctx, it.ID, it.Branch, it.DoneAt, release, releasedAt); err != nil {
			p.Log.Warn("libreta: keep a branch", "entry", it.ID, "branch", it.Branch, "err", err)
		}
	}
}

// noteMerged writes down, on the Ruta entries worked on a branch, that it
// was merged: its head commit then, and when.
func (p *Platform) noteMerged(ctx context.Context, app *store.App, branch, sha string, at time.Time) {
	if branch == "" || branch == app.DefaultBranch {
		return
	}
	n, err := p.Store.MarkRutaMerged(ctx, app.OrgID, app.Name, branch, sha, at)
	if err != nil {
		p.Log.Warn("libreta: note a merge", "app", app.Name, "branch", branch, "err", err)
	} else if n > 0 {
		p.Log.Info("libreta: merge noted", "app", app.Name, "branch", branch, "entries", n)
	}
}

// PullMerged is GitHub saying a pull request was merged (by a person, or
// by rendimiento): every app of the repository notes it on the entries
// worked on its branch.
func (p *Platform) PullMerged(ctx context.Context, repo, branch, sha string, at time.Time) {
	apps, err := p.Store.AppsForRepo(ctx, repo)
	if err != nil {
		p.Log.Warn("libreta: apps of a merged pull request", "repo", repo, "err", err)
		return
	}
	for _, a := range apps {
		p.noteMerged(ctx, a, branch, sha, at)
	}
}

// noteReleased writes down, on the app's merged branches no release had
// carried yet, the release that passed its verification (or needed none):
// its run came after their merge, so it holds them. A rollback carries
// nothing new.
func (p *Platform) noteReleased(ctx context.Context, app *store.App, rel *store.Release) {
	if rel.RollbackOf != nil || rel.RunID == nil {
		return
	}
	run, err := p.Store.GetRun(ctx, *rel.RunID)
	if err != nil {
		p.Log.Warn("libreta: the run of a release", "app", app.Name, "release", rel.Number, "err", err)
		return
	}
	n, err := p.Store.MarkRutaReleased(ctx, app.OrgID, app.Name, rel.Number, run.CreatedAt, time.Now())
	if err != nil {
		p.Log.Warn("libreta: note a release", "app", app.Name, "release", rel.Number, "err", err)
	} else if n > 0 {
		p.Log.Info("libreta: release noted", "app", app.Name, "release", rel.Number, "branches", n)
	}
}

// linkRutaTrailers links a pushed branch to the Ruta entries its commits
// name in a «Ruta: N» trailer (see RutaRefs).
func (p *Platform) linkRutaTrailers(ctx context.Context, app *store.App, e PushEvent) {
	if e.Branch == app.DefaultBranch {
		return
	}
	var ids []int64
	for _, m := range e.Messages {
		ids = append(ids, RutaRefs(m)...)
	}
	if len(ids) == 0 {
		return
	}
	n, err := p.Store.LinkRutaRefs(ctx, app.OrgID, app.Name, e.Branch, ids, "github")
	if err != nil {
		p.Log.Warn("libreta: link a commit's Ruta entries", "app", app.Name, "branch", e.Branch, "err", err)
	} else if n > 0 {
		p.Log.Info("libreta: branch linked from its commits", "app", app.Name, "branch", e.Branch, "entries", n)
	}
}

// rutaTrailer is a commit message line naming Ruta entries: «Ruta: 150»,
// «Ruta: #150», «Ruta: 150, 151», in any case.
var rutaTrailer = regexp.MustCompile(`(?im)^[ \t]*ruta:[ \t]*(#?[0-9]+(?:[ \t]*,[ \t]*#?[0-9]+)*)[ \t\r]*$`)

// RutaRefs are the Ruta entries a commit message names in its «Ruta: N»
// lines, each once, in order.
func RutaRefs(message string) []int64 {
	var out []int64
	seen := map[int64]bool{}
	for _, m := range rutaTrailer.FindAllStringSubmatch(message, -1) {
		for _, f := range strings.Split(m[1], ",") {
			n, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(f), "#"), 10, 64)
			if err != nil || n <= 0 || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}
