package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/mesa"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

func TestMesa(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	me := e.session(t, "p0dxD")
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	app := &store.App{Name: "mesa-prueba", Repo: "p0dxD/mesa-prueba", DefaultBranch: "main", Spec: sp}
	if err := e.store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	status := map[string]string{"main": store.RunSucceeded, "lista": store.RunSucceeded, "rota": store.RunFailed, "en-curso": ""}
	for _, b := range []string{"main", "lista", "rota", "en-curso"} {
		run := &store.Run{AppID: app.ID, SHA: "abc", Branch: b, Event: "push"}
		if err := e.store.CreateRun(ctx, run, nil); err != nil {
			t.Fatal(err)
		}
		if status[b] != "" {
			if err := e.store.FinishRun(ctx, run.ID, status[b], ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	r, body := e.do(t, "GET", "/api/apps/mesa-prueba/mesa", "", me, nil)
	var got mesa.Board
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &got) != nil {
		t.Fatalf("mesa: %d %s", r.StatusCode, body)
	}
	want := map[string]string{"lista": mesa.WhyPassed, "rota": mesa.WhyFailed, "en-curso": mesa.WhyRunning}
	if got.GitHub || len(got.Lines) != len(want) {
		t.Fatalf("mesa = %s", body)
	}
	for _, l := range got.Lines {
		if want[l.Branch] != l.Why {
			t.Errorf("%s is %q, want %q", l.Branch, l.Why, want[l.Branch])
		}
	}

	r, body = e.do(t, "GET", "/api/mesa", "", me, nil)
	var counts []MesaCount
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &counts) != nil {
		t.Fatalf("mesa counts: %d %s", r.StatusCode, body)
	}
	for _, c := range counts {
		if c.App == "mesa-prueba" && c.You != 2 {
			t.Fatalf("mesa-prueba waits on %d, want 2 (%s)", c.You, body)
		}
	}
}
