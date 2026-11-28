package store

import (
	"context"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// A node's drawer counts the steps that ran on it, today and by app this
// week, and the stretches a node «a ratos» was on.
func TestCifrasDeNodo(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Test: &spec.Test{Image: "node:20", Command: "npm test"}}}}
	sp.Default()
	app := &App{Name: "cifras-a", Repo: "ana/cifras", Spec: sp}
	if err := s.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	run := &Run{AppID: app.ID, SHA: "c1f", Branch: "main", Event: "push"}
	if err := s.CreateRun(ctx, run, pipeline.Plan("cifras-a", "reg", sp)); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	aqui := []pipeline.Lugar{{Nodo: "nodo-cifras", Arch: "amd64"}}
	if _, _, err := s.UpdateStepTimes(ctx, run.ID, "web:build", pipeline.StepResult{Status: pipeline.StatusSucceeded, Started: now.Add(-3 * time.Minute), Finished: now.Add(-time.Minute),
		Lugares: []pipeline.Lugar{{Nodo: "nodo-otro", Arch: "arm64"}, aqui[0]}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UpdateStepTimes(ctx, run.ID, "web:test", pipeline.StepResult{Status: pipeline.StatusFailed, Started: now.Add(-time.Minute), Finished: now, Lugares: aqui}); err != nil {
		t.Fatal(err)
	}
	c, err := s.CifrasDeNodo(ctx, "nodo-cifras", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if c.PasosSemana != 2 || c.FallasSemana != 1 || c.MinutosSemana < 2.9 || c.MinutosSemana > 3.1 || len(c.Apps) != 1 || c.Apps[0].App != "cifras-a" || c.Apps[0].Fallas != 1 {
		t.Fatalf("the week: %+v", c)
	}
	if now.UTC().Add(-3*time.Minute).Truncate(24*time.Hour) == now.UTC().Truncate(24*time.Hour) && (c.PasosHoy != 2 || c.ImagenesHoy != 1) {
		t.Fatalf("today: %+v", c)
	}
	if c.PrendidaDesde != nil || c.HorasSemana != 0 {
		t.Fatalf("never on: %+v", c)
	}
	// On twice in a row is one stretch; off closes it.
	for _, on := range []bool{true, true} {
		if err := s.MarcarEncendido(ctx, "nodo-cifras", on); err != nil {
			t.Fatal(err)
		}
	}
	if c, err = s.CifrasDeNodo(ctx, "nodo-cifras", time.Now().Add(time.Hour)); err != nil || c.PrendidaDesde == nil || c.HorasSemana < 0.99 || c.HorasSemana > 1.01 {
		t.Fatalf("on: %+v (%v)", c, err)
	}
	if err := s.MarcarEncendido(ctx, "nodo-cifras", false); err != nil {
		t.Fatal(err)
	}
	if c, err = s.CifrasDeNodo(ctx, "nodo-cifras", time.Now().Add(time.Hour)); err != nil || c.PrendidaDesde != nil || c.HorasSemana > 0.01 {
		t.Fatalf("off: %+v (%v)", c, err)
	}
	if c, _ := s.CifrasDeNodo(ctx, "nodo-otro", now.Add(time.Second)); c.PasosSemana != 1 || c.ImagenesHoy > 1 {
		t.Fatalf("the other half's node: %+v", c)
	}
}

// What a node «a ratos» does is kept with the capacity.
func TestCapacidadARatos(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if err := s.SaveCapacity(ctx, Capacity{ARatos: map[string]ARatosNodo{"desktop-node": {SinPruebas: true, Esperar: true}}}, "ana"); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetCapacity(ctx)
	if err != nil || c.ARatos["desktop-node"] != (ARatosNodo{SinPruebas: true, Esperar: true}) {
		t.Fatalf("saved: %+v (%v)", c, err)
	}
}
