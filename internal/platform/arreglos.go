package platform

import (
	"context"
	"errors"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/arreglos"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// RenovateBot is who asks for the fixes rendimiento runs by itself on
// Renovate's branches.
const RenovateBot = "renovate"

// isRenovateBranch reports whether a branch is Renovate's: named
// renovate/…, and, when it has an open pull request, one a bot opened from
// this repository (a person's branch of that name, or a fork's, is not).
func (p *Platform) isRenovateBranch(ctx context.Context, app *store.App, branch string) bool {
	if !strings.HasPrefix(branch, "renovate/") {
		return false
	}
	pulls, err := p.repos(app).OpenPulls(ctx, app.InstallationID, app.Repo)
	if err != nil {
		return false
	}
	for _, pr := range pulls {
		if pr.Head.Ref != branch {
			continue
		}
		if pr.Head.Repo.FullName != "" && !strings.EqualFold(pr.Head.Repo.FullName, app.Repo) {
			return false
		}
		return pr.User.Type == "Bot"
	}
	return true // Renovate's branch before its pull request (prCreation: not-pending)
}

// fixRenovate answers a failed run of a Renovate branch: when a step failed
// on a stale lockfile and a recipe of the app's (from the default branch)
// covers its folder, rendimiento runs it by itself and commits what it
// wrote to the branch, whose new run decides. Each commit gets it once,
// and never a commit a fix made, so it cannot loop. Only recipes: a
// command of the app's own runs only when someone asks for it.
func (p *Platform) fixRenovate(ctx context.Context, app *store.App, run *store.Run, ran []pipeline.Step, results map[string]pipeline.StepResult) {
	if p.Fixes == nil || run.Deploy || run.Branch == app.DefaultBranch || !p.isRenovateBranch(ctx, app, run.Branch) {
		return
	}
	if made, err := p.Store.MadeByFix(ctx, app.ID, run.SHA); err != nil || made {
		return
	}
	var fixes []string
	seen := map[string]bool{}
	declared, err := p.Fixes.Declared(ctx, app)
	if err != nil || len(declared) == 0 {
		return
	}
	var found []arreglos.Found
	for _, st := range ran {
		if results[st.ID].Status != pipeline.StatusFailed {
			continue
		}
		log, _, err := p.Store.StepLog(ctx, run.ID, st.ID)
		if err != nil {
			continue
		}
		if stale, ok := arreglos.Signature(log); ok {
			found = append(found, arreglos.Found{Step: st.ID, Folder: st.Path, Stale: stale})
		}
	}
	for _, m := range arreglos.Covered(found, declared, true) {
		if !seen[m.Fix.Name] {
			seen[m.Fix.Name] = true
			fixes = append(fixes, m.Fix.Name)
		}
	}
	for _, name := range fixes {
		res, err := p.Fixes.Run(ctx, arreglos.Request{App: app, Branch: run.Branch, SHA: run.SHA, Fix: name,
			AskedKind: store.FixByRenovate, AskedBy: RenovateBot, Run: run.ID, Commit: true})
		switch {
		case errors.Is(err, store.ErrFixDone):
		case err != nil:
			p.Log.Warn("a Renovate branch's fix", "app", app.Name, "branch", run.Branch, "fix", name, "err", err)
		default:
			p.Log.Info("fixed a Renovate branch", "app", app.Name, "branch", run.Branch, "fix", name, "status", res.Status, "commit", res.Commit)
		}
	}
}
