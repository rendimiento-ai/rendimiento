package platform

import (
	"context"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	gh "github.com/rendimiento-ai/rendimiento/internal/github"
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
