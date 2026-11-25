package api

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Each secret an app reads has a preview variant, <name>-preview, which its
// copies get as <name>: an administrator sets it on the same route as the
// secret itself (a guest cannot), saving it does not restart the app, and
// only variants of declared secrets are accepted. Anyone who reads the app
// sees, by name only, which secrets its copies run without.
func TestPreviewSecretVariants(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	if err := e.store.SetMember(ctx, store.DefaultOrg, "invitada-vistas", store.MemberGuest, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	admin, guest := e.session(t, "p0dxD"), e.session(t, "invitada-vistas")
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 3000, Domain: "vitrina.example.com", Secrets: []string{"pagos"},
		SecretEnv: map[string]string{"MAIL_KEY": "correo/key"}}}, Previews: &spec.Previews{}}
	sp.Default()
	if err := e.store.CreateApp(ctx, &store.App{Name: "vitrina", Repo: "p0dxD/vitrina", DefaultBranch: "main", Spec: sp}); err != nil {
		t.Fatal(err)
	}
	type answer struct {
		Secrets []struct {
			Name, Preview string
			Set           bool
		}
		Missing []string
	}
	read := func(who string) answer {
		t.Helper()
		cookie := admin
		if who == "guest" {
			cookie = guest
		}
		r, body := e.do(t, "GET", "/api/apps/vitrina/preview-secrets", "", cookie, nil)
		var a answer
		if r.StatusCode != 200 || json.Unmarshal([]byte(body), &a) != nil {
			t.Fatalf("preview secrets as %s: %d %s", who, r.StatusCode, body)
		}
		return a
	}
	// Before the app has a namespace, nothing is set.
	if a := read("guest"); !slices.Equal(a.Missing, []string{"correo", "pagos"}) || len(a.Secrets) != 2 || a.Secrets[1].Preview != "pagos-preview" {
		t.Fatalf("before a namespace: %+v", a)
	}

	_ = e.kube.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "vitrina", Labels: map[string]string{render.LabelApp: "vitrina"}}})
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "vitrina", Name: "web", Labels: map[string]string{render.LabelApp: "vitrina"}}}
	if err := e.kube.Create(ctx, dep); err != nil {
		t.Fatal(err)
	}
	restarted := func() bool {
		var d appsv1.Deployment
		_ = e.kube.Get(ctx, client.ObjectKey{Namespace: "vitrina", Name: "web"}, &d)
		return d.Spec.Template.Annotations["rendimiento.ai/restartedAt"] != ""
	}

	// A guest neither reads a variant's keys nor writes one.
	if r, _ := e.do(t, "PUT", "/api/apps/vitrina/secrets/pagos-preview", `{"STRIPE_KEY":"de-prueba"}`, guest, nil); r.StatusCode != 403 {
		t.Fatalf("a guest writes a preview variant: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "GET", "/api/apps/vitrina/secrets/pagos-preview", "", guest, nil); r.StatusCode != 403 {
		t.Fatalf("a guest reads a preview variant's keys: %d", r.StatusCode)
	}
	// Only the variant of a declared secret.
	for _, name := range []string{"otro-preview", "-preview", "pagos-preview-preview"} {
		if r, _ := e.do(t, "PUT", "/api/apps/vitrina/secrets/"+name, `{"A":"1"}`, admin, nil); r.StatusCode != 400 {
			t.Fatalf("%s: %d, want 400", name, r.StatusCode)
		}
	}

	// An administrator sets it: written beside the app's secrets, and the
	// app does not restart.
	if r, body := e.do(t, "PUT", "/api/apps/vitrina/secrets/pagos-preview", `{"STRIPE_KEY":"de-prueba"}`, admin, nil); r.StatusCode != 204 {
		t.Fatalf("put the preview variant: %d %s", r.StatusCode, body)
	}
	var sec corev1.Secret
	if err := e.kube.Get(ctx, client.ObjectKey{Namespace: "vitrina", Name: "pagos-preview"}, &sec); err != nil || sec.StringData["STRIPE_KEY"] != "de-prueba" {
		t.Fatalf("pagos-preview = %+v %v", sec, err)
	}
	if restarted() {
		t.Fatal("saving a preview variant restarted the app")
	}
	if err := e.kube.Get(ctx, client.ObjectKey{Namespace: "vitrina", Name: "pagos"}, &corev1.Secret{}); err == nil {
		t.Fatal("saving the preview variant wrote production's pagos")
	}
	_, body := e.do(t, "GET", "/api/apps/vitrina/secrets/pagos-preview", "", admin, nil)
	if !strings.Contains(body, `"keys":["STRIPE_KEY"]`) || strings.Contains(body, "de-prueba") {
		t.Fatalf("the variant's keys: %s", body)
	}
	a := read("guest")
	if !slices.Equal(a.Missing, []string{"correo"}) || !a.Secrets[1].Set || a.Secrets[0].Set {
		t.Fatalf("after setting pagos-preview: %+v", a)
	}
	if _, list := e.do(t, "GET", "/api/apps/vitrina/preview-secrets", "", guest, nil); strings.Contains(list, "STRIPE_KEY") || strings.Contains(list, "de-prueba") {
		t.Fatalf("the list of preview secrets shows keys or values: %s", list)
	}

	// Production's secret still restarts the app.
	if r, body := e.do(t, "PUT", "/api/apps/vitrina/secrets/pagos", `{"STRIPE_KEY":"de-produccion"}`, admin, nil); r.StatusCode != 204 {
		t.Fatalf("put production's: %d %s", r.StatusCode, body)
	}
	if !restarted() {
		t.Fatal("saving production's secret did not restart the app")
	}
}
