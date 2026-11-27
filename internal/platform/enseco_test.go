package platform

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// enSecoYAML is an app whose smoke test and own tests may run en seco.
const enSecoYAML = "services:\n  - name: web\n    path: web\n    domain: ensecos.example.com\n" +
	"builds:\n  - name: e2e\n    path: e2e\n" +
	"tasks:\n  - name: humo\n    stage: post-deploy\n    image: curlimages/curl:8.17.0\n    command: curl -fsS http://web/\n    dryRun: true\n" +
	"tests:\n  build: e2e\n  dryRun: true\n"

// A branch that changes only its checks runs them en seco against the
// published release: a pass is merged as usual; a failure only warns (the
// run passed, its check asks a person, rendimiento does not merge it by
// itself), and a person's yes merges it and notes it in the Ruta. A branch
// that changes the app too, or an app with nothing published, runs nothing
// and says why.
func TestEnSeco(t *testing.T) {
	p, fake, _, _ := setup(t)
	ctx := context.Background()
	fake.files["main"] = fstest.MapFS{"rendimiento.yaml": {Data: []byte(enSecoYAML)}}
	sp, err := spec.Parse([]byte(enSecoYAML))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ensecos", "ensecos-nueva"} {
		if _, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/" + name, DefaultBranch: "main", Name: name, Spec: *sp}, false); err != nil {
			t.Fatalf("onboarding %s: %v", name, err)
		}
	}
	app, err := p.Store.GetApp(ctx, "ensecos")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Store.SetAppAutoMerge(ctx, app.ID, true, true); err != nil {
		t.Fatal(err)
	}
	published := &store.Release{AppID: app.ID, SHA: strings.Repeat("0", 40), Spec: *sp,
		Images: map[string]string{"web": "registry.example.lan:5000/ensecos-web@sha256:live", "build:e2e": "registry.example.lan:5000/ensecos-e2e@sha256:live"}}
	if err := p.Store.CreateRelease(ctx, published); err != nil {
		t.Fatal(err)
	}
	_ = p.Store.SetVerification(ctx, published.ID, store.VerifyPassed, "ok")

	var mu sync.Mutex
	var ran []spec.Task
	p.Verify.RunTask = func(_ context.Context, a *store.App, rel *store.Release, task spec.Task) store.ReleaseTask {
		mu.Lock()
		ran = append(ran, task)
		mu.Unlock()
		if task.Stage != spec.StageDryRun || rel.Number != published.Number || a.Name != "ensecos" {
			t.Errorf("%s ran as %s against release #%d of %s", task.Name, task.Stage, rel.Number, a.Name)
		}
		end := time.Now()
		switch {
		case task.Name == spec.TestsTaskName:
			return store.ReleaseTask{Status: store.TaskSucceeded, FinishedAt: &end,
				Log: "@@prueba\texpected\tcompra.spec.ts › paga\n@@prueba\tflaky\tcompra.spec.ts › entrega\n"}
		case strings.Contains(task.Command, "clima"):
			return store.ReleaseTask{Status: store.TaskFailed, Message: "exit 22 · falta el clima", Log: "curl: (22) 404\n", FinishedAt: &end}
		}
		return store.ReleaseTask{Status: store.TaskSucceeded, Log: "ok\n", FinishedAt: &end}
	}

	yes := true
	push := func(repo string, number int, branch, sha, yaml string, changed []string) *store.Run {
		t.Helper()
		fake.mu.Lock()
		fake.files[sha] = fstest.MapFS{"rendimiento.yaml": {Data: []byte(yaml)}}
		if fake.changed == nil {
			fake.changed = map[string][]string{}
		}
		fake.changed["main..."+sha] = changed
		pr := forja.Change{Number: number, State: "open", Mergeable: &yes, MergeableState: "clean"}
		pr.Head.Ref, pr.Head.SHA, pr.Base.Ref = branch, sha, "main"
		fake.pulls = append(fake.pulls, pr)
		fake.mu.Unlock()
		runs, err := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: repo, Branch: branch, SHA: sha})
		if err != nil || len(runs) != 1 {
			t.Fatalf("push to %s: %v %v", branch, runs, err)
		}
		r := waitRun(t, p, runs[0].ID)
		if r.Status != store.RunSucceeded {
			t.Fatalf("run of %s: %s %q", branch, r.Status, r.Message)
		}
		return r
	}
	check := func(r *store.Run) forja.Check {
		t.Helper()
		if r.CheckRun == nil {
			t.Fatalf("run %d has no check", r.ID)
		}
		// The check is closed a moment after the run ends: under load, more
		// than the push takes to return. Wait for it, a while.
		var c forja.Check
		for deadline := time.Now().Add(testWait); ; time.Sleep(50 * time.Millisecond) {
			fake.mu.Lock()
			c = fake.checks[*r.CheckRun]
			fake.mu.Unlock()
			if c.Status == "completed" || time.Now().After(deadline) {
				return c
			}
		}
	}
	waitMerged := func(r *store.Run) *store.Run {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); r.Merge != store.MergeMerged && time.Now().Before(deadline); {
			time.Sleep(50 * time.Millisecond)
			r, _ = p.Store.GetRun(ctx, r.ID)
		}
		return r
	}

	// A new smoke test that passes en seco, with the tests beside it in the
	// branch's own image of e2e: green, and merged.
	nuevo := push("p0dxD/ensecos", 30, "humo-nuevo", strings.Repeat("a", 40),
		strings.Replace(enSecoYAML, "curl -fsS http://web/\n", "curl -fsS http://web/ && curl -fsS http://web/hora\n", 1), []string{"rendimiento.yaml"})
	if d := nuevo.Dry; d == nil || d.Release != published.Number || d.Failed || len(d.Tasks) != 2 {
		t.Fatalf("en seco: %+v", d)
	}
	for _, task := range nuevo.Dry.Tasks {
		if task.Status != store.TaskSucceeded {
			t.Errorf("%s en seco: %s %q", task.Name, task.Status, task.Message)
		}
		if task.Name == spec.TestsTaskName && task.Message != "2 of 2 tests passed" {
			t.Errorf("the tests' message: %q", task.Message)
		}
	}
	mu.Lock()
	for _, task := range ran {
		if task.Name == spec.TestsTaskName && (task.Image == published.Images["build:e2e"] || !strings.Contains(task.Image, "@")) {
			t.Errorf("the tests ran in %q, not the branch's image of e2e", task.Image)
		}
	}
	mu.Unlock()
	if c := check(nuevo); c.Conclusion != "success" {
		t.Fatalf("the check of a dry run that passed: %+v", c)
	}
	if r := waitMerged(nuevo); r.Merge != store.MergeMerged {
		t.Fatalf("a branch whose checks passed en seco: merge %q", r.Merge)
	}
	if log, err := p.Store.RunDryTaskLog(ctx, nuevo.ID, "humo"); err != nil || log != "ok\n" {
		t.Fatalf("the smoke test's log en seco: %q %v", log, err)
	}

	// A smoke test that fails en seco: the run passed, with a warning.
	clima := push("p0dxD/ensecos", 31, "humo-con-clima", strings.Repeat("b", 40),
		strings.Replace(enSecoYAML, "curl -fsS http://web/\n", "curl -fsS http://web/clima\n", 1), []string{"rendimiento.yaml"})
	if !clima.DryFailed() {
		t.Fatalf("a failing check en seco: %+v", clima.Dry)
	}
	if c := check(clima); c.Conclusion != "action_required" || c.Output == nil || !strings.Contains(c.Output.Title, "`humo` failed en seco against release #1: a person must merge this") {
		t.Fatalf("the check of a failing dry run: %+v %+v", c, c.Output)
	}
	p.MergeReady(ctx, app)
	if r, _ := p.Store.GetRun(ctx, clima.ID); r.Merge == store.MergeMerged {
		t.Fatal("rendimiento merged by itself a branch whose check failed en seco")
	}
	if rel, _ := p.Store.LatestRelease(ctx, app.ID); rel.Number != published.Number || rel.VerifyStatus != store.VerifyPassed {
		t.Fatalf("a dry run touched the release: #%d %s", rel.Number, rel.VerifyStatus)
	}
	// Merge anyway: merged, the check green, and a note in the Ruta.
	if _, err := p.ApproveMerge(ctx, clima.ID, "ana"); err != nil {
		t.Fatal(err)
	}
	if r := waitMerged(clima); r.Merge != store.MergeMerged {
		t.Fatalf("merged anyway: merge %q (%s)", r.Merge, r.Message)
	}
	if c := check(clima); c.Conclusion != "success" || !strings.Contains(c.Output.Title, "ana approved merging it, though `humo` failed en seco") {
		t.Fatalf("the check after the yes: %+v %+v", c, c.Output)
	}
	notes, err := p.Store.SearchRuta(ctx, store.DefaultOrg, store.KindNota, "humo-con-clima", 10)
	if err != nil || len(notes) != 1 || notes[0].Author != "ana" || notes[0].App != "ensecos" || !strings.Contains(notes[0].Body, "versión publicada #1") {
		t.Fatalf("the note in the Ruta: %+v %v", notes, err)
	}

	// A branch that changes the app as well: nothing runs against
	// production, and without previews there is no copy to run them on
	// (seco_copia_test.go has an app with them).
	mu.Lock()
	before := len(ran)
	mu.Unlock()
	app2 := push("p0dxD/ensecos", 32, "renovate/react-19.x", strings.Repeat("c", 40), enSecoYAML, []string{"e2e/compra.spec.ts", "web/package.json"})
	if d := app2.Dry; d == nil || d.Release != 0 || d.Copy == nil || d.Copy.Kind != store.DryCopyOff || !strings.Contains(d.Copy.Note, "has no previews") {
		t.Fatalf("a branch that changes the app: %+v", d)
	}
	mu.Lock()
	if len(ran) != before {
		t.Errorf("checks ran en seco for a branch that changes the app: %v", ran[before:])
	}
	mu.Unlock()

	// Nothing published yet: a note.
	nueva := push("p0dxD/ensecos-nueva", 33, "pruebas-nuevas", strings.Repeat("d", 40), enSecoYAML, []string{"e2e/compra.spec.ts"})
	if d := nueva.Dry; d == nil || d.Release != 0 || !strings.Contains(d.Note, "no release is published yet") {
		t.Fatalf("an app with nothing published: %+v", d)
	}
}
