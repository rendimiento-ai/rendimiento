package platform

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/destino"
	"github.com/rendimiento-ai/rendimiento/internal/events"
	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/generate"
	"github.com/rendimiento-ai/rendimiento/internal/pgprueba"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// destinoLocal stands for the local destination: its name is "local" (so
// rendimiento may sleep), and nothing runs.
type destinoLocal struct{ destino.Ninguno }

func (destinoLocal) Nombre() string { return "local" }

// camaFalsa is the apps' containers: those running stop on Dormir, and
// the ones named start on Despertar.
type camaFalsa struct {
	mu          sync.Mutex
	corriendo   []string
	despertados [][]string
}

func (c *camaFalsa) Dormir(context.Context) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.corriendo
	c.corriendo = nil
	return out, nil
}

func (c *camaFalsa) Despertar(_ context.Context, names []string, _ []*store.App) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.corriendo = append([]string(nil), names...)
	c.despertados = append(c.despertados, names)
	return []string{"dormida"}, nil
}

func (c *camaFalsa) ahora() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.corriendo...)
}

// ejecutorLento runs a step only once its branch is let go, and stops
// when its run is cut.
type ejecutorLento struct {
	mu      sync.Mutex
	sueltas map[string]bool
	ran     map[string]int // branch → steps that ran to the end
}

func (e *ejecutorLento) soltar(branch string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sueltas[branch] = true
}

func (e *ejecutorLento) Execute(ctx context.Context, _ string, src pipeline.Source, _ pipeline.Step, _ io.Writer) pipeline.StepResult {
	for {
		e.mu.Lock()
		suelta := e.sueltas[src.Branch]
		if suelta {
			e.ran[src.Branch]++
		}
		e.mu.Unlock()
		if suelta {
			return pipeline.StepResult{Status: pipeline.StatusSucceeded}
		}
		select {
		case <-ctx.Done():
			return pipeline.StepResult{Status: pipeline.StatusFailed, Message: "cancelled"}
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// setupDormir is a platform on a computer, without a cluster: one app
// whose runs wait to be let go, and the apps' containers faked.
func setupDormir(t *testing.T) (*Platform, *Sueno, *camaFalsa, *ejecutorLento, *store.App) {
	t.Helper()
	dsn := pgprueba.Base(t, migrarStore)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	yaml := "tasks:\n  - name: test\n    image: node:20\n    command: npm test\n    when: always\n"
	gh := &fakeGitHub{checks: map[int64]forja.Check{}, files: map[string]fstest.MapFS{"main": {"rendimiento.yaml": {Data: []byte(yaml)}}}}
	p := &Platform{
		Store: st, Forja: gh, Generator: generate.Templates{}, Hub: events.NewHub(), Destino: destinoLocal{},
		Config: Config{Registry: "registry.example.lan:5000", BaseURL: "http://localhost:8080"},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)), Workers: 3, PerApp: 3,
	}
	exec := &ejecutorLento{sueltas: map[string]bool{}, ran: map[string]int{}}
	p.Runner = pipeline.NewRunner(exec, p, 4)
	app := &store.App{Name: "dormida", Repo: "p0dxD/dormida", DefaultBranch: "main", InstallationID: 7}
	if err := st.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	cama := &camaFalsa{corriendo: []string{"dormida-web-1", "dormida-postgres"}}
	z := &Sueno{P: p, Cama: cama, Cada: 20 * time.Millisecond}
	go p.Work(ctx)
	return p, z, cama, exec, app
}

func queue(t *testing.T, p *Platform, branch string, c byte) *store.Run {
	t.Helper()
	runs, err := p.HandlePush(context.Background(), PushEvent{Installation: 7, Repo: "p0dxD/dormida", Branch: branch, SHA: strings.Repeat(string(c), 40)})
	if err != nil || len(runs) != 1 {
		t.Fatalf("push to %s: %v %v", branch, runs, err)
	}
	return runs[0]
}

func waitStatus(t *testing.T, p *Platform, id int64, status string) *store.Run {
	t.Helper()
	deadline := time.Now().Add(testWait)
	for time.Now().Before(deadline) {
		if r, err := p.Store.GetRun(context.Background(), id); err == nil && r.Status == status {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	r, _ := p.Store.GetRun(context.Background(), id)
	t.Fatalf("run %d did not become %s: %+v", id, status, r)
	return nil
}

func waitSueno(t *testing.T, z *Sueno, estado string) store.Sueno {
	t.Helper()
	deadline := time.Now().Add(testWait)
	for time.Now().Before(deadline) {
		if st, err := z.P.Store.GetSueno(context.Background()); err == nil && st.Estado == estado {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("it did not become %s", estado)
	return store.Sueno{}
}

// Dormir waits for the publish going on: the branch run stops at once and
// goes back to the queue (not failed), the deploying run ends, and only
// then do the apps' containers stop. Nothing is claimed while asleep;
// waking starts the same containers and repeats the cut run.
func TestDormirWaitsForThePublish(t *testing.T) {
	p, z, cama, exec, _ := setupDormir(t)
	ctx := context.Background()
	deploy := queue(t, p, "main", 'a')
	rama := queue(t, p, "rama", 'b')
	waitStatus(t, p, deploy.ID, store.RunRunning)
	waitStatus(t, p, rama.ID, store.RunRunning)

	st, err := z.Dormir(ctx, "p0dxD", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if st.Estado != store.Durmiendose || len(st.Publicando) != 1 || st.Publicando[0].App != "dormida" || st.Publicando[0].Run != deploy.ID {
		t.Fatalf("falling asleep: %+v", st)
	}
	r := waitStatus(t, p, rama.ID, store.RunQueued)
	if r.FinishedAt != nil || !strings.Contains(r.Message, "runs again on waking") {
		t.Fatalf("the cut branch run: %+v", r)
	}
	time.Sleep(200 * time.Millisecond)
	if got := cama.ahora(); len(got) != 2 {
		t.Fatalf("the containers stopped before the publish ended: %v", got)
	}
	exec.soltar("main")
	waitStatus(t, p, deploy.ID, store.RunSucceeded)
	dormido := waitSueno(t, z, store.Dormido)
	if got := cama.ahora(); len(got) != 0 || !slices.Equal(dormido.Contenedores, []string{"dormida-postgres", "dormida-web-1"}) {
		t.Fatalf("asleep, running %v, kept %v", got, dormido.Contenedores)
	}
	// Asleep, the queue waits: a new push is queued, never claimed.
	exec.soltar("rama")
	otra := queue(t, p, "otra", 'c')
	time.Sleep(300 * time.Millisecond)
	if r, _ := p.Store.GetRun(ctx, otra.ID); r.Status != store.RunQueued {
		t.Fatalf("a run was claimed while asleep: %s", r.Status)
	}

	st, err = z.Despertar(ctx, "p0dxD")
	if err != nil {
		t.Fatal(err)
	}
	if st.Estado != store.Despierto || st.Resumen == nil || !slices.Equal(st.Resumen.Repetidas, []int64{rama.ID}) ||
		st.Resumen.Esperada != "dormida" || !slices.Equal(st.Resumen.Apps, []string{"dormida"}) {
		t.Fatalf("awake: %+v %+v", st, st.Resumen)
	}
	if got := cama.ahora(); !slices.Equal(got, []string{"dormida-postgres", "dormida-web-1"}) {
		t.Fatalf("the same containers did not start: %v", got)
	}
	if r := waitStatus(t, p, rama.ID, store.RunSucceeded); r.Status == store.RunFailed {
		t.Fatalf("the cut run failed: %+v", r)
	}
	exec.soltar("otra")
	waitStatus(t, p, otra.ID, store.RunSucceeded)
}

// «Ya» cuts the publish too: the deploying run goes back to the queue (the
// app stays on what it runs) and publishes on waking. «Apagar del todo»
// stops rendimiento once asleep and leaves it to wake when it starts.
func TestDormirYaAndApagar(t *testing.T) {
	p, z, cama, exec, _ := setupDormir(t)
	ctx := context.Background()
	deploy := queue(t, p, "main", 'd')
	waitStatus(t, p, deploy.ID, store.RunRunning)
	apagado := make(chan struct{})
	z.Apagar = func() { close(apagado) }

	if _, err := z.Dormir(ctx, "p0dxD", true, true); err != nil {
		t.Fatal(err)
	}
	r := waitStatus(t, p, deploy.ID, store.RunQueued)
	if r.FinishedAt != nil {
		t.Fatalf("the cut publish finished: %+v", r)
	}
	select {
	case <-apagado:
	case <-time.After(testWait):
		t.Fatal("rendimiento did not stop")
	}
	st := waitSueno(t, z, store.Dormido)
	if !st.AlArrancar || !st.Apagar || len(cama.ahora()) != 0 || !slices.Equal(st.Cortadas, []int64{deploy.ID}) {
		t.Fatalf("shut down: %+v, running %v", st, cama.ahora())
	}

	// It starts again (a new process: nothing claims until it looks), and
	// wakes by itself.
	p.dormido.Store(true)
	if err := z.Arrancar(ctx); err != nil {
		t.Fatal(err)
	}
	if p.Dormido() {
		t.Fatal("it did not wake on start after «Apagar del todo»")
	}
	got, _ := p.Store.GetSueno(ctx)
	if got.Estado != store.Despierto || got.AlArrancar || got.Resumen == nil || got.Resumen.Por != "" {
		t.Fatalf("after starting: %+v", got)
	}
	exec.soltar("main")
	waitStatus(t, p, deploy.ID, store.RunSucceeded)
}

// Asleep without «Apagar del todo», a restart stays asleep, and stops
// again what the engine started with the computer.
func TestDormirSurvivesARestart(t *testing.T) {
	p, z, cama, _, _ := setupDormir(t)
	ctx := context.Background()
	if _, err := z.Dormir(ctx, "p0dxD", false, false); err != nil {
		t.Fatal(err)
	}
	waitSueno(t, z, store.Dormido)
	cama.mu.Lock()
	cama.corriendo = []string{"dormida-web-1"} // the computer started again
	cama.mu.Unlock()
	p.dormido.Store(false)
	if err := z.Arrancar(ctx); err != nil {
		t.Fatal(err)
	}
	if !p.Dormido() || len(cama.ahora()) != 0 {
		t.Fatalf("after a restart: asleep %v, running %v", p.Dormido(), cama.ahora())
	}
	if st, _ := p.Store.GetSueno(ctx); st.Estado != store.Dormido || !slices.Equal(st.Contenedores, []string{"dormida-postgres", "dormida-web-1"}) {
		t.Fatalf("kept %+v", st)
	}
}

// The webhooks kept while asleep are handled on waking, in order: the
// summary counts them.
func TestDormirHandlesTheKeptWebhooks(t *testing.T) {
	p, z, _, _, _ := setupDormir(t)
	ctx := context.Background()
	var orden []string
	z.Avisos = func(ctx context.Context) (int, int) {
		avisos, err := p.Store.AvisosGuardados(ctx)
		if err != nil {
			t.Error(err)
		}
		for _, a := range avisos {
			orden = append(orden, strings.ReplaceAll(string(a.Evento), " ", "")) // jsonb writes it with spaces
			_ = p.Store.BorrarAviso(ctx, a.ID)
		}
		return len(avisos), 0
	}
	if _, err := z.Dormir(ctx, "p0dxD", false, false); err != nil {
		t.Fatal(err)
	}
	waitSueno(t, z, store.Dormido)
	for _, n := range []string{"1", "2", "3"} {
		if err := p.Store.GuardarAviso(ctx, "github", map[string]string{"n": n}); err != nil {
			t.Fatal(err)
		}
	}
	if st, _ := z.Estado(ctx); st.Avisos != 3 {
		t.Fatalf("waiting: %d", st.Avisos)
	}
	st, err := z.Despertar(ctx, "p0dxD")
	if err != nil {
		t.Fatal(err)
	}
	if st.Resumen.Avisos != 3 || st.Avisos != 0 || strings.Join(orden, "") != `{"n":"1"}{"n":"2"}{"n":"3"}` {
		t.Fatalf("handled %d (%d waiting) in the order %v", st.Resumen.Avisos, st.Avisos, orden)
	}
}

// At its hour it wakes by itself; before it, not; and the hour is the next
// one after it fell asleep.
func TestDormirWakesAtItsHour(t *testing.T) {
	zona := time.FixedZone("CST", -6*3600)
	durmio := time.Date(2026, 11, 20, 12, 43, 0, 0, zona)
	if at, ok := ProximoDespertar(durmio, "08:00", zona); !ok || !at.Equal(time.Date(2026, 11, 21, 8, 0, 0, 0, zona)) {
		t.Fatalf("after 12:43, 08:00 is %v", at)
	}
	if at, _ := ProximoDespertar(durmio, "23:30", zona); !at.Equal(time.Date(2026, 11, 20, 23, 30, 0, 0, zona)) {
		t.Fatalf("after 12:43, 23:30 is %v", at)
	}
	if _, ok := ProximoDespertar(durmio, "8am", zona); ok {
		t.Fatal("8am is not an hour")
	}

	p, z, cama, _, _ := setupDormir(t)
	ctx := context.Background()
	now := durmio
	var mu sync.Mutex
	z.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	z.Zona = zona
	if _, err := z.Opciones(ctx, "8:00 am", false); !errors.Is(err, ErrHoraInvalida) {
		t.Fatalf("a bad hour: %v", err)
	}
	if _, err := z.Opciones(ctx, "08:00", false); err != nil {
		t.Fatal(err)
	}
	if _, err := z.Dormir(ctx, "p0dxD", false, false); err != nil {
		t.Fatal(err)
	}
	waitSueno(t, z, store.Dormido)
	if st, _ := z.Estado(ctx); st.DespiertaA == nil || !st.DespiertaA.Equal(time.Date(2026, 11, 21, 8, 0, 0, 0, zona)) {
		t.Fatalf("it wakes at %v", st.DespiertaA)
	}
	mu.Lock()
	now = time.Date(2026, 11, 21, 7, 59, 0, 0, zona)
	mu.Unlock()
	if z.MirarReloj(ctx) || !p.Dormido() {
		t.Fatal("it woke before its hour")
	}
	mu.Lock()
	now = time.Date(2026, 11, 21, 8, 0, 30, 0, zona)
	mu.Unlock()
	if !z.MirarReloj(ctx) || p.Dormido() || len(cama.ahora()) != 2 {
		t.Fatalf("at its hour: asleep %v, running %v", p.Dormido(), cama.ahora())
	}
	if st, _ := p.Store.GetSueno(ctx); st.Resumen == nil || st.Resumen.Por != "reloj" || st.DespertarA != "08:00" {
		t.Fatalf("woken by the clock: %+v", st)
	}
}

// In a cluster rendimiento does not sleep.
func TestDormirRefusedInACluster(t *testing.T) {
	p, z, _, _, _ := setupDormir(t)
	p.Destino = destino.Ninguno{}
	if _, err := z.Dormir(context.Background(), "p0dxD", false, false); !errors.Is(err, ErrSoloComputadora) {
		t.Fatalf("sleeping in a cluster: %v", err)
	}
	if _, err := z.Opciones(context.Background(), "08:00", false); !errors.Is(err, ErrSoloComputadora) {
		t.Fatalf("options in a cluster: %v", err)
	}
	if p.Dormido() {
		t.Fatal("it sleeps in a cluster")
	}
}
