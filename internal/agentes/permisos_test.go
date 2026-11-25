package agentes

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/destino"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// permStore is askStore keeping the coding agent's permissions in memory:
// a mode per permission, the grants a person gave (by permission, with
// their action for effects) and what was asked and noted.
type permStore struct {
	*askStore
	modos   map[string]string
	dados   map[string]*store.PermisoDado
	used    map[int64]bool
	pedidos []store.PermisoPedido
	log     []string
	bitac   []store.Bitacora
}

func (s *permStore) PermisoDecide(_ context.Context, _ int64, sujeto, permiso, _, accion string, _ int64) (store.Decision, error) {
	if sujeto != store.SujetoCoder {
		return store.Decision{}, nil
	}
	m, ok := s.modos[permiso]
	if !ok {
		m = store.PermisoDefault(sujeto, permiso)
	}
	d := store.Decision{Modo: m}
	if g, ok := s.dados[permiso]; ok && m == store.ModoAsk && (g.Accion == accion || permiso != store.PermEffects) && !s.used[g.ID] {
		d.Dado = g
	}
	return d, nil
}

func (s *permStore) UsarPermisoDado(_ context.Context, id int64) (bool, error) {
	if s.used[id] {
		return false, nil
	}
	s.used[id] = true
	return true, nil
}

func (s *permStore) PedirPermiso(_ context.Context, p store.PermisoPedido) (store.PermisoPedido, error) {
	p.ID = int64(len(s.pedidos) + 1)
	s.pedidos = append(s.pedidos, p)
	return p, nil
}

func (s *permStore) NoteEncargoPermiso(_ context.Context, _ int64, _, what, permiso, note string) error {
	s.log = append(s.log, what+" "+permiso+": "+note)
	return nil
}

func (s *permStore) AnotarBitacora(_ context.Context, b store.Bitacora) (store.Bitacora, error) {
	s.bitac = append(s.bitac, b)
	return b, nil
}

func newPermStore(modos map[string]string) (*permStore, *coderStore) {
	st := &coderStore{}
	return &permStore{askStore: &askStore{coderStore: st}, modos: modos, dados: map[string]*store.PermisoDado{}, used: map[int64]bool{}}, st
}

// infraFake is the platform's infra in memory: what a look asked, and a
// pod whose log has a line that looks like a credential.
type infraFake struct {
	looks     []destino.Mirar
	restarted []string
	copies    int
}

func (f *infraFake) MirarApp(_ context.Context, app *store.App, q destino.Mirar) (destino.Mirada, error) {
	f.looks = append(f.looks, q)
	return destino.Mirada{Donde: "cluster", Espacio: app.Name,
		Pods: []destino.PodVisto{{Nombre: app.Name + "-api-7d9", Fase: "Pending", Condiciones: []string{"PodScheduled=False Unschedulable: 0/6 nodes are available: Insufficient memory"}}},
		Logs: []destino.LogVisto{{Pod: app.Name + "-api-7d9", Contenedor: "api", Texto: "starting\nconfig password: wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLEKEY\nready"}}}, nil
}
func (f *infraFake) ReiniciarServicio(_ context.Context, _ *store.App, svc string) error {
	f.restarted = append(f.restarted, svc)
	return nil
}
func (f *infraFake) LanzarRama(context.Context, *store.App, string) (*store.Run, error) {
	return &store.Run{ID: 77}, nil
}
func (f *infraFake) RehacerCopia(context.Context, *store.App, int, string) (string, error) {
	return "remade", nil
}
func (f *infraFake) CopiasTexto(context.Context, *store.App) (string, error) {
	f.copies++
	return "- pull request #3: Healthy", nil
}

// Off hides: a permission turned off leaves its tools out, and the model
// is told so; called anyway, the tool answers it is off and runs nothing.
func TestPermisoOffHidesTheTools(t *testing.T) {
	ps, st := newPermStore(map[string]string{store.PermPlatformRead: store.ModoOff, store.PermInfraRead: store.ModoOff, store.PermCopiesRead: store.ModoOff})
	inf := &infraFake{}
	m := &script{t: t, cost: 1_000, turns: [][]ia.ToolCall{{call("infra_read", map[string]any{"what": []string{"pods"}})}, {call("runs", map[string]any{})}}}
	c := newCoder(st, &repos{st: st, files: map[string][]byte{"a.go": []byte("package a\n")}}, m)
	c.Store, c.Infra = ps, inf
	sw := &seen{m: m}
	c.Model = sw
	_ = c.Do(context.Background(), store.Encargo{ID: 31, OrgID: 1, App: "permoff-app"})
	tools := " " + strings.Join(sw.tools, " ") + " "
	for _, name := range []string{"infra_read", "runs", "step_log", "slow_requests", "app_status", "copies", "restart_service"} {
		if strings.Contains(tools, " "+name+" ") {
			t.Errorf("%s offered while its permission is off: %s", name, tools)
		}
	}
	if !strings.Contains(sw.system, "turned off for you") {
		t.Errorf("not told what is off:\n%s", sw.system)
	}
	if len(inf.looks) != 0 || len(m.seen) < 2 || !strings.Contains(m.seen[0], "turned off") || !strings.Contains(m.seen[1], "turned off") {
		t.Fatalf("looked %d times; results %q", len(inf.looks), m.seen)
	}
}

// Always runs: the infrastructure is read for the encargo's app, a line
// that looks like a credential is left out, and the use goes to the
// encargo's history and the bitácora.
func TestPermisoAlwaysRuns(t *testing.T) {
	ps, st := newPermStore(map[string]string{store.PermInfraRead: store.ModoAlways})
	inf := &infraFake{}
	m := &script{t: t, cost: 1_000, turns: [][]ia.ToolCall{{call("infra_read", map[string]any{"what": []string{"pods", "logs"}, "lines": 50, "minutes": 10})}}}
	c := newCoder(st, &repos{st: st, files: map[string][]byte{"a.go": []byte("package a\n")}}, m)
	c.Store, c.Infra = ps, inf
	_ = c.Do(context.Background(), store.Encargo{ID: 32, OrgID: 1, App: "permalways-app"})
	if len(inf.looks) != 1 || inf.looks[0].Lineas != 50 || inf.looks[0].Ventana != 10*time.Minute {
		t.Fatalf("looks %+v", inf.looks)
	}
	out := m.seen[0]
	if !strings.Contains(out, "Insufficient memory") || strings.Contains(out, "wJalrXUtnFEMIK7MDENG") || !strings.Contains(out, "looks like a secret") {
		t.Fatalf("the look: %s", out)
	}
	if len(ps.log) != 1 || !strings.HasPrefix(ps.log[0], store.LogPermisoUso+" "+store.PermInfraRead) || strings.Contains(ps.log[0], "wJalr") {
		t.Fatalf("history %q", ps.log)
	}
	if len(ps.bitac) != 1 || ps.bitac[0].Accion != "permiso_usar" || ps.bitac[0].Detalle["permiso"] != store.PermInfraRead {
		t.Fatalf("bitácora %+v", ps.bitac)
	}
}

// Ask asks: the tool says so and takes why; without why it says how to
// ask; with it, the encargo asks the person with the request (the ask
// machinery: it stops without spending). Given, the next call runs.
func TestPermisoAskAsks(t *testing.T) {
	ps, st := newPermStore(nil) // infra_read asks by default
	inf := &infraFake{}
	m := &script{t: t, cost: 1_000, turns: [][]ia.ToolCall{
		{call("infra_read", map[string]any{"what": []string{"events"}})},
		{call("infra_read", map[string]any{"what": []string{"events"}, "why": "El rollout se agotó; los eventos dicen si faltó memoria."})},
		{call("read_file", map[string]string{"path": "a.go"})},
	}}
	c := newCoder(st, &repos{st: st, files: map[string][]byte{"a.go": []byte("package a\n")}}, m)
	c.Store, c.Infra = ps, inf
	_ = c.Do(context.Background(), store.Encargo{ID: 33, OrgID: 1, App: "permask-app"})
	if m.n != 2 || len(inf.looks) != 0 {
		t.Fatalf("%d turns, %d looks: it should stop at the request", m.n, len(inf.looks))
	}
	if !strings.Contains(m.seen[0], "call infra_read again with why") {
		t.Fatalf("without why: %q", m.seen[0])
	}
	if len(ps.pedidos) != 1 || len(ps.asked) != 1 {
		t.Fatalf("pedidos %+v, asked %+v", ps.pedidos, ps.asked)
	}
	p, q := ps.pedidos[0], ps.asked[0]
	if p.Permiso != store.PermInfraRead || p.App != "permask-app" || !strings.Contains(p.PorQue, "faltó memoria") || p.EncargoID == nil || *p.EncargoID != 33 ||
		p.Ventana == "" || q.PedidoID == nil || *q.PedidoID != p.ID || !strings.Contains(q.Question, "infraestructura") {
		t.Fatalf("the request %+v, the question %+v", p, q)
	}
	if len(ps.log) != 1 || !strings.HasPrefix(ps.log[0], store.LogPermisoPedido) {
		t.Fatalf("history %q", ps.log)
	}
	// Given for the encargo: the next job sees the tool as its own and it runs.
	ps.dados[store.PermInfraRead] = &store.PermisoDado{ID: 5, Permiso: store.PermInfraRead, Alcance: store.AlcanceEncargo}
	m2 := &script{t: t, cost: 1_000, turns: [][]ia.ToolCall{{call("infra_read", map[string]any{"what": []string{"events"}})}}}
	c.Model = m2
	_ = c.Do(context.Background(), store.Encargo{ID: 33, OrgID: 1, App: "permask-app"})
	if len(inf.looks) != 1 || len(ps.pedidos) != 1 {
		t.Fatalf("after the grant: %d looks, %d requests (%q)", len(inf.looks), len(ps.pedidos), m2.seen)
	}
}

// Effects never go always, and each use is a person's yes to that very
// action, once: a grant for another action does not count, and a used
// one asks again.
func TestPermisoEffectsOnce(t *testing.T) {
	ps, st := newPermStore(map[string]string{store.PermEffects: store.ModoAsk})
	ps.dados[store.PermEffects] = &store.PermisoDado{ID: 8, Permiso: store.PermEffects, Alcance: store.AlcanceOnce, Accion: "restart:api"}
	inf := &infraFake{}
	m := &script{t: t, cost: 1_000, turns: [][]ia.ToolCall{
		{call("restart_service", map[string]any{"service": "web"})},
		{call("restart_service", map[string]any{"service": "api"})},
		{call("restart_service", map[string]any{"service": "api"})},
	}}
	c := newCoder(st, &repos{st: st, files: map[string][]byte{"a.go": []byte("package a\n")}}, m)
	c.Store, c.Infra = ps, inf
	sw := &seen{m: m}
	c.Model = sw
	_ = c.Do(context.Background(), store.Encargo{ID: 34, OrgID: 1, App: "permefecto-app"})
	if strings.Join(inf.restarted, ",") != "api" {
		t.Fatalf("restarted %q; results %q", inf.restarted, m.seen)
	}
	if !strings.Contains(m.seen[0], "needs a person's permission") || !strings.Contains(m.seen[2], "needs a person's permission") {
		t.Fatalf("another action, and the used grant, must ask: %q", m.seen)
	}
	if store.ModoValido(store.PermEffects, store.ModoAlways) {
		t.Fatal("effects may be always")
	}
	if accionOf(call("restart_service", map[string]any{"service": "api"})) != "restart:api" {
		t.Fatal("the action of a restart")
	}
}

// Without a PermisoStore (the platform before permissions), the coder
// does what it did: the platform's views, no infrastructure, no effects.
func TestPermisoLegacy(t *testing.T) {
	st := &coderStore{}
	m := &script{t: t, cost: 1_000, turns: [][]ia.ToolCall{{call("infra_read", map[string]any{})}}}
	c := newCoder(st, &repos{st: st, files: map[string][]byte{"a.go": []byte("package a\n")}}, m)
	c.Infra = &infraFake{}
	sw := &seen{m: m}
	c.Model = sw
	_ = c.Do(context.Background(), store.Encargo{ID: 35, OrgID: 1, App: "permlegacy-app"})
	tools := " " + strings.Join(sw.tools, " ") + " "
	if !strings.Contains(tools, " runs ") || strings.Contains(tools, " infra_read ") || strings.Contains(tools, " restart_service ") {
		t.Fatalf("tools %s", tools)
	}
	if !Permite(context.Background(), st, 1, store.SujetoDiagnostico, store.PermPlatformRead, "x", "") ||
		Permite(context.Background(), st, 1, store.SujetoEstimador, store.PermPlatformRead, "x", "") ||
		!Permite(context.Background(), st, 1, store.SujetoDiagnostico, store.PermInfraRead, "x", store.AccionPasos) ||
		Permite(context.Background(), st, 1, store.SujetoDiagnostico, store.PermInfraRead, "x", "") {
		t.Fatal("the defaults of the hook")
	}
}
