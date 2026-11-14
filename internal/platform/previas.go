package platform

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	gh "github.com/rendimiento-ai/rendimiento/internal/github"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/pruebas"
	"github.com/rendimiento-ai/rendimiento/internal/render"
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
	annotationPushed  = "rendimiento.ai/pushed-at"  // when its branch last ran
	annotationComment = "rendimiento.ai/comment"    // the pull request's comment kept for it
)

// previewSweep is how often copies of closed or idle pull requests are
// looked for.
const previewSweep = 10 * time.Minute

// PreviewName is the App object of pull request number's copy.
func PreviewName(app string, number int) string { return fmt.Sprintf("%s-pr-%d", app, number) }

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
func (p *Platform) deployPreview(ctx context.Context, app *store.App, run *store.Run, sp spec.Spec, images map[string]string) {
	if sp.Previews == nil || p.Kube == nil || run.Deploy {
		return
	}
	if number := p.openPullOf(ctx, app, run.Branch); number != 0 {
		p.deployPreviewOf(ctx, app, run, sp, images, number)
	}
}

// deployPreviewOf writes pull request number's copy from a run that passed.
func (p *Platform) deployPreviewOf(ctx context.Context, app *store.App, run *store.Run, sp spec.Spec, images map[string]string, number int) {
	log := p.Log.With("app", app.Name, "pull", number)
	copy := spec.PreviewOf(sp, number)
	name := PreviewName(app.Name, number)
	if err := p.makeRoom(ctx, app, name, sp.Previews.Copies()); err != nil {
		log.Warn("preview: making room", "err", err)
	}
	org, err := p.orgSlug(ctx, app.ID)
	if err != nil {
		log.Warn("preview: the app's organization", "err", err)
		return
	}
	// One comment on the pull request, edited on each push: the address,
	// the commit and when the copy goes.
	url := previewURL(copy)
	var before v1alpha1.App
	_ = p.Kube.Get(ctx, client.ObjectKey{Name: name}, &before)
	commentID, _ := strconv.ParseInt(before.Annotations[annotationComment], 10, 64)
	if p.GitHub != nil {
		body := previewComment(number, url, run, p.Config.BaseURL, app.Name, time.Now().AddDate(0, 0, sp.Previews.KeepDays()))
		if id, err := p.GitHub.Comment(ctx, app.InstallationID, app.Repo, number, commentID, body); err == nil {
			commentID = id
		} else {
			log.Warn("preview: its comment", "err", err)
		}
	}
	for attempt := 0; ; attempt++ {
		var cr v1alpha1.App
		err := p.Kube.Get(ctx, client.ObjectKey{Name: name}, &cr)
		creating := apierrors.IsNotFound(err)
		if err != nil && !creating {
			log.Warn("preview: reading its App object", "err", err)
			return
		}
		cr.Name = name
		cr.Labels = map[string]string{"rendimiento.ai/app-id": strconv.FormatInt(app.ID, 10), render.LabelOrg: org,
			render.LabelPreview: strconv.Itoa(number), labelPreviewOf: app.Name}
		cr.Annotations = map[string]string{annotationPushed: time.Now().UTC().Format(time.RFC3339)}
		if commentID != 0 {
			cr.Annotations[annotationComment] = strconv.FormatInt(commentID, 10)
		}
		cr.Spec = v1alpha1.AppSpec{Repo: app.Repo, Services: copy.Services, Postgres: copy.Postgres, Redis: copy.Redis,
			Observe: copy.Observe, Images: images, Release: run.ID, Preview: number}
		if creating {
			err = p.Kube.Create(ctx, &cr)
		} else {
			err = p.Kube.Update(ctx, &cr)
		}
		if apierrors.IsConflict(err) && attempt < 5 {
			continue
		}
		if err != nil {
			log.Warn("preview: writing its App object", "err", err)
			return
		}
		break
	}
	log.Info("preview deployed", "run", run.ID, "url", url)
	if len(run.SHA) == 40 && p.GitHub != nil {
		_, err := p.GitHub.CreateCheck(ctx, app.InstallationID, app.Repo, gh.CheckRun{
			Name: "preview: " + app.Name, HeadSHA: run.SHA, Status: "completed", Conclusion: "success", DetailsURL: url,
			Output: gh.CheckOutput(M("Live at %s", url),
				M("This pull request's copy. It is deleted when the pull request is closed, or after %d days without a push.", sp.Previews.KeepDays())),
		})
		if err != nil {
			log.Warn("preview: its check", "err", err)
		}
	}
	if sp.Tests != nil {
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
	url := previewURL(copy)
	say := func(section string) {
		if commentID == 0 || p.GitHub == nil {
			return
		}
		body := previewComment(number, url, run, p.Config.BaseURL, app.Name, time.Now().AddDate(0, 0, sp.Previews.KeepDays())) + "\n\n" + section
		if _, err := p.GitHub.Comment(ctx, app.InstallationID, app.Repo, number, commentID, body); err != nil {
			log.Warn("preview: its comment", "err", err)
		}
	}
	if ok, why := p.waitRollout(ctx, name, run.ID); !ok {
		say(M("**Tests:** not run, the copy did not become healthy: %s.", why))
		return
	}
	task := sp.Tests.Task(&copy)
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
	log.Info("preview tested", "run", run.ID, "tests", len(got))
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

// deletePreview deletes a copy and says why in its pull request's comment.
func (p *Platform) deletePreview(ctx context.Context, cr *v1alpha1.App, why string) error {
	if err := p.Kube.Delete(ctx, cr); err != nil {
		return err
	}
	id, _ := strconv.ParseInt(cr.Annotations[annotationComment], 10, 64)
	if id == 0 || p.GitHub == nil {
		return nil
	}
	app, err := p.Store.GetApp(ctx, cr.Labels[labelPreviewOf])
	if err != nil {
		return nil
	}
	body := M("**Preview of #%d:** deleted, %s.", cr.Spec.Preview, why)
	if _, err := p.GitHub.Comment(ctx, app.InstallationID, app.Repo, cr.Spec.Preview, id, body); err != nil {
		p.Log.Warn("preview: its comment", "copy", cr.Name, "err", err)
	}
	return nil
}

// makeRoom deletes the copies of the app idle longest until a new one fits
// under max (keep is the copy being written, which never goes).
func (p *Platform) makeRoom(ctx context.Context, app *store.App, keep string, max int) error {
	copies, err := p.previews(ctx, app.Name)
	if err != nil {
		return err
	}
	others := copies[:0]
	for _, c := range copies {
		if c.Name != keep {
			others = append(others, c)
		}
	}
	sort.Slice(others, func(i, j int) bool { return pushedAt(others[i]).Before(pushedAt(others[j])) })
	for len(others) >= max {
		if err := p.deletePreview(ctx, &others[0], M("to make room for a newer pull request's copy")); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		p.Log.Info("preview deleted to make room", "app", app.Name, "copy", others[0].Name)
		others = others[1:]
	}
	return nil
}

// previews are the App objects of an app's copies ("" for every app's).
func (p *Platform) previews(ctx context.Context, app string) ([]v1alpha1.App, error) {
	var list v1alpha1.AppList
	sel := client.HasLabels{render.LabelPreview}
	opts := []client.ListOption{sel}
	if app != "" {
		opts = []client.ListOption{client.MatchingLabels{labelPreviewOf: app}}
	}
	if err := p.Kube.List(ctx, &list, opts...); err != nil {
		return nil, err
	}
	var out []v1alpha1.App
	for _, a := range list.Items {
		if a.Spec.Preview > 0 {
			out = append(out, a)
		}
	}
	return out, nil
}

func pushedAt(a v1alpha1.App) time.Time {
	t, err := time.Parse(time.RFC3339, a.Annotations[annotationPushed])
	if err != nil {
		return a.CreationTimestamp.Time
	}
	return t
}

// OpenedPreview gives a pull request opened (or reopened) on a branch whose
// last run already passed its copy now, from that run's images: otherwise it
// would wait for the next push. A run still going makes the copy when it
// passes, as always. A fork's pull request never gets one.
func (p *Platform) OpenedPreview(ctx context.Context, repo string, number int, branch, headRepo string) {
	if p.Kube == nil || p.Store == nil || number == 0 || branch == "" || (headRepo != "" && !strings.EqualFold(headRepo, repo)) {
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
		last, err := p.Store.LastRunOf(ctx, a.ID, branch)
		if err != nil || last.Status != store.RunSucceeded || last.Deploy || branch == a.DefaultBranch {
			continue
		}
		run, err := p.Store.GetRun(ctx, last.ID)
		if err != nil {
			continue
		}
		sp := a.Spec
		if raw, err := p.GitHub.FileAt(ctx, a.InstallationID, a.Repo, spec.FileName, run.SHA); err == nil {
			if s, err := spec.Parse(raw); err == nil {
				sp = *s
			}
		}
		if sp.Previews == nil {
			continue
		}
		p.deployPreviewOf(ctx, a, run, sp, p.imagesOfRun(ctx, a, run, sp), number)
	}
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
func (p *Platform) ClosePreview(ctx context.Context, repo string, number int) {
	if p.Kube == nil {
		return
	}
	apps, err := p.Store.AppsForRepo(ctx, repo)
	if err != nil {
		return
	}
	for _, a := range apps {
		cr := &v1alpha1.App{}
		if err := p.Kube.Get(ctx, client.ObjectKey{Name: PreviewName(a.Name, number)}, cr); err != nil {
			continue
		}
		if err := p.deletePreview(ctx, cr, M("the pull request was closed")); err == nil {
			p.Log.Info("preview deleted: its pull request closed", "app", a.Name, "pull", number)
		} else if !apierrors.IsNotFound(err) {
			p.Log.Warn("preview: deleting", "app", a.Name, "pull", number, "err", err)
		}
	}
}

// PreviewLoop deletes, every previewSweep, the copies whose app no longer
// asks for previews or is gone, whose pull request is closed, or that went
// longer than the app's keep without a push.
func (p *Platform) PreviewLoop(ctx context.Context) {
	if p.Kube == nil {
		return
	}
	tick := time.NewTicker(previewSweep)
	defer tick.Stop()
	for {
		p.sweepPreviews(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (p *Platform) sweepPreviews(ctx context.Context, now time.Time) {
	copies, err := p.previews(ctx, "")
	if err != nil {
		p.Log.Warn("previews: listing", "err", err)
		return
	}
	for i := range copies {
		c := &copies[i]
		if why := p.previewGone(ctx, c, now); why != "" {
			if err := p.deletePreview(ctx, c, why); err != nil && !apierrors.IsNotFound(err) {
				p.Log.Warn("previews: deleting", "copy", c.Name, "err", err)
				continue
			}
			p.Log.Info("preview deleted", "copy", c.Name, "why", why)
		}
	}
}

// previewGone says why a copy should go ("" to keep it).
func (p *Platform) previewGone(ctx context.Context, c *v1alpha1.App, now time.Time) string {
	app, err := p.Store.GetApp(ctx, c.Labels[labelPreviewOf])
	if err != nil {
		return M("its app is gone")
	}
	if app.Spec.Previews == nil {
		return M("the app no longer asks for previews")
	}
	if now.Sub(pushedAt(*c)) > time.Duration(app.Spec.Previews.KeepDays())*24*time.Hour {
		return M("no push for longer than the app keeps a copy")
	}
	got, ok := p.recentPulls(ctx, app)
	if !ok {
		return "" // GitHub did not answer: keep it until it does
	}
	for _, pr := range got.prs {
		if pr.Number == c.Spec.Preview {
			if pr.State != "open" {
				return M("the pull request was closed")
			}
			return ""
		}
	}
	return "" // not among the latest pull requests: the idle rule takes it in time
}
