package platform

import (
	"context"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A copy's App object names every secret the copy reads, its tests' too
// (the object itself holds only the services), so the controller gives it
// each one's preview variant; a short copy's does too.
func TestCopyNamesItsSecrets(t *testing.T) {
	_, _, _, kube := setup(t)
	ctx := context.Background()
	sp, err := spec.Parse([]byte(`services:
  - name: web
    port: 3000
    domain: nombres.example.com
    secrets: [pagos]
    secretFiles: [{ secret: certificados, mount: /etc/certificados }]
builds:
  - name: e2e
    path: e2e
tests:
  build: e2e
  secretEnv: { TEST_PASSWORD: usuario-prueba/password }
previews: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	app := &store.App{ID: 77, Name: "nombres", Repo: "o/nombres"}
	images := map[string]string{"web": "registry.example.lan:5000/nombres-web@sha256:1"}
	k := kubeCopias{kube}
	if err := k.Put(ctx, app, "principal", spec.PreviewOf(*sp, 5), images, &store.Run{ID: 90}, 5, 0); err != nil {
		t.Fatal(err)
	}
	if err := k.PutShort(ctx, app, "principal", spec.ShortCopyOf(*sp, 91), images, &store.Run{ID: 91}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nombres-pr-5", "nombres-seco-91"} {
		var cr v1alpha1.App
		if err := kube.Get(ctx, client.ObjectKey{Name: name}, &cr); err != nil {
			t.Fatal(err)
		}
		if got := cr.Annotations[render.AnnotationCopySecrets]; got != "certificados,pagos,usuario-prueba" {
			t.Fatalf("%s names its secrets %q", name, got)
		}
		if cr.Labels[render.LabelPreviewOf] != "nombres" || cr.Spec.Preview == 0 {
			t.Fatalf("%s: labels %v, preview %d", name, cr.Labels, cr.Spec.Preview)
		}
	}
}
