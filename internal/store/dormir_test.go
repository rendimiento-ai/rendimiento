package store

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

// The sleep's row starts awake and keeps what is saved; the webhooks kept
// while asleep come back in the order they came; a run cut by sleep goes
// back to the queue from its first step, not failed; and the lookout
// leaves out the log lines of the hours it slept.
func TestDormirStore(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	z, err := s.GetSueno(ctx)
	if err != nil || z.Estado != Despierto || z.DespertarA != "" || z.Tapa {
		t.Fatalf("a new install: %+v %v", z, err)
	}
	desde := time.Date(2026, 11, 20, 12, 43, 0, 0, time.UTC)
	z = Sueno{Estado: Dormido, Desde: &desde, Por: "p0dxD", Contenedores: []string{"tienda-web-3", "tienda-postgres"}, Cortadas: []int64{7, 9},
		Esperada: "tienda", DespertarA: "08:00", Resumen: &Despertar{Apps: []string{"tienda"}, Avisos: 2}}
	if err := s.GuardarSueno(ctx, z); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSueno(ctx)
	if err != nil || got.Estado != Dormido || !got.Desde.Equal(desde) || !slices.Equal(got.Contenedores, z.Contenedores) ||
		!slices.Equal(got.Cortadas, z.Cortadas) || got.DespertarA != "08:00" || got.Resumen == nil || got.Resumen.Avisos != 2 {
		t.Fatalf("kept %+v, read %+v (%v)", z, got, err)
	}

	// Webhooks, in order.
	for _, repo := range []string{"o/uno", "o/dos", "o/tres"} {
		if err := s.GuardarAviso(ctx, "github", map[string]any{"Kind": "push", "Push": map[string]any{"Repo": repo}}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.ContarAvisos(ctx); err != nil || n != 3 {
		t.Fatalf("waiting: %d %v", n, err)
	}
	avisos, err := s.AvisosGuardados(ctx)
	if err != nil || len(avisos) != 3 {
		t.Fatalf("kept: %+v %v", avisos, err)
	}
	for i, repo := range []string{"o/uno", "o/dos", "o/tres"} {
		if !strings.Contains(string(avisos[i].Evento), repo) {
			t.Fatalf("webhook %d is %s, want %s", i, avisos[i].Evento, repo)
		}
		if err := s.BorrarAviso(ctx, avisos[i].ID); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := s.ContarAvisos(ctx); n != 0 {
		t.Fatalf("%d still waiting after they were handled", n)
	}

	// A run cut by sleep is queued again, its steps pending.
	var app, run int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO apps (name, repo, installation_id, spec) VALUES ('dormilona', 'x/dormilona', 1, '{}') RETURNING id`).Scan(&app); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `INSERT INTO runs (app_id, sha, branch, event, deploy, status, started_at) VALUES ($1, 'abc', 'rama', 'branch', false, 'running', now()) RETURNING id`, app).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO steps (run_id, step_id, service, kind, status, started_at, finished_at) VALUES
		($1, 'web:build', 'web', 'build', 'succeeded', now(), now()), ($1, 'web:test', 'web', 'test', 'cancelled', now(), now())`, run); err != nil {
		t.Fatal(err)
	}
	if err := s.RepetirAlDespertar(ctx, run, "rendimiento slept; it runs again on waking"); err != nil {
		t.Fatal(err)
	}
	r, err := s.GetRun(ctx, run)
	if err != nil || r.Status != RunQueued || r.StartedAt != nil || r.FinishedAt != nil {
		t.Fatalf("the cut run: %+v %v", r, err)
	}
	for _, st := range r.Steps {
		if st.Status != "pending" {
			t.Fatalf("step %s is %s after the cut, want pending", st.ID, st.Status)
		}
	}

	// The hours it slept: their warnings are not the lookout's.
	now := time.Now().Truncate(time.Second)
	durmio, desperto := now.Add(-5*time.Hour), now.Add(-2*time.Hour)
	if err := s.EmpezarPeriodo(ctx, durmio); err != nil {
		t.Fatal(err)
	}
	if err := s.EmpezarPeriodo(ctx, durmio.Add(time.Minute)); err != nil { // one open at a time
		t.Fatal(err)
	}
	if err := s.TerminarPeriodo(ctx, desperto); err != nil {
		t.Fatal(err)
	}
	if err := s.AddLogLines(ctx, []LogLine{
		{At: now.Add(-4 * time.Hour), Level: "WARN", Process: "control", Message: "an app does not answer while asleep"},
		{At: now.Add(-time.Hour), Level: "WARN", Process: "control", Message: "an app does not answer awake"},
	}); err != nil {
		t.Fatal(err)
	}
	d, err := s.LeerVigia(ctx, now.Add(-24*time.Hour), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Dormido) != 1 || !d.Dormido[0].Desde.Equal(durmio) || d.Dormido[0].Hasta == nil || !d.Dormido[0].Hasta.Equal(desperto) {
		t.Fatalf("the hours it slept: %+v", d.Dormido)
	}
	var mensajes []string
	for _, l := range d.Lineas {
		mensajes = append(mensajes, l.Mensaje)
	}
	if slices.Contains(mensajes, "an app does not answer while asleep") || !slices.Contains(mensajes, "an app does not answer awake") {
		t.Fatalf("the lookout's lines: %v", mensajes)
	}
}
