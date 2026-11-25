package agentes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	gh "github.com/rendimiento-ai/rendimiento/internal/github"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// askStore is coderStore keeping questions in memory.
type askStore struct {
	*coderStore
	asked  []store.EncargoPregunta
	answer *store.EncargoPregunta // what ResumeAnswer gives, once
	open   []store.EncargoPregunta
	before time.Time
	passed []string
}

func (s *askStore) AskEncargo(_ context.Context, id, _ int64, who string, q store.EncargoPregunta) (store.Encargo, error) {
	q.EncargoID, q.AskedBy = id, who
	s.asked = append(s.asked, q)
	return store.Encargo{}, nil
}

func (s *askStore) ResumeAnswer(context.Context, int64) (store.EncargoPregunta, error) {
	if s.answer == nil {
		return store.EncargoPregunta{}, store.ErrNotFound
	}
	p := *s.answer
	s.answer = nil
	return p, nil
}

func (s *askStore) UnansweredSince(_ context.Context, before time.Time) ([]store.EncargoPregunta, error) {
	s.before = before
	return s.open, nil
}

func (s *askStore) PassAskedEncargo(_ context.Context, id, org int64, who, note string) (store.Encargo, error) {
	s.passed = append(s.passed, fmt.Sprintf("%d %d %s: %s", id, org, who, note))
	return store.Encargo{}, nil
}

// seen keeps what the model was asked: the first message, the system
// prompt and the tools.
type seen struct {
	m interface {
		CompleteTools(context.Context, int64, string, ia.ToolRequest) (ia.ToolResponse, error)
	}
	brief  string
	system string
	tools  []string
}

func (s *seen) CompleteTools(ctx context.Context, org int64, source string, req ia.ToolRequest) (ia.ToolResponse, error) {
	if s.brief == "" {
		var blocks []struct{ Text string }
		_ = json.Unmarshal(req.Turns[0].Content, &blocks)
		if len(blocks) > 0 {
			s.brief = blocks[0].Text
		}
		s.system = req.System
		for _, t := range req.Tools {
			s.tools = append(s.tools, t.Name)
		}
	}
	return s.m.CompleteTools(ctx, org, source, req)
}

// The coding agent asks the person and stops without spending more: what
// it changed is committed (and tested meanwhile, no [skip ci]), the
// question keeps its options, its notes and what it spent. Answered, it
// resumes on the same branch (not made again) with the answer in its first
// message and what it spent before counted, tests what it committed and
// delivers.
func TestCoderAsksWaitsAndResumes(t *testing.T) {
	st := &coderStore{}
	as := &askStore{coderStore: st}
	r := &repos{st: st, files: map[string][]byte{"saludo.go": []byte("const Saludo = \"Hi\"\n")}}
	m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("edit_file", map[string]string{"path": "saludo.go", "old_text": "Hi", "new_text": "Hola"})},
		{call("ask", map[string]any{"question": "¿Con signos?", "options": []string{"Sí"}})},
		{call("ask", map[string]any{"question": "¿Con signos?", "options": []string{"Sí, «¡Hola!»", "No, «Hola»"}, "notes": "Cambié saludo.go; falta decidir los signos."})},
		{call("read_file", map[string]string{"path": "saludo.go"})},
	}}
	c := newCoder(st, r, m)
	c.Store = as
	sw := &seen{m: m}
	c.Model = sw
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(sw.tools, " "), "ask") || !strings.Contains(sw.system, "call ask") {
		t.Fatalf("not told it can ask: %v\n%s", sw.tools, sw.system)
	}
	if len(m.seen) < 2 || !strings.Contains(m.seen[1], "offer 2 to 4 options") {
		t.Fatalf("results %q", m.seen)
	}
	if m.n != 3 || len(as.asked) != 1 || st.gaveBack != "" || st.delivered != "" {
		t.Fatalf("%d turns, asked %+v, gave back %q: it should stop at the question", m.n, as.asked, st.gaveBack)
	}
	q := as.asked[0]
	if q.Question != "¿Con signos?" || len(q.Options) != 2 || q.SpentMicros != 30_000 || q.AskedBy != CoderName ||
		!strings.Contains(q.Notes, "falta decidir") || !strings.Contains(q.Notes, "sha0001") {
		t.Fatalf("the question %+v", q)
	}
	if len(r.commits) != 1 || string(r.commits[0]["saludo.go"]) != "const Saludo = \"Hola\"\n" || strings.Contains(r.msgs[0], "[skip ci]") {
		t.Fatalf("commits %q (%q)", r.commits, r.msgs)
	}
	if !strings.Contains(st.handover, "preguntó") {
		t.Fatalf("handover %q", st.handover)
	}

	// The person answers; the queue gives it to the agent again.
	branch := "feature/encargo-12-saludar-en-espanol"
	as.answer = &store.EncargoPregunta{Question: q.Question, Options: q.Options, Notes: q.Notes, SpentMicros: q.SpentMicros, Answer: "Sí, con signos", AnsweredBy: "p0dxD"}
	st.runs = append(st.runs, &store.Run{ID: 950, AppID: 4, Branch: branch, SHA: "head-of-" + branch, Status: store.RunSucceeded})
	r.from = ""
	m2 := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("commit_and_test", map[string]string{"message": "Saludo"})},
		{call("deliver", map[string]string{"summary": "Con signos, como dijo."})},
	}}
	sw2 := &seen{m: m2}
	c.Model = sw2
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sw2.brief, "¿Con signos?") || !strings.Contains(sw2.brief, "p0dxD answered:\nSí, con signos") || !strings.Contains(sw2.brief, "falta decidir") {
		t.Fatalf("the brief does not carry the answer:\n%s", sw2.brief)
	}
	if r.from != "" {
		t.Fatalf("made the branch again from %q, losing what it committed", r.from)
	}
	if len(m2.seen) < 1 || !strings.Contains(m2.seen[0], "run #950 passed") {
		t.Fatalf("results %q", m2.seen)
	}
	// $0.03 before asking, and two turns now.
	if !strings.Contains(st.delivered, "Con signos") || !strings.Contains(st.delivered, "Gastó $0.05") {
		t.Fatalf("delivered %q (gave back %q)", st.delivered, st.gaveBack)
	}
}

// A question nobody answers in 3 days goes to the person's own agent, with
// the question, its options and the agent's notes.
func TestCoderPassesUnansweredQuestions(t *testing.T) {
	st := &coderStore{}
	as := &askStore{coderStore: st, open: []store.EncargoPregunta{{EncargoID: 26, OrgID: 3, Question: "¿El diseño primero?", Options: []string{"Sí", "No"}, Notes: "dos formas"}}}
	c := newCoder(st, &repos{st: st}, &script{t: t})
	c.Store = as
	now := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	c.passUnanswered(context.Background(), now)
	if !as.before.Equal(now.Add(-72 * time.Hour)) {
		t.Fatalf("looked for questions before %s", as.before)
	}
	if len(as.passed) != 1 || !strings.HasPrefix(as.passed[0], "26 3 "+CoderName+": ") || !strings.Contains(as.passed[0], "3 días") ||
		!strings.Contains(as.passed[0], "¿El diseño primero?") || !strings.Contains(as.passed[0], "B. No") || !strings.Contains(as.passed[0], "dos formas") {
		t.Fatalf("passed %q", as.passed)
	}
}

// fakePlataforma answers for the app it is asked about, and some of
// another app's, which must never be shown.
type fakePlataforma struct {
	slowFor []string
	asPlat  []bool
}

func (p *fakePlataforma) ListReleases(_ context.Context, appID int64, _ int) ([]*store.Release, error) {
	return []*store.Release{{AppID: appID, Number: 7, SHA: "abcdef1234", VerifyStatus: "passed"}, {AppID: 99, Number: 8, SHA: "ajeno123456"},
		{AppID: appID, Number: 6, SHA: "fedcba9876", VerifyStatus: store.VerifyFailed, VerifyMessage: "the rollout failed"}}, nil
}

// ReleaseEvidence: the app's release 6 failed with its container exiting.
func (p *fakePlataforma) ReleaseEvidence(_ context.Context, _, number int64) (*store.ReleaseEvidence, error) {
	if number != 6 {
		return nil, store.ErrNotFound
	}
	code := int32(2)
	return &store.ReleaseEvidence{Where: "local", Services: []store.ServiceEvidence{{Name: "web", Pods: []store.PodEvidence{{Name: "tienda-web-r6", Phase: "exited",
		Containers: []store.ContainerEvidence{{Name: "web", State: "terminated", Reason: "exited", ExitCode: &code, Log: "listening\nerror: config.yaml not found"}}}}}}}, nil
}

func (p *fakePlataforma) ListProblems(_ context.Context, app string, _ time.Time, _ bool, _ int) ([]store.Problem, error) {
	return []store.Problem{{App: app, Level: "warn", Component: "ci", Message: "step platform:test is flaky", Count: 3}, {App: "otra", Message: "problema ajeno"}}, nil
}

func (p *fakePlataforma) SlowRequests(_ context.Context, app string, platform bool, _ time.Time, _ int) ([]store.SlowRequest, error) {
	p.slowFor, p.asPlat = append(p.slowFor, app), append(p.asPlat, platform)
	return []store.SlowRequest{{Route: "GET /carrito", Ms: 1500, Status: 200}}, nil
}

func (p *fakePlataforma) HallazgosDeApp(_ context.Context, app string, _ int) ([]store.VigiaHallazgo, error) {
	return []store.VigiaHallazgo{{ID: 3, App: app, Titulo: "platform:test se alargó", Evidencia: "6.6 → 8.2 min", Estado: "encargo"}, {ID: 4, App: "otra", Titulo: "hallazgo ajeno"}}, nil
}

func (p *fakePlataforma) ListTraces(context.Context, store.TraceQuery) ([]store.Trace, error) {
	return nil, nil
}

// A lookout's encargo: the agent is told to verify the numbers first, and
// its read-only tools show the encargo's app only: its runs with step
// durations, a step's log (another app's run is refused), its slow
// requests and its status, never another app's.
func TestCoderSeesOnlyItsApp(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	t1, t2 := t0.Add(6*time.Minute), t0.Add(8*time.Minute)
	st := &coderStore{runs: []*store.Run{
		{ID: 700, AppID: 4, Branch: "main", SHA: "aaaaaaa1", Status: store.RunSucceeded, CreatedAt: t0,
			Steps: []store.Step{{ID: "platform:test", Status: store.RunSucceeded, StartedAt: &t0, FinishedAt: &t2}, {ID: "docs", Status: store.RunSucceeded, StartedAt: &t0, FinishedAt: &t1}}},
		{ID: 701, AppID: 5, Branch: "ajena", SHA: "bbbbbbb2", Status: store.RunFailed, CreatedAt: t0},
	}}
	plat := &fakePlataforma{}
	m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("runs", map[string]any{"step": "platform:test"})},
		{call("step_log", map[string]any{"run": 701, "step": "web:test"})},
		{call("step_log", map[string]any{"run": 700, "step": "platform:test", "lines": 1})},
		{call("slow_requests", map[string]any{})},
		{call("app_status", map[string]any{})},
		{call("give_up", map[string]string{"reason": "Los números no se sostienen."})},
	}}
	c := newCoder(st, &repos{st: st}, m)
	body := "El paso platform:test se alargó.\n\nThe lookout saw it (finding #3, key `paso-mas-lento:tienda/platform:test`) and p0dxD accepted it."
	c.Store = withBody{st, body}
	c.Plataforma, c.PlatformApp = plat, "rendimiento-ai"
	sw := &seen{m: m}
	c.Model = sw
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda", Body: body}); err != nil {
		t.Fatal(err)
	}
	tools := strings.Join(sw.tools, " ")
	for _, want := range []string{"runs", "step_log", "slow_requests", "app_status"} {
		if !strings.Contains(tools, want) {
			t.Fatalf("not offered %s: %s", want, tools)
		}
	}
	if strings.Contains(tools, "ask") {
		t.Fatalf("offered ask without a store for questions: %s", tools)
	}
	if !strings.Contains(sw.system, "comes from the lookout") || !strings.Contains(sw.system, "Verify them first") {
		t.Fatalf("a lookout's encargo without the rule to verify:\n%s", sw.system)
	}
	if len(m.seen) < 5 {
		t.Fatalf("results %q", m.seen)
	}
	if runs := m.seen[0]; !strings.Contains(runs, "#700 main aaaaaaa") || !strings.Contains(runs, "platform:test succeeded 8m0s") || strings.Contains(runs, "docs") ||
		strings.Contains(runs, "#701") || !strings.Contains(runs, "median 8m0s") {
		t.Fatalf("runs: %s", runs)
	}
	if !strings.Contains(m.seen[1], "is not a run of tienda") {
		t.Fatalf("another app's log: %s", m.seen[1])
	}
	if !strings.Contains(m.seen[2], "want \"Hola\"") || !strings.Contains(m.seen[2], "the last 1 of") {
		t.Fatalf("its log: %s", m.seen[2])
	}
	if !strings.Contains(m.seen[3], "GET /carrito 1500 ms, status 200") || len(plat.slowFor) != 1 || plat.slowFor[0] != "tienda" || plat.asPlat[0] {
		t.Fatalf("slow requests: %s (asked for %v, %v)", m.seen[3], plat.slowFor, plat.asPlat)
	}
	status := m.seen[4]
	if !strings.Contains(status, "#7 abcdef1") || !strings.Contains(status, "flaky") || !strings.Contains(status, "platform:test se alargó") ||
		strings.Contains(status, "ajeno") || strings.Contains(status, "#8") {
		t.Fatalf("app status: %s", status)
	}
	// The failed release brings what was seen when it failed.
	if !strings.Contains(status, "#6 fedcba9") || !strings.Contains(status, "web: terminated exited (exit code 2)") || !strings.Contains(status, "| error: config.yaml not found") {
		t.Fatalf("app status without the failed release's evidence: %s", status)
	}
	if !strings.Contains(st.gaveBack, "no se sostienen") {
		t.Fatalf("gave back %q", st.gaveBack)
	}
}

// withBody is a store whose encargo, once taken, has a body.
type withBody struct {
	*coderStore
	body string
}

func (s withBody) TakeEncargo(ctx context.Context, id, key, cargo int64, who string) (store.Encargo, error) {
	e, err := s.coderStore.TakeEncargo(ctx, id, key, cargo, who)
	e.Body = s.body
	return e, err
}

// otherOrgStore says the encargo's app belongs to another organization.
type otherOrgStore struct{ *coderStore }

func (s otherOrgStore) GetApp(_ context.Context, name string) (*store.App, error) {
	return &store.App{ID: 4, Name: name, OrgID: 2, Repo: "o/tienda", DefaultBranch: "main"}, nil
}

// An encargo whose app is another organization's is not worked on at all:
// no model call, no tool, given back.
func TestCoderRefusesAnotherOrgsApp(t *testing.T) {
	st := &coderStore{}
	m := &script{t: t, turns: [][]ia.ToolCall{{call("runs", map[string]any{})}}}
	c := newCoder(st, &repos{st: st}, m)
	c.Store = otherOrgStore{st}
	c.Plataforma = &fakePlataforma{}
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda"}); err != nil {
		t.Fatal(err)
	}
	if m.n != 0 || !strings.Contains(st.gaveBack, "no app") {
		t.Fatalf("%d turns, gave back %q", m.n, st.gaveBack)
	}
}

// flaky fails its first calls with err, then answers with the script.
type flaky struct {
	m     *script
	fails int
	err   error
	calls int
}

func (f *flaky) CompleteTools(ctx context.Context, org int64, source string, req ia.ToolRequest) (ia.ToolResponse, error) {
	f.calls++
	if f.fails > 0 {
		f.fails--
		return ia.ToolResponse{}, f.err
	}
	return f.m.CompleteTools(ctx, org, source, req)
}

// A connection the model's API reset is tried again (up to 3 more times)
// instead of giving the encargo up; a fourth failure in a row gives it
// back, and an error that does not pass is not tried again.
func TestCoderRetriesTheModel(t *testing.T) {
	pause := aiPause
	aiPause = time.Millisecond
	t.Cleanup(func() { aiPause = pause })
	reset := fmt.Errorf("https://api.anthropic.com/v1/messages: read tcp: %w", errors.New("connection reset by peer"))

	st := &coderStore{}
	r := &repos{st: st, files: map[string][]byte{"saludo.go": []byte("const Saludo = \"Hi\"\n")}, results: []string{store.RunSucceeded}}
	m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("edit_file", map[string]string{"path": "saludo.go", "old_text": "Hi", "new_text": "Hola"})},
		{call("commit_and_test", map[string]string{"message": "Saludo"})},
		{call("deliver", map[string]string{"summary": "Listo."})},
	}}
	f := &flaky{m: m, fails: 3, err: reset}
	c := newCoder(st, r, m)
	c.Model = f
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda"}); err != nil {
		t.Fatal(err)
	}
	if st.delivered == "" || f.calls != 6 {
		t.Fatalf("delivered %q after %d calls (gave back %q)", st.delivered, f.calls, st.gaveBack)
	}

	st = &coderStore{}
	f = &flaky{m: &script{t: t}, fails: 10, err: reset}
	c = newCoder(st, &repos{st: st}, f.m)
	c.Model = f
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda"}); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1+aiTries || !strings.Contains(st.gaveBack, "connection reset") {
		t.Fatalf("%d calls, gave back %q", f.calls, st.gaveBack)
	}

	st = &coderStore{}
	f = &flaky{m: &script{t: t}, fails: 10, err: errors.New("https://api.anthropic.com/v1/messages answered 400: bad request")}
	c = newCoder(st, &repos{st: st}, f.m)
	c.Model = f
	_ = c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda"})
	if f.calls != 1 {
		t.Fatalf("a 400 was tried %d times", f.calls)
	}
}

// slowRuns is a store whose runs stay running for a while.
type slowRuns struct {
	*coderStore
	polls, until int
}

func (s *slowRuns) GetRun(ctx context.Context, id int64) (*store.Run, error) {
	r, err := s.coderStore.GetRun(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.polls++; s.polls >= s.until {
		r.Status = store.RunSucceeded
	}
	return r, nil
}

// A run that takes longer than runs usually do is waited for, spending
// nothing (no turn of the model meanwhile), instead of giving up.
func TestCoderKeepsWaitingForALongRun(t *testing.T) {
	wait := runWait
	runWait = 2 * time.Millisecond
	t.Cleanup(func() { runWait = wait })
	st := &coderStore{}
	r := &repos{st: st, files: map[string][]byte{"saludo.go": []byte("const Saludo = \"Hi\"\n")}, results: []string{store.RunRunning}}
	m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("edit_file", map[string]string{"path": "saludo.go", "old_text": "Hi", "new_text": "Hola"})},
		{call("commit_and_test", map[string]string{"message": "Saludo"})},
		{call("deliver", map[string]string{"summary": "Listo."})},
	}}
	slow := &slowRuns{coderStore: st, until: 40}
	c := newCoder(st, r, m)
	c.Store = slow
	start := time.Now()
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda"}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < runWait || slow.polls < 40 {
		t.Fatalf("did not wait: %d polls", slow.polls)
	}
	if st.delivered == "" || m.n != 3 || len(m.seen) < 2 || !strings.Contains(m.seen[1], "run #901 passed") {
		t.Fatalf("delivered %q after %d turns (%q); gave back %q", st.delivered, m.n, m.seen, st.gaveBack)
	}
}

// A run the cluster failed (etcd timed out) runs once more with the
// platform's retry before it counts; one the code failed does not.
func TestCoderRunsAgainWhatTheClusterFailed(t *testing.T) {
	st := &coderStore{}
	r := &repos{st: st, files: map[string][]byte{"saludo.go": []byte("const Saludo = \"Hi\"\n")},
		results: []string{store.RunFailed, store.RunFailed, store.RunSucceeded}}
	m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("edit_file", map[string]string{"path": "saludo.go", "old_text": "Hi", "new_text": "Hola"})},
		{call("commit_and_test", map[string]string{"message": "Saludo"})},
		{call("edit_file", map[string]string{"path": "saludo.go", "old_text": "Hola", "new_text": "Holi"})},
		{call("commit_and_test", map[string]string{"message": "Otro"})},
		{call("edit_file", map[string]string{"path": "saludo.go", "old_text": "Holi", "new_text": "¡Hola!"})},
		{call("commit_and_test", map[string]string{"message": "Con signos"})},
		{call("deliver", map[string]string{"summary": "Listo."})},
	}}
	c := newCoder(st, r, m)
	c.Logs = func(_ context.Context, run int64, _ string) (string, error) {
		if run == 901 {
			return "go: downloading modules\nError from server: etcdserver: request timed out\n", nil
		}
		return "--- FAIL: TestSaludo\n    saludo_test.go:9: got \"Holi\"\n", nil
	}
	var retried []int64
	c.RetryRun = func(_ context.Context, id int64) (*store.Run, error) {
		retried = append(retried, id)
		for _, run := range st.runs {
			if run.ID == id {
				run.Status, run.Steps = store.RunSucceeded, nil
				return run, nil
			}
		}
		return nil, store.ErrNotFound
	}
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda"}); err != nil {
		t.Fatal(err)
	}
	if len(retried) != 1 || retried[0] != 901 {
		t.Fatalf("retried %v, want only the run the cluster failed", retried)
	}
	if len(m.seen) < 4 || !strings.Contains(m.seen[1], "failed for the cluster (etcdserver: request timed out)") || !strings.Contains(m.seen[1], "run #901 passed") {
		t.Fatalf("results %q", m.seen)
	}
	if !strings.Contains(m.seen[3], "run #902 failed") || strings.Contains(m.seen[3], "for the cluster") {
		t.Fatalf("a code failure: %s", m.seen[3])
	}
	if st.delivered == "" {
		t.Fatalf("gave back %q", st.delivered)
	}
}

// What says the cluster failed a run, and what does not.
func TestClusterSigns(t *testing.T) {
	for _, c := range []struct {
		text  string
		infra bool
	}{
		{"Error from server: etcdserver: request timed out", true},
		{"The node was low on resource: memory. Container step was using 2Gi", true},
		{"Evicted: the pod was evicted", true},
		{"Back-off pulling image \"registry.example.lan/x\": ImagePullBackOff", true},
		{"failed to pull image registry.example.lan/x: dial tcp: i/o timeout", true},
		{"0/3 nodes are available: 3 Insufficient cpu.", true},
		{"--- FAIL: TestSaludo (0.00s)", false},
		{"exited with code 1", false},
		{"OOMKilled", false},
	} {
		if got := clusterSigns.MatchString(c.text); got != c.infra {
			t.Errorf("%q: cluster %v, want %v", c.text, got, c.infra)
		}
	}
}

// failingRepos fails its first calls with err.
type failingRepos struct {
	*repos
	fails int
	err   error
	calls int
}

func (f *failingRepos) BranchSHA(ctx context.Context, inst int64, repo, branch string) (string, error) {
	f.calls++
	if f.fails > 0 {
		f.fails--
		return "", f.err
	}
	return f.repos.BranchSHA(ctx, inst, repo, branch)
}

// A forja's call that failed for a reason that passes (a 502, a reset
// connection) is tried once more; a 404 or a second failure is not.
func TestForjaTriesOnceMore(t *testing.T) {
	pause := forjaPause
	forjaPause = time.Millisecond
	t.Cleanup(func() { forjaPause = pause })
	ctx := context.Background()
	for _, c := range []struct {
		err         error
		fails, want int
		ok          bool
	}{
		{&gh.APIError{Status: 502, Method: "GET", Path: "/repos/o/r/git/ref/heads/main", Body: "Bad Gateway"}, 1, 2, true},
		{fmt.Errorf("gitlab GET /projects/1/repository/branches/main: 503"), 1, 2, true},
		{errors.New("read tcp 10.0.0.1:443: connection reset by peer"), 1, 2, true},
		{&gh.APIError{Status: 502, Method: "GET", Path: "/x"}, 2, 2, false},
		{&gh.APIError{Status: 404, Method: "GET", Path: "/x"}, 1, 1, false},
	} {
		f := &failingRepos{repos: &repos{}, fails: c.fails, err: c.err}
		_, err := retryRepos{f}.BranchSHA(ctx, 1, "o/r", "main")
		if f.calls != c.want || (err == nil) != c.ok {
			t.Errorf("%v: %d calls, err %v", c.err, f.calls, err)
		}
	}
}

// A lookout's encargo goes to the coding agent like any other (decision
// 4): the scheduler does not leave it out for having no run.
func TestPickTakesALookoutsEncargo(t *testing.T) {
	e := store.Encargo{ID: 43, OrgID: 1, App: "rendimiento-ai", Body: "Lo vio el vigía (hallazgo #9, clave `paso-mas-lento:x`) y p0dxD lo aceptó."}
	if !vigiaBorn(e) {
		t.Fatal("not seen as the lookout's")
	}
	if got := Pick([]store.CoderCandidate{{Encargo: e, Concurrency: 1}}, map[int64]Slot{}); len(got) != 1 || got[0].ID != 43 {
		t.Fatalf("picked %v", got)
	}
}
