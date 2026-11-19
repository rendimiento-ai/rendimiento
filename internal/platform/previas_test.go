package platform

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A branch's run that passed gives its open pull request a copy, at
// pr-<n>.<domain>, with a check linking to it; the oldest copy goes when
// the app has its most; closing the pull request deletes the copy.
func TestPreviewCopies(t *testing.T) {
	p, fake, _, kube := setup(t)
	ctx := context.Background()
	sp, err := spec.Parse([]byte("services:\n  - name: web\n    port: 3000\n    domain: copias.example.com\npreviews: {max: 1}\n"))
	if err != nil {
		t.Fatal(err)
	}
	app := &store.App{Name: "copias", Repo: "o/copias", DefaultBranch: "main", Spec: *sp}
	if err := p.Store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	pull := func(n int, branch, state string) forja.Change {
		var pr forja.Change
		pr.Number, pr.State = n, state
		pr.Head.Ref, pr.Base.Ref = branch, "main"
		return pr
	}
	fake.recent = []forja.Change{pull(8, "otra", "open"), pull(7, "propina", "open"), pull(6, "sin-copia", "closed")}
	images := map[string]string{"web": "registry.example.lan:5000/copias-web@sha256:1"}
	sha := strings.Repeat("a", 40)

	p.deployPreview(ctx, app, &store.Run{ID: 41, SHA: sha, Branch: "propina"}, *sp, images)
	var cr v1alpha1.App
	if err := kube.Get(ctx, client.ObjectKey{Name: "copias-pr-7"}, &cr); err != nil {
		t.Fatal(err)
	}
	if cr.Spec.Preview != 7 || cr.Labels[render.LabelPreview] != "7" || cr.Spec.Services[0].Domain != "copias-pr-7.example.com" || cr.Spec.Images["web"] != images["web"] {
		t.Fatalf("copy %+v %v", cr.Spec, cr.Labels)
	}
	var check forja.Check
	for _, c := range fake.checks {
		if c.Name == "preview: copias" {
			check = c
		}
	}
	if check.DetailsURL != "https://copias-pr-7.example.com" || check.Conclusion != "success" {
		t.Fatalf("check %+v", check)
	}
	// One comment, kept in the App object and edited on the next push.
	if len(fake.said) != 1 || !strings.Contains(fake.said[1], "#7 **Preview of #7:** https://copias-pr-7.example.com") || cr.Annotations[annotationComment] != "1" {
		t.Fatalf("comments %v, annotation %q", fake.said, cr.Annotations[annotationComment])
	}
	p.deployPreview(ctx, app, &store.Run{ID: 45, SHA: sha, Branch: "propina"}, *sp, images)
	if len(fake.said) != 1 || !strings.Contains(fake.said[1], "run [#45]") {
		t.Fatalf("after a second push: %v", fake.said)
	}

	// A branch with no open pull request gets no copy.
	p.deployPreview(ctx, app, &store.Run{ID: 42, SHA: sha, Branch: "sin-copia"}, *sp, images)
	if err := kube.Get(ctx, client.ObjectKey{Name: "copias-pr-6"}, &cr); !apierrors.IsNotFound(err) {
		t.Fatalf("a closed pull request got a copy: %v", err)
	}

	// max 1: the next pull request's copy replaces the idle one.
	p.deployPreview(ctx, app, &store.Run{ID: 43, SHA: sha, Branch: "otra"}, *sp, images)
	if !gone(ctx, kube, "copias-pr-7") {
		t.Fatal("the older copy stayed past max")
	}
	if !strings.Contains(fake.said[1], "deleted, to make room") {
		t.Fatalf("the replaced copy's comment: %q", fake.said[1])
	}
	p.ClosePreview(ctx, "o/copias", 8)
	if !gone(ctx, kube, "copias-pr-8") {
		t.Fatal("closing the pull request left its copy")
	}
	if !strings.Contains(fake.said[2], "#8 **Preview of #8:** deleted, the pull request was closed") {
		t.Fatalf("the closed copy's comment: %q", fake.said[2])
	}

	// The sweep deletes a copy whose pull request closed meanwhile.
	p.deployPreview(ctx, app, &store.Run{ID: 44, SHA: sha, Branch: "propina"}, *sp, images)
	fake.recent[1].State = "closed"
	p.branchPullsCache.mu.Lock()
	delete(p.branchPullsCache.byApp, app.ID)
	p.branchPullsCache.mu.Unlock()
	p.sweepPreviews(ctx, time.Now())
	if !gone(ctx, kube, "copias-pr-7") {
		t.Fatal("the sweep left the copy of a closed pull request")
	}
}

// gone: the App object is deleted, or being deleted (the controller's
// finalizer still has it).
func gone(ctx context.Context, kube client.Client, name string) bool {
	var cr v1alpha1.App
	err := kube.Get(ctx, client.ObjectKey{Name: name}, &cr)
	return apierrors.IsNotFound(err) || (err == nil && !cr.DeletionTimestamp.IsZero())
}

// A pull request opened after its branch already passed gets its copy at
// once, from that run's images; a fork's never does.
func TestPreviewWhenOpened(t *testing.T) {
	p, fake, _, kube := setup(t)
	ctx := context.Background()
	yaml := "services:\n  - name: web\n    port: 3000\n    domain: abiertas.example.com\npreviews: {}\n"
	sp, err := spec.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	app := &store.App{Name: "abiertas", Repo: "o/abiertas", DefaultBranch: "main", Spec: *sp}
	if err := p.Store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("b", 40)
	fake.files[sha] = fstest.MapFS{spec.FileName: {Data: []byte(yaml)}}
	run := passedRun(t, p, app, sp, sha, "lista")

	p.OpenedPreview(ctx, "o/abiertas", 9, "lista", "otra/abiertas")
	if !gone(ctx, kube, "abiertas-pr-9") {
		t.Fatal("a fork's pull request got a copy")
	}
	p.OpenedPreview(ctx, "o/abiertas", 9, "lista", "o/abiertas")
	var cr v1alpha1.App
	if err := kube.Get(ctx, client.ObjectKey{Name: "abiertas-pr-9"}, &cr); err != nil {
		t.Fatal(err)
	}
	if cr.Spec.Preview != 9 || cr.Spec.Release != run.ID || !strings.HasSuffix(cr.Spec.Images["web"], "@sha256:"+sha[:8]) {
		t.Fatalf("copy %+v", cr.Spec)
	}
}

// Once a copy is healthy, the app's own tests run against its address and
// the pull request's comment says how they went.
func TestPreviewRunsTheAppsTests(t *testing.T) {
	p, fake, _, _ := setup(t)
	ctx := context.Background()
	sp, err := spec.Parse([]byte("services:\n  - name: web\n    port: 3000\n    domain: probadas.example.com\nbuilds:\n  - name: e2e\n    path: e2e\ntests: {build: e2e}\npreviews: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	app := &store.App{Name: "probadas", Repo: "o/probadas", DefaultBranch: "main", Spec: *sp}
	if err := p.Store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	var pr forja.Change
	pr.Number, pr.State = 3, "open"
	pr.Head.Ref, pr.Base.Ref = "con-pruebas", "main"
	fake.recent = []forja.Change{pr}
	p.Verify.Rollout = func(context.Context, string, int64) (RolloutState, string, error) { return RolloutHealthy, "", nil }
	var ran spec.Task
	var where string
	done := make(chan struct{})
	p.Verify.RunTask = func(_ context.Context, a *store.App, _ *store.Release, task spec.Task) store.ReleaseTask {
		ran, where = task, a.Name
		defer close(done)
		return store.ReleaseTask{Status: store.TaskSucceeded,
			Log: "@@prueba\texpected\trepartir.spec.js › abre\n@@prueba\tunexpected\trepartir.spec.js › paga\n"}
	}
	images := map[string]string{"web": "registry.example.lan:5000/probadas-web@sha256:1", "build:e2e": "registry.example.lan:5000/probadas-e2e@sha256:2"}
	p.deployPreview(ctx, app, &store.Run{ID: 51, SHA: strings.Repeat("c", 40), Branch: "con-pruebas"}, *sp, images)
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the copy's tests did not run")
	}
	if where != "probadas-pr-3" || ran.Image != images["build:e2e"] || ran.Env["BASE_URL"] != "https://probadas-pr-3.example.com" {
		t.Fatalf("ran %s in %s against %s", ran.Image, where, ran.Env["BASE_URL"])
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		fake.mu.Lock()
		said := fake.said[1]
		fake.mu.Unlock()
		if strings.Contains(said, "**Tests:** 1 of 2 passed against this copy.") && strings.Contains(said, "- failed: repartir.spec.js › paga") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the comment says %q", said)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// fakeCopias keeps copies in memory, as a computer's containers would.
type fakeCopias struct {
	mu     sync.Mutex
	copies map[string]copia
	images map[string]map[string]string
}

func (f *fakeCopias) List(_ context.Context, app string) ([]copia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []copia
	for _, c := range f.copies {
		if app == "" || c.App == app {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeCopias) Get(_ context.Context, app string, number int) (copia, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.copies[PreviewName(app, number)]
	return c, ok
}

func (f *fakeCopias) Put(_ context.Context, a *store.App, _ string, _ spec.Spec, images map[string]string, _ *store.Run, number int, commentID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.copies[PreviewName(a.Name, number)] = copia{App: a.Name, Number: number, PushedAt: time.Now(), CommentID: commentID}
	f.images[PreviewName(a.Name, number)] = images
	return nil
}

func (f *fakeCopias) Delete(_ context.Context, c copia) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.copies, PreviewName(c.App, c.Number))
	return nil
}

func (f *fakeCopias) URL(_ spec.Spec, app string, number int) string {
	return "http://" + PreviewName(app, number) + ".localhost:8000/"
}

func (f *fakeCopias) TestURL(spec.Spec) string { return "http://web:3000" }

// Where GitHub sends no events (a computer), the sweep makes the copy of a
// pull request opened on a branch that already passed, at the copy's own
// address, and removes it once the pull request closes.
func TestPreviewCatchUp(t *testing.T) {
	p, fake, _, _ := setup(t)
	ctx := context.Background()
	copias := &fakeCopias{copies: map[string]copia{}, images: map[string]map[string]string{}}
	p.Copias = copias
	yaml := "services:\n  - name: web\n    port: 3000\npreviews: {}\n"
	sp, err := spec.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	app := &store.App{Name: "alcanzadas", Repo: "o/alcanzadas", DefaultBranch: "main", Spec: *sp}
	if err := p.Store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("d", 40)
	fake.files[sha] = fstest.MapFS{spec.FileName: {Data: []byte(yaml)}}
	passedRun(t, p, app, sp, sha, "al-dia")
	var pr forja.Change
	pr.Number, pr.State = 4, "open"
	pr.Head.Ref, pr.Base.Ref = "al-dia", "main"
	fake.recent = []forja.Change{pr}
	// setup's Work already sweeps in the background: what it asked GitHub
	// before the pull request existed is kept for branchPullsTTL.
	p.branchPullsCache.mu.Lock()
	delete(p.branchPullsCache.byApp, app.ID)
	p.branchPullsCache.mu.Unlock()

	p.catchUpPreviews(ctx)
	if _, ok := copias.Get(ctx, "alcanzadas", 4); !ok || !strings.HasSuffix(copias.images["alcanzadas-pr-4"]["web"], "@sha256:"+sha[:8]) {
		t.Fatalf("copies after catching up: %+v %v", copias.copies, copias.images)
	}
	for _, c := range fake.checks {
		if c.Name == "preview: alcanzadas" && c.DetailsURL != "http://alcanzadas-pr-4.localhost:8000/" {
			t.Fatalf("the check links to %s", c.DetailsURL)
		}
	}

	fake.recent[0].State = "closed"
	p.branchPullsCache.mu.Lock()
	delete(p.branchPullsCache.byApp, app.ID)
	p.branchPullsCache.mu.Unlock()
	p.sweepPreviews(ctx, time.Now())
	if _, ok := copias.Get(ctx, "alcanzadas", 4); ok {
		t.Fatal("the sweep left the copy of a closed pull request")
	}
}

// passedRun is a push of sha on branch that setup's worker ran and passed;
// its fake build's digest is sha256:<sha[:8]>. Marking a queued run passed
// by hand raced with that worker: when it took the run first, it built the
// step again with its own digest, and the copy had that one (run 1395).
func passedRun(t *testing.T, p *Platform, app *store.App, sp *spec.Spec, sha, branch string) *store.Run {
	t.Helper()
	run := &store.Run{AppID: app.ID, SHA: sha, Branch: branch, Event: "push"}
	if err := p.Store.CreateRun(context.Background(), run, pipeline.Plan(app.Name, p.Config.Registry, *sp)); err != nil {
		t.Fatal(err)
	}
	if got := waitRun(t, p, run.ID); got.Status != store.RunSucceeded {
		t.Fatalf("run %d: %s %s", run.ID, got.Status, got.Message)
	}
	return run
}
