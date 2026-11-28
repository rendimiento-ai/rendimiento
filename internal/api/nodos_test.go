package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A node's drawer: its figures for anyone of the platform's organization,
// its switches for administrators, kept when the capacity is saved.
func TestCajonDelNodo(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	if err := e.store.SetMember(ctx, store.DefaultOrg, "colabora-cajon", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	admin, colabora := e.session(t, "p0dxD"), e.session(t, "colabora-cajon")
	r, body := e.do(t, "GET", "/api/environment/nodes/desktop-node/cifras", "", colabora, nil)
	var c store.CifrasNodo
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &c) != nil || c.Nodo != "desktop-node" || c.Apps == nil {
		t.Fatalf("figures: %d %s", r.StatusCode, body)
	}
	if r, _ := e.do(t, "PUT", "/api/environment/a-ratos/desktop-node", `{"sinPruebas":true}`, colabora, nil); r.StatusCode != 403 {
		t.Fatalf("a collaborator set a node: %d", r.StatusCode)
	}
	if r, body := e.do(t, "PUT", "/api/environment/a-ratos/desktop-node", `{"sinPruebas":true,"esperar":true}`, admin, nil); r.StatusCode != 200 {
		t.Fatalf("set: %d %s", r.StatusCode, body)
	}
	// Saving the capacity without the nodes «a ratos» keeps them.
	if r, body := e.do(t, "PUT", "/api/environment/capacity", `{"parallelRuns":2,"nodesOff":[]}`, admin, nil); r.StatusCode != 200 {
		t.Fatalf("capacity: %d %s", r.StatusCode, body)
	}
	got, err := e.store.GetCapacity(ctx)
	if err != nil || got.ParallelRuns != 2 || got.ARatos["desktop-node"] != (store.ARatosNodo{SinPruebas: true, Esperar: true}) {
		t.Fatalf("kept: %+v (%v)", got, err)
	}
	// Everything back on: the node leaves the list.
	if r, _ := e.do(t, "PUT", "/api/environment/a-ratos/desktop-node", `{}`, admin, nil); r.StatusCode != 200 {
		t.Fatalf("reset: %d", r.StatusCode)
	}
	if got, _ := e.store.GetCapacity(ctx); len(got.ARatos) != 0 {
		t.Fatalf("after reset: %+v", got.ARatos)
	}
}
