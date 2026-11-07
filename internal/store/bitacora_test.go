package store

import (
	"context"
	"sync"
	"testing"
)

// The bitácora is a chain per organization: it holds as written, and it
// shows a row changed or removed in the database, from that row on. Two
// organizations' chains don't touch, and writers at once stay in order.
func TestBitacoraCadena(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	anotar := func(org int64, quien, accion string) Bitacora {
		t.Helper()
		b, err := s.AnotarBitacora(ctx, Bitacora{OrgID: org, QuienTipo: QuienPersona, Quien: quien, Por: "interfaz",
			Accion: accion, Sobre: map[string]string{"app": "tienda"}, Resultado: "200", Detalle: map[string]any{"n": 3}})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	primera := anotar(1, "p0dxD", "POST /api/apps/{app}/rollback")
	if primera.HuellaAnterior != "" || primera.Huella == "" {
		t.Fatalf("the first row: %+v", primera)
	}
	segunda := anotar(1, "ana", "PUT /api/apps/{app}/secrets/{secret}")
	if segunda.HuellaAnterior != primera.Huella {
		t.Fatalf("the second row does not point at the first: %+v", segunda)
	}
	otra := anotar(2, "beto", "POST /api/ruta")
	if otra.HuellaAnterior != "" {
		t.Fatalf("another organization starts its own chain: %+v", otra)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); anotar(1, "agente", "ruta_inicio") }()
	}
	wg.Wait()
	ultima := anotar(1, "p0dxD", "DELETE /api/apps/{app}")

	if v, err := s.VerificarBitacora(ctx, 1); err != nil || v.RotaEn != 0 || v.Revisados != 13 {
		t.Fatalf("an untouched chain: %+v %v", v, err)
	}
	rows, err := s.ListarBitacora(ctx, 1, FiltroBitacora{Quien: "ana"})
	if err != nil || len(rows) != 1 || rows[0].Sobre["app"] != "tienda" || rows[0].Accion != "PUT /api/apps/{app}/secrets/{secret}" {
		t.Fatalf("by who: %+v %v", rows, err)
	}
	if rows, _ := s.ListarBitacora(ctx, 1, FiltroBitacora{Accion: "rollback", App: "tienda"}); len(rows) != 1 {
		t.Fatalf("by action and app: %+v", rows)
	}
	if rows, _ := s.ListarBitacora(ctx, 1, FiltroBitacora{Antes: ultima.ID, Limite: 5}); len(rows) != 5 || rows[0].ID >= ultima.ID {
		t.Fatalf("paging back: %+v", rows)
	}

	// Someone with the database changes the second row: it shows there.
	if _, err := s.pool.Exec(ctx, `UPDATE bitacora SET quien = 'nadie' WHERE id = $1`, segunda.ID); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.VerificarBitacora(ctx, 1); v.RotaEn != segunda.ID || v.Motivo != "this row was changed" {
		t.Fatalf("a changed row: %+v", v)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE bitacora SET quien = 'ana' WHERE id = $1`, segunda.ID); err != nil {
		t.Fatal(err)
	}
	// And removes it: the next row shows it.
	if _, err := s.pool.Exec(ctx, `DELETE FROM bitacora WHERE id = $1`, segunda.ID); err != nil {
		t.Fatal(err)
	}
	v, _ := s.VerificarBitacora(ctx, 1)
	if v.RotaEn <= segunda.ID || v.Motivo != "the row before it was changed or removed" {
		t.Fatalf("a removed row: %+v", v)
	}
	if v, _ := s.VerificarBitacora(ctx, 2); v.RotaEn != 0 || v.Revisados != 1 {
		t.Fatalf("the other organization's chain: %+v", v)
	}
}
