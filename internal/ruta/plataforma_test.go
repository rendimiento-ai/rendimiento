package ruta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rendimiento-ai/rendimiento/internal/libreta"
	"github.com/rendimiento-ai/rendimiento/internal/mesa"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

type fakePlataforma struct {
	mu        sync.Mutex
	apps      []*store.App
	runs      map[int64]*store.Run
	queued    []string // app + " " + branch
	cancelled []int64
}

func (f *fakePlataforma) ListApps(context.Context) ([]*store.App, error) { return f.apps, nil }
func (f *fakePlataforma) GetApp(_ context.Context, name string) (*store.App, error) {
	for _, a := range f.apps {
		if a.Name == name {
			return a, nil
		}
	}
	return nil, store.ErrNotFound
}
func (f *fakePlataforma) GetAppByID(_ context.Context, id int64) (*store.App, error) {
	for _, a := range f.apps {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, store.ErrNotFound
}
func (f *fakePlataforma) ListRuns(_ context.Context, appID int64, _ int) ([]*store.Run, error) {
	var out []*store.Run
	for _, r := range f.runs {
		if r.AppID == appID {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakePlataforma) GetRun(_ context.Context, id int64) (*store.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.runs[id]; ok {
		c := *r
		return &c, nil
	}
	return nil, store.ErrNotFound
}
func (f *fakePlataforma) ListReleases(context.Context, int64, int) ([]*store.Release, error) {
	return []*store.Release{{Number: 7, SHA: "abcdef123456", VerifyStatus: store.VerifyPassed},
		{Number: 6, SHA: "0123456789ab", VerifyStatus: store.VerifyFailed, VerifyMessage: "the rollout failed"}}, nil
}

// ReleaseEvidence: release 6 failed with a pod in a crash loop.
func (f *fakePlataforma) ReleaseEvidence(_ context.Context, _, number int64) (*store.ReleaseEvidence, error) {
	if number != 6 {
		return nil, store.ErrNotFound
	}
	code := int32(1)
	return &store.ReleaseEvidence{Where: "kubernetes", Services: []store.ServiceEvidence{{Name: "web", ReplicaSet: "web-6d9f",
		Pods: []store.PodEvidence{{Name: "web-6d9f-x1", Phase: "Running", Containers: []store.ContainerEvidence{{Name: "web", State: "waiting",
			Reason: "CrashLoopBackOff", Restarts: 4, LastExitCode: &code, LastReason: "Error", PreviousLog: "starting\npanic: no DATABASE_URL"}}}}}},
		Events: []store.EvidenceEvent{{Type: "Warning", Reason: "BackOff", Object: "Pod/web-6d9f-x1", Message: "Back-off restarting failed container"}}}, nil
}
func (f *fakePlataforma) StepLog(_ context.Context, run int64, step string) (string, error) {
	if step != "web:test" {
		return "", store.ErrNotFound
	}
	var b strings.Builder
	for i := 1; i <= 300; i++ {
		fmt.Fprintf(&b, "línea %d\n", i)
	}
	return b.String(), nil
}
func (f *fakePlataforma) ListTraces(context.Context, store.TraceQuery) ([]store.Trace, error) {
	return []store.Trace{{ID: "t1", Services: []string{"shop/web"}}, {ID: "t2", Services: []string{"blog/web"}}}, nil
}
func (f *fakePlataforma) GetTrace(_ context.Context, id string) (*store.Trace, []store.Span, error) {
	if id == "t2" {
		return &store.Trace{ID: "t2", Services: []string{"blog/web"}}, nil, nil
	}
	return nil, nil, store.ErrNotFound
}
func (f *fakePlataforma) ListProblems(context.Context, string, time.Time, bool, int) ([]store.Problem, error) {
	return []store.Problem{{Level: "error", Component: "verify", Message: "web did not answer", Count: 2}}, nil
}
func (f *fakePlataforma) QueueRun(_ context.Context, a *store.App, branch string) (*store.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queued = append(f.queued, a.Name+" "+branch)
	r := &store.Run{ID: 99, AppID: a.ID, Branch: branch, Status: store.RunQueued}
	f.runs[r.ID] = r
	return r, nil
}
func (f *fakePlataforma) WaitRun(ctx context.Context, id int64) (*store.Run, error) {
	for {
		f.mu.Lock()
		r := *f.runs[id]
		f.mu.Unlock()
		if finished(r.Status) {
			return &r, nil
		}
		select {
		case <-ctx.Done():
			return &r, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (f *fakePlataforma) finish(id int64, status string) {
	f.mu.Lock()
	f.runs[id].Status = status
	f.mu.Unlock()
}

func (f *fakePlataforma) Mesa(_ context.Context, a *store.App) (mesa.Board, error) {
	return mesa.Board{Lines: []mesa.Line{{Group: mesa.GroupYou, Why: mesa.WhyPassed, Branch: "lista", Pull: 7, Run: 1,
		Encargos: []mesa.LineEncargo{{ID: 3}}}}}, nil
}

func (f *fakePlataforma) Libreta(_ context.Context, a *store.App) (libreta.Libreta, error) {
	done := time.Now().Add(-time.Hour)
	return libreta.Libreta{GitHub: true, Items: []libreta.Item{
		{Source: libreta.FromRuta, ID: 5, Title: "Que el arreglo se dispare desde el botón", Status: libreta.EnCurso, Why: libreta.WhyRunning, Branch: "arreglo", Run: 99},
		{Source: libreta.FromPruebas, Status: libreta.Esperando, Why: libreta.WhyTests, Tests: []string{"vuelve"}, Progress: &libreta.Progress{Done: 1, Of: 5}},
		{Source: libreta.FromRenovate, ID: 17, Title: "Spring Boot 4", Status: libreta.Publicado, Pull: 17, Release: 70, DoneAt: &done},
		{Source: libreta.FromEncargo, ID: 3, Title: "viejo", Status: libreta.Hecho, DoneAt: ptrTime(time.Now().AddDate(0, 0, -30))},
	}}, nil
}

func ptrTime(t time.Time) *time.Time { return &t }

func (f *fakePlataforma) RetryRun(_ context.Context, id int64) (*store.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs[id].Status = store.RunQueued
	f.runs[id].Retries++
	return f.runs[id], nil
}

func (f *fakePlataforma) CancelRun(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, id)
	f.runs[id].Status = store.RunCancelled
	return nil
}

// Agents read the platform (apps, runs, logs, kept requests) and, with a
// collaborator key and a cargo, test a branch or cancel a run; never the
// default branch, which deploys. A key limited to some apps sees only those.
func TestPlataformaTools(t *testing.T) {
	m := &memory{keys: map[string]store.AgentKey{}}
	tokens := map[string]string{}
	for i, k := range []store.AgentKey{
		{Name: "colaborador", Role: store.RoleCollaborator},
		{Name: "lector", Role: store.RoleReader},
		{Name: "solo-shop", Role: store.RoleCollaborator, Apps: []string{"shop"}},
	} {
		tok, h, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		k.ID, k.ExpiresAt = int64(i+1), time.Now().Add(time.Hour)
		m.keys[h] = k
		tokens[k.Name] = tok
	}
	fp := &fakePlataforma{
		apps: []*store.App{
			{ID: 1, Name: "shop", Repo: "o/shop", DefaultBranch: "main", Spec: spec.Spec{Services: []spec.Service{{Name: "web"}}}},
			{ID: 2, Name: "blog", Repo: "o/blog", DefaultBranch: "main"},
		},
		runs: map[int64]*store.Run{
			10: {ID: 10, AppID: 1, Branch: "main", SHA: "1234567890", Deploy: true, Status: store.RunFailed, Steps: []store.Step{{ID: "web:test", Kind: "test", Status: "failed"}}},
			20: {ID: 20, AppID: 2, Branch: "main", Status: store.RunSucceeded},
		},
	}
	srv := httptest.NewServer((&Server{Store: m, Plataforma: fp}).Handler())
	defer srv.Close()
	session := func(name string) *mcp.ClientSession {
		s, err := connect(t, srv.URL, tokens[name])
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}

	// A reader sees the reading tools only.
	lector := session("lector")
	res, err := lector.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	all := strings.Join(names, ",")
	// Output schemas stay open: a session that listed them before a field
	// was added (app_estado's libreta) still takes the answer.
	for _, tl := range res.Tools {
		raw, _ := json.Marshal(tl.OutputSchema)
		if strings.Contains(string(raw), `"additionalProperties":false`) {
			t.Fatalf("%s's output schema is closed: %s", tl.Name, raw)
		}
	}
	if !strings.Contains(all, "apps_listar") || !strings.Contains(all, "paso_registro") || strings.Contains(all, "ejecucion_lanzar") {
		t.Fatalf("reader's tools = %s", all)
	}

	col := session("colaborador")
	out, msg := call(t, col, "apps_listar", map[string]any{})
	if msg != "" || len(out["apps"].([]any)) != 2 {
		t.Fatalf("apps = %v %s", out, msg)
	}
	out, msg = call(t, col, "mesa_leer", map[string]any{"app": "shop"})
	if lineas, _ := out["lineas"].([]any); msg != "" || len(lineas) != 1 || lineas[0].(map[string]any)["grupo"] != "usted" {
		t.Fatalf("mesa = %v %s", out, msg)
	}
	out, msg = call(t, col, "app_estado", map[string]any{"app": "shop"})
	if msg != "" || len(out["servicios"].([]any)) != 1 || len(out["problemas"].([]any)) != 1 {
		t.Fatalf("estado = %v %s", out, msg)
	}
	// A release that failed verification brings what was seen then; one
	// that passed does not.
	vers, _ := out["versiones"].([]any)
	if len(vers) != 2 || vers[0].(map[string]any)["lo_que_se_vio"] != nil {
		t.Fatalf("versiones = %v", vers)
	}
	if visto, _ := vers[1].(map[string]any)["lo_que_se_vio"].(string); !strings.Contains(visto, "waiting CrashLoopBackOff, 4 restarts, last exited 1 (Error)") ||
		!strings.Contains(visto, "| panic: no DATABASE_URL") || !strings.Contains(visto, "BackOff: Back-off restarting failed container") {
		t.Fatalf("lo que se vio = %q", visto)
	}
	// Its libreta: what is open, and what was done this week (not before).
	lib, _ := out["libreta"].(map[string]any)
	abiertos, _ := lib["abiertos"].([]any)
	hechos, _ := lib["hechos_semana"].([]any)
	if len(abiertos) != 2 || len(hechos) != 1 || abiertos[0].(map[string]any)["estado"] != "en_curso" ||
		abiertos[1].(map[string]any)["progreso"] != "1 de 5 versiones limpias" || hechos[0].(map[string]any)["version"] != float64(70) {
		t.Fatalf("libreta = %v", lib)
	}
	out, msg = call(t, col, "ejecucion_leer", map[string]any{"id": 10})
	if msg != "" || out["estado"] != store.RunFailed || out["commit"] != "1234567" {
		t.Fatalf("run = %v %s", out, msg)
	}
	out, msg = call(t, col, "paso_registro", map[string]any{"ejecucion": 10, "paso": "web:test", "lineas": 50})
	if msg != "" || out["cortado"] != true || !strings.HasPrefix(out["registro"].(string), "línea 251\n") || !strings.HasSuffix(out["registro"].(string), "línea 300") {
		t.Fatalf("log = %v %s", out, msg)
	}

	// Acting needs a cargo, and never the default branch.
	if _, msg := call(t, col, "ejecucion_lanzar", map[string]any{"app": "shop", "rama": "arreglo"}); !strings.Contains(msg, "cargo") {
		t.Fatalf("without a cargo: %q", msg)
	}
	call(t, col, "cargo_tomar", map[string]any{"proposito": "Probar el arreglo de shop"})
	if _, msg := call(t, col, "ejecucion_lanzar", map[string]any{"app": "shop", "rama": "main"}); !strings.Contains(msg, "producción") {
		t.Fatalf("the default branch: %q", msg)
	}
	out, msg = call(t, col, "ejecucion_lanzar", map[string]any{"app": "shop", "rama": "arreglo"})
	if msg != "" || out["id"] != float64(99) || strings.Join(fp.queued, ";") != "shop arreglo" {
		t.Fatalf("queued = %v %s %v", out, msg, fp.queued)
	}
	out, msg = call(t, col, "ejecucion_cancelar", map[string]any{"id": 99})
	if msg != "" || out["estado"] != store.RunCancelled || len(fp.cancelled) != 1 {
		t.Fatalf("cancel = %v %s", out, msg)
	}
	// Retrying what failed: on a branch, yes; on the main branch, which
	// deploys, a person does it.
	if _, msg := call(t, col, "ejecucion_reintentar", map[string]any{"id": 10}); !strings.Contains(msg, "rama principal") {
		t.Fatalf("retry on the main branch: %q", msg)
	}
	out, msg = call(t, col, "ejecucion_reintentar", map[string]any{"id": 99})
	if msg != "" || out["estado"] != store.RunQueued {
		t.Fatalf("retry = %v %s", out, msg)
	}

	// An entry about an app, with the branch it is worked on, goes to the
	// app's libreta; the app must exist, and a branch needs an app.
	out, msg = call(t, col, "ruta_anotar", map[string]any{"tipo": "pendiente", "titulo": "Que el arreglo se dispare desde el botón", "cuerpo": "x", "app": "shop", "rama": "arreglo"})
	if msg != "" || out["app"] != "shop" {
		t.Fatalf("anotar with app = %v %s", out, msg)
	}
	id := int64(out["id"].(float64))
	if got := m.ramas[id]; len(got) != 1 || got[0] != "arreglo" {
		t.Fatalf("linked branches = %v", got)
	}
	if _, msg := call(t, col, "ruta_anotar", map[string]any{"tipo": "pendiente", "titulo": "x", "cuerpo": "y", "app": "nadie"}); !strings.Contains(msg, "no existe") {
		t.Fatalf("an unknown app: %q", msg)
	}
	if _, msg := call(t, col, "ruta_anotar", map[string]any{"tipo": "pendiente", "titulo": "x", "cuerpo": "y", "rama": "suelta"}); !strings.Contains(msg, "app") {
		t.Fatalf("a branch with no app: %q", msg)
	}
	// Written in the cargo that ran a branch of its app: linked to it.
	out, msg = call(t, col, "ruta_anotar", map[string]any{"tipo": "pendiente", "titulo": "El botón en el teléfono", "cuerpo": "x", "app": "shop"})
	sinRama := int64(out["id"].(float64))
	if got := m.ramas[sinRama]; msg != "" || len(got) != 1 || got[0] != "arreglo" {
		t.Fatalf("linked from the cargo's run = %v %s", got, msg)
	}
	out, msg = call(t, col, "ruta_actualizar", map[string]any{"id": id, "rama": "otra-rama", "descartado": "Maven no tiene lockfile"})
	if msg != "" || out["descartado"] != "Maven no tiene lockfile" || len(m.ramas[id]) != 2 {
		t.Fatalf("actualizar = %v %s %v", out, msg, m.ramas[id])
	}
	if out, msg = call(t, col, "ruta_actualizar", map[string]any{"id": id, "app": "", "descartado": ""}); msg != "" || out["app"] != nil || out["descartado"] != nil {
		t.Fatalf("clearing app and descartado = %v %s", out, msg)
	}
	// A branch run later in the cargo answers what it wrote about the app.
	if _, msg := call(t, col, "ejecucion_lanzar", map[string]any{"app": "shop", "rama": "segunda"}); msg != "" {
		t.Fatal(msg)
	}
	if got := m.ramas[sinRama]; len(got) != 2 || got[1] != "segunda" || len(m.ramas[id]) != 2 {
		t.Fatalf("linked from a later run = %v, %v", got, m.ramas[id])
	}

	// A key limited to shop sees and acts on shop only.
	shop := session("solo-shop")
	out, _ = call(t, shop, "apps_listar", map[string]any{})
	if apps := out["apps"].([]any); len(apps) != 1 || apps[0].(map[string]any)["nombre"] != "shop" {
		t.Fatalf("limited apps = %v", apps)
	}
	if _, msg := call(t, shop, "ejecucion_leer", map[string]any{"id": 20}); !strings.Contains(msg, "no tiene acceso") {
		t.Fatalf("another app's run: %q", msg)
	}
	out, _ = call(t, shop, "solicitudes_listar", map[string]any{})
	if list := out["solicitudes"].([]any); len(list) != 1 {
		t.Fatalf("limited requests = %v", list)
	}
	if _, msg := call(t, shop, "solicitud_leer", map[string]any{"id": "t2"}); !strings.Contains(msg, "no tiene acceso") {
		t.Fatalf("another app's request: %q", msg)
	}
	call(t, shop, "cargo_tomar", map[string]any{"proposito": "Anotar en blog"})
	if _, msg := call(t, shop, "ruta_anotar", map[string]any{"tipo": "nota", "titulo": "x", "cuerpo": "y", "app": "blog"}); !strings.Contains(msg, "no tiene acceso") {
		t.Fatalf("an entry about another app: %q", msg)
	}
}
