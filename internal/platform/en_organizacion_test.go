package platform

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// An app of an organization other than the platform's has a namespace of
// its own (no sharedNamespace) and never takes the platform's address; no
// organization takes an address another organization's app claims, even
// before it is served. Checked when the app is created and on every push.
func TestWhatAnOrganizationsAppMayBe(t *testing.T) {
	p, fake, _, _ := setup(t)
	ctx := context.Background()
	huerto, err := p.Store.CreateOrg(ctx, "huerto", "El huerto", "ana")
	if err != nil {
		t.Fatal(err)
	}
	registrar := func(org int64, name, yaml string) (*store.App, error) {
		t.Helper()
		sp, err := spec.Parse([]byte(yaml))
		if err != nil {
			t.Fatal(err)
		}
		return p.Register(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/" + name, DefaultBranch: "main", Name: name, Spec: *sp, OrgID: org})
	}

	if _, err := registrar(huerto.ID, "compartida", "sharedNamespace: true\nservices:\n  - name: web\n"); err == nil || !strings.Contains(err.Error(), "sharedNamespace") {
		t.Fatalf("another organization's app with sharedNamespace: %v", err)
	}
	if _, err := registrar(huerto.ID, "plataforma", "services:\n  - name: web\n    domain: rendimiento.example.com\n"); err == nil || !strings.Contains(err.Error(), "platform's own address") {
		t.Fatalf("another organization's app on the platform's address: %v", err)
	}

	// The platform's app claims an address before it is served: the other
	// organization cannot take it, and is not told whose it is.
	if _, err := registrar(store.DefaultOrg, "orgreglas-tienda", "services:\n  - name: web\n    domain: orgreglas-tienda.example.com\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := registrar(huerto.ID, "tienda", "services:\n  - name: web\n    domain: orgreglas-tienda.example.com\n"); err == nil ||
		!strings.Contains(err.Error(), "another organization") || strings.Contains(err.Error(), `"orgreglas-tienda"`) {
		t.Fatalf("an address another organization's app claims: %v", err)
	}
	// And the other way around.
	caja, err := registrar(huerto.ID, "caja", "services:\n  - name: web\n    domain: huerto-caja.example.com\n    aliases: [caja.example.com]\n")
	if err != nil || caja.Name != "huerto-caja" {
		t.Fatalf("the other organization's app: %+v %v", caja, err)
	}
	if _, err := registrar(store.DefaultOrg, "orgreglas-caja", "services:\n  - name: web\n    domain: caja.example.com\n"); err == nil || !strings.Contains(err.Error(), "another organization") {
		t.Fatalf("the platform's app on another organization's alias: %v", err)
	}

	// A push that brings sharedNamespace is rejected, and nothing of it is kept.
	sha := strings.Repeat("c", 40)
	fake.mu.Lock()
	fake.files[sha] = fstest.MapFS{spec.FileName: {Data: []byte("sharedNamespace: true\nservices:\n  - name: web\n    domain: huerto-caja.example.com\n")}}
	fake.mu.Unlock()
	run, err := p.QueueRun(ctx, caja, "main", sha, "push")
	if err != nil || run == nil || run.Status != store.RunFailed || !strings.Contains(run.Message, "sharedNamespace") {
		t.Fatalf("a push with sharedNamespace: %+v %v", run, err)
	}
	// So is one that moves to the platform's app's address.
	sha2 := strings.Repeat("d", 40)
	fake.mu.Lock()
	fake.files[sha2] = fstest.MapFS{spec.FileName: {Data: []byte("services:\n  - name: web\n    domain: orgreglas-tienda.example.com\n")}}
	fake.mu.Unlock()
	if run, err := p.QueueRun(ctx, caja, "main", sha2, "push"); err != nil || run == nil || run.Status != store.RunFailed || !strings.Contains(run.Message, "another organization") {
		t.Fatalf("a push onto another organization's address: %+v %v", run, err)
	}
	if a, err := p.Store.GetApp(ctx, "huerto-caja"); err != nil || a.Spec.SharedNamespace || a.Spec.Services[0].Domain != "huerto-caja.example.com" {
		t.Fatalf("the app after the rejected pushes: %+v %v", a, err)
	}
}
