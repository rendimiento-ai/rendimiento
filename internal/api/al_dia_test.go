package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// modeloAlDia answers every report with the same text.
type modeloAlDia struct{ text string }

func (m *modeloAlDia) Complete(context.Context, int64, string, ia.Request) (ia.Response, error) {
	return ia.Response{Text: m.text, Provider: store.AILocal, Model: "qwen-prueba"}, nil
}
func (m *modeloAlDia) CompleteVia(ctx context.Context, org int64, source, _ string, req ia.Request) (ia.Response, error) {
	return m.Complete(ctx, org, source, req)
}

// Poner al día, the whole way: a dependency update's run is reported on
// once (a second ask for the same run does nothing); the app's Caminos tab
// lists the report; a collaborator puts the app up to date, which opens the
// encargo on the update's run for the platform's coding agent, once; with
// the person's agent chosen it goes to theirs; a low-risk report under the
// amount starts by itself when that is on; a guest may not put it up to
// date, and only an administrator changes the choices.
func TestAlDia(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	for login, role := range map[string]string{"abi-al-dia": store.MemberCollaborator, "invitada-al-dia": store.MemberGuest} {
		if err := e.store.SetMember(ctx, store.DefaultOrg, login, role, "p0dxD"); err != nil {
			t.Fatal(err)
		}
	}
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	app := &store.App{Name: "al-dia-app", Repo: "p0dxD/al-dia-app", DefaultBranch: "main", Spec: sp}
	if err := e.store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	newRun := func(branch string) *store.Run {
		r := &store.Run{AppID: app.ID, SHA: strings.Repeat("c", 40), Branch: branch, Event: "push"}
		if err := e.store.CreateRun(ctx, r, nil); err != nil {
			t.Fatal(err)
		}
		return r
	}
	ask := func(run *store.Run, pull int, failed bool) bool {
		asked, err := e.store.PedirAlDia(ctx, store.AlDia{AppID: app.ID, Pull: pull, Titulo: "Update react to v19", Notas: "## Breaking\n- ReactDOM.render removed",
			Branch: run.Branch, SHA: run.SHA, RunID: run.ID, RunFailed: failed})
		if err != nil {
			t.Fatal(err)
		}
		return asked
	}
	modelo := &modeloAlDia{text: `{"sin_cambios": false, "falla": ["ReactDOM.render ya no existe"], "cambia": [{"archivo": "web/src/main.tsx", "mas": 4, "menos": 3}], "riesgo": "medio", "dificultad": 3, "resumen": "Hay que usar createRoot."}`}
	agente := &agentes.AlDia{Store: e.store, Model: modelo, Est: &agentes.Estimator{Store: e.store, Model: &ia.Gateway{Store: e.store}}, Lang: "es",
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	escribir := func() {
		pend, err := e.store.AlDiaPendientes(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range pend {
			if err := agente.Informar(ctx, r); err != nil {
				t.Fatal(err)
			}
		}
	}

	run := newRun("renovate/react-19.x")
	if !ask(run, 57, true) || ask(run, 57, true) {
		t.Fatal("a run's report was asked for other than once")
	}
	escribir()
	me, abi, invitada := e.session(t, "p0dxD"), e.session(t, "abi-al-dia"), e.session(t, "invitada-al-dia")

	var v AlDiaView
	_, body := e.do(t, "GET", "/api/apps/al-dia-app/al-dia", "", invitada, nil)
	if err := json.Unmarshal([]byte(body), &v); err != nil || len(v.Informes) != 1 {
		t.Fatalf("the Caminos tab: %s", body)
	}
	rep := v.Informes[0]
	if rep.Estado != store.AlDiaListo || rep.Pull != 57 || len(rep.Cambia) != 1 || rep.Riesgo != store.RiesgoMedio || rep.CostoMicros <= 0 || v.Ajustes.Quien != store.AlDiaQuienPlataforma {
		t.Fatalf("the report: %+v %+v", rep, v.Ajustes)
	}

	poner := fmt.Sprintf("/api/apps/al-dia-app/al-dia/%d/poner", rep.ID)
	if r, body := e.do(t, "POST", poner, "", invitada, nil); r.StatusCode != 403 {
		t.Fatalf("a guest put it up to date: %d %s", r.StatusCode, body)
	}
	r, body := e.do(t, "POST", poner, "", abi, nil)
	var got struct{ Encargo int64 }
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &got) != nil || got.Encargo == 0 {
		t.Fatalf("putting it up to date: %d %s", r.StatusCode, body)
	}
	enc, err := e.store.Encargo(ctx, got.Encargo)
	if err != nil || enc.RunID == nil || *enc.RunID != run.ID || !enc.DoNow || enc.Route != store.RoutePlatform || !strings.Contains(enc.Body, "renovate/react-19.x") {
		t.Fatalf("its encargo: %+v %v", enc, err)
	}
	if r, _ := e.do(t, "POST", poner, "", abi, nil); r.StatusCode != 409 {
		t.Fatalf("put up to date twice: %d", r.StatusCode)
	}
	// Its encargo's own runs on the branch get no report.
	if ask(newRun("renovate/react-19.x"), 57, false) {
		t.Fatal("a run of the encargo's work was reported on")
	}

	// The choices: an administrator's; the person's agent; without asking.
	if r, _ := e.do(t, "PUT", "/api/ia/al-dia", `{"encendido": true, "quien": "local", "cuando": "toda", "sola": true, "topeMicros": 5000000}`, abi, nil); r.StatusCode != 403 {
		t.Fatalf("a collaborator changed the choices: %d", r.StatusCode)
	}
	for _, bad := range []string{`{"quien": "nadie", "cuando": "toda"}`, `{"quien": "local", "cuando": "nunca"}`, `{"quien": "local", "cuando": "toda", "topeMicros": 6000000}`} {
		if r, body := e.do(t, "PUT", "/api/ia/al-dia", bad, me, nil); r.StatusCode != 400 {
			t.Fatalf("%s: %d %s", bad, r.StatusCode, body)
		}
	}
	if r, body := e.do(t, "PUT", "/api/ia/al-dia", `{"encendido": true, "quien": "local", "cuando": "toda", "sola": true, "topeMicros": 5000000}`, me, nil); r.StatusCode != 200 || !strings.Contains(body, `"quien":"local"`) {
		t.Fatalf("the choices: %d %s", r.StatusCode, body)
	}
	modelo.text = `{"sin_cambios": false, "falla": [], "cambia": [{"archivo": "go.mod", "mas": 1, "menos": 1}], "riesgo": "bajo", "dificultad": 1, "resumen": "Un cambio de versión."}`
	otra := newRun("renovate/chi-5.x")
	ask(otra, 58, false)
	escribir()
	enc, err = e.store.OpenEncargoOnBranch(ctx, app, "renovate/chi-5.x")
	if err != nil || enc.Route != store.RouteLocal || enc.DoNow || enc.CreatedBy != agentes.AlDiaName {
		t.Fatalf("a low-risk report did not start by itself for the person's agent: %+v %v", enc, err)
	}

	// An update the run passed and the notes ask nothing of: no changes.
	modelo.text = `{"sin_cambios": true, "falla": [], "cambia": [], "riesgo": "bajo", "dificultad": 0, "resumen": "Solo arreglos."}`
	ask(newRun("renovate/zod-3.x"), 59, false)
	escribir()
	_, body = e.do(t, "GET", "/api/apps/al-dia-app/al-dia", "", me, nil)
	if err := json.Unmarshal([]byte(body), &v); err != nil || len(v.Informes) != 3 || v.Informes[0].Estado != store.AlDiaSinCambios {
		t.Fatalf("an update that needs nothing: %s", body)
	}
}
