package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rendimiento-ai/rendimiento/internal/ajustes"
	"github.com/rendimiento-ai/rendimiento/internal/catalog"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A cluster slow to answer (an update rolling out) does not hold up the
// pages that read the services catalog: an app's wall, the Universe, the
// Services page and the check before turning the wall on all come at once,
// with what the cluster has not said yet pending, and the next look after
// it answers has it. Every page joins one look at the cluster.
func TestCatalogPagesDoNotWaitForTheCluster(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	h := &ajustes.Holder{Store: e.store, Boot: ajustes.Values{ajustes.OrgNetworkPolicy: "true"}, PlatformNS: "rendimiento-system", IngressClass: "nginx"}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	e.s.Settings = h
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	if err := e.store.CreateApp(ctx, &store.App{Name: "sin-fila-muro", Repo: "p0dxD/sin-fila-muro", DefaultBranch: "main", Spec: sp}); err != nil {
		t.Fatal(err)
	}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "sin-fila-muro", Name: "web"},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 8080}}}}
	if err := e.kube.Create(ctx, svc); err != nil {
		t.Fatal(err)
	}
	lento := &kubeLento{Client: e.kube, suelta: make(chan struct{})}
	e.s.Kube = lento
	e.s.Catalog = &catalog.Builder{Kube: lento}
	me := e.session(t, "p0dxD")
	read := func(path string, out any) time.Duration {
		t.Helper()
		start := time.Now()
		r, body := e.do(t, "GET", path, "", me, nil)
		if r.StatusCode != 200 || json.Unmarshal([]byte(body), out) != nil {
			t.Fatalf("%s: %d %s", path, r.StatusCode, body)
		}
		return time.Since(start)
	}
	type universeNode struct {
		ID      string
		Catalog bool
	}
	type universeView struct {
		Nodes          []universeNode
		Barda          *Barda
		CatalogPending bool
	}
	type catalogView struct {
		Entries []catalog.Entry
		Pending bool
	}
	// The longest a page may take: the waits for a first answer overlap.
	patience := max(fueraWait, catalog.DefaultWait) + 2*time.Second

	var b AppBarda
	if took := read("/api/apps/sin-fila-muro/barda", &b); took > patience || !b.OutsidePending || !b.CallersPending || b.Inside || b.Why != "" || b.State != "on" {
		t.Fatalf("app's wall, slow cluster: took %s, %+v", took, b)
	}
	var u universeView
	if took := read("/api/universe", &u); took > patience || !u.CatalogPending || u.Barda == nil || !u.Barda.OutsidePending || !u.Barda.CallersPending {
		t.Fatalf("universe, slow cluster: took %s, %+v", took, u)
	}
	var c catalogView
	if took := read("/api/services", &c); took > patience || !c.Pending || c.Entries == nil || len(c.Entries) != 0 {
		t.Fatalf("services, slow cluster: took %s, %+v", took, c)
	}
	var check BoundaryCheck
	if took := read("/api/environment/settings/boundary-check", &check); took > patience || !check.Pending {
		t.Fatalf("boundary check, slow cluster: took %s, %+v", took, check)
	}

	close(lento.suelta)
	deadline := time.Now().Add(10 * time.Second)
	for (b.OutsidePending || b.CallersPending || u.CatalogPending || c.Pending || check.Pending) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		b, u, c, check = AppBarda{}, universeView{}, catalogView{}, BoundaryCheck{}
		read("/api/apps/sin-fila-muro/barda", &b)
		read("/api/universe", &u)
		read("/api/services", &c)
		read("/api/environment/settings/boundary-check", &check)
	}
	if b.OutsidePending || b.CallersPending || !b.Inside {
		t.Fatalf("app's wall after the cluster answered: %+v", b)
	}
	if u.CatalogPending || u.Barda == nil || u.Barda.OutsidePending || u.Barda.CallersPending {
		t.Fatalf("universe after the cluster answered: %+v", u)
	}
	inCatalog := false
	for _, n := range u.Nodes {
		inCatalog = inCatalog || (n.ID == "sin-fila-muro/web" && n.Catalog)
	}
	if !inCatalog {
		t.Fatalf("universe after the cluster answered has not what runs there: %+v", u.Nodes)
	}
	if c.Pending || len(c.Entries) != 1 || c.Entries[0].ID != "sin-fila-muro/web" {
		t.Fatalf("services after the cluster answered: %+v", c)
	}
	if check.Pending || check.Callers == nil {
		t.Fatalf("boundary check after the cluster answered: %+v", check)
	}
	// One look at the cluster for all of them (each lists every pod once).
	if n := lento.todosPods.Load(); n != 1 {
		t.Fatalf("the services catalog was built %d times, want 1", n)
	}
}
