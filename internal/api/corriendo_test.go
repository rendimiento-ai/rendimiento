package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Running now: the runs going on in the person's organization, with their
// steps, and the last that ended; never another organization's.
func TestCorriendo(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	me := e.session(t, "p0dxD")
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	app := &store.App{Name: "corre-prueba", Repo: "p0dxD/corre-prueba", DefaultBranch: "main", Spec: sp}
	if err := e.store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	pasos := []pipeline.Step{{ID: "web:build", Service: "web", Kind: pipeline.KindBuild}, {ID: "deploy", Kind: pipeline.KindTask, DependsOn: []string{"web:build"}}}
	corre := &store.Run{AppID: app.ID, SHA: "abc", Branch: "rama-que-corre", Event: "push"}
	if err := e.store.CreateRun(ctx, corre, pasos); err != nil {
		t.Fatal(err)
	}
	hecha := &store.Run{AppID: app.ID, SHA: "def", Branch: "rama-hecha", Event: "push"}
	if err := e.store.CreateRun(ctx, hecha, pasos); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishRun(ctx, hecha.ID, store.RunSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	otra, err := e.store.CreateOrg(ctx, "otra-corre", "Otra", "vecina")
	if err != nil {
		t.Fatal(err)
	}
	ajena := &store.App{Name: "corre-ajena", Repo: "vecina/corre-ajena", DefaultBranch: "main", Spec: sp, OrgID: otra.ID}
	if err := e.store.CreateApp(ctx, ajena); err != nil {
		t.Fatal(err)
	}
	if err := e.store.CreateRun(ctx, &store.Run{AppID: ajena.ID, SHA: "a1e", Branch: "rama-ajena", Event: "push"}, nil); err != nil {
		t.Fatal(err)
	}

	r, body := e.do(t, "GET", "/api/corriendo", "", me, nil)
	var got Corriendo
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &got) != nil {
		t.Fatalf("corriendo: %d %s", r.StatusCode, body)
	}
	if len(got.Ahora) != 1 || got.Ahora[0].App != "corre-prueba" || got.Ahora[0].ID != corre.ID || got.Ahora[0].Branch != "rama-que-corre" || len(got.Ahora[0].Steps) != 2 {
		t.Fatalf("going on = %s", body)
	}
	if len(got.Recientes) != 1 || got.Recientes[0].ID != hecha.ID || got.Recientes[0].Status != store.RunSucceeded || len(got.Recientes[0].Steps) != 0 {
		t.Fatalf("ended = %s", body)
	}
	if strings.Contains(body, "rama-ajena") {
		t.Fatalf("another organization's run shows: %s", body)
	}
	if r, _ := e.do(t, "GET", "/api/corriendo", "", nil, nil); r.StatusCode != 401 {
		t.Fatalf("signed out: %d", r.StatusCode)
	}
}
