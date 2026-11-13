package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rendimiento-ai/rendimiento/internal/mercado"
	"github.com/rendimiento-ai/rendimiento/internal/mesa"
	"github.com/rendimiento-ai/rendimiento/internal/ruta"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// plataformaDePrueba is the agents' platform tools over the real store,
// as main wires them, except that it only notes what would have reached
// the CI queue.
type plataformaDePrueba struct {
	*store.Store
	s      *Server
	hechos *[]string
}

func (p plataformaDePrueba) StepLog(ctx context.Context, run int64, step string) (string, error) {
	return p.s.StepLogText(ctx, run, step)
}

func (p plataformaDePrueba) QueueRun(_ context.Context, a *store.App, branch string) (*store.Run, error) {
	*p.hechos = append(*p.hechos, "queued "+a.Name+" "+branch)
	return nil, nil
}

func (p plataformaDePrueba) RetryRun(_ context.Context, id int64) (*store.Run, error) {
	*p.hechos = append(*p.hechos, "retried a run")
	return nil, nil
}

func (p plataformaDePrueba) CancelRun(_ context.Context, id int64) error {
	*p.hechos = append(*p.hechos, "cancelled a run")
	return nil
}

func (p plataformaDePrueba) Mesa(ctx context.Context, a *store.App) (mesa.Board, error) {
	return p.s.Platform.Mesa(ctx, a)
}

func (p plataformaDePrueba) WaitRun(ctx context.Context, id int64) (*store.Run, error) {
	return p.GetRun(ctx, id)
}

// produccionDePrueba notes a request for a rollback instead of making it.
type produccionDePrueba struct {
	*store.Store
	hechos *[]string
}

func (p produccionDePrueba) PedirReversion(_ context.Context, a *store.App, _ int64, _ string, _ store.AgentKey) (store.Aprobacion, error) {
	*p.hechos = append(*p.hechos, "asked to roll back "+a.Name)
	return store.Aprobacion{}, errors.New("not in this test")
}

// mercadoDePrueba is a Mercado with one stall of the principal's; it notes
// every call that reaches it.
type mercadoDePrueba struct{ hechos *[]string }

func (m mercadoDePrueba) Puestos(context.Context) ([]mercado.PuestoBrief, error) {
	*m.hechos = append(*m.hechos, "listed the stalls")
	return []mercado.PuestoBrief{{Puesto: "puesto-principal", Nombre: "Puesto de la principal", Dueno: "p0dxD"}}, nil
}

func (m mercadoDePrueba) LeerPuesto(context.Context, string) (*mercado.Pagina, error) {
	*m.hechos = append(*m.hechos, "read a stall")
	return &mercado.Pagina{Nombre: "Puesto de la principal"}, nil
}

func (m mercadoDePrueba) GuardarPuesto(context.Context, string, mercado.Pagina) error {
	*m.hechos = append(*m.hechos, "changed a stall")
	return nil
}

func (m mercadoDePrueba) Abrir(context.Context, string, mercado.Abrir) (*store.App, error) {
	*m.hechos = append(*m.hechos, "opened a stall")
	return &store.App{Name: "puesto-nuevo"}, nil
}

// Every MCP tool, called with a key of another organization and the first
// organization's names and numbers, finds nothing of it: it says the thing
// does not exist (or refuses), or answers with her own organization's and
// shows none of the first's. The tools are walked as the server lists
// them, so a tool added later needs its case here.
func TestEveryToolStaysInItsOrg(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	// The principal organization's: an app with a run, two releases and a
	// kept request; a note, an encargo, a key with a handed-over cargo,
	// people's comments on both, and a request to roll back.
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	app := &store.App{Name: "tienda", Repo: "p0dxD/tienda", DefaultBranch: "main", Spec: sp}
	if err := e.store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	run := &store.Run{AppID: app.ID, SHA: "abc123def", Branch: "rama-principal", Event: "push"}
	if err := e.store.CreateRun(ctx, run, nil); err != nil {
		t.Fatal(err)
	}
	for n := int64(1); n <= 2; n++ {
		if err := e.store.CreateRelease(ctx, &store.Release{AppID: app.ID, Number: n, SHA: "abc123def", Images: map[string]string{"web": "web@sha256:1"}, Spec: sp}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if err := e.store.EnsureTracePartitions(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SaveTraces(ctx, []store.Trace{{ID: "traza-principal", StartedAt: now, DurationUS: 5, Service: "tienda/web",
		Name: "GET /caja-principal", Services: []string{"tienda/web"}, Spans: 1, Reason: "sample"}}, nil); err != nil {
		t.Fatal(err)
	}
	nota, err := e.store.AddRuta(ctx, store.RutaEntry{Kind: store.KindPendiente, Title: "secreto de la principal", Author: "p0dxD"})
	if err != nil {
		t.Fatal(err)
	}
	enc, err := e.store.CreateEncargo(ctx, store.Encargo{Title: "encargo de la principal", App: "tienda", CreatedBy: "p0dxD"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := e.store.CreateAgentKey(ctx, store.DefaultOrg, "llave principal", "h-principal", store.RoleCollaborator, nil, "p0dxD", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	cargo, err := e.store.TakeCargo(ctx, key.ID, "cargo de la principal")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.HandOver(ctx, cargo.ID, "entrega de la principal", "falta lo de la principal"); err != nil {
		t.Fatal(err)
	}
	com, err := e.store.ComentarCargo(ctx, store.DefaultOrg, cargo.ID, "p0dxD", "comentario de la principal")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.ComentarEncargo(ctx, store.DefaultOrg, enc.ID, "p0dxD", "comentario del encargo principal"); err != nil {
		t.Fatal(err)
	}
	apr, err := e.store.CrearAprobacion(ctx, store.Aprobacion{OrgID: store.DefaultOrg, App: "tienda", Accion: store.AccionRevertir, Version: 1,
		Motivo: "motivo de la principal", PedidaPor: key.Name, KeyID: key.ID, Vence: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	// Every number is the first of its kind, so 1 stands for all of them.
	for what, id := range map[string]int64{"run": run.ID, "note": nota.ID, "encargo": enc.ID, "cargo": cargo.ID, "comment": com.ID, "request": apr.ID} {
		if id != 1 {
			t.Fatalf("the %s is number %d, not 1: the database was not empty", what, id)
		}
	}

	// Another organization, and its agent's key.
	otra, err := e.store.CreateOrg(ctx, "otra", "Otra", "ana")
	if err != nil {
		t.Fatal(err)
	}
	token, hash, err := ruta.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.CreateAgentKey(ctx, otra.ID, "llave de ana", hash, store.RoleCollaborator, nil, "ana", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	var hechos []string
	e.s.MCP = (&ruta.Server{Store: e.store, Log: e.s.Log, Encargos: e.store, Comentarios: e.store,
		Plataforma: plataformaDePrueba{Store: e.store, s: e.s, hechos: &hechos},
		Produccion: produccionDePrueba{Store: e.store, hechos: &hechos},
		Mercado:    mercadoDePrueba{hechos: &hechos}, PausaAprobacion: time.Millisecond}).Handler()
	srv := httptest.NewServer(e.s.Handler())
	t.Cleanup(srv.Close)
	c := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	sess, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerRT{token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	// A call and how it must end: with an error that says dice, or, when
	// dice is empty, well (and showing nothing of the principal's).
	type caso struct {
		args map[string]any
		dice string
	}
	uno := map[string]any{"id": 1}
	const (
		sinApp     = "no existe la aplicación tienda"
		sinEncargo = "no existe el encargo 1"
		sinCorrida = "no existe la ejecución 1"
		sinMercado = "el Mercado es de la organización de la plataforma"
	)
	casos := map[string][]caso{
		"ruta_inicio":     {{}},
		"ruta_buscar":     {{args: map[string]any{"texto": "principal"}}, {args: map[string]any{"tipo": "pendiente"}}},
		"ruta_leer":       {{uno, "no existe la entrada 1"}},
		"cargo_tomar":     {{args: map[string]any{"proposito": "mirar lo ajeno"}}},
		"ruta_anotar":     {{args: map[string]any{"tipo": "nota", "titulo": "nota de ana", "cuerpo": "de la otra"}}},
		"ruta_actualizar": {{map[string]any{"id": 1, "titulo": "cambiado por ana", "hecho": true}, "no existe la entrada 1"}},
		"cargo_entregar":  {{args: map[string]any{"entrega": "nada ajeno"}}},

		"apps_listar":          {{}},
		"app_estado":           {{map[string]any{"app": "tienda"}, sinApp}},
		"mesa_leer":            {{map[string]any{"app": "tienda"}, sinApp}},
		"ejecucion_leer":       {{uno, sinCorrida}},
		"paso_registro":        {{map[string]any{"ejecucion": 1, "paso": "web:build"}, sinCorrida}},
		"solicitudes_listar":   {{}, {map[string]any{"servicio": "tienda/web"}, "no tiene acceso a la aplicación tienda"}},
		"solicitud_leer":       {{map[string]any{"id": "traza-principal"}, "no tiene acceso a las aplicaciones de esa solicitud"}},
		"ejecucion_lanzar":     {{map[string]any{"app": "tienda", "rama": "de-ana"}, sinApp}},
		"ejecucion_cancelar":   {{uno, sinCorrida}},
		"ejecucion_reintentar": {{uno, sinCorrida}},

		"encargos_listar":  {{args: map[string]any{"todos": true}}},
		"encargo_leer":     {{uno, sinEncargo}},
		"encargo_crear":    {{map[string]any{"titulo": "de ana", "app": "tienda"}, sinApp}, {args: map[string]any{"titulo": "encargo de ana"}}},
		"encargo_tomar":    {{uno, sinEncargo}},
		"encargo_esperar":  {{map[string]any{"id": 1, "ejecucion": 1, "segundos": 1}, sinEncargo}},
		"encargo_entregar": {{map[string]any{"id": 1, "resultado": "lo hizo ana"}, sinEncargo}},
		"encargo_soltar":   {{map[string]any{"id": 1, "motivo": "lo suelta ana"}, sinEncargo}},

		"comentarios_leer":     {{args: map[string]any{"cargo": 1}}, {map[string]any{"encargo": 1}, sinEncargo}},
		"comentario_responder": {{map[string]any{"id": 1, "respuesta": "responde ana"}, "no existe el comentario 1"}},

		"produccion_pedir":   {{map[string]any{"app": "tienda", "version": 1, "motivo": "lo pide ana"}, sinApp}},
		"produccion_esperar": {{map[string]any{"id": 1, "segundos": 1}, "no existe la solicitud 1"}},

		"mercado_puestos": {{dice: sinMercado}},
		"mercado_leer":    {{map[string]any{"puesto": "puesto-principal"}, sinMercado}},
		"mercado_editar":  {{map[string]any{"puesto": "puesto-principal", "lema": "de ana"}, sinMercado}},
		"mercado_abrir": {{map[string]any{"direccion": "ana.example.com", "pedido_por_la_persona": true,
			"pagina": map[string]any{"tipo": "negocio", "nombre": "Puesto de ana"}}, sinMercado}},
	}
	// What of the principal organization must never show (a name she sent
	// herself may come back in the refusal).
	marks := []string{"tienda", "abc123d", "rama-principal", "caja-principal", "traza-principal", "secreto de la principal",
		"encargo de la principal", "llave principal", "cargo de la principal", "entrega de la principal", "falta lo de la principal",
		"comentario de la principal", "comentario del encargo principal", "motivo de la principal", "Puesto de la principal", "p0dxD"}

	list, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The cargo first, since most changes need one, and its handover last.
	var names []string
	for _, tl := range list.Tools {
		if tl.Name != "cargo_tomar" && tl.Name != "cargo_entregar" {
			names = append(names, tl.Name)
		}
	}
	slices.Sort(names)
	names = append(append([]string{"cargo_tomar"}, names...), "cargo_entregar")
	if len(names) < 30 {
		t.Fatalf("only %d tools: are they registered?", len(names))
	}
	for _, name := range names {
		cs, ok := casos[name]
		if !ok {
			t.Errorf("%s has no case here: add one that calls it with the principal's names and numbers", name)
			continue
		}
		for _, cs := range cs {
			if cs.args == nil {
				cs.args = map[string]any{}
			}
			res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: cs.args})
			if err != nil {
				t.Errorf("%s %v: %v", name, cs.args, err)
				continue
			}
			sent, _ := json.Marshal(cs.args)
			structured, _ := json.Marshal(res.StructuredContent)
			body := string(structured)
			for _, c := range res.Content {
				if tc, ok := c.(*mcp.TextContent); ok {
					body += " " + tc.Text
				}
			}
			switch {
			case cs.dice == "" && res.IsError:
				t.Errorf("%s %s of her own organization: %s", name, sent, body)
			case cs.dice != "" && !res.IsError:
				t.Errorf("%s %s from the other organization answered, want %q: %.300s", name, sent, cs.dice, body)
			case cs.dice != "" && !strings.Contains(body, cs.dice):
				t.Errorf("%s %s from the other organization: %q, want %q", name, sent, body, cs.dice)
			}
			for _, m := range marks {
				if strings.Contains(body, m) && !strings.Contains(string(sent), m) {
					t.Errorf("%s %s from the other organization shows %q: %.400s", name, sent, m, body)
					break
				}
			}
		}
	}
	for name := range casos {
		if !slices.Contains(names, name) {
			t.Errorf("%s has a case here but the server does not list it", name)
		}
	}

	// Nothing she did reached the principal's.
	if len(hechos) > 0 {
		t.Errorf("her calls reached the platform: %v", hechos)
	}
	if n, err := e.store.Ruta(ctx, nota.ID); err != nil || n.Title != "secreto de la principal" || n.Done {
		t.Errorf("the principal's note after her calls: %+v %v", n, err)
	}
	if en, err := e.store.Encargo(ctx, enc.ID); err != nil || en.Status != store.EncargoPending || en.KeyID != nil {
		t.Errorf("the principal's encargo after her calls: %+v %v", en, err)
	}
	if cm, err := e.store.Comentario(ctx, com.ID); err != nil || cm.AnsweredAt != nil {
		t.Errorf("the principal's comment after her calls: %+v %v", cm, err)
	}
	if r, err := e.store.GetRun(ctx, run.ID); err != nil || r.Status != run.Status {
		t.Errorf("the principal's run after her calls: %+v %v", r, err)
	}
	if a, err := e.store.GetAprobacion(ctx, apr.ID); err != nil || a.Estado != store.AprobacionPendiente {
		t.Errorf("the principal's request after her calls: %+v %v", a, err)
	}
	// And what she did is hers: her note and her encargo are in her
	// organization, and her calls in her bitácora, not the principal's.
	if es, err := e.store.SearchRuta(ctx, otra.ID, "", "", 10); err != nil || len(es) != 1 || es[0].Title != "nota de ana" {
		t.Errorf("her notes: %+v %v", es, err)
	}
	if es, err := e.store.SearchRuta(ctx, store.DefaultOrg, "", "", 10); err != nil || len(es) != 1 {
		t.Errorf("the principal's notes after her calls: %+v %v", es, err)
	}
	if es, err := e.store.ListEncargos(ctx, otra.ID, false, 10); err != nil || len(es) != 1 || es[0].Title != "encargo de ana" {
		t.Errorf("her encargos: %+v %v", es, err)
	}
	rows, err := e.store.ListarBitacora(ctx, store.DefaultOrg, store.FiltroBitacora{Limite: 500})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range rows {
		if b.Quien == "llave de ana" {
			t.Errorf("her call is in the principal's bitácora: %+v", b)
		}
	}
	if rows, err := e.store.ListarBitacora(ctx, otra.ID, store.FiltroBitacora{Limite: 500}); err != nil || len(rows) < len(names) {
		t.Errorf("her bitácora has %d rows for %d tools: %v", len(rows), len(names), err)
	}
	if acts, err := e.store.ListAgentActions(ctx, store.DefaultOrg, 500); err != nil || len(acts) != 0 {
		t.Errorf("the principal's agent activity after her calls: %+v %v", acts, err)
	}
}
