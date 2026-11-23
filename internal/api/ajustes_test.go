package api

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/ajustes"
	"github.com/rendimiento-ai/rendimiento/internal/catalog"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
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

// kubeLento is a cluster slow to answer: its lists wait until suelta is
// closed. It counts the lists of every pod in the cluster (the services
// catalog's), which the settings page must not need.
type kubeLento struct {
	client.Client
	suelta    chan struct{}
	todosPods atomic.Int32
}

func (k *kubeLento) List(ctx context.Context, l client.ObjectList, opts ...client.ListOption) error {
	if _, ok := l.(*corev1.PodList); ok {
		o := (&client.ListOptions{}).ApplyOptions(opts)
		if o.Namespace == "" {
			k.todosPods.Add(1)
		}
	}
	select {
	case <-k.suelta:
	case <-ctx.Done():
		return ctx.Err()
	}
	return k.Client.List(ctx, l, opts...)
}

// A cluster slow to answer (an update rolling out) does not hold the
// settings up: they come at once, with the apps outside the wall to come,
// and the next look has them. The services catalog is never read.
func TestSettingsDoNotWaitForTheCluster(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	h := &ajustes.Holder{Store: e.store, Boot: ajustes.Values{ajustes.OrgNetworkPolicy: "true"}, PlatformNS: "rendimiento-system", IngressClass: "nginx"}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	e.s.Settings = h
	sp := spec.Spec{SharedNamespace: true, Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	if err := e.store.CreateApp(ctx, &store.App{Name: "sin-espera-compartida", Repo: "p0dxD/sin-espera-compartida", DefaultBranch: "main", Spec: sp}); err != nil {
		t.Fatal(err)
	}
	lento := &kubeLento{Client: e.kube, suelta: make(chan struct{})}
	e.s.Kube = lento
	e.s.Catalog = &catalog.Builder{Kube: lento}
	admin := e.session(t, "p0dxD")
	read := func() (SettingsView, time.Duration) {
		t.Helper()
		start := time.Now()
		r, body := e.do(t, "GET", "/api/environment/settings", "", admin, nil)
		var v SettingsView
		if r.StatusCode != 200 || json.Unmarshal([]byte(body), &v) != nil {
			t.Fatalf("settings: %d %s", r.StatusCode, body)
		}
		return v, time.Since(start)
	}
	v, took := read()
	if took > fueraWait+2*time.Second || !v.OutsidePending || v.Apps != 1 || len(v.Live) != len(ajustes.Live) {
		t.Fatalf("slow cluster: took %s, view = %+v", took, v)
	}
	close(lento.suelta)
	deadline := time.Now().Add(10 * time.Second)
	for v.OutsidePending && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		v, _ = read()
	}
	if v.OutsidePending || v.Outside["sin-espera-compartida"] != OutsideShared {
		t.Fatalf("after the cluster answered: %+v", v)
	}
	if n := lento.todosPods.Load(); n != 0 {
		t.Fatalf("the settings listed every pod in the cluster %d times (the services catalog)", n)
	}
}
