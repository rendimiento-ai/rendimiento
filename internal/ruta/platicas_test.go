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

// fakePlaticas keeps conversations in memory.
type fakePlaticas struct {
	mu       sync.Mutex
	platicas map[int64]*store.Platica
	mensajes []store.PlaticaMensaje
}

func (f *fakePlaticas) PlaticasEsperando(_ context.Context, orgID int64, _ int) ([]store.Platica, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []store.Platica{}
	for _, p := range f.platicas {
		last := ""
		for _, m := range f.mensajes {
			if m.PlaticaID == p.ID {
				last = m.Kind
			}
		}
		if p.OrgID == orgID && p.IA == store.PlaticaMiAgente && last == store.PlaticaPersona {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (f *fakePlaticas) PlaticaPorID(_ context.Context, id int64) (store.Platica, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.platicas[id]
	if !ok {
		return store.Platica{}, store.ErrNotFound
	}
	return *p, nil
}

func (f *fakePlaticas) MensajesDePlatica(_ context.Context, id, _ int64, _ int) ([]store.PlaticaMensaje, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []store.PlaticaMensaje{}
	for _, m := range f.mensajes {
		if m.PlaticaID == id {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakePlaticas) AgregarMensaje(_ context.Context, m store.PlaticaMensaje) (store.PlaticaMensaje, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m.ID, m.CreatedAt = int64(len(f.mensajes)+1), time.Now()
	f.mensajes = append(f.mensajes, m)
	return m, nil
}

func (f *fakePlaticas) GuardarIdea(_ context.Context, id int64, idea, who string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.platicas[id].Idea, f.platicas[id].IdeaBy = idea, who
	return nil
}

// «Mi agente»: a person's own agent finds the conversations that chose it
// and wait for it (the last word a person's), with their entry and idea;
// it answers inside a cargo, signed with its key's name, and may update the
// idea; it may not answer one the platform's AI answers, nor one of an app
// its key does not see, nor with a secret; a reading key only lists.
func TestPlaticasMiAgente(t *testing.T) {
	m, _, tokens := setup(t)
	_, _ = m.AddRuta(context.Background(), store.RutaEntry{Kind: store.KindPendiente, Title: "Botón para dormir la plataforma", Body: "En una computadora…", Author: "p0dxD"})
	_, _ = m.AddRuta(context.Background(), store.RutaEntry{Kind: store.KindNota, Title: "De otra app", App: "secreta", Author: "p0dxD"})
	f := &fakePlaticas{platicas: map[int64]*store.Platica{
		1: {ID: 1, EntryID: 1, IA: store.PlaticaMiAgente, Idea: "- Un botón «Dormir»"},
		2: {ID: 2, EntryID: 1, IA: store.AIPlatform},
		3: {ID: 3, EntryID: 2, IA: store.PlaticaMiAgente},
	}}
	for _, p := range []int64{1, 2, 3} {
		_, _ = f.AgregarMensaje(context.Background(), store.PlaticaMensaje{PlaticaID: p, Author: "p0dxD", Kind: store.PlaticaPersona, Body: "¿Qué pasa con las ejecuciones a medias?"})
	}
	for h, k := range m.keys {
		if k.Name == "escritor" {
			k.Apps = []string{"tienda"}
			m.keys[h] = k
		}
	}
	srv := httptest.NewServer((&Server{Store: m, Platicas: f}).Handler())
	defer srv.Close()
	cs, err := connect(t, srv.URL, tokens["escritor"])
	if err != nil {
		t.Fatal(err)
	}

	out, msg := call(t, cs, "platicas_esperando", map[string]any{})
	list, _ := out["platicas"].([]any)
	if msg != "" || len(list) != 1 {
		t.Fatalf("waiting = %v %q, want only the one that chose its agent, of an app it sees", out, msg)
	}
	first := list[0].(map[string]any)
	if first["idea"] != "- Un botón «Dormir»" || first["entrada"].(map[string]any)["titulo"] != "Botón para dormir la plataforma" ||
		first["mensajes"].([]any)[0].(map[string]any)["de"] != "p0dxD" {
		t.Fatalf("a waiting conversation: %v", first)
	}

	if _, msg := call(t, cs, "platica_responder", map[string]any{"platica": 1, "texto": "Propongo dormir ya."}); !strings.Contains(msg, "cargo") {
		t.Fatalf("without a cargo: %q", msg)
	}
	call(t, cs, "cargo_tomar", map[string]any{"proposito": "platicar"})
	if _, msg := call(t, cs, "platica_responder", map[string]any{"platica": 2, "texto": "Hola"}); !strings.Contains(msg, "no escogió a su agente") {
		t.Fatalf("one the platform's AI answers: %q", msg)
	}
	if _, msg := call(t, cs, "platica_responder", map[string]any{"platica": 3, "texto": "Hola"}); !strings.Contains(msg, "no existe") {
		t.Fatalf("one of an app it does not see: %q", msg)
	}
	if _, msg := call(t, cs, "platica_responder", map[string]any{"platica": 1, "texto": "password: Ab3dEf6hIj9kLm2nOp"}); msg == "" {
		t.Fatal("a secret was kept")
	}
	out, msg = call(t, cs, "platica_responder", map[string]any{"platica": 1, "texto": "Propongo dormir ya y repetir al despertar.",
		"idea": []any{"Dormir ya", "- Falta decidir: los webhooks"}})
	if msg != "" || out["de"] != "escritor" || out["tipo"] != store.PlaticaAgente {
		t.Fatalf("answered: %v %q", out, msg)
	}
	if got := f.platicas[1].Idea; got != "- Dormir ya\n- Falta decidir: los webhooks" || f.platicas[1].IdeaBy != "escritor" {
		t.Fatalf("idea %q by %q", got, f.platicas[1].IdeaBy)
	}
	if last := f.mensajes[len(f.mensajes)-1]; last.KeyID == nil || *last.KeyID != 1 {
		t.Fatalf("not signed with its key: %+v", last)
	}
	if out, _ := call(t, cs, "platicas_esperando", map[string]any{}); len(out["platicas"].([]any)) != 0 {
		t.Fatalf("answered, still waiting: %v", out)
	}

	lector, err := connect(t, srv.URL, tokens["lector"])
	if err != nil {
		t.Fatal(err)
	}
	tools, err := lector.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "platica_responder" {
			t.Fatal("a reading key may answer")
		}
	}
}

// The conversation tools have their English, arguments included.
func TestPlaticasEnIngles(t *testing.T) {
	m := &memory{keys: map[string]store.AgentKey{}}
	tok, h, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	m.keys[h] = store.AgentKey{ID: 1, Name: "agent", Role: store.RoleCollaborator, ExpiresAt: time.Now().Add(time.Hour)}
	s := &Server{Store: m, Platicas: &fakePlaticas{platicas: map[int64]*store.Platica{}}}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	if len(s.faltan) > 0 {
		t.Fatalf("no English for:\n%s", strings.Join(s.faltan, "\n"))
	}
	cs, err := connect(t, srv.URL+"?lang=en", tok)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, tool := range tools.Tools {
		if strings.HasPrefix(tool.Name, "platica") {
			seen++
			if tool.Description != toolsEN[tool.Name] {
				t.Errorf("%s: %q", tool.Name, tool.Description)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("%d conversation tools", seen)
	}
}
