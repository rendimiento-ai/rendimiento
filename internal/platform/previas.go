package platform

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/pruebas"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
)

// ---- preview environments (decision 136, step 12, slice 2) ----
//
// A pull request of an app that asks for previews gets a live copy: an App
// object of its own, <app>-pr-<number>, with the copy's spec (spec.PreviewOf)
// and the images its branch's run built, so nothing is built twice. The
// controller deploys it in its own namespace like any app. A copy is never
// a release: it has no history, verification or rollback.

// Labels and annotations of a copy's App object.
const (
	labelPreviewOf    = "rendimiento.ai/preview-of" // the app it copies
	labelShortCopy    = "rendimiento.ai/seco-run"   // a short copy's run, for its checks en seco
	annotationPushed  = "rendimiento.ai/pushed-at"  // when its branch last ran
	annotationComment = "rendimiento.ai/comment"    // the pull request's comment kept for it
)

// previewSweep is how often copies of closed or idle pull requests are
// looked for.
const previewSweep = 10 * time.Minute

// PreviewName is the App object of pull request number's copy.
func PreviewName(app string, number int) string { return fmt.Sprintf("%s-pr-%d", app, number) }

// ShortCopyName is the App object of the short copy run makes for its
// checks en seco, when its branch has no pull request.
func ShortCopyName(app string, run int64) string { return fmt.Sprintf("%s-seco-%d", app, run) }

// previewURL is the copy's address: its first service with a domain.
func previewURL(copy spec.Spec) string {
	for _, svc := range copy.Services {
		if svc.Domain != "" {
			return "https://" + svc.Domain
		}
	}
	return ""
}

// openPullOf is the open pull request of a branch, from the app's own
// repository (0 when none, or a fork's).
func (p *Platform) openPullOf(ctx context.Context, a *store.App, branch string) int {
	got, ok := p.recentPulls(ctx, a)
	if !ok {
		return 0
	}
	for _, pr := range got.prs {
		if pr.Head.Ref == branch && pr.State == "open" && (pr.Head.Repo.FullName == "" || pr.Head.Repo.FullName == a.Repo) {
			return pr.Number
		}
	}
	return 0
}

// deployPreview gives a branch's open pull request its copy once the
// branch's run passed: the App object is written (made the first time) with
// the run's images, the oldest copy goes first when the app has its most,
// and a "preview" check on the commit links to the copy's address.
// testsEnSeco says the branch's tests run en seco against the copy
// (dryOnCopy, after this): the copy's own run of them is left to those.
func (p *Platform) deployPreview(ctx context.Context, app *store.App, run *store.Run, sp spec.Spec, images map[string]string, testsEnSeco bool) {
	if sp.Previews == nil || p.copias() == nil || run.Deploy {
		return
	}
	if number := p.openPullOf(ctx, app, run.Branch); number != 0 {
		p.deployPreviewOf(ctx, app, run, sp, images, number, testsEnSeco)
	}
}

// deployPreviewOf writes pull request number's copy from a run that passed.
func (p *Platform) deployPreviewOf(ctx context.Context, app *store.App, run *store.Run, sp spec.Spec, images map[string]string, number int, testsEnSeco bool) {
	copias := p.copias()
	if copias == nil {
		return
	}
	log := p.Log.With("app", app.Name, "pull", number)
	copy := spec.PreviewOf(sp, number)
	if err := p.makeRoom(ctx, app, number, sp.Previews.Copies()); err != nil {
		log.Warn("preview: making room", "err", err)
	}
	org, err := p.orgSlug(ctx, app.ID)
	if err != nil {
		log.Warn("preview: the app's organization", "err", err)
		return
	}
	// One comment on the pull request, edited on each push: the address,
	// the commit and when the copy goes.
	url := copias.URL(copy, copia{App: app.Name, Number: number})
	if url == "" {
		// In a cluster a copy is reached at its domain's pr-<number> host.
		log.Info("preview: not made, the app has no domain for the copy's address")
		return
	}
	before, _ := copias.Get(ctx, app.Name, number)
	commentID := before.CommentID
	until := p.copyDeadline(ctx, app.ID, number, time.Now(), sp.Previews.KeepDays())
	if p.Forja != nil {
		body := inForjaWords(app, previewComment(number, url, run, p.Config.BaseURL, app.Name, until))
		if id, err := p.repos(app).Comment(ctx, app.InstallationID, app.Repo, number, commentID, body); err == nil {
			commentID = id
		} else {
			log.Warn("preview: its comment", "err", err)
		}
	}
	if err := copias.Put(ctx, app, org, copy, images, run, number, commentID); err != nil {
		log.Warn("preview: writing it", "err", err)
		return
	}
	if err := p.Store.CopiaMade(ctx, app.ID, number, 0, run.ID, run.Branch); err != nil {
		log.Warn("preview: recording it", "err", err)
	}
	log.Info("preview deployed", "run", run.ID, "url", url)
	if len(run.SHA) == 40 && p.Forja != nil {
		_, err := p.repos(app).CreateCheck(ctx, app.InstallationID, app.Repo, forja.Check{
			Name: "preview: " + app.Name, HeadSHA: run.SHA, Status: "completed", Conclusion: "success", DetailsURL: url,
			Output: forja.CheckOutput(M("Live at %s", url),
				inForjaWords(app, M("This pull request's copy. It is deleted when the pull request is closed, or after %d days without a push.", sp.Previews.KeepDays()))),
		})
		if err != nil {
			log.Warn("preview: its check", "err", err)
		}
	}
	if sp.Tests != nil && !testsEnSeco {
		// The copy's tests run once it answers, without holding the run up.
		go p.testPreview(context.WithoutCancel(ctx), app, run, sp, copy, images, number, commentID)
	}
}

// testPreview runs the app's own tests (tests:) against pull request
// number's copy once it is healthy, and adds each test's result to the
// pull request's comment (slice 4). The tests only report here: a copy is
// never a release, so nothing is blocked or rolled back, and no test's
// state (advisory, blocking) moves.
func (p *Platform) testPreview(ctx context.Context, app *store.App, run *store.Run, sp, copy spec.Spec, images map[string]string, number int, commentID int64) {
	name := PreviewName(app.Name, number)
	log := p.Log.With("app", app.Name, "pull", number)
	url := p.copias().URL(copy, copia{App: app.Name, Number: number})
	say := func(section string) { p.sayOnPull(ctx, app, run, sp, url, number, commentID, section) }
	if ok, why := p.waitRollout(ctx, name, run.ID); !ok {
		say(M("**Tests:** not run, the copy did not become healthy: %s.", why))
		return
	}
	task := sp.Tests.Task(&copy)
	if u := p.copias().TestURL(copy); u != "" && sp.Tests.URL == "" {
		task.Env["BASE_URL"] = u
	}
	rel := &store.Release{Number: run.ID, SHA: run.SHA, Images: images, Spec: copy}
	img, ok := buildImage(rel, task.Image)
	if !ok {
		say(M("**Tests:** not run, this run has no image of %s.", sp.Tests.Build))
		return
	}
	task.Image = img
	runTask := p.Verify.RunTask
	if runTask == nil {
		runTask = p.Target().Tarea
	}
	res := runTask(ctx, &store.App{ID: app.ID, Name: name, Repo: app.Repo, OrgID: app.OrgID, InstallationID: app.InstallationID}, rel, task)
	got, reported := pruebas.Parse(res.Log)
	if !reported {
		say(M("**Tests:** they left no report (%s).", res.Message))
		return
	}
	say(previewTests(got))
	p.recordCopyTests(ctx, app.ID, number, got)
	log.Info("preview tested", "run", run.ID, "tests", len(got))
}

// sayOnPull adds a section (the tests' results) to the pull request's
// comment about its copy.
func (p *Platform) sayOnPull(ctx context.Context, app *store.App, run *store.Run, sp spec.Spec, url string, number int, commentID int64, section string) {
	if commentID == 0 || p.Forja == nil {
		return
	}
	until := p.copyDeadline(ctx, app.ID, number, time.Now(), sp.Previews.KeepDays())
	body := previewComment(number, url, run, p.Config.BaseURL, app.Name, until) + "\n\n" + section
	if _, err := p.repos(app).Comment(ctx, app.InstallationID, app.Repo, number, commentID, body); err != nil {
		p.Log.Warn("preview: its comment", "app", app.Name, "pull", number, "err", err)
	}
}

// copyTests counts the copy's own tests as its comment does
// (previewTests): how many passed of how many, and those that failed.
func copyTests(got []pruebas.Result) (passed, total int, failed []string) {
	for _, r := range got {
		switch r.Status {
		case pruebas.Passed:
			passed++
		case pruebas.Failed, pruebas.Flaky:
			failed = append(failed, r.Name)
		}
	}
	return passed, len(got), failed
}

// recordCopyTests keeps the copy's own tests' result for its card on the
// Caminos tab.
func (p *Platform) recordCopyTests(ctx context.Context, appID int64, number int, got []pruebas.Result) {
	passed, total, failed := copyTests(got)
	if err := p.Store.SetCopiaTests(ctx, appID, number, passed, total, failed); err != nil {
		p.Log.Warn("preview: recording its tests", "pull", number, "err", err)
	}
}

// copyDeadline is the day pull request number's copy goes when nothing is
// pushed after pushed: keep days later, or the day a person moved it to.
func (p *Platform) copyDeadline(ctx context.Context, appID int64, number int, pushed time.Time, keep int) time.Time {
	until := pushed.AddDate(0, 0, keep)
	if row, err := p.Store.LiveCopia(ctx, appID, number, 0); err == nil && row.KeepUntil != nil && row.KeepUntil.After(until) {
		until = *row.KeepUntil
	}
	return until
}

// previewTests is the comment's section of the copy's tests: how many
// passed, then each that did not.
func previewTests(got []pruebas.Result) string {
	passed := 0
	var lines []string
	for _, r := range got {
		switch r.Status {
		case pruebas.Passed:
			passed++
		case pruebas.Failed:
			lines = append(lines, M("- failed: %s", r.Name))
		case pruebas.Flaky:
			lines = append(lines, M("- failed, then passed on its retry: %s", r.Name))
		}
	}
	out := M("**Tests:** %d of %d passed against this copy.", passed, len(got))
	if len(lines) > 0 {
		out += "\n" + strings.Join(lines, "\n")
	}
	return out
}

// previewComment is the pull request's comment about its copy.
func previewComment(number int, url string, run *store.Run, baseURL, app string, until time.Time) string {
	sha := run.SHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return M("**Preview of #%d:** %s\n\nIt shows commit `%s` (run [#%d](%s/apps/%s/runs/%d)). It is deleted when this pull request is closed or merged, or on %s if nothing is pushed before.",
		number, url, sha, run.ID, baseURL, app, run.ID, until.Format(time.DateOnly))
}

// deletePreview deletes a copy and says why in its pull request's comment:
// kind is why for the Caminos tab (store.CopiaMerged...), why in words, and
// by the person who deleted it, when one did.
func (p *Platform) deletePreview(ctx context.Context, c copia, kind, why, by string) error {
	copias := p.copias()
	if copias == nil {
		return nil
	}
	if err := copias.Delete(ctx, c); err != nil {
		return err
	}
	app, err := p.Store.GetApp(ctx, c.App)
	if err != nil {
		return nil
	}
	if err := p.Store.CopiaGone(ctx, app.ID, c.Number, c.Run, c.Release, kind, why, by); err != nil {
		p.Log.Warn("preview: recording it gone", "copy", c.name(), "err", err)
	}
	if c.Number != 0 {
		// The runs whose checks ran against it en seco stop linking to it.
		if err := p.Store.DryCopyGone(ctx, app.ID, c.Number); err != nil {
			p.Log.Warn("preview: marking it gone on its runs", "copy", c.name(), "err", err)
		}
	}
	if c.CommentID == 0 || p.Forja == nil {
		return nil
	}
	body := inForjaWords(app, M("**Preview of #%d:** deleted, %s.", c.Number, why))
	if _, err := p.repos(app).Comment(ctx, app.InstallationID, app.Repo, c.Number, c.CommentID, body); err != nil {
		p.Log.Warn("preview: its comment", "copy", c.name(), "err", err)
	}
	return nil
}

// makeRoom deletes the copies of the app idle longest until a new one fits
// under max (keep is the pull request whose copy is being written, which
// never goes).
func (p *Platform) makeRoom(ctx context.Context, app *store.App, keep, max int) error {
	copies, err := p.copias().List(ctx, app.Name)
	if err != nil {
		return err
	}
	// A short copy (en seco) lives minutes and is not a pull request's:
	// it is never made room for, nor taken out to make room.
	others := copies[:0]
	for _, c := range copies {
		if c.Number != keep && c.Run == 0 {
			others = append(others, c)
		}
	}
	sort.Slice(others, func(i, j int) bool { return others[i].PushedAt.Before(others[j].PushedAt) })
	for len(others) >= max {
		if err := p.deletePreview(ctx, others[0], store.CopiaMax, M("to make room for a newer pull request's copy"), ""); err != nil {
			return err
		}
		p.Log.Info("preview deleted to make room", "app", app.Name, "copy", others[0].name())
		others = others[1:]
	}
	return nil
}

// OpenedPreview gives a pull request opened (or reopened) on a branch whose
// last run already passed its copy now, from that run's images: otherwise it
// would wait for the next push. A run still going makes the copy when it
// passes, as always. A fork's pull request never gets one.
func (p *Platform) OpenedPreview(ctx context.Context, repo string, number int, branch, headRepo string) {
	if p.copias() == nil || p.Store == nil || number == 0 || branch == "" || (headRepo != "" && !strings.EqualFold(headRepo, repo)) {
		return
	}
	apps, err := p.Store.AppsForRepo(ctx, repo)
	if err != nil {
		return
	}
	for _, a := range apps {
		// The pull request is new: what GitHub said before does not have it.
		p.branchPullsCache.mu.Lock()
		delete(p.branchPullsCache.byApp, a.ID)
		p.branchPullsCache.mu.Unlock()
		p.previewFromLastRun(ctx, a, number, branch)
	}
}

// previewFromLastRun gives pull request number its copy from its branch's
// last run, when that run passed (and is not a deploy of the main branch).
func (p *Platform) previewFromLastRun(ctx context.Context, a *store.App, number int, branch string) {
	last, err := p.Store.LastRunOf(ctx, a.ID, branch)
	if err != nil || last.Status != store.RunSucceeded || last.Deploy || branch == a.DefaultBranch {
		return
	}
	p.previewFromRun(ctx, a, number, last.ID)
}

// previewFromRun writes pull request number's copy from a run that passed,
// with the spec of its commit; false when that commit asks for no previews
// or the run cannot be read.
func (p *Platform) previewFromRun(ctx context.Context, a *store.App, number int, runID int64) bool {
	run, err := p.Store.GetRun(ctx, runID)
	if err != nil {
		return false
	}
	sp := a.Spec
	if raw, err := p.repos(a).FileAt(ctx, a.InstallationID, a.Repo, spec.FileName, run.SHA); err == nil {
		if s, err := spec.Parse(raw); err == nil {
			sp = *s
		}
	}
	if sp.Previews == nil {
		return false
	}
	p.deployPreviewOf(ctx, a, run, sp, p.imagesOfRun(ctx, a, run, sp), number, false)
	return true
}

// catchUpPreviews makes the copies a lost "opened" event left out: an open
// pull request of an app that asks for previews, with no copy, whose
// branch already passed. On a computer GitHub sends no events at all, so
// this is how a pull request opened there gets its copy.
func (p *Platform) catchUpPreviews(ctx context.Context) {
	apps, err := p.Store.ListApps(ctx)
	if err != nil {
		return
	}
	copias := p.copias()
	for _, a := range apps {
		if a.Spec.Previews == nil {
			continue
		}
		got, ok := p.recentPulls(ctx, a)
		if !ok {
			continue
		}
		for _, pr := range got.prs {
			if pr.State != "open" || (pr.Head.Repo.FullName != "" && pr.Head.Repo.FullName != a.Repo) {
				continue
			}
			if _, has := copias.Get(ctx, a.Name, pr.Number); !has && !p.deletedByPerson(ctx, a, pr.Number, pr.Head.Ref) {
				p.previewFromLastRun(ctx, a, pr.Number, pr.Head.Ref)
			}
		}
	}
}

// deletedByPerson says a person deleted pull request number's copy after
// its branch's last run: the next push makes it again, not the sweep.
func (p *Platform) deletedByPerson(ctx context.Context, a *store.App, number int, branch string) bool {
	row, err := p.Store.LastCopia(ctx, a.ID, number)
	if err != nil || row.GoneWhy != store.CopiaPerson {
		return false
	}
	last, err := p.Store.LastRunOf(ctx, a.ID, branch)
	return err == nil && last.ID <= row.RunID
}

// imagesOfRun are the images a finished run passed, from its recorded
// steps: what it built (by digest), what it reused from the app's latest
// release, and ready-made images as given.
func (p *Platform) imagesOfRun(ctx context.Context, a *store.App, run *store.Run, sp spec.Spec) map[string]string {
	targets := map[string]string{}
	for _, st := range pipeline.Plan(a.Name, p.Config.Registry, sp) {
		if st.Kind == pipeline.KindBuild {
			targets[st.ID] = st.Target
		}
	}
	var latest map[string]string
	if rel, err := p.Store.LatestRelease(ctx, a.ID); err == nil {
		latest = rel.Images
	}
	images := map[string]string{}
	for _, st := range run.Steps {
		target, built := targets[st.ID]
		switch {
		case built && st.Digest != "":
			images[st.Service] = target + "@" + st.Digest
		case built && latest[st.Service] != "":
			images[st.Service] = latest[st.Service] // reused: unchanged since the last release
		}
	}
	for _, svc := range sp.Services {
		if svc.Image != "" {
			images[svc.Name] = svc.Image
		}
	}
	return images
}

// ClosePreview deletes the copy of a closed or merged pull request, when
// GitHub says so; the sweep catches what a lost webhook leaves.
func (p *Platform) ClosePreview(ctx context.Context, repo string, number int, merged bool) {
	copias := p.copias()
	if copias == nil {
		return
	}
	apps, err := p.Store.AppsForRepo(ctx, repo)
	if err != nil {
		return
	}
	for _, a := range apps {
		c, ok := copias.Get(ctx, a.Name, number)
		if !ok {
			continue
		}
		kind, why := store.CopiaClosed, M("the pull request was closed")
		if merged {
			kind, why = store.CopiaMerged, M("the pull request was merged")
		}
		if err := p.deletePreview(ctx, c, kind, why, ""); err == nil {
			p.Log.Info("preview deleted: its pull request closed", "app", a.Name, "pull", number)
		} else {
			p.Log.Warn("preview: deleting", "app", a.Name, "pull", number, "err", err)
		}
	}
}

// PreviewLoop deletes, every PreviewEvery (previewSweep), the copies whose
// app no longer asks for previews or is gone, whose pull request is closed,
// or that went longer than the app's keep without a push, and makes those
// that are missing.
func (p *Platform) PreviewLoop(ctx context.Context) {
	if p.copias() == nil {
		return
	}
	every := p.PreviewEvery
	if every <= 0 {
		every = previewSweep
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		if !p.Dormido() { // asleep, no copy starts nor goes
			p.sweepPreviews(ctx, time.Now())
			p.catchUpPreviews(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (p *Platform) sweepPreviews(ctx context.Context, now time.Time) {
	copies, err := p.copias().List(ctx, "")
	if err != nil {
		p.Log.Warn("previews: listing", "err", err)
		return
	}
	for _, c := range copies {
		if kind, why := p.previewGone(ctx, c, now); why != "" {
			name := c.name()
			if err := p.deletePreview(ctx, c, kind, why, ""); err != nil {
				p.Log.Warn("previews: deleting", "copy", name, "err", err)
				continue
			}
			p.Log.Info("preview deleted", "copy", name, "why", why)
		}
	}
}

// previewGone says why a copy should go (kind for the Caminos tab, and
// why in words; "" to keep it).
func (p *Platform) previewGone(ctx context.Context, c copia, now time.Time) (string, string) {
	app, err := p.Store.GetApp(ctx, c.App)
	if err != nil {
		return store.CopiaApp, M("its app is gone")
	}
	if c.Run != 0 {
		// Its run deletes it once the checks end; one left behind (the
		// platform restarted meanwhile) goes here.
		if now.Sub(c.PushedAt) > shortCopyLife {
			return store.CopiaSeco, M("its checks en seco ended long ago")
		}
		return "", ""
	}
	if app.Spec.Previews == nil {
		return store.CopiaApp, M("the app no longer asks for previews")
	}
	if now.After(p.copyDeadline(ctx, app.ID, c.Number, c.PushedAt, app.Spec.Previews.KeepDays())) {
		return store.CopiaIdle, M("no push for longer than the app keeps a copy")
	}
	got, ok := p.recentPulls(ctx, app)
	if !ok {
		return "", "" // GitHub did not answer: keep it until it does
	}
	for _, pr := range got.prs {
		if pr.Number == c.Number {
			switch {
			case pr.State == "open":
				return "", ""
			case pr.MergedAt != nil:
				return store.CopiaMerged, M("the pull request was merged")
			}
			return store.CopiaClosed, M("the pull request was closed")
		}
	}
	return "", "" // not among the latest pull requests: the idle rule takes it in time
}
