package ruta

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// fakeComentarios keeps comments in memory, as the store does.
type fakeComentarios struct {
	mu   sync.Mutex
	list []store.Comentario
}

// add is a person's comment, as the API makes it.
func (f *fakeComentarios) add(c store.Comentario) store.Comentario {
	f.mu.Lock()
	defer f.mu.Unlock()
	c.ID, c.CreatedAt = int64(len(f.list)+1), time.Now()
	f.list = append(f.list, c)
	return c
}

func (f *fakeComentarios) Comentario(_ context.Context, id int64) (store.Comentario, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id < 1 || int(id) > len(f.list) {
		return store.Comentario{}, store.ErrNotFound
	}
	return f.list[id-1], nil
}

func (f *fakeComentarios) ListComentarios(_ context.Context, orgID, cargoID, encargoID int64, _ string, _ int) ([]store.Comentario, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Comentario
	for _, c := range f.list {
		if c.OrgID == orgID && (cargoID == 0 || (c.CargoID != nil && *c.CargoID == cargoID)) && (encargoID == 0 || (c.EncargoID != nil && *c.EncargoID == encargoID)) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeComentarios) ComentariosSinResponder(_ context.Context, orgID int64, _ int) ([]store.Comentario, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Comentario
	for _, c := range f.list {
		if c.OrgID == orgID && !c.ByAgent && c.AnsweredAt == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeComentarios) ResponderComentario(_ context.Context, orgID, id, _ int64, author, body string) (store.Comentario, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id < 1 || int(id) > len(f.list) || f.list[id-1].OrgID != orgID || f.list[id-1].ByAgent {
		return store.Comentario{}, store.ErrNotFound
	}
	p := &f.list[id-1]
	now := time.Now()
	p.AnsweredAt = &now
	a := store.Comentario{ID: int64(len(f.list) + 1), OrgID: orgID, CargoID: p.CargoID, EncargoID: p.EncargoID, AnswersTo: &p.ID,
		Author: author, ByAgent: true, Body: body, CreatedAt: now, About: p.About, App: p.App, WorkOf: p.WorkOf}
	f.list = append(f.list, a)
	return a, nil
}

// A person comments on an agent's encargo and on another agent's
// handover: the agent finds both at the start (its own first), reads them
// with the encargo, and answers inside a cargo; an answered comment no
// longer waits. Readers read them but do not answer.
func TestComentarioTools(t *testing.T) {
	m := &memory{keys: map[string]store.AgentKey{}}
	tokens := map[string]string{}
	fe := &fakeEncargos{keys: map[int64]string{}}
	for i, k := range []store.AgentKey{
		{Name: "uno", Role: store.RoleCollaborator, CreatedBy: "p0dxD"},
		{Name: "lector", Role: store.RoleReader},
	} {
		tok, h, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		k.ID, k.ExpiresAt = int64(i+1), time.Now().Add(time.Hour)
		m.keys[h] = k
		tokens[k.Name] = tok
		fe.keys[k.ID] = k.Name
	}
	ctx := context.Background()
	e, _ := fe.CreateEncargo(ctx, store.Encargo{Title: "Arreglar el carrito", App: "shop", CreatedBy: "p0dxD"})
	fc := &fakeComentarios{}
	uno, otro, cargo := int64(1), int64(7), int64(3)
	fc.add(store.Comentario{CargoID: &cargo, Author: "abi", Body: "La entrega no dice cómo probarlo", About: "Limpiar las ramas", WorkOf: &otro})
	enEncargo := fc.add(store.Comentario{EncargoID: &e.ID, Author: "p0dxD", Body: "El botón quedó muy chico", About: e.Title, App: "shop", WorkOf: &uno})
	fc.add(store.Comentario{EncargoID: &e.ID, Author: "p0dxD", Body: "de otra organización", OrgID: 2})

	srv := httptest.NewServer((&Server{Store: m, Encargos: fe, Comentarios: fc}).Handler())
	defer srv.Close()
	cs, err := connect(t, srv.URL, tokens["uno"])
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	out, msg := call(t, cs, "ruta_inicio", map[string]any{})
	waiting, _ := out["comentarios_sin_responder"].([]any)
	if msg != "" || len(waiting) != 2 {
		t.Fatalf("inicio = %v %q", out["comentarios_sin_responder"], msg)
	}
	if first := waiting[0].(map[string]any); first["texto"] != "El botón quedó muy chico" || first["para_usted"] != true {
		t.Fatalf("its own work comes first: %v", waiting)
	}
	if _, msg := call(t, cs, "comentario_responder", map[string]any{"id": enEncargo.ID, "respuesta": "Lo agrandé"}); !strings.Contains(msg, "cargo") {
		t.Fatalf("answering without a cargo: %q", msg)
	}
	call(t, cs, "cargo_tomar", map[string]any{"proposito": "Atender los comentarios del carrito"})
	out, msg = call(t, cs, "encargo_leer", map[string]any{"id": e.ID})
	if hilo, _ := out["comentarios"].([]any); msg != "" || len(hilo) != 1 {
		t.Fatalf("encargo_leer comments = %v %q", out["comentarios"], msg)
	}
	if _, msg := call(t, cs, "comentario_responder", map[string]any{"id": enEncargo.ID, "respuesta": "token: ghp_abcdefghijklmnopqrstuvwxyz0123456789"}); !strings.Contains(msg, "secreto") {
		t.Fatalf("a secret in the answer: %q", msg)
	}
	if _, msg := call(t, cs, "comentario_responder", map[string]any{"id": 3, "respuesta": "x"}); !strings.Contains(msg, "no existe") {
		t.Fatalf("another organization's comment: %q", msg)
	}
	out, msg = call(t, cs, "comentario_responder", map[string]any{"id": enEncargo.ID, "respuesta": "Lo agrandé en la rama boton-grande; la ejecución pasó."})
	if msg != "" || out["por_agente"] != true || out["autor"] != "uno" {
		t.Fatalf("answer = %v %q", out, msg)
	}
	if _, msg := call(t, cs, "comentario_responder", map[string]any{"id": out["id"], "respuesta": "otra vez"}); !strings.Contains(msg, "de un agente") {
		t.Fatalf("answering an answer: %q", msg)
	}
	out, _ = call(t, cs, "comentarios_leer", map[string]any{"encargo": e.ID})
	if hilo, _ := out["comentarios"].([]any); len(hilo) != 2 || hilo[0].(map[string]any)["respondido"] != true {
		t.Fatalf("thread = %v", out["comentarios"])
	}
	if _, msg := call(t, cs, "comentarios_leer", map[string]any{}); !strings.Contains(msg, "uno de los dos") {
		t.Fatalf("reading with neither: %q", msg)
	}
	out, _ = call(t, cs, "ruta_inicio", map[string]any{})
	if waiting, _ := out["comentarios_sin_responder"].([]any); len(waiting) != 1 || waiting[0].(map[string]any)["cargo"] != float64(3) {
		t.Fatalf("still waiting = %v", out["comentarios_sin_responder"])
	}

	lector, _ := connect(t, srv.URL, tokens["lector"])
	defer lector.Close()
	if out, msg := call(t, lector, "comentarios_leer", map[string]any{"cargo": 3}); msg != "" || len(out["comentarios"].([]any)) != 1 {
		t.Fatalf("a reader's thread = %v %q", out, msg)
	}
	res, err := lector.CallTool(ctx, &mcp.CallToolParams{Name: "comentario_responder", Arguments: map[string]any{"id": 1, "respuesta": "x"}})
	if err == nil && !res.IsError {
		t.Fatal("a reader answered a comment")
	}
}
