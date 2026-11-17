package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The lookout's week adds up in SQL: a step's runs, times and whether it
// was the last of its run; an app's runs; the log's warnings by message;
// slow routes. A review runs one at a time, and a finding is decided once
// and remembered.
func TestVigiaWeek(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	desde := now.Add(-7 * 24 * time.Hour)
	var app int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO apps (name, repo, installation_id, spec) VALUES ('vigilada', 'x/vigilada', 1, '{}') RETURNING id`).Scan(&app); err != nil {
		t.Fatal(err)
	}
	// Four runs this week: build 1 min, test 10 min (always the last); one
	// failed. Two runs the weeks before, with a test of 5 min.
	for i, c := range []struct {
		ago    time.Duration
		status string
		test   time.Duration
	}{{1, "succeeded", 10}, {2, "succeeded", 10}, {3, "failed", 10}, {4, "succeeded", 10}, {10 * 24, "succeeded", 5}, {11 * 24, "succeeded", 5}} {
		start := now.Add(-c.ago * time.Hour)
		var run int64
		if err := s.pool.QueryRow(ctx, `INSERT INTO runs (app_id, sha, branch, event, deploy, status, created_at, started_at, finished_at, retries)
			VALUES ($1, 'abc', 'main', 'push', false, $2, $3, $3, $4, $5) RETURNING id`, app, c.status, start, start.Add(c.test*time.Minute), i%2).Scan(&run); err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, `INSERT INTO steps (run_id, step_id, service, kind, status, started_at, finished_at, tries) VALUES
			($1, 'web:build', 'web', 'build', 'succeeded', $2, $3, 1), ($1, 'web:test', 'web', 'test', $4, $2, $5, $6)`,
			run, start, start.Add(time.Minute), c.status, start.Add(c.test*time.Minute), 1+i%2); err != nil {
			t.Fatal(err)
		}
	}
	var lines []LogLine
	for i := range 6 {
		lines = append(lines, LogLine{At: now.Add(-time.Duration(i) * time.Hour), Level: "WARN", Process: "control", Message: "could not reach the registry", Attrs: map[string]any{"err": "dial tcp"}})
	}
	lines = append(lines,
		LogLine{At: now.Add(-20 * 24 * time.Hour), Level: "WARN", Process: "control", Message: "could not reach the registry"},
		LogLine{At: now.Add(-time.Hour), Level: "INFO", Process: "api", Message: "slow request", Attrs: map[string]any{"route": "GET /api/apps", "ms": 1500}},
		LogLine{At: now.Add(-2 * time.Hour), Level: "INFO", Process: "api", Message: "slow request", Attrs: map[string]any{"route": "GET /api/apps", "ms": 2500}},
		LogLine{At: now.Add(-time.Hour), Level: "INFO", Process: "api", Message: "all good"})
	if err := s.AddLogLines(ctx, lines); err != nil {
		t.Fatal(err)
	}

	d, err := s.LeerVigia(ctx, desde, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var test VigiaPaso
	for _, p := range d.Pasos {
		if p.Paso == "web:test" {
			test = p
		}
	}
	if test.Veces != 4 || test.Fallas != 1 || test.Ultimo != 4 || test.Segundos != 600 || test.VecesAntes != 2 || test.SegundosAntes != 300 || test.Reintentos != 2 {
		t.Fatalf("web:test = %+v", test)
	}
	if len(d.Apps) != 1 || d.Apps[0].Ejecuciones != 4 || d.Apps[0].Fallidas != 1 || d.Apps[0].EjecucionesAntes != 2 {
		t.Fatalf("apps = %+v", d.Apps)
	}
	if len(d.Lineas) != 1 || d.Lineas[0].Veces != 6 || d.Lineas[0].VecesAntes != 1 || d.Lineas[0].Mensaje != "could not reach the registry" {
		t.Fatalf("lines = %+v", d.Lineas)
	}
	if len(d.Lentas) != 1 || d.Lentas[0].Veces != 2 || d.Lentas[0].Ms != 2000 {
		t.Fatalf("slow = %+v", d.Lentas)
	}

	// One review at a time.
	id, err := s.EmpezarRevision(ctx, desde, "prueba")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EmpezarRevision(ctx, desde, ""); !errors.Is(err, ErrVigiaOcupado) {
		t.Fatalf("a second review at once = %v", err)
	}
	if err := s.TerminarRevision(ctx, VigiaRevision{ID: id, Estado: RevisionHecha, Senales: 3, CostoMicros: 900, Hallazgos: []VigiaHallazgo{
		{Clave: "paso-decide:vigilada/web:test", Titulo: "web:test decides", Evidencia: "4 runs", Accionable: true, App: "vigilada"},
		{Clave: "pagina-sin-uso:/mercado", Titulo: "Nobody opened Mercado", Evidencia: "usage fact"},
	}}); err != nil {
		t.Fatal(err)
	}
	r, err := s.UltimaRevision(ctx)
	if err != nil || r.ID != id || r.Estado != RevisionHecha || r.PedidaPor != "prueba" || len(r.Hallazgos) != 2 || r.Hallazgos[0].Estado != HallazgoNuevo {
		t.Fatalf("latest = %+v %v", r, err)
	}
	if n, _ := s.VigiaNuevos(ctx); n != 2 {
		t.Fatalf("new = %d", n)
	}
	if at, estado, err := s.UltimaTerminada(ctx); err != nil || estado != RevisionHecha || at.IsZero() {
		t.Fatalf("last finished = %v %s %v", at, estado, err)
	}

	// Decided once; the second person is told.
	h := r.Hallazgos[1]
	if _, err := s.DecidirHallazgo(ctx, h.ID, HallazgoDescartado, "prueba"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecidirHallazgo(ctx, h.ID, HallazgoRuta, "otra"); !errors.Is(err, ErrVigiaDecidido) {
		t.Fatalf("decided twice = %v", err)
	}
	if _, err := s.DecidirHallazgo(ctx, 99999, HallazgoRuta, "otra"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no such finding = %v", err)
	}
	e := r.Hallazgos[0]
	if _, err := s.DecidirHallazgo(ctx, e.ID, HallazgoEncargo, "prueba"); err != nil {
		t.Fatal(err)
	}
	enc := int64(12)
	if err := s.EnlazarHallazgo(ctx, e.ID, &enc, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Hallazgo(ctx, e.ID); got.EncargoID == nil || *got.EncargoID != 12 || got.DecididoPor != "prueba" || got.DecididoAt == nil {
		t.Fatalf("the encargo one = %+v", got)
	}
	// Undo leaves a linked finding as it is.
	if err := s.DeshacerHallazgo(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Hallazgo(ctx, e.ID); got.Estado != HallazgoEncargo {
		t.Fatalf("undone though linked: %+v", got)
	}
	dec, err := s.VigiaDecididos(ctx, now)
	if err != nil || len(dec) != 2 {
		t.Fatalf("decided = %+v %v", dec, err)
	}
	// Ninety days on, the encargo may be proposed again; the dismissed one never.
	if dec, _ = s.VigiaDecididos(ctx, now.Add(VigiaRecuerdo+time.Hour)); len(dec) != 1 || dec[0].Clave != "pagina-sin-uso:/mercado" {
		t.Fatalf("decided after 90 days = %+v", dec)
	}
	if n, _ := s.VigiaNuevos(ctx); n != 0 {
		t.Fatalf("new after deciding = %d", n)
	}

	// Its settings, and what its source spent.
	if a, _ := s.GetVigia(ctx); !a.Encendido || a.TopeMicros != VigiaTope {
		t.Fatalf("default settings = %+v", a)
	}
	if err := s.SetVigia(ctx, VigiaAjuste{Encendido: false, TopeMicros: 0}, "prueba"); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.GetVigia(ctx); a.Encendido || a.TopeMicros != 0 || a.UpdatedBy != "prueba" {
		t.Fatalf("saved settings = %+v", a)
	}
	if err := s.RecordAIUsage(ctx, AIUsage{OrgID: DefaultOrg, Source: "agent: lookout", Provider: AIPlatform, CostMicros: 700, OK: true}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.AISpentBy(ctx, DefaultOrg, "agent: lookout", time.Now()); n != 700 {
		t.Fatalf("spent = %d", n)
	}
}
