package store

import (
	"context"
	"reflect"
	"testing"
	"time"
)

const coderEnPrueba = "agente de código"

// cuentaHecho is one entry of an encargo's log in a fixture: who did what,
// with the note and the cause.
type cuentaHecho struct{ who, what, note, cause string }

// encargoDeCuentas adds an encargo with its log, its entries a minute apart
// from at; its id.
func encargoDeCuentas(t *testing.T, s *Store, org int64, status string, d int, est, spent int64, at time.Time, log ...cuentaHecho) int64 {
	t.Helper()
	ctx := context.Background()
	var dif *int
	var estimate *int64
	if d > 0 {
		dif, estimate = &d, &est
	}
	var id int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO encargos (title, created_by, org_id, status, difficulty, estimate_micros, spent_micros)
		VALUES ('un encargo', 'p0dxD', $1, $2, $3, $4, $5) RETURNING id`, org, status, dif, estimate, spent).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for i, h := range log {
		if _, err := s.pool.Exec(ctx, `INSERT INTO encargo_log (encargo_id, at, who, what, note, cause) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`,
			id, at.Add(time.Duration(i)*time.Minute), h.who, h.what, h.note, h.cause); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// How the coding agent is doing, counted from a log: what it finished (at
// the first try or after giving it back), gave back and has; what the
// person answered to its first delivery; why it gave back, recorded or read
// from old notes; spent over estimate by band; the warning of three
// releases in a row for one cause; only its own encargos, of its own
// organization, in the period.
func TestCoderCuentas(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	now := time.Now()
	ago := func(days float64) time.Time { return now.Add(-time.Duration(days * 24 * float64(time.Hour))) }
	C, yo, mio := coderEnPrueba, "p0dxD", "agente de ana"
	hecho := func(who, what string, more ...string) cuentaHecho {
		h := cuentaHecho{who: who, what: what}
		if len(more) > 0 {
			h.note = more[0]
		}
		if len(more) > 1 {
			h.cause = more[1]
		}
		return h
	}

	// Long ago: finished, accepted.
	viejo := encargoDeCuentas(t, s, DefaultOrg, EncargoReviewed, 8, 100, 300, ago(40),
		hecho(yo, "sent"), hecho(C, "taken"), hecho(C, "delivered"), hecho(yo, "reviewed"))
	// First try, accepted (1.2× its estimate).
	encargoDeCuentas(t, s, DefaultOrg, EncargoReviewed, 3, 100, 120, ago(5),
		hecho(C, "taken"), hecho(C, "waiting"), hecho(C, "resumed"), hecho(C, "delivered"), hecho(yo, "reviewed"))
	// After giving it back once (an old note: no cause), then sent back by
	// the person and finished by their own agent (2.5×).
	e2 := encargoDeCuentas(t, s, DefaultOrg, EncargoReviewed, 5, 200, 500, ago(4),
		hecho(C, "taken"), hecho(C, "released", `No pude terminarlo: Post "https://api.anthropic.com/v1/messages": connection reset by peer.`),
		hecho(yo, "sent"), hecho(C, "taken"), hecho(C, "delivered"), hecho(yo, "returned", "falta la prueba"),
		hecho(mio, "taken"), hecho(mio, "delivered"), hecho(yo, "reviewed"))
	// First try, not answered yet (0.8×).
	encargoDeCuentas(t, s, DefaultOrg, EncargoDelivered, 2, 100, 80, ago(3),
		hecho(C, "taken"), hecho(C, "delivered"))
	// Given back three times in a row for a conflict, one of them an old
	// note; then a person stopped one, which is not its own doing.
	e4 := encargoDeCuentas(t, s, DefaultOrg, EncargoPending, 4, 100, 90, ago(2),
		hecho(C, "taken"), hecho(C, "released", "No pude fusionar la rama principal.", CauseConflict))
	e5 := encargoDeCuentas(t, s, DefaultOrg, EncargoPending, 0, 0, 40, ago(1.5),
		hecho(C, "taken"), hecho(C, "released", "No pude resolver el conflicto de main.go: no tengo git."))
	e6 := encargoDeCuentas(t, s, DefaultOrg, EncargoPending, 0, 0, 40, ago(1),
		hecho(C, "taken"), hecho(C, "released", "Otra vez el conflicto.", CauseConflict), hecho(mio, "taken"))
	e7 := encargoDeCuentas(t, s, DefaultOrg, EncargoPending, 0, 0, 10, ago(0.5),
		hecho(C, "taken"), hecho(C, "released", "Una persona lo detuvo.", CauseStopped))
	// It has this one now.
	encargoDeCuentas(t, s, DefaultOrg, EncargoTaken, 0, 0, 10, ago(0.2), hecho(C, "taken"))
	// Not its own: the person's agent's; another organization's.
	encargoDeCuentas(t, s, DefaultOrg, EncargoReviewed, 3, 100, 0, ago(2), hecho(mio, "taken"), hecho(mio, "delivered"), hecho(yo, "reviewed"))
	otra, err := s.CreateOrg(ctx, "otra-cuentas", "Otra", "ana")
	if err != nil {
		t.Fatal(err)
	}
	ajeno := encargoDeCuentas(t, s, otra.ID, EncargoDelivered, 3, 100, 100, ago(1), hecho(C, "taken"), hecho(C, "delivered"))

	c, err := s.CoderCuentasOf(ctx, DefaultOrg, C, now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	counts := [9]int{c.Encargos, c.Finished, c.FirstTry, c.AfterRetry, c.GaveBack, c.Working, c.Accepted, c.Returned, c.ToReview}
	if want := [9]int{8, 3, 2, 1, 4, 1, 1, 1, 1}; counts != want {
		t.Fatalf("30 days: counts %v, want %v (%+v)", counts, want, c)
	}
	if want := []CuentaCausa{
		{Cause: CauseConflict, N: 3, Encargos: []int64{e4, e5, e6}, Approx: 1},
		{Cause: CauseStopped, N: 1, Encargos: []int64{e7}},
	}; !reflect.DeepEqual(c.Causes, want) {
		t.Fatalf("causes = %+v", c.Causes)
	}
	if want := []CuentaCausa{{Cause: CauseAIError, N: 1, Encargos: []int64{e2}, Approx: 1}}; !reflect.DeepEqual(c.Retried, want) {
		t.Fatalf("retried = %+v", c.Retried)
	}
	if want := []CuentaRazon{{From: 1, To: 3, X: 1, N: 2}, {From: 4, To: 6, X: 2.5, N: 1}}; !reflect.DeepEqual(c.Ratios, want) {
		t.Fatalf("ratios = %+v", c.Ratios)
	}
	if c.All != (CuentaRazon{X: 1.2, N: 3}) {
		t.Fatalf("all = %+v", c.All)
	}
	if c.Racha == nil || c.Racha.Cause != CauseConflict || c.Racha.N != 3 || !reflect.DeepEqual(c.Racha.Encargos, []int64{e4, e5, e6}) {
		t.Fatalf("racha = %+v", c.Racha)
	}
	if c.Since == nil {
		t.Fatal("no since")
	}

	// All of it: the old one too.
	c, err = s.CoderCuentasOf(ctx, DefaultOrg, C, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Encargos != 9 || c.Finished != 4 || c.FirstTry != 3 || c.Accepted != 2 || c.Since != nil {
		t.Fatalf("all time: %+v", c)
	}
	if len(c.Ratios) != 3 || c.Ratios[2] != (CuentaRazon{From: 7, To: 10, X: 3, N: 1}) || c.All != (CuentaRazon{X: 1.85, N: 4}) {
		t.Fatalf("all time ratios = %+v, all %+v (old one %d)", c.Ratios, c.All, viejo)
	}

	// The other organization sees its own only.
	c, err = s.CoderCuentasOf(ctx, otra.ID, C, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Encargos != 1 || c.Finished != 1 || c.ToReview != 1 || c.Racha != nil || len(c.Causes) != 0 {
		t.Fatalf("other organization (%d): %+v", ajeno, c)
	}
}

// A delivery ends a run of releases; fewer than three are no warning; a
// person's stop and a restart neither make nor end one.
func TestRacha(t *testing.T) {
	ev := func(encargo int64, what, cause string) cuentaEvento {
		return cuentaEvento{encargo: encargo, what: what, cause: cause}
	}
	rel := func(encargo int64, cause string) cuentaEvento { return ev(encargo, "released", cause) }
	del := func(encargo int64) cuentaEvento { return ev(encargo, "delivered", "") }
	for _, c := range []struct {
		name     string
		outcomes []cuentaEvento
		want     *CuentaRacha
	}{
		{"three", []cuentaEvento{del(1), rel(2, CauseBudget), rel(3, CauseBudget), rel(4, CauseBudget)}, &CuentaRacha{CauseBudget, 3, []int64{2, 3, 4}}},
		{"two", []cuentaEvento{rel(2, CauseBudget), del(1), rel(3, CauseBudget), rel(4, CauseBudget)}, nil},
		{"mixed causes", []cuentaEvento{rel(2, CauseNoTools), rel(3, CauseBudget), rel(4, CauseBudget)}, nil},
		{"stops in between", []cuentaEvento{rel(2, CauseNoTools), rel(5, CauseStopped), rel(3, CauseNoTools), rel(6, CauseRestart), rel(4, CauseNoTools), rel(7, CauseStopped)},
			&CuentaRacha{CauseNoTools, 3, []int64{2, 3, 4}}},
		{"one encargo, thrice", []cuentaEvento{rel(9, CauseNoTools), rel(9, CauseNoTools), rel(9, CauseNoTools)}, &CuentaRacha{CauseNoTools, 3, []int64{9}}},
		{"nothing", nil, nil},
	} {
		if got := rachaOf(c.outcomes); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// The notes from before the cause was recorded are read by their words,
// in Spanish and English (invented examples, like the coding agent's own).
func TestClassifyCause(t *testing.T) {
	for _, c := range []struct{ note, want string }{
		{"Una persona lo detuvo. Lo que llevaba quedó SIN PROBAR en el commit abc1234", CauseStopped},
		{"The platform restarted while it worked on it; what it got to test is on branch feature/x.", CauseRestart},
		{"Me detuve antes de pasar del doble de lo estimado: la siguiente vuelta costaría unos $0.20", CauseBudget},
		{"Llegué al doble de lo estimado. Lo que hice está en la rama encargo-3", CauseBudget},
		{"Hice el commit 1234567, pero la ejecución #77 no terminó en 50 minutos.", CauseCITimeout},
		{`Could not finish it: Post "https://api.anthropic.com/v1/messages": EOF`, CauseAIError},
		{"No pude terminarlo: el modelo se negó a seguir", CauseAIError},
		{`No pude terminarlo: github GET /repos/o/tienda/commits/x: 422 {"message":"No commit found"}`, CauseForgeError},
		{"No pude resolver el conflicto: mis herramientas no me dejan ver la rama principal.", CauseConflict},
		{"I could not merge the default branch: too many conflicts.", CauseConflict},
		{"Le dejo el diseño y las decisiones que necesito; con su visto bueno lo hago.", CauseNeedsDecision},
		{"The encargo is unclear: which page does it mean?", CauseNeedsDecision},
		{"La corrección es regenerar package-lock.json con npm install, y no puedo ejecutar comandos.", CauseNoTools},
		{"I only have tools to read and edit files; I cannot read the run's logs.", CauseNoTools},
		{"El código con el error no está en mi rama.", CauseOther},
		{"", CauseOther},
	} {
		if got := ClassifyCause(c.note); got != c.want {
			t.Errorf("%q: %s, want %s", c.note, got, c.want)
		}
	}
}

// The coding agent's give-back keeps its cause; one it does not know is
// left to the note.
func TestGiveBackEncargoCause(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	k, err := s.SystemKey(ctx, DefaultOrg, coderEnPrueba)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ cause, note, want string }{
		{CauseBudget, "Me detuve.", CauseBudget},
		{"a guess", "No pude terminarlo: el modelo se negó a seguir", CauseAIError},
	} {
		cg, err := s.TakeCargo(ctx, k.ID, "probar")
		if err != nil {
			t.Fatal(err)
		}
		e, err := s.CreateEncargo(ctx, Encargo{Title: "Arreglar la tienda", App: "tienda", CreatedBy: "p0dxD"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.TakeEncargo(ctx, e.ID, k.ID, cg.ID, coderEnPrueba); err != nil {
			t.Fatal(err)
		}
		if e, err = s.GiveBackEncargoCause(ctx, e.ID, k.ID, coderEnPrueba, c.note, c.cause); err != nil || e.Status != EncargoPending || e.Route != RouteLocal {
			t.Fatalf("given back %+v (%v)", e, err)
		}
		if err := s.HandOver(ctx, cg.ID, "listo", ""); err != nil {
			t.Fatal(err)
		}
		var stored *string
		if err := s.pool.QueryRow(ctx, `SELECT cause FROM encargo_log WHERE encargo_id = $1 AND what = 'released'`, e.ID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if (c.cause == CauseBudget) != (stored != nil) {
			t.Fatalf("%s: stored %v", c.cause, stored)
		}
		got, err := s.CoderCuentasOf(ctx, DefaultOrg, coderEnPrueba, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, kc := range got.Causes {
			for _, id := range kc.Encargos {
				if id == e.ID {
					found = kc.Cause == c.want && (kc.Approx > 0) == (stored == nil)
				}
			}
		}
		if !found {
			t.Fatalf("%s: causes %+v", c.cause, got.Causes)
		}
	}
}
