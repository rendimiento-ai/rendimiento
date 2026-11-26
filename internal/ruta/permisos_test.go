package ruta

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/destino"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// fakePermisos keeps the keys' permissions in memory: a mode per
// permission (the default otherwise), the grants given and the requests.
type fakePermisos struct {
	mu      sync.Mutex
	modos   map[string]string
	dados   []*store.PermisoDado
	used    map[int64]bool
	pedidos []store.PermisoPedido
}

func (f *fakePermisos) PermisoDecide(_ context.Context, _ int64, sujeto, permiso, app, accion string, _ int64) (store.Decision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.modos[permiso]
	if !ok {
		m = store.PermisoDefault(sujeto, permiso)
	}
	d := store.Decision{Modo: m}
	for _, g := range f.dados {
		if m == store.ModoAsk && g.Sujeto == sujeto && g.Permiso == permiso && g.App == app && (permiso != store.PermEffects || g.Accion == accion) && !f.used[g.ID] {
			d.Dado = g
		}
	}
	return d, nil
}

func (f *fakePermisos) UsarPermisoDado(_ context.Context, id int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.dados {
		if g.ID == id && g.Alcance != store.AlcanceOnce {
			return true, nil
		}
	}
	if f.used[id] {
		return false, nil
	}
	f.used[id] = true
	return true, nil
}

func (f *fakePermisos) PedirPermiso(_ context.Context, p store.PermisoPedido) (store.PermisoPedido, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, o := range f.pedidos {
		if o.Sujeto == p.Sujeto && o.Permiso == p.Permiso && o.App == p.App && o.Accion == p.Accion {
			if p.PorQue != "" {
				f.pedidos[i].PorQue = p.PorQue
			}
			return f.pedidos[i], nil
		}
	}
	p.ID = int64(len(f.pedidos) + 1)
	f.pedidos = append(f.pedidos, p)
	return p, nil
}

func (f *fakePermisos) PermisoPedidoDe(_ context.Context, id int64) (store.PermisoPedido, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.pedidos {
		if p.ID == id {
			return p, nil
		}
	}
	return store.PermisoPedido{}, store.ErrNotFound
}

func (f *fakePermisos) dar(sujeto, permiso, app, alcance, accion string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dados = append(f.dados, &store.PermisoDado{ID: int64(len(f.dados) + 1), Sujeto: sujeto, Permiso: permiso, App: app, Alcance: alcance, Accion: accion})
}

type fakeInfra struct {
	mu    sync.Mutex
	looks []string
}

func (f *fakeInfra) MirarApp(_ context.Context, app *store.App, q destino.Mirar) (destino.Mirada, error) {
	f.mu.Lock()
	f.looks = append(f.looks, app.Name)
	f.mu.Unlock()
	return destino.Mirada{Donde: "cluster", Espacio: app.Name, Logs: []destino.LogVisto{{Pod: "web-1", Contenedor: "web", Texto: "arrancó\n  password: wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLEKEY\nlisto"}}}, nil
}
func (f *fakeInfra) ReiniciarServicio(context.Context, *store.App, string) error { return nil }
func (f *fakeInfra) RehacerCopia(context.Context, *store.App, int, string) (string, error) {
	return "rehecha", nil
}
func (f *fakeInfra) CopiasTexto(context.Context, *store.App) (string, error) {
	return "sin copias", nil
}

// An MCP key's permissions (Ruta 184): off answers it is off and reads
// nothing; ask asks a person (with the key's cargo, once for the same
// thing; permiso_pedir adds why) and says so; given always for the app,
// the next call works, with a secret-looking line left out. Effects need a
// person's yes for that very action, once: running a branch included.
func TestPermisosDeLlave(t *testing.T) {
	m := &memory{keys: map[string]store.AgentKey{}}
	tok, h, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	k := store.AgentKey{ID: 41, Name: "claude-de-ana", Role: store.RoleCollaborator, CreatedBy: "p0dxD", ExpiresAt: time.Now().Add(time.Hour)}
	m.keys[h] = k
	fp := &fakePlataforma{apps: []*store.App{{ID: 1, Name: "shop", Repo: "o/shop", DefaultBranch: "main", Spec: spec.Spec{Services: []spec.Service{{Name: "web"}}}}},
		runs: map[int64]*store.Run{10: {ID: 10, AppID: 1, Branch: "arreglo", Status: store.RunFailed}}}
	perms := &fakePermisos{modos: map[string]string{store.PermPlatformRead: store.ModoOff}, used: map[int64]bool{}}
	inf := &fakeInfra{}
	srv := httptest.NewServer((&Server{Store: m, Plataforma: fp, Permisos: perms, Infra: inf}).Handler())
	defer srv.Close()
	s, err := connect(t, srv.URL, tok)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sujeto := store.SujetoLlave(k.ID)

	if _, msg := call(t, s, "app_estado", map[string]any{"app": "shop"}); !strings.Contains(msg, "apagado") {
		t.Fatalf("platform_read off: %q", msg)
	}
	if _, msg := call(t, s, "ejecucion_leer", map[string]any{"id": 10}); !strings.Contains(msg, "apagado") {
		t.Fatalf("platform_read off, a run: %q", msg)
	}
	call(t, s, "cargo_tomar", map[string]any{"proposito": "Ver por qué no arranca shop"})
	_, msg := call(t, s, "infra_leer", map[string]any{"app": "shop", "que": []string{"logs"}})
	if !strings.Contains(msg, "necesita permiso") || !strings.Contains(msg, "p0dxD") || !strings.Contains(msg, "permiso_pedir") || len(inf.looks) != 0 {
		t.Fatalf("ask: %q (looked %v)", msg, inf.looks)
	}
	call(t, s, "infra_leer", map[string]any{"app": "shop"})
	if len(perms.pedidos) != 1 || perms.pedidos[0].Sujeto != sujeto || perms.pedidos[0].CargoID == nil || perms.pedidos[0].Permiso != store.PermInfraRead {
		t.Fatalf("requests %+v", perms.pedidos)
	}
	out, msg := call(t, s, "permiso_pedir", map[string]any{"pedido": perms.pedidos[0].ID, "por_que": "El pod no arranca; los registros dicen por qué."})
	if msg != "" || out["pedido"] != float64(perms.pedidos[0].ID) || !strings.Contains(perms.pedidos[0].PorQue, "no arranca") {
		t.Fatalf("permiso_pedir: %v %q %+v", out, msg, perms.pedidos)
	}
	perms.dar(sujeto, store.PermInfraRead, "shop", store.AlcanceAlways, "")
	out, msg = call(t, s, "infra_leer", map[string]any{"app": "shop", "que": []string{"logs"}})
	if texto, _ := out["texto"].(string); msg != "" || !strings.Contains(texto, "arrancó") || strings.Contains(texto, "wJalrXUtnFEMIK7MDENG") || len(inf.looks) != 1 {
		t.Fatalf("given always: %v %q", out, msg)
	}
	// Effects: running a branch asks; another branch's yes does not count; used, it asks again.
	if _, msg := call(t, s, "ejecucion_lanzar", map[string]any{"app": "shop", "rama": "arreglo"}); !strings.Contains(msg, "apagado") {
		t.Fatalf("effects off by default: %q", msg)
	}
	perms.mu.Lock()
	perms.modos[store.PermEffects] = store.ModoAsk
	perms.mu.Unlock()
	perms.dar(sujeto, store.PermEffects, "shop", store.AlcanceOnce, "rerun:otra")
	if _, msg := call(t, s, "ejecucion_lanzar", map[string]any{"app": "shop", "rama": "arreglo"}); !strings.Contains(msg, "necesita permiso") || len(fp.queued) != 0 {
		t.Fatalf("another action's yes: %q %v", msg, fp.queued)
	}
	perms.dar(sujeto, store.PermEffects, "shop", store.AlcanceOnce, "rerun:arreglo")
	if _, msg := call(t, s, "ejecucion_lanzar", map[string]any{"app": "shop", "rama": "arreglo"}); msg != "" || len(fp.queued) != 1 {
		t.Fatalf("its yes: %q %v", msg, fp.queued)
	}
	if _, msg := call(t, s, "ejecucion_lanzar", map[string]any{"app": "shop", "rama": "arreglo"}); !strings.Contains(msg, "necesita permiso") || len(fp.queued) != 1 {
		t.Fatalf("used once: %q %v", msg, fp.queued)
	}
	// Another app is never its: the key's own limits come first.
	if _, msg := call(t, s, "infra_leer", map[string]any{"app": "blog"}); !strings.Contains(msg, "no existe") {
		t.Fatalf("another app: %q", msg)
	}
}
