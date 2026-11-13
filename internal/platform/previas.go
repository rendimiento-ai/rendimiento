package platform

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	gh "github.com/rendimiento-ai/rendimiento/internal/github"
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
	labelPreviewOf   = "rendimiento.ai/preview-of" // the app it copies
	annotationPushed = "rendimiento.ai/pushed-at"  // when its branch last ran
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
	number := p.openPullOf(ctx, app, run.Branch)
	if number == 0 {
		return
	}
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
	url := previewURL(copy)
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
		if err := p.Kube.Delete(ctx, &others[0]); err != nil && !apierrors.IsNotFound(err) {
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
		cr := &v1alpha1.App{ObjectMeta: metav1.ObjectMeta{Name: PreviewName(a.Name, number)}}
		if err := p.Kube.Delete(ctx, cr); err == nil {
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
			if err := p.Kube.Delete(ctx, c); err != nil && !apierrors.IsNotFound(err) {
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
		return "its app is gone"
	}
	if app.Spec.Previews == nil {
		return "the app no longer asks for previews"
	}
	if now.Sub(pushedAt(*c)) > time.Duration(app.Spec.Previews.KeepDays())*24*time.Hour {
		return "no push for longer than the app keeps a copy"
	}
	got, ok := p.recentPulls(ctx, app)
	if !ok {
		return "" // GitHub did not answer: keep it until it does
	}
	for _, pr := range got.prs {
		if pr.Number == c.Spec.Preview {
			if pr.State != "open" {
				return "its pull request is closed"
			}
			return ""
		}
	}
	return "" // not among the latest pull requests: the idle rule takes it in time
}
