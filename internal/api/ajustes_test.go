package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rendimiento-ai/rendimiento/internal/ajustes"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

func TestLiveSettings(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	h := &ajustes.Holder{Store: e.store, Boot: ajustes.Values{ajustes.OrgQuotaCPU: "2"}, PlatformNS: "rendimiento-system", IngressClass: "nginx"}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	e.s.Settings = h
	for _, ns := range []string{"ingress-nginx", "monitoring-ajustes"} {
		if err := e.kube.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.store.SetMember(ctx, store.DefaultOrg, "colabora", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	admin, colabora := e.session(t, "p0dxD"), e.session(t, "colabora")

	// Administrators of the platform's organization only, to read too.
	if r, _ := e.do(t, "GET", "/api/environment/settings", "", colabora, nil); r.StatusCode != 403 {
		t.Fatalf("collaborator reads: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "PUT", "/api/environment/settings/ORG_NETWORK_POLICY", `{"value":"true"}`, colabora, nil); r.StatusCode != 403 {
		t.Fatalf("collaborator changes: %d", r.StatusCode)
	}
	r, body := e.do(t, "GET", "/api/environment/settings", "", admin, nil)
	var v SettingsView
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &v) != nil {
		t.Fatalf("read: %d %s", r.StatusCode, body)
	}
	if len(v.Live) != len(ajustes.Live) || v.IngressNamespace != "ingress-nginx" {
		t.Fatalf("view = %+v", v)
	}
	for _, f := range v.Fixed {
		if f.Secret && f.Value != "" {
			t.Fatalf("a secret's value shown: %+v", f)
		}
	}

	// Checked like at boot, and the namespaces named must exist.
	for _, c := range []struct{ key, value, want string }{
		{"ORG_NETWORK_POLICY", "quizás", "must be true or false"},
		{"ORG_NETWORK_POLICY_CIDRS", "10.0.0.1", "must be address ranges"},
		{"ORG_NETWORK_POLICY_NAMESPACES", "monitoring-ajustes, no-existe", "namespace no-existe does not exist"},
		{"ORG_QUOTA_MEMORY", "0", "must be a quantity"},
	} {
		if r, body := e.do(t, "PUT", "/api/environment/settings/"+c.key, `{"value":"`+c.value+`"}`, admin, nil); r.StatusCode != 400 || !strings.Contains(body, c.want) {
			t.Errorf("%s=%s: %d %s", c.key, c.value, r.StatusCode, body)
		}
	}
	if r, _ := e.do(t, "PUT", "/api/environment/settings/DATABASE_URL", `{"value":"x"}`, admin, nil); r.StatusCode != 404 {
		t.Fatalf("a setting that is not live: %d", r.StatusCode)
	}

	// Saved: in force in this process at once, and in the bitácora.
	if r, body := e.do(t, "PUT", "/api/environment/settings/ORG_NETWORK_POLICY_NAMESPACES", `{"value":"monitoring-ajustes"}`, admin, nil); r.StatusCode != 200 {
		t.Fatalf("save: %d %s", r.StatusCode, body)
	}
	if r, body := e.do(t, "PUT", "/api/environment/settings/ORG_NETWORK_POLICY", `{"value":"true"}`, admin, nil); r.StatusCode != 200 {
		t.Fatalf("turn on: %d %s", r.StatusCode, body)
	}
	if n := h.Network(); !n.Enabled || !strings.Contains(strings.Join(n.Namespaces, ","), "monitoring-ajustes") {
		t.Fatalf("not followed: %+v", n)
	}
	rows, err := e.store.ListarBitacora(ctx, store.DefaultOrg, store.FiltroBitacora{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range rows {
		if b.Accion == "PUT /api/environment/settings/{key}" && b.Sobre["key"] == "ORG_NETWORK_POLICY" && b.Detalle["despues"] == "true" && b.Quien == "p0dxD" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no bitácora row for the change: %+v", rows)
	}

	// Back to the environment's value.
	if r, body := e.do(t, "DELETE", "/api/environment/settings/ORG_NETWORK_POLICY", "", admin, nil); r.StatusCode != 200 {
		t.Fatalf("forget: %d %s", r.StatusCode, body)
	}
	if h.Network().Enabled {
		t.Fatal("still on after going back to the environment's value")
	}
	if saved, _ := e.store.GetAjustes(ctx); saved["ORG_NETWORK_POLICY"].Value != "" || saved["ORG_NETWORK_POLICY_NAMESPACES"].Value != "monitoring-ajustes" {
		t.Fatalf("saved = %+v", saved)
	}
}
