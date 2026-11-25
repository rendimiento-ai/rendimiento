package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// A copy of an app gets each secret X it reads from the app's preview
// variant X-preview, copied into its namespace as X with the same type and
// keys; production's X never reaches it, and without X-preview the copy has
// no X. An edit to X-preview reaches the copy (and rolls its pods), a
// removed one takes X away, a short copy en seco gets them too, and they
// are the copy's own, so deleting it deletes them.
func TestCopySecretsFromPreviewVariants(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	// The app's namespace, with production's secrets and two preview variants.
	const app = "llavero"
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: app, Labels: map[string]string{render.LabelApp: app}}}); err != nil {
		t.Fatal(err)
	}
	secret := func(name string, typ corev1.SecretType, data map[string]string) {
		t.Helper()
		s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: app, Name: name}, Type: typ, Data: map[string][]byte{}}
		for k, v := range data {
			s.Data[k] = []byte(v)
		}
		if err := c.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	secret("pagos", corev1.SecretTypeOpaque, map[string]string{"STRIPE_KEY": "de-produccion"})
	secret("pagos-preview", corev1.SecretTypeOpaque, map[string]string{"STRIPE_KEY": "de-prueba", "MODO": "prueba"})
	secret("correo", corev1.SecretTypeOpaque, map[string]string{"key": "de-produccion"}) // no preview variant
	secret("certificados-preview", corev1.SecretTypeTLS, map[string]string{"tls.crt": "crt-de-prueba", "tls.key": "key-de-prueba"})
	secret("usuario-prueba-preview", corev1.SecretTypeOpaque, map[string]string{"password": "clave-de-prueba"})

	services := []spec.Service{{Name: "web", Path: ".", Port: 3000, Size: spec.SizeSmall, Replicas: 1,
		Secrets: []string{"pagos"}, SecretEnv: map[string]string{"MAIL_KEY": "correo/key"},
		SecretFiles: []spec.SecretFile{{Secret: "certificados", Mount: "/etc/certificados"}}}}
	copia := func(name, preview string, number int) *v1alpha1.App {
		return &v1alpha1.App{
			ObjectMeta: metav1.ObjectMeta{Name: name,
				Labels: map[string]string{render.LabelPreviewOf: app, render.LabelPreview: preview},
				// The tests' secret is only on the annotation: the App object
				// holds the services.
				Annotations: map[string]string{render.AnnotationCopySecrets: "certificados,correo,pagos,usuario-prueba"}},
			Spec: v1alpha1.AppSpec{Repo: "p0dxD/llavero", Services: services, Images: map[string]string{"web": "reg/llavero-web@sha256:1"},
				Release: 1, Preview: number},
		}
	}
	pr := copia("llavero-pr-3", "3", 3)
	if err := c.Create(ctx, pr); err != nil {
		t.Fatal(err)
	}
	get := func(ns, name string) (*corev1.Secret, bool) {
		var s corev1.Secret
		err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, &s)
		return &s, err == nil
	}
	eventually(t, "the copy's secrets", func() bool {
		_, a := get("llavero-pr-3", "pagos")
		_, b := get("llavero-pr-3", "certificados")
		_, d := get("llavero-pr-3", "usuario-prueba")
		return a && b && d
	})
	pagos, _ := get("llavero-pr-3", "pagos")
	if string(pagos.Data["STRIPE_KEY"]) != "de-prueba" || string(pagos.Data["MODO"]) != "prueba" || len(pagos.Data) != 2 || pagos.Type != corev1.SecretTypeOpaque {
		t.Fatalf("the copy's pagos is not pagos-preview: %s %v", pagos.Type, pagos.Data)
	}
	if pagos.Labels[render.LabelPreviewSecret] != "true" || pagos.Labels[render.LabelApp] != "llavero-pr-3" {
		t.Fatalf("labels %v", pagos.Labels)
	}
	if ref := metav1.GetControllerOf(pagos); ref == nil || ref.Kind != "App" || ref.Name != "llavero-pr-3" {
		t.Fatalf("the copy's pagos is not owned by the copy: %v", pagos.OwnerReferences)
	}
	certs, _ := get("llavero-pr-3", "certificados")
	if certs.Type != corev1.SecretTypeTLS || string(certs.Data["tls.crt"]) != "crt-de-prueba" {
		t.Fatalf("the copy's mounted certificados: %s %v", certs.Type, certs.Data)
	}
	if _, has := get("llavero-pr-3", "correo"); has {
		t.Fatal("the copy got production's correo, which has no preview variant")
	}
	if _, has := get("llavero-pr-3", "pagos-preview"); has {
		t.Fatal("the preview variant itself was copied under its own name")
	}
	var dep appsv1.Deployment
	eventually(t, "the copy's web", func() bool { return c.Get(ctx, client.ObjectKey{Namespace: "llavero-pr-3", Name: "web"}, &dep) == nil })
	version := dep.Spec.Template.Annotations[render.AnnotationSecretsVersion]
	if version == "" {
		t.Fatalf("the copy's pods carry no secrets version: %v", dep.Spec.Template.Annotations)
	}
	pod := dep.Spec.Template.Spec
	if len(pod.Volumes) != 1 || pod.Volumes[0].Secret == nil || pod.Volumes[0].Secret.SecretName != "certificados" {
		t.Fatalf("the copy mounts %+v", pod.Volumes)
	}

	// An edit of the preview variant reaches the copy, and rolls its pods.
	var edit corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{Namespace: app, Name: "pagos-preview"}, &edit); err != nil {
		t.Fatal(err)
	}
	edit.Data["STRIPE_KEY"] = []byte("otra-de-prueba")
	if err := c.Update(ctx, &edit); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the edit in the copy", func() bool {
		s, ok := get("llavero-pr-3", "pagos")
		return ok && string(s.Data["STRIPE_KEY"]) == "otra-de-prueba"
	})
	eventually(t, "the copy's pods rolled", func() bool {
		_ = c.Get(ctx, client.ObjectKey{Namespace: "llavero-pr-3", Name: "web"}, &dep)
		v := dep.Spec.Template.Annotations[render.AnnotationSecretsVersion]
		return v != "" && v != version
	})

	// Production's secrets are untouched and never in the copy.
	prod, _ := get(app, "pagos")
	if string(prod.Data["STRIPE_KEY"]) != "de-produccion" {
		t.Fatalf("production's pagos changed: %v", prod.Data)
	}

	// A removed preview variant takes the secret away from the copy; a
	// secret of the same name made by hand stays.
	if err := c.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "llavero-pr-3", Name: "correo"},
		Data: map[string][]byte{"key": []byte("a-mano")}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: app, Name: "usuario-prueba-preview"}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "usuario-prueba gone from the copy", func() bool {
		_, has := get("llavero-pr-3", "usuario-prueba")
		return !has
	})
	if s, has := get("llavero-pr-3", "correo"); !has || string(s.Data["key"]) != "a-mano" {
		t.Fatal("a secret made by hand in the copy's namespace was touched")
	}

	// A short copy en seco gets them the same way.
	if err := c.Create(ctx, copia("llavero-seco-41", "seco-41", 41)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the short copy's pagos", func() bool {
		s, ok := get("llavero-seco-41", "pagos")
		return ok && string(s.Data["STRIPE_KEY"]) == "otra-de-prueba"
	})
	if _, has := get("llavero-seco-41", "correo"); has {
		t.Fatal("the short copy got production's correo")
	}

	// The app itself (not a copy) is never given a preview variant.
	if err := c.Create(ctx, &v1alpha1.App{ObjectMeta: metav1.ObjectMeta{Name: "llavero-solo"},
		Spec: v1alpha1.AppSpec{Services: services, Images: map[string]string{"web": "reg/llavero-web@sha256:1"}}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the app's web", func() bool {
		return c.Get(ctx, client.ObjectKey{Namespace: "llavero-solo", Name: "web"}, &dep) == nil
	})
	if dep.Spec.Template.Annotations[render.AnnotationSecretsVersion] != "" {
		t.Fatalf("an app that is not a copy has a secrets version: %v", dep.Spec.Template.Annotations)
	}
	var none corev1.SecretList
	if err := c.List(ctx, &none, client.InNamespace("llavero-solo"), client.HasLabels{render.LabelPreviewSecret}); err != nil || len(none.Items) != 0 {
		t.Fatalf("an app that is not a copy got preview secrets: %d %v", len(none.Items), err)
	}

	// Deleting the copy: its namespace and secrets are its own, so the
	// cluster's garbage collector removes them (envtest has none: the
	// owner is what is checked).
	var ns corev1.Namespace
	if err := c.Get(ctx, client.ObjectKey{Name: "llavero-pr-3"}, &ns); err != nil || metav1.GetControllerOf(&ns) == nil || metav1.GetControllerOf(&ns).Name != "llavero-pr-3" {
		t.Fatalf("the copy's namespace is not the copy's: %v %v", ns.OwnerReferences, err)
	}
	if err := c.Delete(ctx, pr); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the copy gone", func() bool {
		return apierrors.IsNotFound(c.Get(ctx, client.ObjectKey{Name: "llavero-pr-3"}, &v1alpha1.App{}))
	})
}
