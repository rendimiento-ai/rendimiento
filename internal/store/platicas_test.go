package store

import (
	"context"
	"testing"
)

// A conversation is kept with its entry, one per entry: its messages
// signed and in order, what its AI cost added up, who answers it, its idea
// and who wrote it, its cap (and what people allowed past it), one AI turn
// at a time, and the ones that wait for a person's own agent.
func TestPlaticas(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	org, err := s.CreateOrg(ctx, "platica-org", "Plática", "p0dxD")
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.AddRuta(ctx, RutaEntry{Kind: KindPendiente, Title: "Dormir la computadora", Author: "p0dxD", OrgID: org.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlaticaDe(ctx, e.ID); err != ErrNotFound {
		t.Fatalf("before anyone wrote: %v", err)
	}
	p, err := s.AbrirPlatica(ctx, org.ID, e.ID, "p0dxD")
	if err != nil {
		t.Fatal(err)
	}
	if again, err := s.AbrirPlatica(ctx, org.ID, e.ID, "abi"); err != nil || again.ID != p.ID || again.CreatedBy != "p0dxD" {
		t.Fatalf("a second one for the same entry: %+v %v", again, err)
	}

	for _, m := range []PlaticaMensaje{
		{PlaticaID: p.ID, Author: "p0dxD", Kind: PlaticaPersona, Body: "¿Qué pasa con lo que corre?"},
		{PlaticaID: p.ID, Author: "claude-haiku-5-5", Kind: PlaticaIA, Provider: AIPlatform, Model: "claude-haiku-5-5", Body: "Dos caminos.",
			Read: []string{"ruta:92", "libro:guide/ai/|AI"}, CostMicros: 300_000},
		{PlaticaID: p.ID, Author: "abi", Kind: PlaticaPersona, Body: "La segunda."},
	} {
		if _, err := s.AgregarMensaje(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := s.MensajesDePlatica(ctx, p.ID, 0, 0)
	if err != nil || len(msgs) != 3 {
		t.Fatalf("messages %v %v", msgs, err)
	}
	if ai := msgs[1]; ai.Author != "claude-haiku-5-5" || ai.Provider != AIPlatform || len(ai.Read) != 2 || ai.Read[1] != "libro:guide/ai/|AI" {
		t.Fatalf("the AI's message, signed: %+v", ai)
	}
	if msgs[2].Author != "abi" || msgs[0].ID >= msgs[2].ID {
		t.Fatalf("not in order, or not signed: %+v", msgs)
	}
	if after, _ := s.MensajesDePlatica(ctx, p.ID, msgs[1].ID, 0); len(after) != 1 || after[0].Author != "abi" {
		t.Fatalf("after the second: %+v", after)
	}

	if err := s.ElegirIAPlatica(ctx, p.ID, PlaticaMiAgente); err != nil {
		t.Fatal(err)
	}
	if err := s.GuardarIdea(ctx, p.ID, "- Dormir ya\n- Falta decidir: los webhooks", "abi"); err != nil {
		t.Fatal(err)
	}
	p, _ = s.PlaticaPorID(ctx, p.ID)
	if p.IA != PlaticaMiAgente || p.IdeaBy != "abi" || p.IdeaAt == nil || p.SpentMicros != 300_000 || p.Messages != 3 {
		t.Fatalf("the conversation: %+v", p)
	}

	// Its cap: the organization's (50 cents by default), and what people allowed past it.
	if c, _ := s.PlaticaTope(ctx, org.ID); c != DefaultPlaticaCap {
		t.Fatalf("default cap %d", c)
	}
	if err := s.SetPlaticaTope(ctx, org.ID, 250_000, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.PlaticaTope(ctx, org.ID)
	if c != 250_000 || !PlaticaLlegoAlTope(p, c) {
		t.Fatalf("cap %d, spent %d: it should ask", c, p.SpentMicros)
	}
	if set, _ := s.AISettingsOf(ctx, org.ID); set.Provider != AILocal {
		t.Fatalf("setting the cap changed the provider: %+v", set)
	}
	_ = s.PermitirMasPlatica(ctx, p.ID, c)
	if p, _ = s.PlaticaPorID(ctx, p.ID); PlaticaLlegoAlTope(p, c) {
		t.Fatalf("allowed one cap more, still at the cap: %+v", p)
	}

	// One AI turn at a time.
	if ok, err := s.TomarTurnoPlatica(ctx, p.ID); !ok || err != nil {
		t.Fatalf("first turn: %v %v", ok, err)
	}
	if ok, _ := s.TomarTurnoPlatica(ctx, p.ID); ok {
		t.Fatal("two turns at once")
	}
	if p, _ = s.PlaticaPorID(ctx, p.ID); !p.Thinking {
		t.Fatal("not thinking during its turn")
	}
	_ = s.SoltarTurnoPlatica(ctx, p.ID)
	if ok, _ := s.TomarTurnoPlatica(ctx, p.ID); !ok {
		t.Fatal("the turn was not let go")
	}
	_ = s.SoltarTurnoPlatica(ctx, p.ID)

	// It chose the person's own agent and a person spoke last: it waits for it.
	if w, err := s.PlaticasEsperando(ctx, org.ID, 10); err != nil || len(w) != 1 || w[0].ID != p.ID {
		t.Fatalf("waiting %+v %v", w, err)
	}
	if _, err := s.AgregarMensaje(ctx, PlaticaMensaje{PlaticaID: p.ID, Author: "claude en main", Kind: PlaticaAgente, Body: "Anotado."}); err != nil {
		t.Fatal(err)
	}
	if w, _ := s.PlaticasEsperando(ctx, org.ID, 10); len(w) != 0 {
		t.Fatalf("answered, still waiting: %+v", w)
	}
	if w, _ := s.PlaticasEsperando(ctx, DefaultOrg, 10); len(w) != 0 {
		t.Fatalf("another organization's: %+v", w)
	}

	// What came out of it.
	d, _ := s.AddRuta(ctx, RutaEntry{Kind: KindDecision, Title: "Decisión: dormir", Author: "p0dxD", OrgID: org.ID})
	if err := s.PlaticaHecha(ctx, p.ID, &d.ID, []int64{12, 13}); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaticaHecha(ctx, p.ID, nil, []int64{14}); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.PlaticaPorID(ctx, p.ID); p.DecisionID == nil || *p.DecisionID != d.ID || len(p.Encargos) != 3 {
		t.Fatalf("what came of it: %+v", p)
	}
	if _, err := s.AgregarMensaje(ctx, PlaticaMensaje{PlaticaID: 99999, Author: "x", Kind: PlaticaPersona, Body: "?"}); err == nil {
		t.Fatal("a message without its conversation")
	}
}
