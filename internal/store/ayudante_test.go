package store

import (
	"context"
	"testing"
	"time"
)

// A person's conversation with the helper is theirs alone (not another
// person's, not another organization's), keeps its links and what was
// read, and goes after 7 days.
func TestAyudante(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	org, err := s.CreateOrg(ctx, "ayudante-org", "Ayudante", "p0dxD")
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.AgregarAyudante(ctx, AyudanteMensaje{OrgID: org.ID, Who: "p0dxD", Kind: AyudantePersona, Body: "¿Dónde están las llaves?", Pagina: "/"})
	if err != nil || q.ID == 0 || len(q.Enlaces) != 0 || len(q.Leido) != 0 {
		t.Fatalf("question: %+v %v", q, err)
	}
	a, err := s.AgregarAyudante(ctx, AyudanteMensaje{OrgID: org.ID, Who: "p0dxD", Kind: AyudanteRespuesta, Body: "En Entorno → Llaves.",
		Enlaces: []AyudanteEnlace{{Texto: "Llaves", Href: "/environment/llaves", Tipo: "pagina"}}, Leido: []string{"libro:guide/keys/|Keys"}, Provider: AILocal, Model: "qwen"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AgregarAyudante(ctx, AyudanteMensaje{OrgID: org.ID, Who: "abi", Kind: AyudantePersona, Body: "Otra persona"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AgregarAyudante(ctx, AyudanteMensaje{OrgID: DefaultOrg, Who: "p0dxD", Kind: AyudantePersona, Body: "Otra organización"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.AyudanteDe(ctx, org.ID, "p0dxD", time.Now().Add(-AyudanteGuarda))
	if err != nil || len(got) != 2 || got[0].ID != q.ID || got[1].ID != a.ID {
		t.Fatalf("their conversation: %+v %v", got, err)
	}
	if l := got[1].Enlaces; len(l) != 1 || l[0].Href != "/environment/llaves" || l[0].Tipo != "pagina" || got[1].Leido[0] != "libro:guide/keys/|Keys" {
		t.Fatalf("links and what it read: %+v", got[1])
	}
	if _, err := s.pool.Exec(ctx, `UPDATE ayudante_mensajes SET created_at = now() - interval '8 days' WHERE id = $1`, q.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PodarAyudante(ctx, time.Now().Add(-AyudanteGuarda)); err != nil || n < 1 {
		t.Fatalf("pruned %d: %v", n, err)
	}
	if got, err := s.AyudanteDe(ctx, org.ID, "p0dxD", time.Time{}); err != nil || len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("after 7 days: %+v %v", got, err)
	}
}
