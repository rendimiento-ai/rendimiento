package platform

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	gh "github.com/rendimiento-ai/rendimiento/internal/github"
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
	pull := func(n int, branch, state string) gh.PullInfo {
		var pr gh.PullInfo
		pr.Number, pr.State = n, state
		pr.Head.Ref, pr.Base.Ref = branch, "main"
		return pr
	}
	fake.recent = []gh.PullInfo{pull(8, "otra", "open"), pull(7, "propina", "open"), pull(6, "sin-copia", "closed")}
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
	var check gh.CheckRun
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
	run := &store.Run{AppID: app.ID, SHA: sha, Branch: "lista", Event: "push"}
	if err := p.Store.CreateRun(ctx, run, pipeline.Plan(app.Name, p.Config.Registry, *sp)); err != nil {
		t.Fatal(err)
	}
	if err := p.Store.UpdateStep(ctx, run.ID, "web:build", pipeline.StepResult{Status: pipeline.StatusSucceeded, Digest: "sha256:abc"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Store.FinishRun(ctx, run.ID, store.RunSucceeded, ""); err != nil {
		t.Fatal(err)
	}

	p.OpenedPreview(ctx, "o/abiertas", 9, "lista", "otra/abiertas")
	if !gone(ctx, kube, "abiertas-pr-9") {
		t.Fatal("a fork's pull request got a copy")
	}
	p.OpenedPreview(ctx, "o/abiertas", 9, "lista", "o/abiertas")
	var cr v1alpha1.App
	if err := kube.Get(ctx, client.ObjectKey{Name: "abiertas-pr-9"}, &cr); err != nil {
		t.Fatal(err)
	}
	if cr.Spec.Preview != 9 || cr.Spec.Release != run.ID || !strings.HasSuffix(cr.Spec.Images["web"], "@sha256:abc") {
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
	var pr gh.PullInfo
	pr.Number, pr.State = 3, "open"
	pr.Head.Ref, pr.Base.Ref = "con-pruebas", "main"
	fake.recent = []gh.PullInfo{pr}
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
