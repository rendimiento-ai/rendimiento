package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The coding agent asks about an encargo (Ruta 178): it is asking, with
// its question on the encargo, until a person answers (back to the agent's
// queue, the answer kept once for it to resume with) or passes it to their
// own agent; one nobody answered in AskWait is found to pass. Another
// organization cannot answer it, and an encargo that asks nothing cannot
// be answered.
func TestEncargoQuestions(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	k, err := s.CreateAgentKey(ctx, DefaultOrg, "agente de código", "h-pregunta", RoleCollaborator, nil, "rendimiento", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	take := func(title string) Encargo {
		t.Helper()
		e, err := s.CreateEncargo(ctx, Encargo{Title: title, App: "pregunta-app", CreatedBy: "p0dxD"})
		if err != nil {
			t.Fatal(err)
		}
		c, err := s.TakeCargo(ctx, k.ID, title)
		if errors.Is(err, ErrCargoOpen) {
			c, err = s.OpenCargo(ctx, k.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
		if e, err = s.TakeEncargo(ctx, e.ID, k.ID, c.ID, k.Name); err != nil {
			t.Fatal(err)
		}
		return e
	}
	e := take("Comentarios al agente")
	if _, err := s.AnswerEncargo(ctx, e.ID, DefaultOrg, "p0dxD", "la A"); !errors.Is(err, ErrEncargoState) {
		t.Fatalf("answered an encargo that asks nothing: %v", err)
	}
	q := EncargoPregunta{Question: "¿Botón o plática?", Options: []string{"Un botón", "Una plática"}, Notes: "dos formas en la rama", SpentMicros: 90_000}
	if e, err = s.AskEncargo(ctx, e.ID, k.ID, k.Name, q); err != nil || e.Status != EncargoAsking || e.Question == nil ||
		e.Question.Question != q.Question || len(e.Question.Options) != 2 || e.Question.AskedAt.IsZero() {
		t.Fatalf("asked %+v %+v (%v)", e, e.Question, err)
	}
	if list, err := s.ListEncargos(ctx, DefaultOrg, true, 50); err != nil || len(list) != 1 || list[0].Question == nil {
		t.Fatalf("open encargos %+v (%v)", list, err)
	}
	if _, err := s.ResumeAnswer(ctx, e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an answer before anyone answered: %v", err)
	}
	if _, err := s.AnswerEncargo(ctx, e.ID, 999, "intrusa", "la B"); err == nil {
		t.Fatal("another organization answered it")
	}
	if e, err = s.AnswerEncargo(ctx, e.ID, DefaultOrg, "p0dxD", "La A, pero que también se vean en el encargo"); err != nil ||
		e.Status != EncargoPending || !e.DoNow || e.Route != RoutePlatform || e.KeyID != nil || e.Question != nil {
		t.Fatalf("answered %+v (%v)", e, err)
	}
	p, err := s.ResumeAnswer(ctx, e.ID)
	if err != nil || p.Answer != "La A, pero que también se vean en el encargo" || p.AnsweredBy != "p0dxD" || p.SpentMicros != 90_000 || p.Notes != q.Notes {
		t.Fatalf("resumed with %+v (%v)", p, err)
	}
	if _, err := s.ResumeAnswer(ctx, e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the answer was given twice: %v", err)
	}

	// Passed by the person: pending for their own agent, out of the queue.
	e2 := take("Otra pregunta")
	if _, err := s.AskEncargo(ctx, e2.ID, k.ID, k.Name, EncargoPregunta{Question: "¿Cuál?"}); err != nil {
		t.Fatal(err)
	}
	if e2, err = s.PassAskedEncargo(ctx, e2.ID, DefaultOrg, "p0dxD", "The coding agent asked: ¿Cuál?"); err != nil ||
		e2.Status != EncargoPending || e2.Route != RouteLocal || e2.DoNow || e2.KeyID != nil {
		t.Fatalf("passed %+v (%v)", e2, err)
	}
	if _, err := s.AnswerEncargo(ctx, e2.ID, DefaultOrg, "p0dxD", "tarde"); !errors.Is(err, ErrEncargoState) {
		t.Fatalf("answered one passed already: %v", err)
	}

	// Nobody answered: found once AskWait went by, not before.
	e3 := take("Sin respuesta")
	if _, err := s.AskEncargo(ctx, e3.ID, k.ID, k.Name, EncargoPregunta{Question: "¿Sigo?"}); err != nil {
		t.Fatal(err)
	}
	if list, err := s.UnansweredSince(ctx, time.Now().Add(-AskWait)); err != nil || len(list) != 0 {
		t.Fatalf("unanswered before 3 days: %+v (%v)", list, err)
	}
	list, err := s.UnansweredSince(ctx, time.Now().Add(time.Minute))
	if err != nil || len(list) != 1 || list[0].EncargoID != e3.ID || list[0].OrgID != DefaultOrg {
		t.Fatalf("unanswered %+v (%v)", list, err)
	}
	if open, err := s.OpenQuestion(ctx, e3.ID); err != nil || open.Question != "¿Sigo?" {
		t.Fatalf("open question %+v (%v)", open, err)
	}
	full, err := s.Encargo(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	var steps []string
	for _, l := range full.Log {
		steps = append(steps, l.What)
	}
	if got := joinWords(steps); got != "created taken asked answered" {
		t.Fatalf("log = %s", got)
	}
	acts, _, _, err := s.ActivitySince(ctx, DefaultOrg, time.Now().Add(-time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, a := range acts {
		seen[a.Event] = true
	}
	if !seen["asked"] || !seen["answered"] {
		t.Fatalf("activity %+v", acts)
	}
}

// The coding agent's views of one app: its slow requests in the Registro
// (the platform's own lines only for the platform's app), and the lookout's
// findings about it, never another app's.
func TestWhatTheCoderSees(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	now := time.Now()
	slow := func(app, route string, ms int) LogLine {
		return LogLine{At: now, Level: "INFO", Process: "api", Message: "slow request", App: app,
			Attrs: map[string]any{"component": "http", "route": route, "ms": ms, "status": 200}}
	}
	if err := s.AddLogLines(ctx, []LogLine{slow("tienda", "GET /carrito", 1500), slow("otra", "GET /ajeno", 9000), slow("", "GET /api/encargos", 2100),
		{At: now, Level: "INFO", Process: "api", Message: "something else", App: "tienda"}}); err != nil {
		t.Fatal(err)
	}
	list, err := s.SlowRequests(ctx, "tienda", false, now.Add(-time.Hour), 50)
	if err != nil || len(list) != 1 || list[0].Route != "GET /carrito" || list[0].Ms != 1500 || list[0].Status != 200 {
		t.Fatalf("tienda's slow requests %+v (%v)", list, err)
	}
	if list, err = s.SlowRequests(ctx, "rendimiento-ai", true, now.Add(-time.Hour), 50); err != nil || len(list) != 1 || list[0].Route != "GET /api/encargos" {
		t.Fatalf("the platform's slow requests %+v (%v)", list, err)
	}
	id, err := s.EmpezarRevision(ctx, now.Add(-7*24*time.Hour), "prueba")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TerminarRevision(ctx, VigiaRevision{ID: id, Estado: RevisionHecha, Hallazgos: []VigiaHallazgo{
		{Clave: "a", Titulo: "de tienda", App: "tienda"}, {Clave: "b", Titulo: "de otra", App: "otra"}}}); err != nil {
		t.Fatal(err)
	}
	hs, err := s.HallazgosDeApp(ctx, "tienda", 10)
	if err != nil || len(hs) != 1 || hs[0].Titulo != "de tienda" {
		t.Fatalf("findings %+v (%v)", hs, err)
	}
}
