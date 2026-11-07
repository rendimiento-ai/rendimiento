package ruta

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// fakeProduccion keeps requests in memory; decidir plays the person.
type fakeProduccion struct {
	mu    sync.Mutex
	todas map[int64]store.Aprobacion
}

func (f *fakeProduccion) PedirReversion(_ context.Context, app *store.App, version int64, motivo string, k store.AgentKey) (store.Aprobacion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := store.Aprobacion{ID: int64(len(f.todas) + 1), OrgID: app.OrgID, App: app.Name, Accion: store.AccionRevertir, Version: version,
		Motivo: motivo, PedidaPor: k.Name, Estado: store.AprobacionPendiente}
	f.todas[a.ID] = a
	return a, nil
}

func (f *fakeProduccion) GetAprobacion(_ context.Context, id int64) (store.Aprobacion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.todas[id]
	if !ok {
		return a, store.ErrNotFound
	}
	return a, nil
}

func (f *fakeProduccion) decidir(id int64, estado, resultado string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.todas[id]
	a.Estado, a.Resultado = estado, resultado
	f.todas[id] = a
}

// An agent asks to roll back (with a cargo, on an app it may use), never
// does it, and waits for the person's decision; a reader has no such tool.
func TestProduccionTools(t *testing.T) {
	m := &memory{keys: map[string]store.AgentKey{}}
	tokens := map[string]string{}
	for i, k := range []store.AgentKey{
		{Name: "diagnosticador", Role: store.RoleCollaborator},
		{Name: "lector", Role: store.RoleReader},
		{Name: "solo-shop", Role: store.RoleCollaborator, Apps: []string{"shop"}},
	} {
		tok, h, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		k.ID, k.ExpiresAt = int64(i+1), time.Now().Add(time.Hour)
		m.keys[h] = k
		tokens[k.Name] = tok
	}
	fp := &fakePlataforma{apps: []*store.App{{ID: 1, Name: "propinas", DefaultBranch: "main"}}, runs: map[int64]*store.Run{}}
	prod := &fakeProduccion{todas: map[int64]store.Aprobacion{}}
	srv := httptest.NewServer((&Server{Store: m, Plataforma: fp, Produccion: prod, PausaAprobacion: 10 * time.Millisecond}).Handler())
	defer srv.Close()

	ag, err := connect(t, srv.URL, tokens["diagnosticador"])
	if err != nil {
		t.Fatal(err)
	}
	defer ag.Close()
	pedir := map[string]any{"app": "propinas", "version": 41, "motivo": "la 42 rompió el pago"}
	if _, msg := call(t, ag, "produccion_pedir", pedir); !strings.Contains(msg, "cargo") {
		t.Fatalf("asking without a cargo: %q", msg)
	}
	call(t, ag, "cargo_tomar", map[string]any{"proposito": "Arreglar el pago de propinas"})
	out, msg := call(t, ag, "produccion_pedir", pedir)
	if msg != "" || out["estado"] != "pendiente" || out["pedidaPor"] != "diagnosticador" {
		t.Fatalf("ask: %v %q", out, msg)
	}
	if out, _ := call(t, ag, "produccion_esperar", map[string]any{"id": 1, "segundos": 1}); out["estado"] != "pendiente" {
		t.Fatalf("still waiting: %v", out)
	}
	go func() { time.Sleep(50 * time.Millisecond); prod.decidir(1, store.AprobacionAprobada, "release #43") }()
	if out, _ := call(t, ag, "produccion_esperar", map[string]any{"id": 1, "segundos": 5}); out["estado"] != "aprobada" || out["resultado"] != "release #43" {
		t.Fatalf("the decision: %v", out)
	}

	solo, _ := connect(t, srv.URL, tokens["solo-shop"])
	defer solo.Close()
	call(t, solo, "cargo_tomar", map[string]any{"proposito": "Ver"})
	if _, msg := call(t, solo, "produccion_pedir", pedir); !strings.Contains(msg, "no tiene acceso") {
		t.Fatalf("a key limited to another app: %q", msg)
	}

	lector, _ := connect(t, srv.URL, tokens["lector"])
	defer lector.Close()
	tools, err := lector.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if strings.HasPrefix(tool.Name, "produccion_") {
			t.Fatalf("a reader has %s", tool.Name)
		}
	}
}
