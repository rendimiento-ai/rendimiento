package platform

import (
	"context"
	"encoding/json"
	"errors"

	"fmt"
	"github.com/go-logr/logr"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/controller"
	"github.com/rendimiento-ai/rendimiento/internal/dns"
	"github.com/rendimiento-ai/rendimiento/internal/destino"
	"github.com/rendimiento-ai/rendimiento/internal/events"
	"github.com/rendimiento-ai/rendimiento/internal/generate"
	gh "github.com/rendimiento-ai/rendimiento/internal/github"
	"github.com/rendimiento-ai/rendimiento/internal/notify"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/spec/ejemplos"
	"github.com/rendimiento-ai/rendimiento/internal/store"
	"github.com/rendimiento-ai/rendimiento/internal/testenv"
)

// fakeGitHub holds one repo whose files change per ref.
type fakeGitHub struct {
	mu      sync.Mutex
	files   map[string]fstest.MapFS // ref → tree
	prs     []map[string]string
	checks  map[int64]gh.CheckRun
	changed map[string][]string // "base...head" → changed files
	pulls   []gh.PullInfo       // the open pull requests, with whether each can be merged
	merged  []int
	deleted []string          // the branches deleted after their merge
	heads   map[string]string // branch → its commit, for a pull request opened on approval
	opened  []string          // "branch: title" of each pull request opened on approval
	base    string            // what MergeBase answers
	merges  []map[string][]byte
	removed [][]string
}

func (f *fakeGitHub) tree(ref string) fstest.MapFS {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.files[ref]; ok {
		return t
	}
	return f.files["main"]
}
func (f *fakeGitHub) FileAt(_ context.Context, _ int64, _, path, ref string) ([]byte, error) {
	b, err := fs.ReadFile(f.tree(ref), path)
	if err != nil {
		return nil, &gh.APIError{Status: 404}
	}
	return b, nil
}
func (f *fakeGitHub) RepoFS(_ context.Context, _ int64, _, ref string) (fs.FS, error) {
	return f.tree(ref), nil
}
func (f *fakeGitHub) OpenPR(_ context.Context, _ int64, _, _, _, _, _ string, files map[string]string) (*gh.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prs = append(f.prs, files)
	// "Merge" it: the files land on main.
	for p, c := range files {
		f.files["main"][p] = &fstest.MapFile{Data: []byte(c)}
	}
	return &gh.PullRequest{Number: 1, HTMLURL: "https://github.com/p0dxD/hello/pull/1"}, nil
}
func (f *fakeGitHub) CreateCheck(_ context.Context, _ int64, _ string, cr gh.CheckRun) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := int64(len(f.checks) + 1)
	f.checks[id] = cr
	return id, nil
}
func (f *fakeGitHub) UpdateCheck(_ context.Context, _ int64, _ string, id int64, cr gh.CheckRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks[id] = cr
	return nil
}
func (f *fakeGitHub) CloneToken(context.Context, int64) (string, error) { return "ghs_x", nil }
func (f *fakeGitHub) RecentPullRequests(context.Context, int64, string) ([]gh.PullInfo, error) {
	return nil, nil
}
func (f *fakeGitHub) OpenPulls(context.Context, int64, string) ([]gh.PullInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]gh.PullInfo(nil), f.pulls...), nil
}
func (f *fakeGitHub) Pull(_ context.Context, _ int64, _ string, number int) (*gh.PullInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, pr := range f.pulls {
		if pr.Number == number {
			return &pr, nil
		}
	}
	return nil, &gh.APIError{Status: 404}
}
func (f *fakeGitHub) MergePull(_ context.Context, _ int64, _ string, number int, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merged = append(f.merged, number)
	return nil
}
func (f *fakeGitHub) CreatePull(_ context.Context, _ int64, _, base, branch, title, _ string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, pr := range f.pulls {
		if pr.Head.Ref == branch {
			return pr.Number, nil
		}
	}
	yes := true
	pr := gh.PullInfo{Number: 100 + len(f.opened), State: "open", Mergeable: &yes, MergeableState: "clean"}
	pr.Head.Ref, pr.Head.SHA, pr.Base.Ref = branch, f.heads[branch], base
	f.pulls = append(f.pulls, pr)
	f.opened = append(f.opened, branch+": "+title)
	return pr.Number, nil
}
func (f *fakeGitHub) CommitSubject(context.Context, int64, string, string) (string, error) {
	return "What the commit says", nil
}
func (f *fakeGitHub) DeleteBranch(_ context.Context, _ int64, _, branch string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, branch)
	return nil
}
func (f *fakeGitHub) MergeBase(context.Context, int64, string, string, string) (string, time.Time, error) {
	return f.base, time.Time{}, nil
}
func (f *fakeGitHub) FirstParent(context.Context, int64, string, string) (string, error) { return "", nil }
func (f *fakeGitHub) CommitMerge(_ context.Context, _ int64, _, _, _, _, _ string, files map[string][]byte, remove []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merges, f.removed = append(f.merges, files), append(f.removed, remove)
	return "4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d", nil
}
func (f *fakeGitHub) BranchSHA(context.Context, int64, string, string) (string, error) {
	return strings.Repeat("9", 40), nil
}
func (f *fakeGitHub) ChangedFiles(_ context.Context, _ int64, _, base, head string) ([]string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	files, ok := f.changed[base+"..."+head]
	if !ok {
		return nil, false, nil // unknown diff: treat as incomplete
	}
	return files, true, nil
}

// fakeExec "builds" by returning a digest derived from the commit.
type fakeExec struct {
	failSHA   string
	failSteps map[string]bool // step IDs that fail
	tokens    []string
	ran       []string // step IDs executed
	p         *Platform
	mu        sync.Mutex
}

func (e *fakeExec) Execute(ctx context.Context, _ string, src pipeline.Source, s pipeline.Step, w io.Writer) pipeline.StepResult {
	tok, _ := e.p.CloneToken(ctx, src.Repo)
	e.mu.Lock()
	e.tokens = append(e.tokens, tok)
	e.ran = append(e.ran, s.ID)
	e.mu.Unlock()
	io.WriteString(w, "running "+s.ID+"\n")
	if src.SHA == e.failSHA && s.Kind == pipeline.KindTest {
		return pipeline.StepResult{Status: pipeline.StatusFailed, Message: "tests failed"}
	}
	if e.failSteps[s.ID] {
		return pipeline.StepResult{Status: pipeline.StatusFailed, Message: "task failed"}
	}
	return pipeline.StepResult{Status: pipeline.StatusSucceeded, Digest: "sha256:" + src.SHA[:8]}
}

func setup(t *testing.T) (*Platform, *fakeGitHub, *fakeExec, client.Client) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctrl.SetLogger(logr.Discard())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := resetDB(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	cfg := kubeConfig(t)
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	skip := true
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme, Metrics: metricsserver.Options{BindAddress: "0"}, Controller: config.Controller{SkipNameValidation: &skip}})
	if err != nil {
		t.Fatal(err)
	}
	if err := (&controller.AppReconciler{Client: mgr.GetClient(), Scheme: scheme, Render: render.DefaultOptions(), DNS: dns.Noop{}}).SetupWithManager(mgr); err != nil {
		t.Fatal(err)
	}
	go func() { _ = mgr.Start(ctx) }()
	kube, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}

	gh := &fakeGitHub{checks: map[int64]gh.CheckRun{}, files: map[string]fstest.MapFS{
		"main": {"go.mod": {Data: []byte("module hello\ngo 1.23\n")}, "main.go": {Data: []byte("package main")}},
	}}
	p := &Platform{
		Store: st, Kube: kube, GitHub: gh, Generator: generate.Templates{}, Hub: events.NewHub(),
		Config: Config{Registry: "registry.example.lan:5000", BaseURL: "https://rendimiento.example.com", Zone: "example.com"},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	exec := &fakeExec{p: p}
	p.Runner = pipeline.NewRunner(exec, p, 2)
	go p.Work(ctx)
	return p, gh, exec, kube
}

// The package's tests share one envtest: starting an API server and etcd
// took about half of each test on the cluster's workers (the package ran
// 409 s for 7 tests). Each test still gets its own manager, Platform and
// freshly migrated database; the Kubernetes objects of earlier tests stay,
// so every test that calls setup uses its own app names (hello, secplus,
// shop, cambios, tareas, invalida, verifica, predespliegue, version).
var (
	sharedEnv     *envtest.Environment
	sharedCfg     *rest.Config
	sharedEnvErr  error
	sharedEnvOnce sync.Once
)

// kubeConfig starts the shared envtest the first time a test needs it, or
// skips (fails, when KUBEBUILDER_ASSETS is set) if it cannot start.
func kubeConfig(t *testing.T) *rest.Config {
	t.Helper()
	sharedEnvOnce.Do(func() {
		sharedEnv = testenv.New(filepath.Join("..", "..", "deploy", "crds"))
		sharedCfg, sharedEnvErr = sharedEnv.Start()
	})
	if sharedEnvErr != nil {
		if os.Getenv("KUBEBUILDER_ASSETS") != "" {
			// Configured but failed to start (e.g. an overloaded machine):
			// fail, so a test gate never passes with these tests skipped.
			t.Fatalf("envtest failed to start: %v", sharedEnvErr)
		}
		t.Skipf("envtest unavailable: %v", sharedEnvErr)
	}
	return sharedCfg
}

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedEnv != nil && sharedCfg != nil {
		_ = sharedEnv.Stop()
	}
	os.Exit(code)
}

func resetDB(ctx context.Context, dsn string) error {
	st, err := store.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer st.Close()
	return st.ResetForTests(ctx)
}

// testWait is how long a test waits for a run or a condition. A run on a
// busy worker (the controller's envtest running beside it) has taken more
// than 20 seconds; a passing test returns as soon as it is done.
const testWait = 60 * time.Second

func waitRun(t *testing.T, p *Platform, id int64) *store.Run {
	t.Helper()
	deadline := time.Now().Add(testWait)
	for time.Now().Before(deadline) {
		r, err := p.Store.GetRun(context.Background(), id)
		if err == nil && r.FinishedAt != nil {
			return r
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("run %d did not finish", id)
	return nil
}

func TestOnboardPushDeployRollback(t *testing.T) {
	p, fake, exec, kube := setup(t)
	ctx := context.Background()
	sha1 := strings.Repeat("a", 40)
	sha2 := strings.Repeat("b", 40)
	sha3 := strings.Repeat("c", 40)

	// 1. Wizard: detect and propose.
	prop, err := p.Propose(ctx, 7, "p0dxD/hello", "main")
	if err != nil {
		t.Fatal(err)
	}
	if prop.Existing || prop.Spec.Services[0].Domain != "hello.example.com" || prop.Files["Dockerfile"] == "" {
		t.Fatalf("proposal = %+v", prop)
	}

	// 2. Confirm: app registered, App object waiting, PR opened.
	res, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/hello", DefaultBranch: "main", Name: "hello", Spec: prop.Spec, Files: prop.Files}, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.PR == nil || !strings.Contains(fake.prs[0]["rendimiento.yaml"], "hello.example.com") {
		t.Fatalf("PR files = %v", fake.prs)
	}
	if _, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "x/y", DefaultBranch: "main", Name: "hello", Spec: prop.Spec}, false); err == nil {
		t.Fatal("duplicate app name accepted")
	}

	// 3. PR merged → push to main → build → release #1 → App object points at it.
	fake.files[sha1] = fake.files["main"]
	runs, err := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "P0DXD/hello", Branch: "main", SHA: sha1})
	if err != nil || len(runs) != 1 || !runs[0].Deploy {
		t.Fatalf("runs = %+v %v", runs, err)
	}
	if r := waitRun(t, p, runs[0].ID); r.Status != store.RunSucceeded || r.Message != "released #1" {
		t.Fatalf("run = %+v", r)
	}
	var cr v1alpha1.App
	if err := kube.Get(ctx, client.ObjectKey{Name: "hello"}, &cr); err != nil {
		t.Fatal(err)
	}
	if cr.Spec.Release != 1 || cr.Spec.Images["hello"] != "registry.example.lan:5000/hello-hello@sha256:aaaaaaaa" {
		t.Fatalf("App spec = %+v", cr.Spec)
	}
	eventually(t, "deployment from release 1", func() bool {
		var d appsv1.Deployment
		return kube.Get(ctx, client.ObjectKey{Namespace: "hello", Name: "hello"}, &d) == nil &&
			strings.HasSuffix(d.Spec.Template.Spec.Containers[0].Image, "@sha256:aaaaaaaa")
	})
	if log, _, _ := p.Store.StepLog(ctx, runs[0].ID, "hello:build"); log != "running hello:build\n" {
		t.Fatalf("log = %q", log)
	}
	if c := fake.checks[1]; c.Conclusion != "success" {
		t.Fatalf("check = %+v", c)
	}
	if exec.tokens[0] != "ghs_x" {
		t.Fatal("clone token not passed to executor")
	}

	// 4. Feature branch push: builds, never deploys.
	fake.files[sha2] = fake.files["main"]
	runs, _ = p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/hello", Branch: "feature", SHA: sha2})
	if r := waitRun(t, p, runs[0].ID); r.Deploy || r.Status != store.RunSucceeded {
		t.Fatalf("branch run = %+v", r)
	}
	_ = kube.Get(ctx, client.ObjectKey{Name: "hello"}, &cr)
	if cr.Spec.Release != 1 {
		t.Fatal("branch push deployed")
	}

	// 5. Release #2, then a failing commit that must not deploy.
	fake.files[sha2] = fake.files["main"]
	runs, _ = p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/hello", Branch: "main", SHA: sha2})
	waitRun(t, p, runs[0].ID)
	exec.failSHA = sha3
	// Add a test step so the failure is in "test".
	fake.files[sha3] = fstest.MapFS{"rendimiento.yaml": {Data: []byte("services:\n  - name: hello\n    domain: hello.example.com\n    test: {image: golang:1.23, command: go test ./...}\n")}}
	runs, _ = p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/hello", Branch: "main", SHA: sha3})
	r := waitRun(t, p, runs[0].ID)
	// The build runs beside the test; the run (named after the test) fails
	// and nothing it built is released.
	if r.Status != store.RunFailed || !strings.Contains(r.Message, "hello:test") {
		t.Fatalf("failing run = %+v", r)
	}
	_ = kube.Get(ctx, client.ObjectKey{Name: "hello"}, &cr)
	if cr.Spec.Release != 2 {
		t.Fatalf("release = %d, failed run must not deploy", cr.Spec.Release)
	}

	// 6. One-click rollback to #1 creates release #3 with #1's images.
	rel, err := p.Rollback(ctx, res.App, 1)
	if err != nil || rel.Number != 3 || *rel.RollbackOf != 1 {
		t.Fatalf("rollback = %+v %v", rel, err)
	}
	eventually(t, "rolled back deployment", func() bool {
		var d appsv1.Deployment
		return kube.Get(ctx, client.ObjectKey{Namespace: "hello", Name: "hello"}, &d) == nil &&
			strings.HasSuffix(d.Spec.Template.Spec.Containers[0].Image, "@sha256:aaaaaaaa")
	})

	// 7. A branch made from a commit the default branch already ran (an
	// agent's new branch) does not run it again; after a failure it does,
	// and a commit that says [skip ci] does not run.
	if runs, err := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/hello", Branch: "encargo-7", SHA: sha2}); err != nil || len(runs) != 0 {
		t.Fatalf("branch at a commit main ran: %v %v", runs, err)
	}
	runs, _ = p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/hello", Branch: "encargo-8", SHA: sha3})
	if len(runs) != 1 {
		t.Fatalf("branch at a commit that failed on main: %v", runs)
	}
	waitRun(t, p, runs[0].ID)
	if runs, err := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/hello", Branch: "encargo-9", SHA: "9999999999999999999999999999999999999999", SkipCI: true}); err != nil || len(runs) != 0 {
		t.Fatalf("[skip ci] push: %v %v", runs, err)
	}

	// 8. Pushes to repos that are not onboarded are ignored.
	if runs, err := p.HandlePush(ctx, PushEvent{Repo: "p0dxD/other", Branch: "main", SHA: sha1}); err != nil || len(runs) != 0 {
		t.Fatalf("unknown repo: %v %v", runs, err)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testWait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A repo with no code, only rendimiento.yaml with a ready-made image (p0dxD/ai,
// Ollama): the wizard got "detected": null and crashed on it.
func TestProposeRepoWithoutCode(t *testing.T) {
	p, fake, _, _ := setup(t)
	fake.files["image-only"] = fstest.MapFS{
		"rendimiento.yaml": {Data: []byte("services:\n  - name: ollama\n    image: ollama/ollama:0.40.6\n    port: 11434\n")},
		"README.md":        {Data: []byte("# ai\n")},
	}
	prop, err := p.Propose(context.Background(), 7, "p0dxD/ai", "image-only")
	if err != nil {
		t.Fatal(err)
	}
	if !prop.Existing || len(prop.Spec.Services) != 1 || prop.Spec.Services[0].Image != "ollama/ollama:0.40.6" {
		t.Fatalf("proposal = %+v", prop)
	}
	raw, _ := json.Marshal(prop)
	if !strings.Contains(string(raw), `"detected":[]`) {
		t.Fatalf("detected must be an empty list: %s", raw)
	}
}

func TestProposeDetectsExistingDeployment(t *testing.T) {
	p, _, _, kube := setup(t)
	ctx := context.Background()
	one := int32(1)
	lbl := map[string]string{"app": "secplus"}
	for _, o := range []client.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "secplus"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "secplus", Name: "secplus",
			Annotations: map[string]string{"argocd.argoproj.io/tracking-id": "secplus:apps/Deployment:secplus/secplus"}},
			Spec: appsv1.DeploymentSpec{Replicas: &one, Selector: &metav1.LabelSelector{MatchLabels: lbl},
				Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: lbl}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "registry.example.lan:5000/secplus:1.0.8"}}}}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "secplus", Name: "secplus-practice"}, Type: corev1.SecretTypeOpaque},
	} {
		if err := kube.Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	prop, err := p.Propose(ctx, 7, "p0dxD/secplus", "main")
	if err != nil {
		t.Fatal(err)
	}
	m := prop.Migration
	if m == nil || m.ArgoCD != "secplus" || m.Managed || len(m.Deployments) != 1 || !strings.Contains(m.Deployments[0], "secplus:1.0.8") || m.Secrets[0] != "secplus-practice" {
		t.Fatalf("migration = %+v", m)
	}
	// A new repo has no namespace yet.
	if prop, _ := p.Propose(ctx, 7, "p0dxD/brand-new", "main"); prop.Migration != nil {
		t.Fatalf("unexpected migration %+v", prop.Migration)
	}

	// Onboarding with adopt passes consent to the App object.
	if _, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/secplus", DefaultBranch: "main", Name: "secplus",
		Spec: prop.Spec, Files: prop.Files, Adopt: true}, false); err != nil {
		t.Fatal(err)
	}
	var cr v1alpha1.App
	if err := kube.Get(ctx, client.ObjectKey{Name: "secplus"}, &cr); err != nil || !cr.Spec.Adopt {
		t.Fatalf("App adopt = %v, %v", cr.Spec.Adopt, err)
	}
}

// Only what changed since the last release is tested, built and rolled.
func TestChangeDetection(t *testing.T) {
	p, fake, exec, kube := setup(t)
	ctx := context.Background()
	yaml := `services:
  - name: ui
    path: ui
    test: {image: node:20, command: npm test}
  - name: api
    path: api
    watch: [shared]
  - name: db
    image: postgres:15-alpine
jobs:
  - name: scraper
    schedule: "@daily"
    path: congress
`
	sha := func(c byte) string { return strings.Repeat(string(c), 40) }
	tree := fstest.MapFS{"rendimiento.yaml": {Data: []byte(yaml)}}
	for _, c := range "abcdef9" {
		fake.files[sha(byte(c))] = tree
	}
	fake.files["main"] = tree
	fake.changed = map[string][]string{
		sha('a') + "..." + sha('b'): {"ui/src/App.tsx"},
		sha('b') + "..." + sha('c'): {"shared/util.py", "README.md"},
		sha('c') + "..." + sha('d'): {"rendimiento.yaml"},
	}
	sp, _ := spec.Parse([]byte(yaml))
	if _, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/cambios", DefaultBranch: "main", Name: "cambios", Spec: *sp}, false); err != nil {
		t.Fatal(err)
	}
	push := func(c byte) (*store.Run, []string) {
		t.Helper()
		exec.mu.Lock()
		exec.ran = nil
		exec.mu.Unlock()
		runs, err := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/cambios", Branch: "main", SHA: sha(c)})
		if err != nil || len(runs) != 1 {
			t.Fatalf("push: %v %v", runs, err)
		}
		r := waitRun(t, p, runs[0].ID)
		exec.mu.Lock()
		defer exec.mu.Unlock()
		ran := append([]string(nil), exec.ran...)
		sort.Strings(ran)
		return r, ran
	}
	images := func() map[string]string {
		var cr v1alpha1.App
		_ = kube.Get(ctx, client.ObjectKey{Name: "cambios"}, &cr)
		return cr.Spec.Images
	}

	// 1. First release: everything builds.
	r, ran := push('a')
	if r.Status != store.RunSucceeded || strings.Join(ran, ",") != "api:build,job-scraper:build,ui:build,ui:test" {
		t.Fatalf("first release ran %v (%s %s)", ran, r.Status, r.Message)
	}
	first := images()

	// 2. UI-only change: only the UI is tested and built; the rest is reused.
	r, ran = push('b')
	if r.Status != store.RunSucceeded || strings.Join(ran, ",") != "ui:build,ui:test" {
		t.Fatalf("ui-only push ran %v (%s %s)", ran, r.Status, r.Message)
	}
	second := images()
	if second["api"] != first["api"] || second["job:scraper"] != first["job:scraper"] || second["ui"] == first["ui"] || second["db"] != "postgres:15-alpine" {
		t.Fatalf("images after ui-only push: %v (before %v)", second, first)
	}
	for _, st := range r.Steps {
		if st.Service == "api" && (st.Status != "reused" || !strings.Contains(st.Message, "release #1")) {
			t.Fatalf("api step = %+v", st)
		}
	}

	// 3. A change in a watched path rebuilds that service.
	if _, ran = push('c'); strings.Join(ran, ",") != "api:build" {
		t.Fatalf("shared/ change ran %v", ran)
	}
	// 4. rendimiento.yaml changed: everything builds.
	if _, ran = push('d'); len(ran) != 4 {
		t.Fatalf("rendimiento.yaml change ran %v", ran)
	}
	// 5. Unknown or truncated diff: everything builds.
	if _, ran = push('e'); len(ran) != 4 {
		t.Fatalf("unknown diff ran %v", ran)
	}
	// 6. Manual "Build now": everything builds.
	app, _ := p.Store.GetApp(ctx, "cambios")
	run, err := p.QueueRun(ctx, app, "main", "", "manual")
	if err != nil {
		t.Fatal(err)
	}
	exec.mu.Lock()
	exec.ran = nil
	exec.mu.Unlock()
	waitRun(t, p, run.ID)
	exec.mu.Lock()
	n := len(exec.ran)
	exec.mu.Unlock()
	if n != 4 {
		t.Fatalf("manual build ran %d steps", n)
	}
}

// Every example is accepted by the App object's real schema (envtest with
// the CRDs): created, and given a release when something of it runs; one
// with nothing to run gets no App object.
func TestEjemplosEnElEsquema(t *testing.T) {
	_, _, _, kube := setup(t)
	ctx := context.Background()
	todos, err := ejemplos.Todos()
	if err != nil {
		t.Fatal(err)
	}
	k := destino.Kubernetes{Kube: kube}
	for i, e := range todos {
		name := "ejemplo-" + e.Nombre
		app := &store.App{ID: int64(9000 + i), Name: name, Repo: "p0dxD/" + name, Spec: *e.Spec}
		if err := k.Crear(ctx, app, false); err != nil {
			t.Errorf("%s: create: %v", e.Nombre, err)
			continue
		}
		var cr v1alpha1.App
		err := kube.Get(ctx, client.ObjectKey{Name: name}, &cr)
		if !e.Spec.Runs() {
			if !apierrors.IsNotFound(err) {
				t.Errorf("%s: nothing runs, but it got an App object (%v)", e.Nombre, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", e.Nombre, err)
			continue
		}
		images := map[string]string{}
		for _, s := range e.Spec.Services {
			images[s.Name] = "registry.example.lan:5000/" + name + "-" + s.Name + "@sha256:1"
		}
		if err := k.Aplicar(ctx, name, &store.Release{Number: 1, Spec: *e.Spec, Images: images}); err != nil {
			t.Errorf("%s: release: %v", e.Nombre, err)
		}
	}
}

// A library has only tasks: it is onboarded without an App object in the
// cluster (whose schema needs a service), its runs test it, and a run on
// its default branch succeeds without a release.
func TestLibrary(t *testing.T) {
	p, fake, _, kube := setup(t)
	ctx := context.Background()
	yaml := "tasks:\n  - name: test\n    image: node:20\n    command: npm test\n    when: always\n"
	fake.files["main"] = fstest.MapFS{"rendimiento.yaml": {Data: []byte(yaml)}}
	sha := strings.Repeat("d", 40)
	fake.files[sha] = fake.files["main"]
	sp, err := spec.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/libreria", DefaultBranch: "main", Name: "libreria", Spec: *sp}, false); err != nil {
		t.Fatalf("onboarding a library: %v", err)
	}
	var cr v1alpha1.App
	if err := kube.Get(ctx, client.ObjectKey{Name: "libreria"}, &cr); !apierrors.IsNotFound(err) {
		t.Fatalf("a library got an App object: %v", err)
	}
	runs, err := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/libreria", Branch: "main", SHA: sha})
	if err != nil || len(runs) != 1 {
		t.Fatalf("push: %v %v", runs, err)
	}
	r := waitRun(t, p, runs[0].ID)
	if r.Status != store.RunSucceeded || !strings.Contains(r.Message, "nothing to deploy") {
		t.Fatalf("the library's run: %s %q", r.Status, r.Message)
	}
	if rels, _ := p.Store.ListReleases(ctx, r.AppID, 10); len(rels) != 0 {
		t.Fatalf("a library got a release: %+v", rels)
	}
}

func TestTasks(t *testing.T) {
	p, fake, exec, _ := setup(t)
	ctx := context.Background()
	yaml := `services:
  - name: api
    path: api
tasks:
  - name: mobile
    image: node:20
    command: npx eas-cli build
    path: mobile
  - name: smoke
    image: curl
    command: curl -f x
    after: [api]
    when: always
    optional: true
`
	sha := func(c byte) string { return strings.Repeat(string(c), 40) }
	tree := fstest.MapFS{"rendimiento.yaml": {Data: []byte(yaml)}}
	for _, c := range "abcf" {
		fake.files[sha(byte(c))] = tree
	}
	fake.files["main"] = tree
	fake.changed = map[string][]string{sha('a') + "..." + sha('b'): {"api/main.go"}}
	sp, _ := spec.Parse([]byte(yaml))
	if _, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/tareas", DefaultBranch: "main", Name: "tareas", Spec: *sp}, false); err != nil {
		t.Fatal(err)
	}
	push := func(branch string, c byte) (*store.Run, []string) {
		t.Helper()
		exec.mu.Lock()
		exec.ran = nil
		exec.mu.Unlock()
		runs, err := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/tareas", Branch: branch, SHA: sha(c)})
		if err != nil || len(runs) != 1 {
			t.Fatalf("push: %v %v", runs, err)
		}
		r := waitRun(t, p, runs[0].ID)
		exec.mu.Lock()
		defer exec.mu.Unlock()
		ran := append([]string(nil), exec.ran...)
		sort.Strings(ran)
		return r, ran
	}
	step := func(r *store.Run, id string) store.Step {
		for _, st := range r.Steps {
			if st.ID == id {
				return st
			}
		}
		t.Fatalf("no step %s in %+v", id, r.Steps)
		return store.Step{}
	}

	// 1. First release: everything runs; the optional task fails without failing the run.
	exec.failSteps = map[string]bool{"smoke:task": true}
	r, ran := push("main", 'a')
	if r.Status != store.RunSucceeded || strings.Join(ran, ",") != "api:build,mobile:task,smoke:task" {
		t.Fatalf("first push ran %v (%s %s)", ran, r.Status, r.Message)
	}
	// 2. A change to api only: mobile is skipped as unchanged; smoke runs after the new build.
	exec.failSteps = nil
	r, ran = push("main", 'b')
	if strings.Join(ran, ",") != "api:build,smoke:task" || !strings.Contains(step(r, "mobile:task").Message, "nothing under mobile changed") {
		t.Fatalf("api-only push ran %v; mobile = %+v", ran, step(r, "mobile:task"))
	}
	// 3. A branch: the deploy-only task never runs there (it may read secrets).
	r, ran = push("feature", 'f')
	if strings.Join(ran, ",") != "api:build,smoke:task" || !strings.Contains(step(r, "mobile:task").Message, "default branch") {
		t.Fatalf("branch push ran %v; mobile = %+v", ran, step(r, "mobile:task"))
	}
	// 4. A required task that fails fails the run: no release.
	exec.failSteps = map[string]bool{"mobile:task": true}
	fake.changed[sha('b')+"..."+sha('c')] = []string{"mobile/app.json"}
	if r, _ = push("main", 'c'); r.Status != store.RunFailed || !strings.Contains(r.Message, "mobile:task") {
		t.Fatalf("failed task: %s %s", r.Status, r.Message)
	}
}

// Disconnect leaves everything running and unowned; the wizard suggests the
// namespace that already serves a repo's hosts.
func TestDisconnectAndSuggestedName(t *testing.T) {
	p, fake, _, kube := setup(t)
	ctx := context.Background()
	sha := strings.Repeat("a", 40)
	yaml := "services:\n  - name: web\n    domain: shop.example.com\n"
	fake.files["main"] = fstest.MapFS{"rendimiento.yaml": {Data: []byte(yaml)}}
	fake.files[sha] = fake.files["main"]
	sp, _ := spec.Parse([]byte(yaml))
	if _, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/shop", DefaultBranch: "main", Name: "shop", Spec: *sp}, false); err != nil {
		t.Fatal(err)
	}
	runs, _ := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/shop", Branch: "main", SHA: sha})
	waitRun(t, p, runs[0].ID)
	key := client.ObjectKey{Namespace: "shop", Name: "web"}
	eventually(t, "deployed", func() bool { return kube.Get(ctx, key, &appsv1.Deployment{}) == nil })

	im, err := p.DeleteImpact(ctx, mustApp(t, p, "shop"))
	if err != nil || im.Namespace != "shop" {
		t.Fatalf("impact = %+v %v", im, err)
	}
	if err := p.DisconnectApp(ctx, mustApp(t, p, "shop")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Store.GetApp(ctx, "shop"); err == nil {
		t.Fatal("app record should be gone")
	}
	eventually(t, "App deleted (after the controller's finalizer)", func() bool {
		return apierrors.IsNotFound(kube.Get(ctx, client.ObjectKey{Name: "shop"}, &v1alpha1.App{}))
	})
	var d appsv1.Deployment
	var ns corev1.Namespace
	if err := kube.Get(ctx, key, &d); err != nil {
		t.Fatalf("deployment must keep running: %v", err)
	}
	_ = kube.Get(ctx, client.ObjectKey{Name: "shop"}, &ns)
	for _, o := range []client.Object{&d, &ns} {
		if len(o.GetOwnerReferences()) != 0 || o.GetLabels()["app.kubernetes.io/managed-by"] == "rendimiento" {
			t.Fatalf("%T still owned/labeled: %v %v", o, o.GetOwnerReferences(), o.GetLabels())
		}
	}
	// It now looks like any unmanaged app: the wizard offers to migrate it
	// back, found by its host even though the repo is named differently.
	prop, err := p.Propose(ctx, 7, "p0dxD/storefront-repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if prop.SuggestedName != "shop" || prop.Migration == nil || prop.Migration.Namespace != "shop" {
		t.Fatalf("suggested %q migration %+v", prop.SuggestedName, prop.Migration)
	}
}

func mustApp(t *testing.T, p *Platform, name string) *store.App {
	t.Helper()
	a, err := p.Store.GetApp(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type captureSender struct{ subjects chan string }

func (c captureSender) Send(_ context.Context, _ []string, subject, _, _ string) error {
	c.subjects <- subject
	return nil
}

// A push whose rendimiento.yaml is invalid is a failed run with the reason,
// a failed check on the commit and an email; the app's spec is unchanged
// and nothing is built.
func TestInvalidSpecIsAFailedRun(t *testing.T) {
	p, fake, _, _ := setup(t)
	ctx := context.Background()
	mails := captureSender{subjects: make(chan string, 4)}
	p.Notify = &notify.Notifier{Sender: mails, To: []string{"you@example.com"}}
	good := "services:\n  - name: web\n    domain: invalida.example.com\n"
	bad := "services:\n  - name: web\n    domain: invalida.example.com\n    aliases: [invalida.example.com]\n"
	fake.files["main"] = fstest.MapFS{"rendimiento.yaml": {Data: []byte(good)}}
	fake.files[strings.Repeat("b", 40)] = fstest.MapFS{"rendimiento.yaml": {Data: []byte(bad)}}
	sp, _ := spec.Parse([]byte(good))
	if _, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/invalida", DefaultBranch: "main", Name: "invalida", Spec: *sp}, false); err != nil {
		t.Fatal(err)
	}

	runs, err := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/invalida", Branch: "main", SHA: strings.Repeat("b", 40)})
	if err != nil || len(runs) != 1 {
		t.Fatalf("push: %v %v", runs, err)
	}
	time.Sleep(3 * time.Second) // a worker polls every 2 s: it must leave the run alone
	run, err := p.Store.GetRun(ctx, runs[0].ID)
	if err != nil || run.Status != store.RunFailed || !strings.Contains(run.Message, `rendimiento.yaml at bbbbbbb`) ||
		!strings.Contains(run.Message, "used more than once") || len(run.Steps) != 0 || run.FinishedAt == nil {
		t.Fatalf("run = %+v, %v", run, err)
	}
	fake.mu.Lock()
	var concluded string
	for _, c := range fake.checks {
		concluded = c.Conclusion
	}
	fake.mu.Unlock()
	if concluded != "failure" {
		t.Fatalf("check conclusion = %q", concluded)
	}
	select {
	case s := <-mails.subjects:
		if !strings.Contains(s, "invalida: rendimiento.yaml has an error") {
			t.Fatalf("email subject %q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no email")
	}
	app, _ := p.Store.GetApp(ctx, "invalida")
	if len(app.Spec.Services[0].Aliases) != 0 {
		t.Fatalf("the invalid spec was saved: %+v", app.Spec.Services[0])
	}

	// The problem the rejection recorded is resolved by the next good push
	// on the same branch.
	_ = p.Store.RecordProblem(ctx, store.Problem{Fingerprint: "rej", Level: "WARN", App: "invalida", Message: msgSpecRejected,
		Attrs: fmt.Sprintf("run=%d branch=main", run.ID), LastAt: time.Now()})
	fake.files[strings.Repeat("c", 40)] = fstest.MapFS{"rendimiento.yaml": {Data: []byte(good)}}
	if runs, err := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/invalida", Branch: "main", SHA: strings.Repeat("c", 40)}); err != nil || len(runs) != 1 {
		t.Fatalf("good push: %v %v", runs, err)
	}
	if n, _ := p.Store.OpenProblems(ctx); n != 0 {
		t.Fatalf("open problems after the fix = %d", n)
	}
	list, _ := p.Store.ListProblems(ctx, "invalida", time.Now().Add(-time.Hour), false, 10)
	if len(list) != 1 || list[0].Resolution != "fixed by ccccccc" {
		t.Fatalf("problems = %+v", list)
	}
}

// The Caminos tab: an app's runs by branch, rebuilt only when a newer run
// appears (or after caminosTTL).
func TestCaminos(t *testing.T) {
	p, _, _, _ := setup(t)
	ctx := context.Background()
	app := &store.App{Name: "caminos-app", Repo: "p0dxD/caminos-app", DefaultBranch: "main", Spec: spec.Spec{Services: []spec.Service{{Name: "web"}}}}
	if err := p.Store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*store.Run{{SHA: strings.Repeat("a", 40), Branch: "main"}, {SHA: strings.Repeat("b", 40), Branch: "vereda"}} {
		r.AppID, r.Event = app.ID, "push"
		if err := p.Store.CreateRun(ctx, r, nil); err != nil {
			t.Fatal(err)
		}
	}
	h, err := p.Caminos(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Main) != 1 || len(h.Branches) != 1 || h.Branches[0].Name != "vereda" || h.Branches[0].State != "open" || h.DefaultBranch != "main" {
		t.Fatalf("history = %+v", h)
	}
	if again, _ := p.Caminos(ctx, app); again != h {
		t.Fatal("an unchanged history should come from the cache")
	}
	if err := p.Store.CreateRun(ctx, &store.Run{AppID: app.ID, SHA: strings.Repeat("c", 40), Branch: "vereda", Event: "push"}, nil); err != nil {
		t.Fatal(err)
	}
	if fresh, _ := p.Caminos(ctx, app); fresh == h || len(fresh.Branches[0].Runs) != 2 {
		t.Fatalf("a new run should rebuild the history: %+v", fresh)
	}
}

// An app that merges by itself: the open pull request of a branch that
// passed is merged and its branch deleted; one in conflict is not, and its run says so; with the
// option off, nothing is merged.
func TestFusionAutomatica(t *testing.T) {
	p, fake, _, _ := setup(t)
	ctx := context.Background()
	yaml := "tasks:\n  - name: test\n    image: node:20\n    command: npm test\n    when: always\n"
	fake.files["main"] = fstest.MapFS{"rendimiento.yaml": {Data: []byte(yaml)}}
	sp, err := spec.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/fusion", DefaultBranch: "main", Name: "fusion", Spec: *sp}, false); err != nil {
		t.Fatalf("onboarding: %v", err)
	}
	app, err := p.Store.GetApp(ctx, "fusion")
	if err != nil {
		t.Fatal(err)
	}
	yes, no := true, false
	pull := func(number int, branch, sha string, mergeable *bool) gh.PullInfo {
		pr := gh.PullInfo{Number: number, State: "open", Mergeable: mergeable, MergeableState: "clean"}
		pr.Head.Ref, pr.Head.SHA, pr.Base.Ref = branch, sha, "main"
		return pr
	}
	// push runs the branch and waits for what became of its pull request.
	push := func(branch, sha, want string) *store.Run {
		t.Helper()
		fake.mu.Lock()
		fake.files[sha] = fake.files["main"]
		if fake.heads == nil {
			fake.heads = map[string]string{}
		}
		fake.heads[branch] = sha
		fake.mu.Unlock()
		runs, err := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/fusion", Branch: branch, SHA: sha})
		if err != nil || len(runs) != 1 {
			t.Fatalf("push to %s: %v %v", branch, runs, err)
		}
		r := waitRun(t, p, runs[0].ID)
		if r.Status != store.RunSucceeded {
			t.Fatalf("run of %s: %s %q", branch, r.Status, r.Message)
		}
		for deadline := time.Now().Add(5 * time.Second); r.Merge != want && time.Now().Before(deadline); {
			time.Sleep(50 * time.Millisecond)
			r, _ = p.Store.GetRun(ctx, r.ID)
		}
		if r.Merge != want {
			t.Fatalf("pull request of %s: merge %q, want %q (%s)", branch, r.Merge, want, r.Message)
		}
		return r
	}
	merged := func() []int {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return append([]int(nil), fake.merged...)
	}

	// Off: a branch that passes is only tested.
	fake.pulls = []gh.PullInfo{pull(4, "apagada", strings.Repeat("1", 40), &yes)}
	off := push("apagada", strings.Repeat("1", 40), "")
	if got := merged(); len(got) != 0 {
		t.Fatalf("merged with the option off: %v", got)
	}

	if _, err := p.ApproveMerge(ctx, off.ID, "ana"); !errors.As(err, new(*NotApprovable)) {
		t.Fatalf("approving with the option off: %v", err)
	}

	// Any open pull request is the yes, except a fork's and a dependency
	// update's.
	if err := p.Store.SetAppAutoMerge(ctx, app.ID, true, true); err != nil {
		t.Fatal(err)
	}
	fork := pull(8, "bifurcada", strings.Repeat("6", 40), &yes)
	fork.Head.Repo.FullName = "otra/fusion"
	fake.mu.Lock()
	fake.pulls = []gh.PullInfo{
		pull(5, "lista", strings.Repeat("2", 40), &yes),
		pull(6, "choca", strings.Repeat("3", 40), &no),
		pull(7, "otra-vez", strings.Repeat("f", 40), &yes), // its head is not the commit that ran
		fork,
		pull(11, "renovate/react-19.x", strings.Repeat("d", 40), &yes), // Renovate left it for a person
	}
	fake.mu.Unlock()
	if r := push("lista", strings.Repeat("2", 40), store.MergeMerged); r.Pull != 5 || !strings.Contains(r.Message, "merged pull request #5") {
		t.Fatalf("the merged run: %+v", r)
	}
	if r := push("choca", strings.Repeat("3", 40), store.MergeConflict); r.Pull != 6 || !strings.Contains(r.Message, "conflicts with main") {
		t.Fatalf("the run in conflict: %+v", r)
	}
	push("otra-vez", strings.Repeat("4", 40), "")
	push("sin-solicitud", strings.Repeat("5", 40), "")
	push("bifurcada", strings.Repeat("6", 40), "")
	push("renovate/react-19.x", strings.Repeat("d", 40), "")
	if got := merged(); len(got) != 1 || got[0] != 5 {
		t.Fatalf("merged %v, want only #5", got)
	}
	// Only the merged branch is deleted.
	fake.mu.Lock()
	deleted := append([]string(nil), fake.deleted...)
	fake.mu.Unlock()
	if len(deleted) != 1 || deleted[0] != "lista" {
		t.Fatalf("deleted %v, want only lista", deleted)
	}

	// Only what a person approved: an open pull request waits for the yes,
	// and the yes opens the pull request of a branch that has none.
	if err := p.Store.SetAppAutoMerge(ctx, app.ID, true, false); err != nil {
		t.Fatal(err)
	}
	// approved waits for the run to be merged, as the person asked.
	approved := func(r *store.Run, who string) *store.Run {
		t.Helper()
		if _, err := p.ApproveMerge(ctx, r.ID, who); err != nil {
			t.Fatalf("approving %s: %v", r.Branch, err)
		}
		for deadline := time.Now().Add(15 * time.Second); r.Merge != store.MergeMerged && time.Now().Before(deadline); {
			time.Sleep(50 * time.Millisecond)
			r, _ = p.Store.GetRun(ctx, r.ID)
		}
		if r.Merge != store.MergeMerged || r.ApprovedBy != who {
			t.Fatalf("the approved run of %s: merge %q, approved by %q (%s)", r.Branch, r.Merge, r.ApprovedBy, r.Message)
		}
		return r
	}
	fake.mu.Lock()
	fake.pulls = []gh.PullInfo{pull(9, "espera", strings.Repeat("7", 40), &yes)}
	fake.mu.Unlock()
	waits := push("espera", strings.Repeat("7", 40), "")
	if app, err = p.Store.GetApp(ctx, "fusion"); err != nil {
		t.Fatal(err)
	}
	p.MergeReady(ctx, app)
	if got := merged(); len(got) != 1 {
		t.Fatalf("merged without anyone's yes: %v", got)
	}
	if r := approved(waits, "ana"); r.Pull != 9 {
		t.Fatalf("the approved run: %+v", r)
	}
	approved(push("sin-abrir", strings.Repeat("8", 40), ""), "beto")
	fake.mu.Lock()
	opened := append([]string(nil), fake.opened...)
	fake.mu.Unlock()
	if len(opened) != 1 || opened[0] != "sin-abrir: What the commit says" {
		t.Fatalf("opened %v, want the pull request of sin-abrir", opened)
	}
	if got := merged(); len(got) != 3 || got[1] != 9 || got[2] != 100 {
		t.Fatalf("merged %v, want #5, #9 and #100", got)
	}
	// An older commit of the branch is not what gets merged.
	old := push("avanza", strings.Repeat("a", 40), "")
	push("avanza", strings.Repeat("b", 40), "")
	if _, err := p.ApproveMerge(ctx, old.ID, "ana"); !errors.As(err, new(*NotApprovable)) {
		t.Fatalf("approving an older commit: %v", err)
	}
	// An accepted encargo approves the branch its links name.
	enc := push("del-encargo", strings.Repeat("c", 40), "")
	p.ApproveLinks(ctx, []string{"https://example.com/x", "https://github.com/p0dxD/fusion/pull/new/del-encargo"}, "ana")
	for deadline := time.Now().Add(15 * time.Second); enc.Merge != store.MergeMerged && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		enc, _ = p.Store.GetRun(ctx, enc.ID)
	}
	if enc.Merge != store.MergeMerged || enc.ApprovedBy != "ana" {
		t.Fatalf("the encargo's branch: merge %q, approved by %q", enc.Merge, enc.ApprovedBy)
	}
}
