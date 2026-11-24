package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/events"
	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/store"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
)

// Dormir (Ruta 92, decision 177): rendimiento on a computer can sleep and
// wake. Asleep, its apps' containers and the CI are stopped, and the
// data, releases and the Ruta stay as they are; rendimiento itself keeps
// running (its page, the Ruta), and the state is kept in the database, so
// a restart stays asleep. A publish going on is waited for, or cut ("ya":
// the app stays on its release, and the new one publishes on waking);
// branch runs stop at once and run again on waking, never marked failed.
// The forjas' webhooks that arrive meanwhile are kept and handled on
// waking, in order. «Apagar del todo» sleeps and then stops rendimiento
// itself, which wakes when it starts again with the computer.

// Cama is what sleeps on a computer: its apps' containers (destino.Local).
type Cama interface {
	// Dormir stops the apps' running containers and returns their names.
	Dormir(ctx context.Context) ([]string, error)
	// Despertar starts those containers again and returns the apps that run.
	Despertar(ctx context.Context, contenedores []string, apps []*store.App) ([]string, error)
}

// ErrSoloComputadora: only rendimiento on a computer sleeps; a cluster
// serves others and stays awake.
var ErrSoloComputadora = errors.New(M("only rendimiento on a computer can sleep: a cluster stays awake"))

// ErrHoraInvalida: the hour to wake at is not HH:MM.
var ErrHoraInvalida = errors.New(M("the hour to wake at must be HH:MM, such as 08:00"))

// Sueno keeps rendimiento's sleep on a computer: what it stops, waits for
// and keeps, and what it does on waking.
type Sueno struct {
	P    *Platform
	Cama Cama
	// Avisos handles the webhooks kept while asleep, in the order they
	// came, and says how many it handled and how many it could not (the
	// API's; nil: none are kept).
	Avisos func(ctx context.Context) (hechos, fallidos int)
	// Apagar stops rendimiento itself, once asleep («Apagar del todo»).
	Apagar func()
	// Now is the clock and Zona the time zone of the hour to wake at
	// (time.Now and time.Local when nil).
	Now  func() time.Time
	Zona *time.Location
	// Cada is how often it looks at a publish it waits for and at the hour
	// to wake (default 5s).
	Cada time.Duration

	mu        sync.Mutex // one change at a time
	esperando bool       // a goroutine waits for the publish to end
}

// Publicacion is a publish going on: a deploying run, or a release that
// starts or is watched.
type Publicacion struct {
	App string `json:"app"`
	// Run is the deploying run (0: its release is starting or watched), at
	// step Paso of Pasos.
	Run   int64 `json:"run,omitempty"`
	Paso  int   `json:"paso,omitempty"`
	Pasos int   `json:"pasos,omitempty"`
	// Actual is the release the app runs now (0: none yet), the one it
	// stays on when the publish is cut.
	Actual int64 `json:"actual,omitempty"`
}

// EstadoSueno is the sleep as the page and the tray show it.
type EstadoSueno struct {
	store.Sueno
	// Avisos is how many webhooks wait for the wake.
	Avisos int `json:"avisos"`
	// Corriendo is how many runs go now, and Publicando the publishes among
	// them (what Dormir would wait for).
	Corriendo  int           `json:"corriendo"`
	Publicando []Publicacion `json:"publicando"`
	// DespiertaA is when it wakes by itself (nil: when someone wakes it).
	DespiertaA *time.Time `json:"despiertaA,omitempty"`
}

func (z *Sueno) now() time.Time {
	if z.Now != nil {
		return z.Now()
	}
	return time.Now()
}

func (z *Sueno) zona() *time.Location {
	if z.Zona != nil {
		return z.Zona
	}
	return time.Local
}

func (z *Sueno) cada() time.Duration {
	if z.Cada > 0 {
		return z.Cada
	}
	return 5 * time.Second
}

// Estado is where the sleep stands now.
func (z *Sueno) Estado(ctx context.Context) (EstadoSueno, error) {
	st, err := z.P.Store.GetSueno(ctx)
	if err != nil {
		return EstadoSueno{}, err
	}
	out := EstadoSueno{Sueno: st, Publicando: z.P.publicaciones(ctx)}
	if out.Avisos, err = z.P.Store.ContarAvisos(ctx); err != nil {
		return out, err
	}
	z.P.mu.Lock()
	out.Corriendo = len(z.P.vivas)
	z.P.mu.Unlock()
	if st.Estado == store.Dormido && st.Desde != nil && st.DespertarA != "" {
		if at, ok := ProximoDespertar(*st.Desde, st.DespertarA, z.zona()); ok {
			out.DespiertaA = &at
		}
	}
	return out, nil
}

// Dormir puts rendimiento to sleep, for por. Branch runs stop now and run
// again on waking; a publish going on is waited for, unless ya cuts it
// too. apagar («Apagar del todo») then stops rendimiento itself, which
// wakes when it starts again. Asked again while it falls asleep, ya and
// apagar are added to what was asked.
func (z *Sueno) Dormir(ctx context.Context, por string, ya, apagar bool) (EstadoSueno, error) {
	if z.P.Target().Nombre() != "local" || z.Cama == nil {
		return EstadoSueno{}, ErrSoloComputadora
	}
	z.mu.Lock()
	st, err := z.P.Store.GetSueno(ctx)
	if err != nil {
		z.mu.Unlock()
		return EstadoSueno{}, err
	}
	if st.Estado == store.Dormido {
		// Already asleep: «Apagar del todo» can still be asked.
		if apagar && !st.Apagar {
			st.Apagar, st.AlArrancar = true, true
			err = z.P.Store.GuardarSueno(ctx, st)
			z.mu.Unlock()
			if err == nil && z.Apagar != nil {
				z.P.Log.Info("rendimiento shuts down: it wakes when it starts again", "by", por)
				go z.Apagar()
			}
		} else {
			z.mu.Unlock()
		}
		if err != nil {
			return EstadoSueno{}, err
		}
		return z.Estado(ctx)
	}
	z.P.dormido.Store(true) // nothing more is claimed
	if st.Estado == store.Despierto {
		now := z.now()
		st.Desde, st.Por, st.Cortadas, st.Contenedores, st.Esperada = &now, por, nil, nil, ""
	}
	st.Estado = store.Durmiendose
	st.Apagar = st.Apagar || apagar
	cortadas := z.P.cortarCorriendo(func(r *store.Run) bool { return ya || !r.Deploy })
	for _, id := range cortadas {
		if !slices.Contains(st.Cortadas, id) {
			st.Cortadas = append(st.Cortadas, id)
		}
	}
	if !ya && st.Esperada == "" {
		var nombres []string
		for _, pub := range z.P.publicaciones(ctx) {
			nombres = append(nombres, pub.App)
		}
		st.Esperada = strings.Join(nombres, ", ")
	}
	if err := z.P.Store.GuardarSueno(ctx, st); err != nil {
		z.mu.Unlock()
		return EstadoSueno{}, err
	}
	z.P.Log.Info("rendimiento falls asleep", "by", por, "now", ya, "shutDown", st.Apagar, "cut", len(cortadas))
	espera := !z.esperando
	z.esperando = true
	z.mu.Unlock()
	if espera {
		go z.terminarDeDormir(context.WithoutCancel(ctx))
	}
	return z.Estado(ctx)
}

// terminarDeDormir waits for the runs going to end (the cut ones go back
// to the queue; a publish ends), then stops the apps' containers.
func (z *Sueno) terminarDeDormir(ctx context.Context) {
	defer func() {
		z.mu.Lock()
		z.esperando = false
		z.mu.Unlock()
	}()
	for z.P.ocupada() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(z.cada()):
		}
		if !z.P.dormido.Load() {
			return // woken while it waited
		}
		// A branch run claimed just as it fell asleep stops too.
		if extra := z.P.cortarCorriendo(func(r *store.Run) bool { return !r.Deploy }); len(extra) > 0 {
			z.anotarCortadas(ctx, extra)
		}
	}
	z.mu.Lock()
	st, err := z.P.Store.GetSueno(ctx)
	if err != nil || st.Estado != store.Durmiendose {
		z.mu.Unlock()
		return // woken meanwhile
	}
	if err := z.dormirCama(ctx, &st); err != nil {
		z.P.Log.Warn("sleep: some containers did not stop", "err", err)
	}
	apagar := st.Apagar
	if apagar {
		st.AlArrancar = true
	}
	err = z.P.Store.GuardarSueno(ctx, st)
	z.mu.Unlock()
	if err != nil {
		z.P.Log.Error("sleep: could not keep the state", "err", err)
		return
	}
	z.P.Hub.Publish(TemaSueno, events.Event{Type: "sueno", Data: st.Estado})
	z.P.Log.Info("rendimiento is asleep", "containers", len(st.Contenedores))
	if apagar && z.Apagar != nil {
		z.P.Log.Info("rendimiento shuts down: it wakes when it starts again")
		z.Apagar()
	}
}

// anotarCortadas adds runs cut while it fell asleep to those repeated on
// waking.
func (z *Sueno) anotarCortadas(ctx context.Context, ids []int64) {
	z.mu.Lock()
	defer z.mu.Unlock()
	st, err := z.P.Store.GetSueno(ctx)
	if err != nil || st.Estado != store.Durmiendose {
		return
	}
	for _, id := range ids {
		if !slices.Contains(st.Cortadas, id) {
			st.Cortadas = append(st.Cortadas, id)
		}
	}
	if err := z.P.Store.GuardarSueno(ctx, st); err != nil {
		z.P.Log.Warn("sleep: could not note the runs it cut", "err", err)
	}
}

// dormirCama stops the apps' containers, adds them to those to start on
// waking and notes the time asleep (z.mu held).
func (z *Sueno) dormirCama(ctx context.Context, st *store.Sueno) error {
	names, err := z.Cama.Dormir(ctx)
	for _, n := range names {
		if !slices.Contains(st.Contenedores, n) {
			st.Contenedores = append(st.Contenedores, n)
		}
	}
	sort.Strings(st.Contenedores)
	st.Estado = store.Dormido
	if st.Desde == nil {
		now := z.now()
		st.Desde = &now
	}
	if perr := z.P.Store.EmpezarPeriodo(ctx, *st.Desde); perr != nil {
		z.P.Log.Warn("sleep: could not note when it slept", "err", perr)
	}
	return err
}

// TemaSueno is the events topic of the sleep's changes.
const TemaSueno = "sueno"

// Despertar wakes rendimiento, for por: the same containers start again
// (the same apps, at the same releases), the kept webhooks are handled in
// order, and the runs sleep cut are claimed again. What it did is kept as
// the wake's summary.
func (z *Sueno) Despertar(ctx context.Context, por string) (EstadoSueno, error) {
	z.mu.Lock()
	st, err := z.P.Store.GetSueno(ctx)
	if err != nil {
		z.mu.Unlock()
		return EstadoSueno{}, err
	}
	if st.Estado == store.Despierto {
		z.mu.Unlock()
		return z.Estado(ctx)
	}
	now := z.now()
	res := &store.Despertar{Hasta: now, Por: por, Esperada: st.Esperada, Repetidas: st.Cortadas, Apps: []string{}}
	if st.Desde != nil {
		res.Desde = *st.Desde
	}
	if res.Repetidas == nil {
		res.Repetidas = []int64{}
	}
	if len(st.Contenedores) > 0 && z.Cama != nil {
		apps, err := z.P.Store.ListApps(ctx)
		if err != nil {
			z.mu.Unlock()
			return EstadoSueno{}, err
		}
		started, err := z.Cama.Despertar(ctx, st.Contenedores, apps)
		res.Apps = append(res.Apps, started...)
		if err != nil {
			res.Problema = err.Error()
			z.P.Log.Warn("wake: some containers did not start", "err", err)
		}
	}
	z.P.dormido.Store(false)
	if z.Avisos != nil {
		res.Avisos, res.Fallidos = z.Avisos(ctx)
	}
	if err := z.P.Store.TerminarPeriodo(ctx, now); err != nil {
		z.P.Log.Warn("wake: could not note when it woke", "err", err)
	}
	st = store.Sueno{Estado: store.Despierto, DespertarA: st.DespertarA, Tapa: st.Tapa, Resumen: res}
	err = z.P.Store.GuardarSueno(ctx, st)
	z.mu.Unlock()
	if err != nil {
		return EstadoSueno{}, err
	}
	z.P.Hub.Publish(TemaSueno, events.Event{Type: "sueno", Data: st.Estado})
	z.P.Log.Info("rendimiento woke", "by", por, "apps", len(res.Apps), "webhooks", res.Avisos, "repeated", len(res.Repetidas))
	return z.Estado(ctx)
}

// Arrancar is the sleep when rendimiento starts, before the queue is
// claimed: awake stays awake; after «Apagar del todo» it wakes; asleep it
// stays asleep, and what the engine started with the computer (the apps'
// containers restart with it) is stopped again.
func (z *Sueno) Arrancar(ctx context.Context) error {
	st, err := z.P.Store.GetSueno(ctx)
	if err != nil || st.Estado == store.Despierto {
		return err
	}
	if st.AlArrancar {
		_, err := z.Despertar(ctx, "")
		return err
	}
	z.P.dormido.Store(true)
	if z.Cama == nil {
		return nil
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if err := z.dormirCama(ctx, &st); err != nil {
		z.P.Log.Warn("sleep: some containers did not stop", "err", err)
	}
	return z.P.Store.GuardarSueno(ctx, st)
}

// Opciones keeps the sleep's options: the hour to wake at by itself
// ("08:00"; "" off) and «Dormir al cerrar la tapa» (for the desktop app).
func (z *Sueno) Opciones(ctx context.Context, despertarA string, tapa bool) (EstadoSueno, error) {
	if z.P.Target().Nombre() != "local" || z.Cama == nil {
		return EstadoSueno{}, ErrSoloComputadora
	}
	if despertarA != "" {
		if _, err := time.Parse("15:04", despertarA); err != nil {
			return EstadoSueno{}, ErrHoraInvalida
		}
	}
	z.mu.Lock()
	st, err := z.P.Store.GetSueno(ctx)
	if err == nil {
		st.DespertarA, st.Tapa = despertarA, tapa
		err = z.P.Store.GuardarSueno(ctx, st)
	}
	z.mu.Unlock()
	if err != nil {
		return EstadoSueno{}, err
	}
	return z.Estado(ctx)
}

// Run wakes rendimiento at its hour, when one is set, until ctx ends.
func (z *Sueno) Run(ctx context.Context) {
	for {
		z.MirarReloj(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(z.cada()):
		}
	}
}

// MirarReloj wakes rendimiento when its hour came; it reports whether it did.
func (z *Sueno) MirarReloj(ctx context.Context) bool {
	st, err := z.P.Store.GetSueno(ctx)
	if err != nil || st.Estado != store.Dormido || st.DespertarA == "" || st.Desde == nil {
		return false
	}
	at, ok := ProximoDespertar(*st.Desde, st.DespertarA, z.zona())
	if !ok || z.now().Before(at) {
		return false
	}
	if _, err := z.Despertar(ctx, "reloj"); err != nil {
		z.P.Log.Warn("could not wake at its hour", "err", err)
		return false
	}
	return true
}

// ProximoDespertar is the first time at hhmm ("08:00") after desde, in loc.
func ProximoDespertar(desde time.Time, hhmm string, loc *time.Location) (time.Time, bool) {
	h, err := time.Parse("15:04", hhmm)
	if err != nil {
		return time.Time{}, false
	}
	d := desde.In(loc)
	at := time.Date(d.Year(), d.Month(), d.Day(), h.Hour(), h.Minute(), 0, 0, loc)
	if !at.After(d) {
		at = at.AddDate(0, 0, 1)
	}
	return at, true
}

// ---- the platform's side ----

// Dormido reports whether rendimiento sleeps (or falls asleep): no run is
// claimed, the forjas are not asked for commits, and webhooks are kept.
func (p *Platform) Dormido() bool { return p.dormido.Load() }

func (p *Platform) cortada(id int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cortes[id]
}

// cortarCorriendo cuts the runs going here that cual picks: each goes back
// to the queue when it stops (execute).
func (p *Platform) cortarCorriendo(cual func(*store.Run) bool) []int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []int64
	for id, r := range p.vivas {
		if p.cortes[id] || !cual(r) {
			continue
		}
		if p.cortes == nil {
			p.cortes = map[int64]bool{}
		}
		p.cortes[id] = true
		if c := p.cancels[id]; c != nil {
			c()
		}
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// desplegando is the destination's release starting now, when it can say.
func (p *Platform) desplegando() bool {
	d, ok := p.Target().(interface{ Desplegando() bool })
	return ok && d.Desplegando()
}

// publicaciones are the publishes going on: deploying runs, and releases
// still watched by their verification.
func (p *Platform) publicaciones(ctx context.Context) []Publicacion {
	p.mu.Lock()
	var runs []*store.Run
	for _, r := range p.vivas {
		if r.Deploy && !p.cortes[r.ID] {
			runs = append(runs, r)
		}
	}
	watched := make([]int64, 0, len(p.verifying))
	for app := range p.verifying {
		watched = append(watched, app)
	}
	p.mu.Unlock()
	out := []Publicacion{}
	seen := map[int64]bool{}
	for _, r := range runs {
		pub := Publicacion{Run: r.ID}
		if a, err := p.Store.GetAppByID(ctx, r.AppID); err == nil {
			pub.App = a.Name
		}
		if full, err := p.Store.GetRun(ctx, r.ID); err == nil {
			pub.Pasos = len(full.Steps)
			for _, s := range full.Steps {
				if s.Status != "pending" && s.Status != "running" {
					pub.Paso++
				}
			}
		}
		if rel, err := p.Store.LatestRelease(ctx, r.AppID); err == nil {
			pub.Actual = rel.Number
		}
		seen[r.AppID] = true
		out = append(out, pub)
	}
	for _, id := range watched {
		if seen[id] {
			continue
		}
		pub := Publicacion{}
		if a, err := p.Store.GetAppByID(ctx, id); err == nil {
			pub.App = a.Name
		}
		if rel, err := p.Store.LatestRelease(ctx, id); err == nil {
			pub.Actual = rel.Number
		}
		out = append(out, pub)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].App < out[j].App })
	return out
}

// ocupada reports whether runs or a publish still go: sleep waits for them.
func (p *Platform) ocupada() bool {
	p.mu.Lock()
	n := len(p.vivas) + len(p.verifying)
	p.mu.Unlock()
	return n > 0 || p.desplegando()
}

// repetirAlDespertar puts a run sleep cut back in the queue and says so on
// its check: it is waiting, not failed.
func (p *Platform) repetirAlDespertar(ctx context.Context, app *store.App, run *store.Run, checkID int64) {
	msg := M("rendimiento slept; it runs again on waking")
	if err := p.Store.RepetirAlDespertar(ctx, run.ID, msg); err != nil {
		p.Log.Error("sleep: could not put the run back in the queue", "run", run.ID, "err", err)
	}
	if checkID != 0 {
		if err := p.repos(app).UpdateCheck(ctx, app.InstallationID, app.Repo, checkID, forja.Check{
			Name: "rendimiento: " + app.Name, HeadSHA: run.SHA, DetailsURL: fmt.Sprintf("%s/apps/%s/runs/%d", p.Config.BaseURL, app.Name, run.ID),
			Status: "queued", Output: forja.CheckOutput("rendimiento is asleep", "This run was stopped while rendimiento slept on its computer; it runs again when it wakes."),
		}); err != nil {
			p.Log.Warn("update check run", "err", err)
		}
	}
	p.publishRun(app, run.ID)
	p.Log.Info("run cut by sleep: it runs again on waking", "app", app.Name, "run", run.ID)
}
