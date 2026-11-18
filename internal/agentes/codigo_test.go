package agentes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/arreglos"
	gh "github.com/rendimiento-ai/rendimiento/internal/github"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

type coderStore struct {
	runID     *int64 // the run the encargo is about
	runs      []*store.Run
	delivered string
	links     []string
	gaveBack  string
	spent     int64
	handover  string
	waited    []int64
	notes     []string
}

func (s *coderStore) EncargoForCoder(context.Context) (store.Encargo, error) {
	return store.Encargo{}, store.ErrNotFound
}
func (s *coderStore) EncargosOfSystemKey(context.Context, string) ([]store.Encargo, error) {
	return nil, nil
}
func (s *coderStore) SystemKey(_ context.Context, org int64, name string) (store.AgentKey, error) {
	return store.AgentKey{ID: 9, Name: name, OrgID: org}, nil
}
func (s *coderStore) TakeCargo(_ context.Context, key int64, purpose string) (store.Cargo, error) {
	return store.Cargo{ID: 3, KeyID: key, Purpose: purpose}, nil
}
func (s *coderStore) OpenCargo(context.Context, int64) (store.Cargo, error) {
	return store.Cargo{}, store.ErrNotFound
}
func (s *coderStore) HandOver(_ context.Context, _ int64, entrega, _ string) error {
	s.handover = entrega
	return nil
}
func (s *coderStore) TakeEncargo(_ context.Context, id, _, _ int64, _ string) (store.Encargo, error) {
	return store.Encargo{ID: id, OrgID: 1, App: "tienda", Title: "Saludar en español", Status: store.EncargoTaken, RunID: s.runID}, nil
}
func (s *coderStore) WaitEncargo(_ context.Context, _, _, run int64, _ string) (store.Encargo, error) {
	s.waited = append(s.waited, run)
	return store.Encargo{}, nil
}
func (s *coderStore) ResumeEncargo(context.Context, int64, int64, string, string) (store.Encargo, error) {
	return store.Encargo{}, nil
}
func (s *coderStore) DeliverEncargo(_ context.Context, _, _ int64, _, result string, links []string) (store.Encargo, error) {
	s.delivered, s.links = result, links
	return store.Encargo{}, nil
}
func (s *coderStore) GiveBackEncargo(_ context.Context, _, _ int64, _, note string) (store.Encargo, error) {
	s.gaveBack = note
	return store.Encargo{}, nil
}
func (s *coderStore) AddEncargoSpent(_ context.Context, _, micros int64) error {
	s.spent += micros
	return nil
}
func (s *coderStore) GetApp(_ context.Context, name string) (*store.App, error) {
	return &store.App{ID: 4, Name: name, OrgID: 1, Repo: "o/tienda", InstallationID: 7, DefaultBranch: "main"}, nil
}
func (s *coderStore) ListRuns(context.Context, int64, int) ([]*store.Run, error) { return s.runs, nil }
func (s *coderStore) GetRun(_ context.Context, id int64) (*store.Run, error) {
	for _, r := range s.runs {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, store.ErrNotFound
}
func (s *coderStore) NoteEncargoFix(_ context.Context, _ int64, _, note string) error {
	s.notes = append(s.notes, note)
	return nil
}
func (s *coderStore) GetDiagnosis(context.Context, int64) (store.Diagnosis, error) {
	return store.Diagnosis{Text: "La prueba espera «Hola»."}, nil
}

// repos is a repository in memory: each commit is kept, and the platform's
// CI answers with the next result for it.
type repos struct {
	files   map[string][]byte
	commits []map[string][]byte
	removed [][]string
	results []string
	st      *coderStore
	from    string // the sha the agent's branch was made from
	branch  string // the branch commits must go to ("": the encargo's own)
	// For merging: files at a commit, the files changed between two
	// commits ("base...head"), and the merge commits made (head+other).
	at      map[string]map[string][]byte
	changes map[string][]string
	merges  []string
}

func (r *repos) Files(context.Context, int64, string, string, int) (map[string][]byte, error) {
	out := map[string][]byte{}
	for k, v := range r.files {
		out[k] = v
	}
	return out, nil
}
func (r *repos) BranchSHA(_ context.Context, _ int64, _, branch string) (string, error) {
	return "head-of-" + branch, nil
}
func (r *repos) CreateBranch(_ context.Context, _ int64, _, _, sha string) error {
	r.from = sha
	return nil
}
func (r *repos) MergeBase(_ context.Context, _ int64, _, base, head string) (string, time.Time, error) {
	return "base-sha", time.Time{}, nil
}
func (r *repos) ChangedFiles(_ context.Context, _ int64, _, base, head string) ([]string, bool, error) {
	return r.changes[base+"..."+head], true, nil
}
func (r *repos) FileAt(_ context.Context, _ int64, _, p, ref string) ([]byte, error) {
	if f, ok := r.at[ref][p]; ok {
		return f, nil
	}
	return nil, &gh.APIError{Status: 404}
}
func (r *repos) CommitMerge(ctx context.Context, inst int64, repo, branch, head, other, msg string, files map[string][]byte, remove []string) (string, error) {
	r.merges = append(r.merges, head+"+"+other)
	return r.CommitFiles(ctx, inst, repo, branch, msg, files, remove)
}
func (r *repos) CommitFiles(_ context.Context, _ int64, _, branch, _ string, files map[string][]byte, remove []string) (string, error) {
	if r.branch != "" && branch != r.branch || r.branch == "" && !strings.HasSuffix(branch, "encargo-12-saludar-en-espanol") {
		return "", fmt.Errorf("committed to %s", branch)
	}
	r.commits, r.removed = append(r.commits, files), append(r.removed, remove)
	sha := fmt.Sprintf("sha%04d", len(r.commits))
	if n := len(r.commits); n <= len(r.results) {
		run := &store.Run{ID: int64(900 + n), SHA: sha, Status: r.results[n-1]}
		if run.Status == store.RunFailed {
			run.Steps = []store.Step{{ID: "web:test", Status: store.RunFailed, Message: "exited with code 1"}}
		}
		r.st.runs = append(r.st.runs, run)
	}
	return sha, nil
}

// script answers each turn with the next tool calls, and checks what came back.
type script struct {
	t     *testing.T
	turns [][]ia.ToolCall
	seen  []string // each tool result, in order
	cost  int64
	n     int
}

func call(name string, input any) ia.ToolCall {
	b, _ := json.Marshal(input)
	return ia.ToolCall{ID: "toolu_" + name, Name: name, Input: b}
}

func (s *script) CompleteTools(_ context.Context, _ int64, source string, req ia.ToolRequest) (ia.ToolResponse, error) {
	if source != CoderSource || req.Model != WorkModel || len(req.Tools) == 0 {
		s.t.Fatalf("asked %s with %s", source, req.Model)
	}
	last := req.Turns[len(req.Turns)-1]
	var results []ia.ToolResult
	if json.Unmarshal(last.Content, &results) == nil {
		for _, r := range results {
			if r.Type == "tool_result" {
				s.seen = append(s.seen, r.Content)
			}
		}
	}
	if s.n >= len(s.turns) {
		return ia.ToolResponse{Response: ia.Response{CostMicros: s.cost}, Content: json.RawMessage(`[]`), Stop: "end_turn"}, nil
	}
	calls := s.turns[s.n]
	s.n++
	content, _ := json.Marshal([]map[string]string{{"type": "text", "text": "sigo"}})
	return ia.ToolResponse{Response: ia.Response{CostMicros: s.cost}, Content: content, Calls: calls, Stop: "tool_use"}, nil
}

func newCoder(st *coderStore, r *repos, m *script) *Coder {
	return &Coder{Store: st, Repos: r, Model: m, BaseURL: "https://rendimiento.example.com", Lang: "es", Log: slog.Default(),
		Poll: time.Millisecond, Logs: func(context.Context, int64, string) (string, error) {
			return "--- FAIL: TestSaludo\n    saludo_test.go:9: got \"Hi\", want \"Hola\"\n", nil
		}}
}

// The coding agent searches, edits, commits and reads why the run failed,
// fixes it, and delivers once a run passed, with the branch and the run.
func TestCoderDelivers(t *testing.T) {
	st := &coderStore{}
	r := &repos{st: st, files: map[string][]byte{"AGENTS.md": []byte("Pruebas con go test."), "saludo.go": []byte("package tienda\n\nconst Saludo = \"Hi\"\n")},
		results: []string{store.RunFailed, store.RunSucceeded}}
	est := int64(400_000)
	m := &script{t: t, cost: 50_000, turns: [][]ia.ToolCall{
		{call("search", map[string]string{"pattern": "Saludo ="})},
		{call("deliver", map[string]string{"summary": "antes de tiempo"})},
		{call("write_file", map[string]string{"path": "saludo.go", "content": "package tienda\n\nconst Saludo = \"Hola\"\n"}), call("delete_file", map[string]string{"path": "AGENTS.md"})},
		{call("commit_and_test", map[string]string{"message": "Saludar en español"})},
		{call("edit_file", map[string]string{"path": "saludo.go", "old_text": "\"Hola\"", "new_text": "\"¡Hola!\""})},
		{call("commit_and_test", map[string]string{"message": "Con signos"})},
		{call("deliver", map[string]string{"summary": "Saludo en español."})},
	}}
	c := newCoder(st, r, m)
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda", EstimateMicros: &est}); err != nil {
		t.Fatal(err)
	}
	if len(m.seen) < 6 || !strings.Contains(m.seen[0], "saludo.go:3:") || !strings.Contains(m.seen[1], "no passing run") {
		t.Fatalf("results %q", m.seen)
	}
	if fail := m.seen[4]; !strings.Contains(fail, "run #901 failed") || !strings.Contains(fail, "want \"Hola\"") || !strings.Contains(fail, "La prueba espera") {
		t.Fatalf("the failure it read: %s", fail)
	}
	if len(r.commits) != 2 || string(r.commits[0]["saludo.go"]) != "package tienda\n\nconst Saludo = \"Hola\"\n" || r.removed[0][0] != "AGENTS.md" ||
		string(r.commits[1]["saludo.go"]) != "package tienda\n\nconst Saludo = \"¡Hola!\"\n" || len(r.removed[1]) != 0 {
		t.Fatalf("commits %q, removed %v", r.commits, r.removed)
	}
	if !strings.Contains(st.delivered, "Saludo en español.") || !strings.Contains(st.delivered, "Gastó $0.35") ||
		len(st.links) != 2 || st.links[0] != "https://github.com/o/tienda/pull/new/feature/encargo-12-saludar-en-espanol" || st.links[1] != "https://rendimiento.example.com/apps/tienda/runs/902" {
		t.Fatalf("delivered %q %v", st.delivered, st.links)
	}
	if st.spent != 350_000 || st.gaveBack != "" || len(st.waited) != 2 || !strings.Contains(st.handover, "lo entregó") {
		t.Fatalf("spent %d, gave back %q, waited %v, handover %q", st.spent, st.gaveBack, st.waited, st.handover)
	}
}

// At twice the estimate it stops: what it changed is committed on its
// branch, and the encargo goes to the person's own agent with a note.
func TestCoderStopsAtTwiceTheEstimate(t *testing.T) {
	st := &coderStore{}
	r := &repos{st: st, files: map[string][]byte{"saludo.go": []byte("const Saludo = \"Hi\"\n")}}
	est := int64(300_000)
	m := &script{t: t, cost: 250_000, turns: [][]ia.ToolCall{
		{call("edit_file", map[string]string{"path": "saludo.go", "old_text": "Hi", "new_text": "Hola"})},
		{call("read_file", map[string]string{"path": "saludo.go"})},
		{call("read_file", map[string]string{"path": "saludo.go"})},
	}}
	if err := newCoder(st, r, m).Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda", EstimateMicros: &est}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.gaveBack, "doble de lo estimado") || !strings.Contains(st.gaveBack, "encargo-12") || st.delivered != "" {
		t.Fatalf("gave back %q, delivered %q", st.gaveBack, st.delivered)
	}
	if len(r.commits) != 1 || string(r.commits[0]["saludo.go"]) != "const Saludo = \"Hola\"\n" || m.n != 3 {
		t.Fatalf("commits %q after %d turns", r.commits, m.n)
	}
}

// An encargo about a run that failed on a person's branch is worked on
// that branch itself (Ruta 130): no copy of it, and the link is its own
// pull request.
func TestCoderFixesTheFailedRunsBranchInPlace(t *testing.T) {
	run := int64(808)
	st := &coderStore{runID: &run, runs: []*store.Run{{ID: 808, AppID: 4, Branch: "propina-redonda", SHA: "abc", Status: store.RunFailed}}}
	r := &repos{st: st, branch: "propina-redonda", files: map[string][]byte{"saludo.go": []byte("const Saludo = \"Hi\"\n")}, results: []string{store.RunSucceeded}}
	m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("edit_file", map[string]string{"path": "saludo.go", "old_text": "Hi", "new_text": "Hola"})},
		{call("commit_and_test", map[string]string{"message": "Saludo"})},
		{call("deliver", map[string]string{"summary": "Listo."})},
	}}
	if err := newCoder(st, r, m).Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda", RunID: &run}); err != nil {
		t.Fatal(err)
	}
	if r.from != "" || len(r.commits) != 1 {
		t.Fatalf("made a branch from %q, %d commits; want none and one on propina-redonda", r.from, len(r.commits))
	}
	if len(st.links) == 0 || st.links[0] != "https://github.com/o/tienda/pull/new/propina-redonda" {
		t.Fatalf("links %v", st.links)
	}
}

// A failure on an encargo's own branch is fixed on that branch: no copy of
// it, and the link is its own pull request.
func TestCoderFixesAnEncargosBranchInPlace(t *testing.T) {
	run := int64(809)
	st := &coderStore{runID: &run, runs: []*store.Run{{ID: 809, AppID: 4, Branch: "encargo-12-saludar-en-espanol", SHA: "abc", Status: store.RunFailed}}}
	r := &repos{st: st, files: map[string][]byte{"saludo.go": []byte("const Saludo = \"Hi\"\n")}, results: []string{store.RunSucceeded}}
	m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("edit_file", map[string]string{"path": "saludo.go", "old_text": "Hi", "new_text": "Hola"})},
		{call("commit_and_test", map[string]string{"message": "Saludo"})},
		{call("deliver", map[string]string{"summary": "Listo."})},
	}}
	if err := newCoder(st, r, m).Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda", RunID: &run}); err != nil {
		t.Fatal(err)
	}
	if r.from != "" {
		t.Fatalf("made a branch from %q, want none", r.from)
	}
	if len(r.commits) != 1 {
		t.Fatalf("%d commits on the encargo's branch", len(r.commits))
	}
	if len(st.links) == 0 || st.links[0] != "https://github.com/o/tienda/pull/new/encargo-12-saludar-en-espanol" {
		t.Fatalf("links %v", st.links)
	}
}

// A conflict with the default branch is resolved on the branch itself
// without git: merge_default_branch brings in what only main changed and
// marks where both did; a commit with a mark left is refused; once
// resolved, the commit is a merge with main as its second parent.
func TestCoderMergesTheDefaultBranch(t *testing.T) {
	run := int64(810)
	st := &coderStore{runID: &run, runs: []*store.Run{{ID: 810, AppID: 4, Branch: "comentarios", SHA: "abc", Status: store.RunSucceeded}}}
	base := "import a\nimport b\n\nfunc main() {}\n"
	ours := "import a\nimport c\n\nfunc main() {}\n"
	theirs := "import a\nimport b, s\n\nfunc main() {}\n"
	r := &repos{st: st, branch: "comentarios", results: []string{store.RunSucceeded},
		files: map[string][]byte{"main.ts": []byte(ours)},
		at: map[string]map[string][]byte{
			"base-sha":     {"main.ts": []byte(base)},
			"head-of-main": {"main.ts": []byte(theirs), "nuevo.ts": []byte("export {}\n")},
		},
		changes: map[string][]string{"base-sha...head-of-main": {"main.ts", "nuevo.ts"}, "base-sha...head-of-comentarios": {"main.ts"}},
	}
	resolved := "import a\nimport b, s\nimport c\n\nfunc main() {}\n"
	m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("merge_default_branch", map[string]string{})},
		{call("commit_and_test", map[string]string{"message": "Merge main"})},
		{call("read_file", map[string]string{"path": "main.ts", "ref": "main"})},
		{call("write_file", map[string]string{"path": "main.ts", "content": resolved})},
		{call("commit_and_test", map[string]string{"message": "Merge main into comentarios"})},
		{call("deliver", map[string]string{"summary": "Conservé los dos imports."})},
	}}
	r.at["main"] = r.at["head-of-main"]
	if err := newCoder(st, r, m).Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda", RunID: &run}); err != nil {
		t.Fatal(err)
	}
	if len(m.seen) < 4 || !strings.Contains(m.seen[0], "CONFLICTS to resolve") || !strings.Contains(m.seen[0], "main.ts") || !strings.Contains(m.seen[0], "nuevo.ts") {
		t.Fatalf("the merge said %q", m.seen)
	}
	if !strings.Contains(m.seen[1], "conflict marks") || m.seen[2] != theirs {
		t.Fatalf("a commit with marks: %q; main's file: %q", m.seen[1], m.seen[2])
	}
	if len(r.merges) != 1 || r.merges[0] != "head-of-comentarios+head-of-main" || len(r.commits) != 1 ||
		string(r.commits[0]["main.ts"]) != resolved || string(r.commits[0]["nuevo.ts"]) != "export {}\n" {
		t.Fatalf("merges %v, commits %q", r.merges, r.commits)
	}
	if st.delivered == "" {
		t.Fatalf("not delivered: gave back %q", st.gaveBack)
	}
}

// A file left out of the branch's snapshot for its size (a package-lock.json
// over 256 KB) is still read and edited by its path, from the branch.
func TestCoderReadsAndEditsABigFile(t *testing.T) {
	run := int64(811)
	lock := `{"name":"web","packages":{"node_modules/react":{"version":"19.2.8"}}}` + strings.Repeat("\n", 300<<10)
	st := &coderStore{runID: &run, runs: []*store.Run{{ID: 811, AppID: 4, Branch: "renovate/react", SHA: "abc", Status: store.RunFailed}}}
	r := &repos{st: st, branch: "renovate/react", files: map[string][]byte{"package.json": []byte(`{"dependencies":{"react":"19.3.1"}}`)},
		at: map[string]map[string][]byte{"renovate/react": {"package-lock.json": []byte(lock)}}, results: []string{store.RunSucceeded}}
	m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("read_file", map[string]any{"path": "package-lock.json", "from_line": 1, "to_line": 1})},
		{call("edit_file", map[string]string{"path": "package-lock.json", "old_text": `"version":"19.2.8"`, "new_text": `"version":"19.3.1"`})},
		{call("read_file", map[string]string{"path": "no-such.json"})},
		{call("commit_and_test", map[string]string{"message": "Lockfile"})},
		{call("deliver", map[string]string{"summary": "Listo."})},
	}}
	if err := newCoder(st, r, m).Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda", RunID: &run}); err != nil {
		t.Fatal(err)
	}
	if len(m.seen) < 3 || !strings.Contains(m.seen[0], `"version":"19.2.8"`) || m.seen[1] != "edited package-lock.json" || !strings.Contains(m.seen[2], "no such file") {
		t.Fatalf("results %q", m.seen)
	}
	if len(r.commits) != 1 || !strings.Contains(string(r.commits[0]["package-lock.json"]), `"version":"19.3.1"`) {
		t.Fatalf("commits %d", len(r.commits))
	}
}

// fakeFixes is the app's declared fixes, run without a pod: candado writes
// package-lock.json from the pending package.json.
type fakeFixes struct {
	reqs []arreglos.Request
}

func (f *fakeFixes) Declared(context.Context, *store.App) ([]spec.Fix, error) {
	return []spec.Fix{{Name: "candado", Recipe: "npm", Path: "web"}}, nil
}

func (f *fakeFixes) Run(_ context.Context, req arreglos.Request) (arreglos.Result, error) {
	f.reqs = append(f.reqs, req)
	lock := []byte(`{"lockfileVersion":3,"from":` + fmt.Sprintf("%q", req.Pending["web/package.json"]) + `}`)
	return arreglos.Result{ID: 1, Status: store.FixSucceeded, Plan: arreglos.Plan{Name: "candado", Path: "web", Image: "node:20-bookworm-slim", ImageFrom: ".nvmrc"},
		Files: map[string][]byte{"web/package-lock.json": lock}, Written: []string{"web/package-lock.json"}, Took: 38 * time.Second}, nil
}

// The coding agent sees the app's declared fixes, runs one on its branch
// with what it changed, and the file it wrote goes in its next commit; the
// encargo's history says so.
func TestCoderRunsAFix(t *testing.T) {
	run := int64(812)
	st := &coderStore{runID: &run, runs: []*store.Run{{ID: 812, AppID: 4, Branch: "renovate/react", SHA: "abc", Status: store.RunFailed}}}
	r := &repos{st: st, branch: "renovate/react", files: map[string][]byte{"web/package.json": []byte(`{"dependencies":{"react":"19.2.0"}}`)},
		results: []string{store.RunSucceeded}}
	fixes := &fakeFixes{}
	m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("edit_file", map[string]string{"path": "web/package.json", "old_text": "19.2.0", "new_text": "19.3.1"})},
		{call("run_fix", map[string]string{"name": "otro"})},
		{call("run_fix", map[string]string{"name": "candado"})},
		{call("commit_and_test", map[string]string{"message": "React 19.3.1, with its lockfile"})},
		{call("deliver", map[string]string{"summary": "Listo."})},
	}}
	c := newCoder(st, r, m)
	c.Fixes = fixes
	var tools []string
	c.Model = toolsSeen{m, &tools}
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda", RunID: &run}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(tools, " "), "run_fix") {
		t.Fatalf("tools %v", tools)
	}
	if len(m.seen) < 3 || !strings.Contains(m.seen[1], "no such fix") || !strings.Contains(m.seen[2], "It wrote web/package-lock.json") {
		t.Fatalf("results %q", m.seen)
	}
	if len(fixes.reqs) != 1 || fixes.reqs[0].Branch != "renovate/react" || fixes.reqs[0].AskedKind != store.FixByAgent || fixes.reqs[0].Encargo != 12 ||
		!strings.Contains(string(fixes.reqs[0].Pending["web/package.json"]), "19.3.1") {
		t.Fatalf("asked %+v", fixes.reqs)
	}
	if len(r.commits) != 1 || !strings.Contains(string(r.commits[0]["web/package-lock.json"]), "19.3.1") || r.commits[0]["web/package.json"] == nil {
		t.Fatalf("commits %q", r.commits)
	}
	if len(st.notes) != 1 || !strings.Contains(st.notes[0], "run_fix candado") || !strings.Contains(st.notes[0], "38s") {
		t.Fatalf("history %q", st.notes)
	}
	if st.delivered == "" {
		t.Fatalf("not delivered: %q", st.gaveBack)
	}
}

// toolsSeen keeps the names of the tools the model was offered.
type toolsSeen struct {
	m     *script
	names *[]string
}

func (s toolsSeen) CompleteTools(ctx context.Context, org int64, source string, req ia.ToolRequest) (ia.ToolResponse, error) {
	if len(*s.names) == 0 {
		for _, t := range req.Tools {
			*s.names = append(*s.names, t.Name)
		}
	}
	return s.m.CompleteTools(ctx, org, source, req)
}
