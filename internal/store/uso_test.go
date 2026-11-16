package store

import (
	"context"
	"testing"
	"time"
)

// Use is kept as totals per day: views, moves and actions add up, times
// fall in their bucket, a time out of bounds counts the view but not the
// time, and old days are deleted.
func TestUsoAggregates(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	ms := func(n int) *int { return &n }
	hoy := time.Date(2026, 11, 16, 23, 30, 0, 0, time.UTC)
	vistas := []VistaUso{
		{Ruta: "/", Desde: ""},
		{Ruta: "/apps/:name", Desde: "/", Ms: ms(300)},
		{Ruta: "/apps/:name/runs/:id", Desde: "/apps/:name", Ms: ms(2500)},
		{Ruta: "/apps/:name", Desde: "/", Ms: ms(900)},
		{Ruta: "/apps/:name", Desde: "/", Ms: ms(-5)},     // not a time
		{Ruta: "/apps/:name", Desde: "/", Ms: ms(120000)}, // beyond the cap
	}
	if err := s.AnotarUso(ctx, hoy, vistas, []AccionUso{{"/apps/:name", "build_now"}, {"/apps/:name", "build_now"}, {"/apps/:name/runs/:id", "retry"}}); err != nil {
		t.Fatal(err)
	}
	// A second batch the same day adds to the same rows.
	if err := s.AnotarUso(ctx, hoy.Add(-time.Hour), []VistaUso{{Ruta: "/apps/:name", Desde: "/", Ms: ms(5000)}}, nil); err != nil {
		t.Fatal(err)
	}
	// And one long ago.
	viejo := hoy.AddDate(0, 0, -(UsoDias + 5))
	if err := s.AnotarUso(ctx, viejo, []VistaUso{{Ruta: "/ruta", Desde: ""}}, nil); err != nil {
		t.Fatal(err)
	}

	r, err := s.LeerUso(ctx, hoy.AddDate(0, 0, -6))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Paginas) != 3 || r.Paginas[0].Ruta != "/apps/:name" {
		t.Fatalf("pages = %+v", r.Paginas)
	}
	app := r.Paginas[0]
	if app.Vistas != 5 || app.Medidas != 3 || app.MsTotal != 300+900+5000 || app.Cubetas != [5]int64{1, 1, 0, 0, 1} {
		t.Fatalf("the app's page = %+v", app)
	}
	for _, p := range r.Paginas {
		if p.Ruta == "/apps/:name/runs/:id" && (p.Medidas != 1 || p.Cubetas[3] != 1) {
			t.Fatalf("the run's page = %+v", p)
		}
		if p.Ruta == "/ruta" {
			t.Fatal("a day before the period was counted")
		}
	}
	if r.Pasos[0] != (PasoUso{Desde: "/", Hacia: "/apps/:name", Veces: 5}) {
		t.Fatalf("moves = %+v", r.Pasos)
	}
	if r.Acciones[0] != (AccionUsada{Ruta: "/apps/:name", Accion: "build_now", Veces: 2}) || len(r.Acciones) != 2 {
		t.Fatalf("actions = %+v", r.Acciones)
	}

	// Older than 90 days: deleted.
	if err := s.PruneUso(ctx, hoy.AddDate(0, 0, -UsoDias)); err != nil {
		t.Fatal(err)
	}
	r, _ = s.LeerUso(ctx, viejo.AddDate(0, 0, -1))
	for _, p := range r.Paginas {
		if p.Ruta == "/ruta" {
			t.Fatal("the old day was not deleted")
		}
	}
	if len(r.Paginas) != 3 {
		t.Fatalf("recent days deleted too: %+v", r.Paginas)
	}
}

// The switch is on until an administrator turns it off.
func TestUsoSwitch(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if a, err := s.GetUso(ctx); err != nil || !a.Encendido {
		t.Fatalf("by default: %+v %v", a, err)
	}
	if err := s.SetUso(ctx, false, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.GetUso(ctx); a.Encendido || a.UpdatedBy != "p0dxD" {
		t.Fatalf("turned off: %+v", a)
	}
	_ = s.SetUso(ctx, true, "p0dxD")
	if a, _ := s.GetUso(ctx); !a.Encendido {
		t.Fatalf("turned on again: %+v", a)
	}
}
