package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The Permissions section (Ruta 184): every subject with its permissions
// in one call, which members see; only an administrator changes them, an
// action with effect is never always, and an agent's key never reaches
// these routes (so it cannot grant itself). What the coding agent asks for
// shows in «Para usted» as a request, not a question; a collaborator may
// say no, an administrator gives it, and the encargo goes on with it.
func TestPermisosRoutes(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	me := e.session(t, "p0dxD")
	if err := e.store.SetMember(ctx, store.DefaultOrg, "colab-permisos", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	colab := e.session(t, "colab-permisos")
	tok := randomToken()
	key, err := e.store.CreateAgentKey(ctx, store.DefaultOrg, "claude-permisos", hashToken(tok), store.RoleCollaborator, nil, "p0dxD", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	r, body := e.do(t, "GET", "/api/permisos", "", colab, nil)
	var v PermisosView
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &v) != nil || len(v.Sujetos) != len(store.SujetosPlataforma)+1 ||
		v.Sujetos[0].ID != store.SujetoCoder || v.Sujetos[len(v.Sujetos)-1].ID != store.SujetoLlave(key.ID) || v.Sujetos[len(v.Sujetos)-1].Name != "claude-permisos" {
		t.Fatalf("a member sees: %d %s", r.StatusCode, body)
	}
	set := `{"permisos":[{"permiso":"infra_read","modo":"always","apps":["permisos-tienda"]}]}`
	if r, _ := e.do(t, "PUT", "/api/permisos/coder", set, colab, nil); r.StatusCode != 403 {
		t.Fatalf("a collaborator changed it: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "PUT", "/api/permisos/coder", set, nil, map[string]string{"Authorization": "Bearer " + tok}); r.StatusCode != 401 {
		t.Fatalf("an agent key changed it: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "PUT", "/api/permisos/coder", `{"permisos":[{"permiso":"effects","modo":"always"}]}`, me, nil); r.StatusCode != 400 {
		t.Fatalf("effects always: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "PUT", "/api/permisos/mcp:999", set, me, nil); r.StatusCode != 404 {
		t.Fatalf("a key that is none: %d", r.StatusCode)
	}
	if r, body := e.do(t, "PUT", "/api/permisos/coder", set, me, nil); r.StatusCode != 200 || !strings.Contains(body, `"permisos-tienda"`) {
		t.Fatalf("an administrator: %d %s", r.StatusCode, body)
	}
	if p, _ := e.store.PermisoOf(ctx, store.DefaultOrg, store.SujetoCoder, store.PermInfraRead); p.Modo != store.ModoAlways || p.UpdatedBy != "p0dxD" {
		t.Fatalf("saved %+v", p)
	}

	// The coding agent asks for the infrastructure of its encargo's app.
	coder, err := e.store.CreateAgentKey(ctx, store.DefaultOrg, "agente de código", "h-coder-permisos", store.RoleCollaborator, nil, "rendimiento", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := e.store.CreateEncargo(ctx, store.Encargo{Title: "Por qué no arranca", App: "permisos-tienda", CreatedBy: "p0dxD"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.store.TakeCargo(ctx, coder.ID, "permisos")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.TakeEncargo(ctx, enc.ID, coder.ID, c.ID, coder.Name); err != nil {
		t.Fatal(err)
	}
	id := enc.ID
	p, err := e.store.PedirPermiso(ctx, store.PermisoPedido{OrgID: store.DefaultOrg, Sujeto: store.SujetoCoder, Permiso: store.PermInfraRead, App: "permisos-tienda",
		Que: "ver los eventos de permisos-tienda", PorQue: "el rollout se agotó", EncargoID: &id, AskedBy: coder.Name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.AskEncargo(ctx, enc.ID, coder.ID, coder.Name, store.EncargoPregunta{Question: "Permiso: ver los eventos", Options: []string{}, PedidoID: &p.ID}); err != nil {
		t.Fatal(err)
	}
	r, body = e.do(t, "GET", "/api/ruta/portada", "", me, nil)
	if r.StatusCode != 200 || !strings.Contains(body, `"asking":[]`) || !strings.Contains(body, `"porQue":"el rollout se agotó"`) {
		t.Fatalf("«Para usted»: %s", body)
	}
	path := fmt.Sprintf("/api/permisos/pedidos/%d", p.ID)
	if r, _ := e.do(t, "POST", path, `{"answer":"always"}`, colab, nil); r.StatusCode != 403 {
		t.Fatalf("a collaborator gave it: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "POST", path, `{"answer":"encargo"}`, nil, map[string]string{"Authorization": "Bearer " + tok}); r.StatusCode != 401 {
		t.Fatalf("an agent key gave it: %d", r.StatusCode)
	}
	if r, body := e.do(t, "POST", path, `{"answer":"encargo"}`, me, nil); r.StatusCode != 200 || !strings.Contains(body, `"alcance":"encargo"`) {
		t.Fatalf("given: %d %s", r.StatusCode, body)
	}
	if r, _ := e.do(t, "POST", path, `{"answer":"no"}`, me, nil); r.StatusCode != 409 {
		t.Fatalf("answered twice: %d", r.StatusCode)
	}
	got, err := e.store.Encargo(ctx, enc.ID)
	if err != nil || got.Status != store.EncargoPending || !got.DoNow {
		t.Fatalf("the encargo goes on: %+v (%v)", got, err)
	}
	var dado bool
	for _, l := range got.Log {
		dado = dado || (l.What == store.LogPermisoDado && l.Permiso == store.PermInfraRead && l.Who == "p0dxD")
	}
	if !dado {
		t.Fatalf("the history: %+v", got.Log)
	}
	if d, _ := e.store.PermisoDecide(ctx, store.DefaultOrg, store.SujetoCoder, store.PermInfraRead, "permisos-tienda", "", enc.ID); d.Modo != store.ModoAlways {
		// infra_read is always for permisos-tienda since the PUT above; the grant is there too
		t.Fatalf("decision %+v", d)
	}
	r, body = e.do(t, "GET", "/api/permisos", "", me, nil)
	if r.StatusCode != 200 || !strings.Contains(body, `"alcance":"encargo"`) {
		t.Fatalf("the grant is listed: %s", body)
	}
	_ = json.Unmarshal([]byte(body), &v)
	if r, _ := e.do(t, "DELETE", fmt.Sprintf("/api/permisos/dados/%d", v.Dados[0].ID), "", colab, nil); r.StatusCode != 403 {
		t.Fatalf("a collaborator took it back: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "DELETE", fmt.Sprintf("/api/permisos/dados/%d", v.Dados[0].ID), "", me, nil); r.StatusCode != 204 {
		t.Fatalf("taken back: %d", r.StatusCode)
	}
	// An MCP key's request: a collaborator may say no.
	kp, err := e.store.PedirPermiso(ctx, store.PermisoPedido{OrgID: store.DefaultOrg, Sujeto: store.SujetoLlave(key.ID), Permiso: store.PermEffects, App: "permisos-tienda",
		Accion: "restart:web", Que: "reiniciar web", AskedBy: key.Name})
	if err != nil {
		t.Fatal(err)
	}
	if r, body := e.do(t, "POST", fmt.Sprintf("/api/permisos/pedidos/%d", kp.ID), `{"answer":"no"}`, colab, nil); r.StatusCode != 200 || !strings.Contains(body, `"answer":"no"`) {
		t.Fatalf("a collaborator's no: %d %s", r.StatusCode, body)
	}
}
