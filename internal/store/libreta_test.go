package store

import (
	"context"
	"testing"
)

// What a libreta reads: an app's to-dos and decisions (not its notes, not
// another app's, not the archived ones), the branches each is worked on,
// and every encargo about the app with why it was cancelled.
func TestLibretaQueries(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	pedido, err := s.AddRuta(ctx, RutaEntry{Kind: KindPendiente, Title: "Que el arreglo se dispare desde el botón", Author: "p0dxD", App: "goconcalma"})
	if err != nil || pedido.App != "goconcalma" || pedido.Discarded != "" {
		t.Fatalf("add with app: %+v %v", pedido, err)
	}
	decision, _ := s.AddRuta(ctx, RutaEntry{Kind: KindDecision, Title: "Pruebas que solo avisan", Author: "claude", ByAgent: true, App: "goconcalma"})
	s.AddRuta(ctx, RutaEntry{Kind: KindNota, Title: "una nota", Author: "p0dxD", App: "goconcalma"})
	s.AddRuta(ctx, RutaEntry{Kind: KindPendiente, Title: "de otra", Author: "p0dxD", App: "stockpulse"})
	s.AddRuta(ctx, RutaEntry{Kind: KindPendiente, Title: "de ninguna", Author: "p0dxD"})
	viejo, _ := s.AddRuta(ctx, RutaEntry{Kind: KindPendiente, Title: "archivado", Author: "p0dxD", App: "goconcalma"})
	if err := s.ArchiveRuta(ctx, viejo.ID); err != nil {
		t.Fatal(err)
	}

	list, err := s.RutaOfApp(ctx, DefaultOrg, "goconcalma")
	if err != nil || len(list) != 2 || list[0].ID != decision.ID || list[1].ID != pedido.ID {
		t.Fatalf("entries of the app: %+v %v", list, err)
	}

	// Setting one aside, and taking it up again; the app can change or go.
	pedido.Discarded = "Maven no tiene lockfile"
	if pedido, err = s.UpdateRuta(ctx, pedido, "p0dxD"); err != nil || pedido.Discarded != "Maven no tiene lockfile" || pedido.App != "goconcalma" {
		t.Fatalf("discarded: %+v %v", pedido, err)
	}
	pedido.Discarded = ""
	if pedido, err = s.UpdateRuta(ctx, pedido, "p0dxD"); err != nil || pedido.Discarded != "" {
		t.Fatalf("taken up again: %+v %v", pedido, err)
	}
	var nulls int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ruta_entries WHERE discarded IS NULL AND id = $1`, pedido.ID).Scan(&nulls); err != nil || nulls != 1 {
		t.Fatalf("an empty reason is stored as NULL: %d %v", nulls, err)
	}

	for _, b := range []string{"oferta-y-pruebas-propias", "otra", "oferta-y-pruebas-propias", " "} {
		if err := s.LinkRutaBranch(ctx, pedido.ID, b, "claude"); err != nil {
			t.Fatal(err)
		}
	}
	ramas, err := s.RutaBranches(ctx, []int64{pedido.ID, decision.ID})
	if err != nil || len(ramas[pedido.ID]) != 2 || ramas[pedido.ID][0] != "oferta-y-pruebas-propias" || len(ramas[decision.ID]) != 0 {
		t.Fatalf("branches: %v %v", ramas, err)
	}

	e1, _ := s.CreateEncargo(ctx, Encargo{Title: "Pruebas propias", App: "goconcalma", CreatedBy: "p0dxD"})
	e2, _ := s.CreateEncargo(ctx, Encargo{Title: "Arreglo para Maven", App: "goconcalma", CreatedBy: "p0dxD"})
	s.CreateEncargo(ctx, Encargo{Title: "de otra", App: "stockpulse", CreatedBy: "p0dxD"})
	if _, err := s.CancelEncargo(ctx, e2.ID, "p0dxD", "no hace falta"); err != nil {
		t.Fatal(err)
	}
	encargos, err := s.EncargosOfApp(ctx, DefaultOrg, "goconcalma")
	if err != nil || len(encargos) != 2 || encargos[0].ID != e2.ID || encargos[0].CancelNote != "no hace falta" ||
		encargos[1].ID != e1.ID || encargos[1].CancelNote != "" {
		t.Fatalf("encargos of the app: %+v %v", encargos, err)
	}
}
