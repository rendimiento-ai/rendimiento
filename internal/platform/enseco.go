package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/pruebas"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
)

// ---- en seco: a branch's new checks, against the published release or a copy ----
//
// A branch's checks (post-deploy tasks with dryRun, the app's tests with
// dryRun) run once its steps passed, where they cannot hurt (see
// spec.DryRunPlan):
//   - against the release already published, in the app's namespace, with
//     the same Jobs as after a release, when the branch changes only its
//     checks (step 1);
//   - against a copy of the branch when it changes the app too (step 2),
//     or when a check asks for it (dryRunOn): its pull request's copy,
//     once that copy runs this commit, or, with no pull request yet, a
//     short copy made only for the checks (spec.ShortCopyOf: a quarter of
//     the resources, a database of its own, never production's data),
//     deleted when they end.
//
// It only warns:
//   - the run still passed; nothing is rolled back and the release is not
//     touched;
//   - when a check fails (or the copy never comes to run the commit),
//     rendimiento does not merge the branch by itself, and its GitHub
//     check concludes action_required (as for untested paths,
//     cobertura.go), so Renovate's automerge waits too;
//   - a person can still merge it ("Merge anyway"), which is noted in the
//     Ruta.

// shortCopyLife is how long a short copy may outlive its run before the
// sweep deletes it: its run deletes it when the checks end, unless the
// platform restarted meanwhile.
const shortCopyLife = 2 * time.Hour

// dryPlan is what a branch runs en seco, and the release published (nil
// when none is): against it when there is one, else against the app's
// default branch.
func (p *Platform) dryPlan(ctx context.Context, app *store.App, sp spec.Spec, files []string) (spec.DryPlan, *store.Release, error) {
	published, err := p.Store.PublishedRelease(ctx, app.ID)
	switch {
	case errors.Is(err, store.ErrNotFound) || (err == nil && !published.Spec.Runs()):
		return sp.DryRunPlan(app.Spec, files), nil, nil
	case err != nil:
		return spec.DryPlan{}, nil, err
	}
	return sp.DryRunPlan(published.Spec, files), published, nil
}

// testsOnCopy says whether the branch's tests run en seco against its pull
// request's copy: the copy's own tests (testPreview) then wait for them.
func (p *Platform) testsOnCopy(ctx context.Context, app *store.App, sp spec.Spec, files []string) bool {
	if len(sp.DryChecks()) == 0 {
		return false
	}
	plan, _, err := p.dryPlan(ctx, app, sp, files)
	if err != nil {
		return false
	}
	for _, t := range plan.Copy {
		if t.Name == spec.TestsTaskName {
			return true
		}
	}
	return false
}

// dryRun runs the branch's checks en seco when it touches them or the app,
// and records it on the run (run.Dry). images are the run's images: a
// check in a build (build:<name>) uses the branch's own image of it.
func (p *Platform) dryRun(ctx context.Context, app *store.App, run *store.Run, sp spec.Spec, files []string, images map[string]string) {
	if run.Deploy || len(sp.DryChecks()) == 0 {
		return
	}
	log := p.Log.With("app", app.Name, "run", run.ID)
	plan, published, err := p.dryPlan(ctx, app, sp, files)
	if err != nil {
		log.Warn("en seco: the published release", "err", err)
		return
	}
	if !plan.Touched() {
		return // only the book, say: nothing runs
	}
	var mu sync.Mutex
	d := store.DryRun{}
	pubFailed := false // a check failed against the published release
	record := func(change func(*store.DryRun)) {
		mu.Lock()
		change(&d)
		d.Failed = pubFailed || (d.Copy != nil && d.Copy.Failed)
		now := d
		if d.Copy != nil {
			c := *d.Copy
			now.Copy = &c
		}
		mu.Unlock()
		run.Dry = &now
		wctx := context.WithoutCancel(ctx)
		if err := p.Store.SetRunDry(wctx, run.ID, now); err != nil {
			log.Warn("en seco: record", "err", err)
		}
		if now.Copy != nil {
			if err := p.Store.SetRunDryCopy(wctx, run.ID, *now.Copy, now.Failed); err != nil {
				log.Warn("en seco: record the copy", "err", err)
			}
		}
		p.publishRun(app, run.ID)
	}

	// Against the published release: a branch that changes only its checks.
	switch {
	case plan.Runs() && published == nil:
		record(func(d *store.DryRun) {
			d.Note = M("no release is published yet: the checks run en seco once there is one")
		})
	case plan.Runs():
		results, failed := p.dryOnPublished(ctx, app, run, plan, published, images, record)
		mu.Lock()
		pubFailed = failed
		mu.Unlock()
		record(func(d *store.DryRun) { d.Tasks = results })
	case plan.AppChanged && len(plan.Changed) > 0 && !plan.RunsOnCopy():
		record(func(d *store.DryRun) {
			d.Note = M("this branch changes the app too: its checks do not run en seco against the published release")
		})
	}

	// Against a copy of the branch: it changes the app, or a check asks.
	if plan.RunsOnCopy() {
		p.dryOnCopy(ctx, app, run, sp, plan, images, record)
	}
}

// dryOnPublished runs the plan's checks against the published release:
// their results, and whether one did not pass.
func (p *Platform) dryOnPublished(ctx context.Context, app *store.App, run *store.Run, plan spec.DryPlan, published *store.Release, images map[string]string, record func(func(*store.DryRun))) ([]store.ReleaseTask, bool) {
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
	record(func(d *store.DryRun) { d.Release = published.Number })
	p.Log.Info("en seco", "app", app.Name, "run", run.ID, "release", published.Number, "checks", len(checks), "changed", plan.Changed)
	save := func(t store.ReleaseTask) {
		if err := p.Store.SaveRunDryTask(context.WithoutCancel(ctx), run.ID, t); err != nil {
			p.Log.Warn("en seco: record a check", "app", app.Name, "run", run.ID, "task", t.Name, "err", err)
		}
		p.publishRun(app, run.ID)
	}
	return judged(p.runTasks(ctx, app, &rel, checks, spec.StageDryRun, save), len(checks), save)
}

// judged reads the tests' report in a dry run's results, and says whether
// one of the checks (want of them) did not pass, or did not run.
func judged(results []store.ReleaseTask, want int, save func(store.ReleaseTask)) ([]store.ReleaseTask, bool) {
	failed := len(results) < want
	for i, r := range results {
		if r.Name == spec.TestsTaskName && r.Status == store.TaskSucceeded {
			results[i] = judgeDryTests(r)
			save(results[i])
		}
		failed = failed || results[i].Status != store.TaskSucceeded
	}
	return results, failed
}

// dryOnCopy runs the plan's copy checks against a copy of the branch: its
// pull request's, once it runs this commit, or a short one made for them.
func (p *Platform) dryOnCopy(ctx context.Context, app *store.App, run *store.Run, sp spec.Spec, plan spec.DryPlan, images map[string]string, record func(func(*store.DryRun))) {
	log := p.Log.With("app", app.Name, "run", run.ID)
	note := func(kind string, failed bool, msg string) {
		record(func(d *store.DryRun) {
			if d.Copy == nil {
				d.Copy = &store.DryCopy{}
			}
			if kind != "" {
				d.Copy.Kind = kind
			}
			d.Copy.Note, d.Copy.Failed = msg, d.Copy.Failed || failed
		})
	}
	copias := p.copias()
	switch {
	case sp.Previews == nil:
		note(store.DryCopyOff, false, M("this branch changes the app, so its checks would run en seco against a copy of it, and the app has no previews: nothing ran"))
		return
	case copias == nil:
		note("", false, M("copies of the app cannot be made here: nothing ran en seco against one"))
		return
	}

	var c copia
	var copySpec spec.Spec
	if number := p.openPullOf(ctx, app, run.Branch); number != 0 {
		// The pull request's copy: deployPreview wrote this run's images
		// to it just before.
		c, copySpec = copia{App: app.Name, Number: number}, spec.PreviewOf(sp, number)
		record(func(d *store.DryRun) { d.Copy = &store.DryCopy{Kind: store.DryCopyPull, Pull: number} })
		if _, ok := copias.Get(ctx, app.Name, number); !ok {
			note("", false, M("the copy of pull request #%d was not made (the app needs a domain for its address): nothing ran en seco against it", number))
			return
		}
		record(func(d *store.DryRun) { d.Copy.URL = copias.URL(copySpec, c) })
	} else {
		// No pull request yet: a short copy, only if there is room for it
		// (a pull request's copy is never deleted for it).
		copies, err := copias.List(ctx, app.Name)
		if err != nil {
			note(store.DryCopyShort, false, M("the app's copies could not be read (%v): nothing ran en seco", err))
			return
		}
		if room := sp.Previews.Copies(); len(copies) >= room {
			note(store.DryCopyShort, false, M("no room for a short copy of this branch: the app already has %d of its %d copies (previews.max), and a pull request's copy is not deleted for one. Nothing ran en seco; opening a pull request gives the branch its copy", len(copies), room))
			return
		}
		org, err := p.orgSlug(ctx, app.ID)
		if err != nil {
			note(store.DryCopyShort, false, M("the app's organization could not be read: nothing ran en seco"))
			return
		}
		c, copySpec = copia{App: app.Name, Run: run.ID}, spec.ShortCopyOf(sp, run.ID)
		if err := copias.PutShort(ctx, app, org, copySpec, images, run); err != nil {
			note(store.DryCopyShort, false, M("the short copy could not be made (%v): nothing ran en seco", err))
			return
		}
		log.Info("en seco: short copy made", "copy", c.name())
		if err := p.Store.CopiaMade(ctx, app.ID, 0, run.ID, run.ID, run.Branch); err != nil {
			log.Warn("en seco: recording the short copy", "err", err)
		}
		record(func(d *store.DryRun) {
			d.Copy = &store.DryCopy{Kind: store.DryCopyShort, URL: copias.URL(copySpec, c)}
		})
		defer func() {
			// Its checks ended: the short copy goes, whatever they said.
			if err := copias.Delete(context.WithoutCancel(ctx), c); err != nil {
				log.Warn("en seco: deleting the short copy", "copy", c.name(), "err", err)
				return
			}
			log.Info("en seco: short copy deleted", "copy", c.name())
			if err := p.Store.CopiaGone(context.WithoutCancel(ctx), app.ID, 0, run.ID, run.ID, store.CopiaSeco, "", ""); err != nil {
				log.Warn("en seco: recording the short copy gone", "err", err)
			}
			record(func(d *store.DryRun) { d.Copy.Gone = true })
		}()
	}

	// The copy must run this commit first.
	if ok, why := p.waitRollout(ctx, c.name(), run.ID); !ok {
		switch {
		case why == errSuperseded.Error():
			note("", false, M("a newer push replaced the copy before it ran this commit: that push's run checks it"))
		case why == "":
			note("", false, M("the run was cancelled before the copy ran this commit"))
		default:
			// The branch's app did not come up: a person should look.
			note("", true, M("the copy did not come to run this commit: %s", why))
		}
		return
	}

	target := &store.App{ID: app.ID, Name: c.name(), Repo: app.Repo, OrgID: app.OrgID, InstallationID: app.InstallationID}
	rel := &store.Release{Number: run.ID, SHA: run.SHA, Images: images, Spec: copySpec}
	checks := copySpec.DryChecksAmong(plan.Copy)
	for i := range checks {
		checks[i].Stage = spec.StageDryRun
		if checks[i].Name == spec.TestsTaskName && copySpec.Tests != nil && copySpec.Tests.URL == "" {
			if u := copias.TestURL(copySpec); u != "" {
				env := map[string]string{}
				for k, v := range checks[i].Env {
					env[k] = v
				}
				env["BASE_URL"] = u
				checks[i].Env = env
			}
		}
	}
	log.Info("en seco against a copy", "copy", c.name(), "checks", len(checks))
	save := func(t store.ReleaseTask) {
		if err := p.Store.SaveRunDryCopyTask(context.WithoutCancel(ctx), run.ID, t); err != nil {
			log.Warn("en seco: record a check against the copy", "task", t.Name, "err", err)
		}
		p.publishRun(app, run.ID)
	}
	raw := p.runTasks(ctx, target, rel, checks, spec.StageDryRun, save)
	var testsLog string
	for _, r := range raw {
		if r.Name == spec.TestsTaskName {
			testsLog = r.Log
		}
	}
	results, failed := judged(raw, len(checks), save)
	record(func(d *store.DryRun) { d.Copy.Tasks, d.Copy.Failed = results, d.Copy.Failed || failed })
	// The pull request's comment says how its tests went, as its copy's
	// own run of them (testPreview) does, which waited for these.
	if c.Number != 0 && testsLog != "" {
		if got, reported := pruebas.Parse(testsLog); reported {
			if live, ok := copias.Get(ctx, app.Name, c.Number); ok {
				p.sayOnPull(ctx, app, run, sp, copias.URL(copySpec, c), c.Number, live.CommentID, previewTests(got))
			}
			p.recordCopyTests(ctx, app.ID, c.Number, got)
		}
	}
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

// dryFailedNames are the checks that did not pass en seco, against either.
func dryFailedNames(run *store.Run) string {
	var names []string
	seen := map[string]bool{}
	add := func(tasks []store.ReleaseTask) {
		for _, t := range tasks {
			if t.Status != store.TaskSucceeded && !seen[t.Name] {
				seen[t.Name] = true
				names = append(names, "`"+t.Name+"`")
			}
		}
	}
	if run.Dry != nil {
		add(run.Dry.Tasks)
		if run.Dry.Copy != nil {
			add(run.Dry.Copy.Tasks)
		}
	}
	if len(names) == 0 {
		if c := dryCopyOf(run); c != nil && c.Failed {
			return "the copy of the branch"
		}
		return "a check"
	}
	return strings.Join(names, ", ")
}

// dryCopyOf is what the run checked against a copy of the branch (nil:
// nothing).
func dryCopyOf(run *store.Run) *store.DryCopy {
	if run.Dry == nil {
		return nil
	}
	return run.Dry.Copy
}

// dryWhere says, in the forge's words, where what failed en seco ran:
// against the published release, the pull request's copy, or a short
// copy of the branch.
func dryWhere(run *store.Run) string {
	var where []string
	failedOn := false
	for _, t := range run.Dry.Tasks {
		failedOn = failedOn || t.Status != store.TaskSucceeded
	}
	if run.Dry.Release != 0 && failedOn {
		where = append(where, fmt.Sprintf("against release #%d", run.Dry.Release))
	}
	if c := run.Dry.Copy; c != nil && c.Failed {
		switch c.Kind {
		case store.DryCopyPull:
			where = append(where, fmt.Sprintf("against the copy of pull request #%d", c.Pull))
		default:
			where = append(where, "against a short copy of this branch")
		}
	}
	if len(where) == 0 && run.Dry.Release != 0 {
		where = append(where, fmt.Sprintf("against release #%d", run.Dry.Release))
	}
	return strings.Join(where, " and ")
}

// dryFailedTitle is the GitHub check's title for a run that passed with a
// check that failed en seco.
func dryFailedTitle(run *store.Run) string {
	return fmt.Sprintf("Build succeeded, but %s failed en seco %s: a person must merge this", dryFailedNames(run), dryWhere(run))
}

// approvedDryTitle is the check's title once a person approved it anyway.
func approvedDryTitle(who string, run *store.Run) string {
	return fmt.Sprintf("Build succeeded; %s approved merging it, though %s failed en seco", who, dryFailedNames(run))
}

// dryFailedSummary is the check's text: what failed, and how to merge it.
func dryFailedSummary(p *Platform, app *store.App, run *store.Run) string {
	why := fmt.Sprintf("The run passed, and this branch changes only %s's checks, so the new checks ran en seco against release #%d, the one published.", app.Name, run.Dry.Release)
	if c := dryCopyOf(run); c != nil && c.Failed {
		why = fmt.Sprintf("The run passed, and the checks ran en seco %s, where the branch's change is live.", dryWhere(run))
		if c.Note != "" {
			why += " " + c.Note + "."
		}
	}
	return fmt.Sprintf("%s %s failed there. "+
		"Nothing was rolled back and the release was not touched, but rendimiento does not merge it by itself, and this check stays short of success so tools that wait for green checks (Renovate's automerge) wait too.\n\n"+
		"To merge it anyway, use **Merge anyway** on the [run's page](%s/apps/%s/runs/%d) (it is noted in the Ruta, and the check turns green), or merge it in GitHub.",
		why, dryFailedNames(run), p.Config.BaseURL, app.Name, run.ID)
}

// noteMergedDespiteDry writes in the Ruta that a person merged a branch
// whose checks failed en seco: when that release rolls back, whoever looks
// finds why. Only the first yes is noted.
func (p *Platform) noteMergedDespiteDry(ctx context.Context, app *store.App, run *store.Run, who string) {
	if !run.DryFailed() || run.ApprovedBy != "" {
		return
	}
	var where []string
	failedOn := false
	for _, t := range run.Dry.Tasks {
		failedOn = failedOn || t.Status != store.TaskSucceeded
	}
	if run.Dry.Release != 0 && failedOn {
		where = append(where, fmt.Sprintf("contra la versión publicada #%d", run.Dry.Release))
	}
	if c := run.Dry.Copy; c != nil && c.Failed {
		if c.Kind == store.DryCopyPull {
			where = append(where, fmt.Sprintf("contra la copia de la solicitud #%d", c.Pull))
		} else {
			where = append(where, "contra una copia corta de la rama")
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s integró de todos modos la rama %s de %s (ejecución #%d), el %s, aunque sus pruebas en seco no pasaron %s.\n\n",
		who, run.Branch, app.Name, run.ID, time.Now().Format("2006-01-02"), strings.Join(where, " ni "))
	list := func(tasks []store.ReleaseTask) {
		for _, t := range tasks {
			if t.Status != store.TaskSucceeded {
				fmt.Fprintf(&b, "- **%s** (%s): %s\n", t.Name, t.Status, t.Message)
			}
		}
	}
	list(run.Dry.Tasks)
	if c := run.Dry.Copy; c != nil {
		list(c.Tasks)
		if c.Failed && c.Note != "" {
			fmt.Fprintf(&b, "- %s\n", c.Note)
		}
	}
	fmt.Fprintf(&b, "\nLa ejecución: %s/apps/%s/runs/%d", p.Config.BaseURL, app.Name, run.ID)
	if _, err := p.Store.AddRuta(ctx, store.RutaEntry{Kind: store.KindNota, Title: fmt.Sprintf("%s: se integró %s aunque falló en seco", app.Name, run.Branch),
		Body: b.String(), Tags: []string{"en-seco", strings.ToLower(app.Name)}, Author: who, OrgID: app.OrgID, App: app.Name}); err != nil {
		p.Log.Warn("en seco: note the merge in the Ruta", "app", app.Name, "run", run.ID, "err", err)
	}
}
