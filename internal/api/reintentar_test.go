package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

func TestRetryWhatFailed(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	app := &store.App{Name: "reintento", Repo: "p0dxD/reintento", DefaultBranch: "main", Spec: sp}
	if err := e.store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetMember(ctx, store.DefaultOrg, "colabora", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	admin, colabora := e.session(t, "p0dxD"), e.session(t, "colabora")
	failedRun := func(branch string, deploy bool) *store.Run {
		t.Helper()
		r := &store.Run{AppID: app.ID, SHA: "aaaabbbbcccc", Branch: branch, Event: "push", Deploy: deploy}
		if err := e.store.CreateRun(ctx, r, nil); err != nil {
			t.Fatal(err)
		}
		if err := e.store.FinishRun(ctx, r.ID, store.RunFailed, "web:test failed"); err != nil {
			t.Fatal(err)
		}
		return r
	}
	retry := func(r *store.Run, who string) (int, string) {
		c := admin
		if who == "colabora" {
			c = colabora
		}
		res, body := e.do(t, "POST", fmt.Sprintf("/api/runs/%d/retry", r.ID), "", c, nil)
		return res.StatusCode, body
	}

	// A branch: a collaborator retries it, twice at most.
	rama := failedRun("arreglo", false)
	if code, body := retry(rama, "colabora"); code != 200 {
		t.Fatalf("branch retry: %d %s", code, body)
	}
	var got store.Run
	_, body := e.do(t, "GET", fmt.Sprintf("/api/runs/%d", rama.ID), "", admin, nil)
	if json.Unmarshal([]byte(body), &got) != nil || got.Status != store.RunQueued || got.Retries != 1 {
		t.Fatalf("after a retry: %s", body)
	}
	if code, _ := retry(rama, "colabora"); code != 409 {
		t.Fatalf("a queued run retried: %d", code)
	}
	for i := 2; i <= 3; i++ {
		_ = e.store.FinishRun(ctx, rama.ID, store.RunFailed, "web:test failed")
		code, body := retry(rama, "colabora")
		if i == 2 && code != 200 || i == 3 && code != 409 {
			t.Fatalf("retry %d: %d %s", i, code, body)
		}
	}

	// The main branch deploys: administrators only, and only its last run.
	viejo := failedRun("main", true)
	nuevo := failedRun("main", true)
	if code, _ := retry(nuevo, "colabora"); code != 403 {
		t.Fatalf("a collaborator retried the main branch: %d", code)
	}
	if code, body := retry(viejo, "admin"); code != 409 {
		t.Fatalf("an older main run retried: %d %s", code, body)
	}
	if code, body := retry(nuevo, "admin"); code != 200 {
		t.Fatalf("the last main run: %d %s", code, body)
	}
}
