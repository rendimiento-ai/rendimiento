package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/pruebas"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
)

// ---- en seco: a branch's new checks, against the published release ----
//
// A branch that changes only its checks (post-deploy tasks with dryRun,
// the app's tests with dryRun, or the build that holds them) runs them
// once its steps passed: in the app's namespace, with the same Jobs as
// after a release, against the release already published (see
// spec.DryRunPlan). It only warns:
//   - the run still passed; nothing is rolled back and the release is not
//     touched;
//   - when a check fails, rendimiento does not merge the branch by itself,
//     and its GitHub check concludes action_required (as for untested
//     paths, cobertura.go), so Renovate's automerge waits too;
//   - a person can still merge it ("Merge anyway"), which is noted in the
//     Ruta.
//
// Step 2, not built yet: a branch that also changes the app (a dependency
// update) would run them against a preview copy of the branch instead.
// That starts where plan.AppChanged is seen below.

// dryRun runs the branch's checks en seco when its change is only to
// them, and records it on the run (run.Dry). images are the run's images:
// a check in a build (build:<name>) uses the branch's own image of it.
func (p *Platform) dryRun(ctx context.Context, app *store.App, run *store.Run, sp spec.Spec, files []string, images map[string]string) {
	if run.Deploy || len(sp.DryChecks()) == 0 {
		return
	}
	log := p.Log.With("app", app.Name, "run", run.ID)
	record := func(d store.DryRun) {
		run.Dry = &d
		if err := p.Store.SetRunDry(context.WithoutCancel(ctx), run.ID, d); err != nil {
			log.Warn("en seco: record", "err", err)
		}
		p.publishRun(app, run.ID)
	}
	published, err := p.Store.PublishedRelease(ctx, app.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !published.Spec.Runs()) {
		record(store.DryRun{Note: M("no release is published yet: the checks run en seco once there is one")})
		return
	}
	if err != nil {
		log.Warn("en seco: the published release", "err", err)
		return
	}
	plan := sp.DryRunPlan(published.Spec, files)
	if plan.AppChanged {
		// Step 2: against a preview copy of the branch, once previews can
		// carry the checks. Until then nothing runs against production for
		// a branch that changes the app.
		if len(plan.Changed) > 0 {
			record(store.DryRun{Note: M("this branch changes the app too: its checks do not run en seco against the published release")})
		}
		return
	}
	if !plan.Runs() {
		return
	}

	// The published release, with the branch's images of the checks'
	// builds: the Jobs reach what is live, nothing of it changes.
	rel := *published
	rel.Images = map[string]string{}
	for k, v := range published.Images {
		rel.Images[k] = v
	}
	checks := make([]spec.Task, len(plan.Checks))
	for i, t := range plan.Checks {
		if img := images[t.Image]; strings.HasPrefix(t.Image, "build:") && img != "" {
			rel.Images[t.Image] = img
		}
		t.Stage = spec.StageDryRun
		checks[i] = t
	}
	record(store.DryRun{Release: published.Number})
	log.Info("en seco", "release", published.Number, "checks", len(checks), "changed", plan.Changed)
	save := func(t store.ReleaseTask) {
		if err := p.Store.SaveRunDryTask(context.WithoutCancel(ctx), run.ID, t); err != nil {
			log.Warn("en seco: record a check", "task", t.Name, "err", err)
		}
		p.publishRun(app, run.ID)
	}
	results := p.runTasks(ctx, app, &rel, checks, spec.StageDryRun, save)
	failed := len(results) < len(checks)
	for i, r := range results {
		if r.Name == spec.TestsTaskName && r.Status == store.TaskSucceeded {
			results[i] = judgeDryTests(r)
			save(results[i])
		}
		failed = failed || results[i].Status != store.TaskSucceeded
	}
	record(store.DryRun{Release: published.Number, Failed: failed, Tasks: results})
}

// judgeDryTests reads the app's tests' report en seco: a test that failed
// fails the check (en seco nothing is advisory: the tests are new), and
// the message counts them.
func judgeDryTests(r store.ReleaseTask) store.ReleaseTask {
	got, reported := pruebas.Parse(r.Log)
	if !reported {
		r.Status, r.Message = store.TaskFailed, M("the tests left no report: see the log of the %s task", spec.TestsTaskName)
		return r
	}
	passed, total := 0, 0
	var failed []string
	for _, t := range got {
		switch t.Status {
		case pruebas.Skipped:
			continue
		case pruebas.Passed, pruebas.Flaky:
			passed++
		default:
			failed = append(failed, t.Name)
		}
		total++
	}
	if len(failed) > 0 {
		r.Status, r.Message = store.TaskFailed, M("%d of %d tests failed: %s", len(failed), total, strings.Join(failed, "; "))
		return r
	}
	r.Message = M("%d of %d tests passed", passed, total)
	return r
}

// dryFailedNames are the checks that did not pass en seco.
func dryFailedNames(run *store.Run) string {
	var names []string
	if run.Dry != nil {
		for _, t := range run.Dry.Tasks {
			if t.Status != store.TaskSucceeded {
				names = append(names, "`"+t.Name+"`")
			}
		}
	}
	if len(names) == 0 {
		return "a check"
	}
	return strings.Join(names, ", ")
}

// dryFailedTitle is the GitHub check's title for a run that passed with a
// check that failed en seco.
func dryFailedTitle(run *store.Run) string {
	return fmt.Sprintf("Build succeeded, but %s failed en seco against release #%d: a person must merge this", dryFailedNames(run), run.Dry.Release)
}

// approvedDryTitle is the check's title once a person approved it anyway.
func approvedDryTitle(who string, run *store.Run) string {
	return fmt.Sprintf("Build succeeded; %s approved merging it, though %s failed en seco", who, dryFailedNames(run))
}

// dryFailedSummary is the check's text: what failed, and how to merge it.
func dryFailedSummary(p *Platform, app *store.App, run *store.Run) string {
	return fmt.Sprintf("The run passed, and this branch changes only %s's checks, so the new checks ran en seco against release #%d, the one published. %s failed there. "+
		"Nothing was rolled back and the release was not touched, but rendimiento does not merge it by itself, and this check stays short of success so tools that wait for green checks (Renovate's automerge) wait too.\n\n"+
		"To merge it anyway, use **Merge anyway** on the [run's page](%s/apps/%s/runs/%d) (it is noted in the Ruta, and the check turns green), or merge it in GitHub.",
		app.Name, run.Dry.Release, dryFailedNames(run), p.Config.BaseURL, app.Name, run.ID)
}

// noteMergedDespiteDry writes in the Ruta that a person merged a branch
// whose checks failed en seco: when that release rolls back, whoever looks
// finds why. Only the first yes is noted.
func (p *Platform) noteMergedDespiteDry(ctx context.Context, app *store.App, run *store.Run, who string) {
	if !run.DryFailed() || run.ApprovedBy != "" {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s integró de todos modos la rama %s de %s (ejecución #%d), el %s, aunque sus pruebas en seco no pasaron contra la versión publicada #%d.\n\n",
		who, run.Branch, app.Name, run.ID, time.Now().Format("2006-01-02"), run.Dry.Release)
	for _, t := range run.Dry.Tasks {
		if t.Status != store.TaskSucceeded {
			fmt.Fprintf(&b, "- **%s** (%s): %s\n", t.Name, t.Status, t.Message)
		}
	}
	fmt.Fprintf(&b, "\nLa ejecución: %s/apps/%s/runs/%d", p.Config.BaseURL, app.Name, run.ID)
	if _, err := p.Store.AddRuta(ctx, store.RutaEntry{Kind: store.KindNota, Title: fmt.Sprintf("%s: se integró %s aunque falló en seco", app.Name, run.Branch),
		Body: b.String(), Tags: []string{"en-seco", strings.ToLower(app.Name)}, Author: who, OrgID: app.OrgID, App: app.Name}); err != nil {
		p.Log.Warn("en seco: note the merge in the Ruta", "app", app.Name, "run", run.ID, "err", err)
	}
}
