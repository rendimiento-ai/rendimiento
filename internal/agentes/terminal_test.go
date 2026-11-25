package agentes

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// fakeSandbox answers each run with the next result, and keeps what it was
// asked and which sandboxes it closed.
type fakeSandbox struct {
	mu      sync.Mutex
	reqs    []pipeline.SandboxRun
	results []pipeline.SandboxResult
	closed  []string
	// clones are what each run's context carried for the clone token.
	clones []any
}

// cloneKey is what the test's Clone puts in the context, as the
// platform's ForClone puts the app's installation.
type cloneKey struct{}

func (f *fakeSandbox) Run(ctx context.Context, req pipeline.SandboxRun) (pipeline.SandboxResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clones = append(f.clones, ctx.Value(cloneKey{}))
	pending := map[string][]byte{}
	for p, b := range req.Pending {
		pending[p] = b
	}
	req.Pending = pending
	f.reqs = append(f.reqs, req)
	res := f.results[0]
	f.results = f.results[1:]
	res.Image, res.ImageFrom = req.Image, req.ImageFrom
	return res, nil
}

func (f *fakeSandbox) Close(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, key)
}

func (f *fakeSandbox) closedKeys() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.closed, " ")
}

// termStore is the organization's setting and the encargo's history.
type termStore struct {
	on    bool
	notes []string
}

func (s *termStore) AISettingsOf(_ context.Context, org int64) (store.AISettings, error) {
	return store.AISettings{OrgID: org, CodeRun: s.on}, nil
}
func (s *termStore) NoteEncargoRun(_ context.Context, _ int64, _, note string) error {
	s.notes = append(s.notes, note)
	return nil
}

// withSpec is the coder's store with an app whose rendimiento.yaml has a
// test in web.
type withSpec struct{ *coderStore }

func (s withSpec) GetApp(ctx context.Context, name string) (*store.App, error) {
	a, err := s.coderStore.GetApp(ctx, name)
	if a != nil {
		a.Spec = spec.Spec{Services: []spec.Service{{Name: "web", Path: "web", Test: &spec.Test{Image: "node:22-bookworm-slim", Command: "npm test"}}}}
	}
	return a, err
}

// promptSeen keeps the first system prompt and tools the model got.
type promptSeen struct {
	m      *script
	system *string
	tools  *[]string
}

func (p promptSeen) CompleteTools(ctx context.Context, org int64, source string, req ia.ToolRequest) (ia.ToolResponse, error) {
	if *p.system == "" {
		*p.system = req.System
		for _, t := range req.Tools {
			*p.tools = append(*p.tools, t.Name)
		}
	}
	return p.m.CompleteTools(ctx, org, source, req)
}

// The coding agent runs a command in its sandbox with its changes: it reads
// the exit code and the output, the files the command changed become its
// changes (not a binary one) and go in its commit, the encargo's history
// says what it ran, and the sandbox goes when the job ends.
func TestCoderRunsCommands(t *testing.T) {
	st := &coderStore{}
	r := &repos{st: st, files: map[string][]byte{"web/hola.ts": []byte("export const hola = 'Hi'\n"), "web/viejo.txt": []byte("x\n")},
		results: []string{store.RunSucceeded}}
	box := &fakeSandbox{results: []pipeline.SandboxResult{{
		ExitCode: 1, Took: 12 * time.Second, Stdout: "1 failing\n\x1b[31mexpected 'Hola'\x1b[0m\n", StdoutBytes: 9000, Stderr: "npm ERR! test failed\n", StderrBytes: 21,
		Fresh: true, Changes: []pipeline.FixChange{{Path: "web/__snapshots__/hola.snap", Data: []byte("Hola\n")}, {Path: "web/viejo.txt", Deleted: true},
			{Path: "web/logo.png", Data: []byte("\x89PNG\x00\x01")}},
	}}}
	terms := &termStore{on: true}
	m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
		{call("edit_file", map[string]string{"path": "web/hola.ts", "old_text": "Hi", "new_text": "Hola"})},
		{call("run", map[string]string{"command": "npm ci && npm test", "folder": "web"})},
		{call("run", map[string]string{"command": "  "})},
		{call("commit_and_test", map[string]string{"message": "Hola"})},
		{call("deliver", map[string]string{"summary": "Listo."})},
	}}
	c := newCoder(st, r, m)
	c.Store = withSpec{st}
	c.Terminal = &Terminal{Sandbox: box, Store: terms, Clone: func(ctx context.Context, app *store.App) (context.Context, string) {
		return context.WithValue(ctx, cloneKey{}, app.InstallationID), "https://gitlab.example.com/o/tienda.git"
	}}
	var system string
	var tools []string
	c.Model = promptSeen{m, &system, &tools}
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(tools, " "), "run") || !strings.Contains(system, "You can run code") || !strings.Contains(system, "run the tests touching your change") ||
		strings.Contains(system, "You cannot run code") {
		t.Fatalf("tools %v, system %s", tools, system)
	}
	if len(box.reqs) != 1 {
		t.Fatalf("%d runs", len(box.reqs))
	}
	req := box.reqs[0]
	if req.Key != "encargo-12" || req.Folder != "web" || req.Command != "npm ci && npm test" || req.Image != "node:22-bookworm-slim" ||
		!strings.Contains(req.ImageFrom, "service web") || string(req.Pending["web/hola.ts"]) != "export const hola = 'Hola'\n" ||
		!strings.HasSuffix(req.Src.Branch, "encargo-12-saludar-en-espanol") {
		t.Fatalf("asked %+v", req)
	}
	// It clones as a run's steps do (Ruta 178: a private repository refused
	// a clone without the token): with the app's installation in the
	// context, from its forja's address, at the commit its branch started
	// from (it has committed nothing yet).
	if req.Src.Repo != "https://gitlab.example.com/o/tienda.git" || box.clones[0] != int64(7) || req.Src.SHA != "head-of-main" {
		t.Fatalf("clones %s at %q with %v", req.Src.Repo, req.Src.SHA, box.clones)
	}
	out := m.seen[1]
	for _, want := range []string{"$ npm ci && npm test", "exit code 1, in 12s", "the last", "1 failing", "expected 'Hola'", "npm ERR! test failed",
		"web/__snapshots__/hola.snap", "deleted among your changes: web/viejo.txt", "not brought back", "web/logo.png", "a new sandbox"} {
		if !strings.Contains(out, want) {
			t.Errorf("the run's answer lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("color codes reached the model")
	}
	if !strings.Contains(m.seen[2], "give a command") {
		t.Fatalf("an empty command: %q", m.seen[2])
	}
	if len(r.commits) != 1 || string(r.commits[0]["web/__snapshots__/hola.snap"]) != "Hola\n" || r.commits[0]["web/logo.png"] != nil ||
		len(r.removed[0]) != 1 || r.removed[0][0] != "web/viejo.txt" {
		t.Fatalf("commits %q, removed %v", r.commits, r.removed)
	}
	if len(terms.notes) != 1 || !strings.Contains(terms.notes[0], "npm ci && npm test") || !strings.Contains(terms.notes[0], "salió con 1") || !strings.Contains(terms.notes[0], "12s") {
		t.Fatalf("history %q", terms.notes)
	}
	if !strings.Contains(strings.Join(st.doing, " "), "run:npm ci && npm test") {
		t.Fatalf("doing %v", st.doing)
	}
	if st.delivered == "" {
		t.Fatalf("not delivered: %q", st.gaveBack)
	}
	for i := 0; i < 200 && box.closedKeys() == ""; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if box.closedKeys() != "encargo-12" {
		t.Fatalf("closed %q: the sandbox stayed after the job", box.closedKeys())
	}
}

// With the setting off (or no sandbox on the platform) run is not offered,
// the prompt says it cannot run code, and a call to it anyway is refused.
func TestCoderWithoutTerminal(t *testing.T) {
	for _, terminal := range []*Terminal{nil, {Sandbox: &fakeSandbox{}, Store: &termStore{on: false}}} {
		st := &coderStore{}
		r := &repos{st: st, files: map[string][]byte{"saludo.go": []byte("const Saludo = \"Hi\"\n")}}
		m := &script{t: t, cost: 10_000, turns: [][]ia.ToolCall{
			{call("run", map[string]string{"command": "go test ./..."})},
			{call("give_up", map[string]string{"reason": "nada"})},
		}}
		c := newCoder(st, r, m)
		c.Terminal = terminal
		var system string
		var tools []string
		c.Model = promptSeen{m, &system, &tools}
		if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda"}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(" "+strings.Join(tools, " ")+" ", " run ") || !strings.Contains(system, "You cannot run code") {
			t.Fatalf("tools %v, system %s", tools, system)
		}
		if !strings.Contains(m.seen[0], "turned off") {
			t.Fatalf("run without the tool: %q", m.seen[0])
		}
		if terminal != nil && len(terminal.Sandbox.(*fakeSandbox).reqs) != 0 {
			t.Fatal("it ran with the setting off")
		}
	}
}

// A command runs in the image of the test (or build task) whose folder
// holds it most closely; with none, the sandbox's general image.
func TestSandboxImage(t *testing.T) {
	sp := spec.Spec{
		Services: []spec.Service{{Name: "api", Path: ".", Test: &spec.Test{Image: "golang:1.27"}}, {Name: "web", Path: "web", Test: &spec.Test{Image: "node:22"}}, {Name: "db", Image: "postgres:17"}},
		Builds:   []spec.ImageBuild{{Name: "app", Path: "app", Test: &spec.Test{Image: "rust:1"}}},
		Tasks:    []spec.Task{{Name: "e2e", Path: "web/e2e", Image: "playwright"}, {Name: "migrar", Stage: "pre-deploy", Path: "db", Image: "migrate"}},
	}
	for folder, want := range map[string]string{".": "golang:1.27", "internal/x": "golang:1.27", "web": "node:22", "web/src": "node:22", "web/e2e": "playwright",
		"webby": "golang:1.27", "app/src": "rust:1", "db": "golang:1.27"} {
		if got, _ := sandboxImage(sp, folder); got != want {
			t.Errorf("%s: %s, want %s", folder, got, want)
		}
	}
	if got, from := sandboxImage(spec.Spec{}, "web"); got != "" || from != "" {
		t.Errorf("no tests: %s %s", got, from)
	}
}
