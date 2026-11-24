package platform

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// conCopiaYAML is an app with previews whose smoke test and own tests may
// run en seco: against the published release when only they change, and
// against a copy of the branch when the app changes too.
const conCopiaYAML = "services:\n  - name: web\n    path: web\n    domain: secacopia.example.com\n" +
	"builds:\n  - name: e2e\n    path: e2e\n" +
	"tasks:\n  - name: humo\n    stage: post-deploy\n    image: curlimages/curl:8.17.0\n    command: curl -fsS http://web/\n    dryRun: true\n" +
	"tests:\n  build: e2e\n  dryRun: true\n" +
	"previews: {max: 2}\n"

// What a branch's checks run against en seco (step 2): the published
// release when only the checks changed; the pull request's copy when the
// app changed, once that copy runs the commit; a short copy, deleted when
// they end, when the branch has no pull request; nothing, saying why, when
// the app has no previews, the copies are at previews.max (a pull
// request's is never deleted for one) or the copy never runs the commit;
// nothing at all for a branch of documentation. A failure only warns.
func TestEnSecoConCopia(t *testing.T) {
	p, fake, _, kube := setup(t)
	ctx := context.Background()
	sola := strings.Replace(conCopiaYAML, "previews: {max: 2}\n", "", 1)
	sola = strings.ReplaceAll(sola, "secacopia.example.com", "secacopia-sola.example.com")
	fake.mu.Lock()
	fake.files["main"] = fstest.MapFS{"rendimiento.yaml": {Data: []byte(conCopiaYAML)}}
	fake.mu.Unlock()
	apps := map[string]*store.App{}
	published := map[string]int64{}
	for name, yaml := range map[string]string{"secacopia": conCopiaYAML, "secacopia-sola": sola} {
		sp, err := spec.Parse([]byte(yaml))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/" + name, DefaultBranch: "main", Name: name, Spec: *sp}, false); err != nil {
			t.Fatalf("onboarding %s: %v", name, err)
		}
		app, err := p.Store.GetApp(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		apps[name] = app
		rel := &store.Release{AppID: app.ID, SHA: strings.Repeat("0", 40), Spec: *sp,
			Images: map[string]string{"web": "registry.example.lan:5000/" + name + "-web@sha256:live", "build:e2e": "registry.example.lan:5000/" + name + "-e2e@sha256:live"}}
		if err := p.Store.CreateRelease(ctx, rel); err != nil {
			t.Fatal(err)
		}
		_ = p.Store.SetVerification(ctx, rel.ID, store.VerifyPassed, "ok")
		published[name] = rel.Number
	}
	app := apps["secacopia"]
	if err := p.Store.SetAppAutoMerge(ctx, app.ID, true, true); err != nil {
		t.Fatal(err)
	}

	type ran struct{ where, name, stage, base, image string }
	var mu sync.Mutex
	var runs []ran
	rollouts := map[string][]RolloutState{} // a copy's next answers; then healthy
	failing := map[string]bool{}            // copies where a test fails
	shortSeen := map[string]string{}        // a short copy, and its domain, while its checks ran
	p.Verify.Rollout = func(_ context.Context, name string, _ int64) (RolloutState, string, error) {
		mu.Lock()
		defer mu.Unlock()
		if next := rollouts[name]; len(next) > 0 {
			rollouts[name] = next[1:]
			return next[0], "web: 0 of 1 ready", nil
		}
		return RolloutHealthy, "", nil
	}
	p.Verify.RunTask = func(_ context.Context, a *store.App, _ *store.Release, task spec.Task) store.ReleaseTask {
		if strings.Contains(a.Name, "-seco-") {
			var cr v1alpha1.App
			if err := kube.Get(ctx, client.ObjectKey{Name: a.Name}, &cr); err == nil && cr.Spec.Preview > 0 && len(cr.Spec.Services) == 1 {
				mu.Lock()
				shortSeen[a.Name] = cr.Spec.Services[0].Domain
				mu.Unlock()
			}
		}
		mu.Lock()
		runs = append(runs, ran{a.Name, task.Name, task.Stage, task.Env["BASE_URL"], task.Image})
		bad := failing[a.Name]
		mu.Unlock()
		end := time.Now()
		if task.Name == spec.TestsTaskName {
			log := "@@prueba\texpected\treservas.spec.ts › abre\n"
			if bad {
				log += "@@prueba\tunexpected\treservas.spec.ts › reserva una cita\n"
			}
			return store.ReleaseTask{Status: store.TaskSucceeded, FinishedAt: &end, Log: log}
		}
		return store.ReleaseTask{Status: store.TaskSucceeded, Log: "ok\n", FinishedAt: &end}
	}
	ranFrom := func(i int) []ran {
		mu.Lock()
		defer mu.Unlock()
		return append([]ran(nil), runs[i:]...)
	}
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(runs)
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
		if number != 0 {
			pr := forja.Change{Number: number, State: "open", Mergeable: &yes, MergeableState: "clean"}
			pr.Head.Ref, pr.Head.SHA, pr.Base.Ref = branch, sha, "main"
			fake.pulls = append(fake.pulls, pr)
			fake.recent = append([]forja.Change{pr}, fake.recent...)
		}
		fake.mu.Unlock()
		// What GitHub said before this pull request existed is kept a while.
		p.branchPullsCache.mu.Lock()
		for id := range p.branchPullsCache.byApp {
			delete(p.branchPullsCache.byApp, id)
		}
		p.branchPullsCache.mu.Unlock()
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
	sha := func(c string) string { return strings.Repeat(c, 40) }

	// Only the tests changed: against the published release, as in step 1.
	before := count()
	solo := push("p0dxD/secacopia", 0, "pruebas-nuevas", sha("a"), conCopiaYAML, []string{"e2e/reservas.spec.ts"})
	if d := solo.Dry; d == nil || d.Release != published["secacopia"] || d.Copy != nil || d.Failed {
		t.Fatalf("only the checks changed: %+v", d)
	}
	for _, r := range ranFrom(before) {
		if r.where != "secacopia" || r.stage != spec.StageDryRun {
			t.Errorf("only the checks changed: %s ran in %s as %s", r.name, r.where, r.stage)
		}
	}

	// The app changed, and the branch has a pull request: against its
	// copy, once the copy runs this commit (it is still starting at first).
	mu.Lock()
	rollouts["secacopia-pr-41"] = []RolloutState{RolloutPending}
	mu.Unlock()
	before = count()
	conPR := push("p0dxD/secacopia", 41, "renovate/react-19.x", sha("b"), conCopiaYAML, []string{"web/package.json"})
	d := conPR.Dry
	if d == nil || d.Release != 0 || d.Copy == nil || d.Copy.Kind != store.DryCopyPull || d.Copy.Pull != 41 ||
		d.Copy.URL != "https://secacopia-pr-41.example.com" || d.Copy.Gone || d.Failed || len(d.Copy.Tasks) != 2 {
		t.Fatalf("against the pull request's copy: %+v %+v", d, d.Copy)
	}
	mu.Lock()
	waited := len(rollouts["secacopia-pr-41"]) == 0
	mu.Unlock()
	if !waited {
		t.Fatal("the checks did not wait for the copy to run the commit")
	}
	got := ranFrom(before)
	if len(got) != 2 {
		t.Fatalf("against the pull request's copy, ran %+v", got)
	}
	for _, r := range got {
		if r.where != "secacopia-pr-41" || r.stage != spec.StageDryRun {
			t.Errorf("%s ran in %s as %s, not the pull request's copy", r.name, r.where, r.stage)
		}
		if r.name == spec.TestsTaskName && (r.base != "https://secacopia-pr-41.example.com" || r.image == "" || strings.HasPrefix(r.image, "build:")) {
			t.Errorf("the tests reached %s in %s", r.base, r.image)
		}
	}
	// The copy's own run of the tests waits for these: they ran once, and
	// the pull request's comment says how.
	time.Sleep(300 * time.Millisecond)
	if n := len(ranFrom(before)); n != 2 {
		t.Errorf("the tests ran against the copy %d times, want once en seco", n-1)
	}
	fake.mu.Lock()
	said := ""
	for _, s := range fake.said {
		if strings.HasPrefix(s, "#41 ") {
			said = s
		}
	}
	fake.mu.Unlock()
	if !strings.Contains(said, "**Tests:** 1 of 1 passed against this copy.") {
		t.Errorf("the pull request's comment: %q", said)
	}

	// No pull request yet: a short copy, only for the checks, at its own
	// address, deleted when they end.
	before = count()
	corta := push("p0dxD/secacopia", 0, "arreglo-sin-pr", sha("c"), conCopiaYAML, []string{"web/src/App.tsx"})
	name := ShortCopyName("secacopia", corta.ID)
	d = corta.Dry
	if d == nil || d.Copy == nil || d.Copy.Kind != store.DryCopyShort || d.Copy.URL != fmt.Sprintf("https://secacopia-seco-%d.example.com", corta.ID) ||
		!d.Copy.Gone || d.Failed || len(d.Copy.Tasks) != 2 {
		t.Fatalf("against a short copy: %+v %+v", d, d.Copy)
	}
	mu.Lock()
	domain := shortSeen[name]
	mu.Unlock()
	if domain != fmt.Sprintf("secacopia-seco-%d.example.com", corta.ID) {
		t.Fatalf("the short copy while its checks ran: %q (%v)", domain, shortSeen)
	}
	for _, r := range ranFrom(before) {
		if r.where != name {
			t.Errorf("%s ran in %s, not the short copy", r.name, r.where)
		}
	}
	if !gone(ctx, kube, name) {
		t.Fatal("the short copy stayed after its checks")
	}
	if gone(ctx, kube, "secacopia-pr-41") {
		t.Fatal("the short copy took the pull request's copy's place")
	}

	// A test fails against another pull request's copy: it only warns.
	mu.Lock()
	failing["secacopia-pr-42"] = true
	mu.Unlock()
	falla := push("p0dxD/secacopia", 42, "renovate/vite-8.x", sha("d"), conCopiaYAML, []string{"web/package.json"})
	if !falla.DryFailed() || !falla.Dry.Copy.Failed || falla.Dry.Copy.Pull != 42 {
		t.Fatalf("a test that fails against the copy: %+v", falla.Dry)
	}
	fake.mu.Lock()
	c := fake.checks[*falla.CheckRun]
	fake.mu.Unlock()
	if c.Conclusion != "action_required" || c.Output == nil || !strings.Contains(c.Output.Title, "`pruebas` failed en seco against the copy of pull request #42: a person must merge this") {
		t.Fatalf("the check of a failing dry run against the copy: %+v %+v", c, c.Output)
	}
	p.MergeReady(ctx, app)
	if r, _ := p.Store.GetRun(ctx, falla.ID); r.Merge == store.MergeMerged {
		t.Fatal("rendimiento merged by itself a branch whose test failed against its copy")
	}

	// Two copies, previews.max: no short copy, and neither pull request's
	// copy goes for one.
	before = count()
	llena := push("p0dxD/secacopia", 0, "sin-lugar", sha("e"), conCopiaYAML, []string{"web/src/App.tsx"})
	if d := llena.Dry; d == nil || d.Copy == nil || d.Copy.Kind != store.DryCopyShort || !strings.Contains(d.Copy.Note, "no room for a short copy") || d.Failed {
		t.Fatalf("at previews.max: %+v", llena.Dry)
	}
	if n := len(ranFrom(before)); n != 0 {
		t.Fatalf("checks ran with no room for a copy: %+v", ranFrom(before))
	}
	if gone(ctx, kube, "secacopia-pr-41") || gone(ctx, kube, "secacopia-pr-42") || !gone(ctx, kube, ShortCopyName("secacopia", llena.ID)) {
		t.Fatal("a pull request's copy went for a short copy")
	}

	// A copy that never runs the commit: it says why, and warns.
	p.Verify.RolloutTimeout = time.Millisecond
	mu.Lock()
	rollouts["secacopia-pr-43"] = []RolloutState{RolloutPending, RolloutPending}
	rollouts["secacopia-pr-44"] = []RolloutState{RolloutSuperseded}
	mu.Unlock()
	before = count()
	lenta := push("p0dxD/secacopia", 43, "lenta", sha("f"), conCopiaYAML, []string{"web/src/App.tsx"})
	if d := lenta.Dry; d == nil || d.Copy == nil || !strings.Contains(d.Copy.Note, "the copy did not come to run this commit") || !lenta.DryFailed() {
		t.Fatalf("a copy that never ran the commit: %+v", lenta.Dry)
	}
	nueva := push("p0dxD/secacopia", 44, "reemplazada", sha("7"), conCopiaYAML, []string{"web/src/App.tsx"})
	if d := nueva.Dry; d == nil || d.Copy == nil || !strings.Contains(d.Copy.Note, "a newer push replaced the copy") || nueva.DryFailed() {
		t.Fatalf("a copy a newer push replaced: %+v", nueva.Dry)
	}
	p.Verify.RolloutTimeout = 0
	if n := len(ranFrom(before)); n != 0 {
		t.Fatalf("checks ran against a copy that did not run the commit: %+v", ranFrom(before))
	}

	// Only the book: nothing at all.
	libro := push("p0dxD/secacopia", 0, "libro", sha("8"), conCopiaYAML, []string{"docs/guia.md", "README.md"})
	if libro.Dry != nil {
		t.Fatalf("a branch of documentation ran en seco: %+v", libro.Dry)
	}

	// The app changed, and it has no previews: nothing ran, and it says so.
	before = count()
	sin := push("p0dxD/secacopia-sola", 0, "renovate/vite-8.x", sha("9"), sola, []string{"web/package.json"})
	if d := sin.Dry; d == nil || d.Copy == nil || d.Copy.Kind != store.DryCopyOff || !strings.Contains(d.Copy.Note, "has no previews") || d.Failed {
		t.Fatalf("an app without previews: %+v", sin.Dry)
	}
	if n := len(ranFrom(before)); n != 0 {
		t.Fatalf("checks ran for an app without previews: %+v", ranFrom(before))
	}
}
