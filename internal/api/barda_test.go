package api

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/ajustes"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

func TestBardaShowsWhatTheBoundaryCovers(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	h := &ajustes.Holder{Store: e.store, Boot: ajustes.Values{ajustes.OrgNetworkPolicy: "true", ajustes.OrgNetworkNamespaces: "monitoring"},
		PlatformNS: "rendimiento-system", IngressClass: "nginx"}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	e.s.Settings = h
	mk := func(name string, sp spec.Spec) {
		sp.Default()
		if err := e.store.CreateApp(ctx, &store.App{Name: name, Repo: "p0dxD/" + name, DefaultBranch: "main", Spec: sp}); err != nil {
			t.Fatal(err)
		}
	}
	mk("muralla", spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080, LAN: &spec.LANOptions{}}}})
	mk("compartida", spec.Spec{SharedNamespace: true, Services: []spec.Service{{Name: "web", Port: 8080}}})
	mk("en-el-xolo", spec.Spec{Services: []spec.Service{{Name: "api", Port: 8080}}})
	for _, o := range []client.Object{
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "nodo-sin-red", Labels: map[string]string{LabelNoPolicy: "no"}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "en-el-xolo"}, Spec: corev1.PodSpec{NodeName: "nodo-sin-red"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "muralla"}, Spec: corev1.PodSpec{NodeName: "nodo-con-red"}},
	} {
		if err := e.kube.Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	me := e.session(t, "p0dxD")
	get := func(app string) AppBarda {
		t.Helper()
		r, body := e.do(t, "GET", "/api/apps/"+app+"/barda", "", me, nil)
		var b AppBarda
		if r.StatusCode != 200 || json.Unmarshal([]byte(body), &b) != nil {
			t.Fatalf("%s: %d %s", app, r.StatusCode, body)
		}
		return b
	}
	if b := get("muralla"); !b.Inside || b.State != "on" || b.Org != "principal" || b.Ingress != "ingress-nginx" ||
		!slices.Equal(b.Namespaces, []string{"monitoring"}) || !slices.Equal(b.LAN["muralla"], []string{"web"}) {
		t.Fatalf("muralla = %+v", b)
	}
	if b := get("compartida"); b.Inside || b.Why != OutsideShared {
		t.Fatalf("compartida = %+v", b)
	}
	if b := get("en-el-xolo"); b.Inside || b.Why != OutsideNode || !slices.Equal(b.Nodes["en-el-xolo"], []string{"nodo-sin-red"}) {
		t.Fatalf("en-el-xolo = %+v", b)
	}

	// The Universe draws the same, and the page of settings counts it.
	r, body := e.do(t, "GET", "/api/universe", "", me, nil)
	var u struct{ Barda *Barda }
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &u) != nil || u.Barda == nil {
		t.Fatalf("universe: %d %s", r.StatusCode, body)
	}
	if u.Barda.Outside["compartida"] != OutsideShared || u.Barda.Outside["en-el-xolo"] != OutsideNode || u.Barda.Outside["muralla"] != "" {
		t.Fatalf("universe barda = %+v", u.Barda)
	}
	r, body = e.do(t, "GET", "/api/environment/settings", "", me, nil)
	var v SettingsView
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &v) != nil || v.Apps != 3 || len(v.Outside) != 2 {
		t.Fatalf("settings: %d %s", r.StatusCode, body)
	}

	// Turned off: every app is outside, for that reason.
	if r, body := e.do(t, "PUT", "/api/environment/settings/ORG_NETWORK_POLICY", `{"value":"false"}`, me, nil); r.StatusCode != 200 {
		t.Fatalf("off: %d %s", r.StatusCode, body)
	}
	if b := get("muralla"); b.Inside || b.Why != "off" || b.State != "off" {
		t.Fatalf("off: %+v", b)
	}
}
