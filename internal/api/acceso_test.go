package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rendimiento-ai/rendimiento/internal/ruta"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// cajaConLista makes the principal organization's app "caja" (with a list
// the test fills) and "bodega" (without one), and one of each thing an
// app has: a run, a release, an encargo with a comment, a request to roll
// back, a bitácora row, a problem, a kept request and a known failure in
// the Ruta. Every number of caja's is 1.
func cajaConLista(t *testing.T, e *testEnv) (caja, bodega *store.App, run *store.Run) {
	t.Helper()
	ctx := context.Background()
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	caja = &store.App{Name: "caja", Repo: "p0dxD/caja", DefaultBranch: "main", Spec: sp}
	bodega = &store.App{Name: "bodega", Repo: "p0dxD/bodega", DefaultBranch: "main", Spec: sp}
	for _, a := range []*store.App{caja, bodega} {
		if err := e.store.CreateApp(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	run = &store.Run{AppID: caja.ID, SHA: "c4j4c4j4c4", Branch: "rama-de-la-caja", Event: "push"}
	if err := e.store.CreateRun(ctx, run, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.store.CreateRun(ctx, &store.Run{AppID: bodega.ID, SHA: "b0d3g4b0d3", Branch: "main", Event: "push"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.store.CreateRelease(ctx, &store.Release{AppID: caja.ID, Number: 1, SHA: "c4j4c4j4c4", Images: map[string]string{"web": "web@sha256:1"}, Spec: sp}); err != nil {
		t.Fatal(err)
	}
	enc, err := e.store.CreateEncargo(ctx, store.Encargo{Title: "encargo de la caja", App: "caja", CreatedBy: "p0dxD"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.ComentarEncargo(ctx, store.DefaultOrg, enc.ID, "p0dxD", "comentario de la caja"); err != nil {
		t.Fatal(err)
	}
	key, err := e.store.CreateAgentKey(ctx, store.DefaultOrg, "llave", "h-llave", store.RoleCollaborator, nil, "p0dxD", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.CrearAprobacion(ctx, store.Aprobacion{OrgID: store.DefaultOrg, App: "caja", Accion: store.AccionRevertir, Version: 1,
		Motivo: "motivo de la caja", PedidaPor: key.Name, KeyID: key.ID, Vence: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.AnotarBitacora(ctx, store.Bitacora{OrgID: store.DefaultOrg, QuienTipo: store.QuienPersona, Quien: "p0dxD", Por: "interfaz",
		Accion: "POST /api/apps/{app}/rollback", Sobre: map[string]string{"app": "caja"}, Resultado: "200"}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.RecordProblem(ctx, store.Problem{Fingerprint: "f-caja", Level: "error", Component: "ci", App: "caja",
		Message: "problema de la caja", LastAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := e.store.EnsureTracePartitions(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SaveTraces(ctx, []store.Trace{{ID: "1", StartedAt: now, DurationUS: 5, Service: "caja/web",
		Name: "GET /cobro-caja", Services: []string{"caja/web"}, Spans: 1, Reason: "sample"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.AddRuta(ctx, store.RutaEntry{Kind: store.KindManual, Title: "falla de la caja", Tags: []string{"falla-conocida", "caja"}, Author: "p0dxD"}); err != nil {
		t.Fatal(err)
	}
	if run.ID != 1 || enc.ID != 1 {
		t.Fatalf("caja's run is %d and its encargo %d, not 1: the database was not empty", run.ID, enc.ID)
	}
	return caja, bodega, run
}

// readStream reads a server-sent stream for a while (d), and its status.
func readStream(t *testing.T, e *testEnv, path string, c *http.Cookie, d time.Duration) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+path, nil)
	req.AddCookie(c)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, ""
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// An app with a list: a person on it as guest sees it and only reads it;
// a collaborator of the organization left off it finds nothing of it
// (404, never 403), in lists, by number and in the streams, and keeps
// acting on the other app; a guest of the organization the list makes
// collaborator runs CI on it only; the administrator always enters and
// keeps the list. Leaving the organization takes a person off its lists,
// and clearing the list lets everyone in again.
func TestPermisosPorApp(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	_, _, run := cajaConLista(t, e)
	for login, role := range map[string]string{"dentro": store.MemberCollaborator, "fuera": store.MemberCollaborator, "sube": store.MemberGuest} {
		if err := e.store.SetMember(ctx, store.DefaultOrg, login, role, "p0dxD"); err != nil {
			t.Fatal(err)
		}
	}
	me, dentro, fuera, sube := e.session(t, "p0dxD"), e.session(t, "dentro"), e.session(t, "fuera"), e.session(t, "sube")

	// Only the organization's administrators keep the list, with people of
	// the organization who are not administrators.
	if r, _ := e.do(t, "PUT", "/api/apps/caja/members/dentro", `{"role":"guest"}`, fuera, nil); r.StatusCode != 403 {
		t.Fatalf("a collaborator keeping the list: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "PUT", "/api/apps/caja/members/nadie", `{"role":"guest"}`, me, nil); r.StatusCode != 400 {
		t.Fatalf("someone not in the organization: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "PUT", "/api/apps/caja/members/p0dxD", `{"role":"guest"}`, me, nil); r.StatusCode != 400 {
		t.Fatalf("an administrator on the list: %d", r.StatusCode)
	}
	if r, body := e.do(t, "PUT", "/api/apps/caja/members/dentro", `{"role":"guest"}`, me, nil); r.StatusCode != 200 || !strings.Contains(body, `"list":true`) || !strings.Contains(body, `"login":"dentro"`) {
		t.Fatalf("putting dentro on the list: %d %s", r.StatusCode, body)
	}
	if r, body := e.do(t, "PUT", "/api/apps/caja/members/sube", `{"role":"collaborator"}`, me, nil); r.StatusCode != 200 {
		t.Fatalf("putting sube on the list: %d %s", r.StatusCode, body)
	}

	// fuera: caja does not exist for her.
	for path, leak := range map[string]string{
		"/api/apps": "caja", "/api/encargos?all=1": "encargo de la caja", "/api/aprobaciones": "motivo de la caja",
		"/api/bitacora": "caja", "/api/problems": "problema de la caja", "/api/universe/traces": "cobro-caja",
		"/api/comentarios": "comentario de la caja", "/api/ruta": "falla de la caja", "/api/universe": "caja",
	} {
		r, body := e.do(t, "GET", path, "", fuera, nil)
		if r.StatusCode != 200 || strings.Contains(body, leak) {
			t.Errorf("%s for someone off the list: %d %.300s", path, r.StatusCode, body)
		}
	}
	if _, body := e.do(t, "GET", "/api/apps", "", fuera, nil); !strings.Contains(body, `"name":"bodega"`) {
		t.Errorf("the app without a list is still hers: %s", body)
	}
	if _, body := e.do(t, "GET", "/api/problems/count", "", fuera, nil); !strings.Contains(body, `"open":0`) {
		t.Errorf("the problem count counts caja's: %s", body)
	}
	for _, p := range []struct{ method, path string }{
		{"GET", "/api/apps/caja"}, {"GET", "/api/apps/caja/runs"}, {"GET", "/api/apps/caja/releases"},
		{"GET", fmt.Sprintf("/api/runs/%d", run.ID)}, {"GET", fmt.Sprintf("/api/runs/%d/steps/web:build/log", run.ID)},
		{"GET", "/api/encargos/1"}, {"GET", "/api/universe/traces/1"}, {"POST", "/api/apps/caja/runs"},
		{"POST", fmt.Sprintf("/api/runs/%d/cancel", run.ID)}, {"POST", "/api/encargos/1/cancel"}, {"POST", "/api/problems/1/dismiss"},
	} {
		if r, body := e.do(t, p.method, p.path, "{}", fuera, nil); r.StatusCode != 404 {
			t.Errorf("%s %s for someone off the list: %d, want 404 (%.200s)", p.method, p.path, r.StatusCode, body)
		}
	}
	for _, path := range []string{"/api/apps/caja/events", fmt.Sprintf("/api/runs/%d/events", run.ID)} {
		if status, _ := readStream(t, e, path, fuera, time.Second); status != 404 {
			t.Errorf("stream %s for someone off the list: %d, want 404", path, status)
		}
	}
	if r, body := e.do(t, "POST", "/api/encargos", `{"title":"de fuera","app":"caja"}`, fuera, nil); r.StatusCode != 400 {
		t.Errorf("an encargo about caja from someone off the list: %d %s", r.StatusCode, body)
	}
	if r, _ := e.do(t, "POST", "/api/apps/bodega/runs", "{}", fuera, nil); r.StatusCode == 403 || r.StatusCode == 404 {
		t.Errorf("a collaborator runs CI on the app without a list: %d", r.StatusCode)
	}

	// dentro: guest on caja, collaborator on bodega.
	if r, body := e.do(t, "GET", "/api/apps/caja", "", dentro, nil); r.StatusCode != 200 || !strings.Contains(body, `"role":"guest"`) {
		t.Fatalf("caja for someone on the list: %d %.200s", r.StatusCode, body)
	}
	if _, body := e.do(t, "GET", "/api/apps", "", dentro, nil); !strings.Contains(body, `"name":"caja"`) || !strings.Contains(body, `"role":"collaborator"`) {
		t.Errorf("the apps for someone on the list: %s", body)
	}
	for path, want := range map[string]string{"/api/encargos": "encargo de la caja", "/api/universe/traces": "cobro-caja", fmt.Sprintf("/api/runs/%d", run.ID): "c4j4c4j4c4"} {
		if r, body := e.do(t, "GET", path, "", dentro, nil); r.StatusCode != 200 || !strings.Contains(body, want) {
			t.Errorf("%s for someone on the list: %d %.200s", path, r.StatusCode, body)
		}
	}
	for _, path := range []string{"/api/apps/caja/runs", fmt.Sprintf("/api/runs/%d/cancel", run.ID), "/api/encargos/1/cancel"} {
		if r, _ := e.do(t, "POST", path, "{}", dentro, nil); r.StatusCode != 403 {
			t.Errorf("POST %s as a guest of caja: %d, want 403", path, r.StatusCode)
		}
	}
	if r, _ := e.do(t, "POST", "/api/encargos", `{"title":"de dentro","app":"caja"}`, dentro, nil); r.StatusCode != 403 {
		t.Errorf("an encargo about caja from its guest: %d, want 403", r.StatusCode)
	}
	if r, _ := e.do(t, "POST", "/api/apps/bodega/runs", "{}", dentro, nil); r.StatusCode == 403 || r.StatusCode == 404 {
		t.Errorf("a guest of caja runs CI on bodega, where she is a collaborator: %d", r.StatusCode)
	}

	// sube: a guest of the organization whom caja's list makes collaborator.
	if r, _ := e.do(t, "POST", "/api/apps/caja/runs", "{}", sube, nil); r.StatusCode == 403 || r.StatusCode == 404 {
		t.Errorf("a collaborator of caja runs CI on it: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "POST", "/api/apps/bodega/runs", "{}", sube, nil); r.StatusCode != 403 {
		t.Errorf("a guest of the organization runs CI on bodega: %d, want 403", r.StatusCode)
	}

	// The activity stream: what happens to caja reaches who is on the list.
	defer func(p time.Duration) { activityPoll = p }(activityPoll)
	activityPoll = 50 * time.Millisecond
	var wg sync.WaitGroup
	bodies := map[string]string{}
	var mu sync.Mutex
	for who, c := range map[string]*http.Cookie{"dentro": dentro, "fuera": fuera} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, body := readStream(t, e, "/api/activity/stream", c, 1500*time.Millisecond)
			mu.Lock()
			bodies[who] = body
			mu.Unlock()
		}()
	}
	time.Sleep(400 * time.Millisecond)
	if _, err := e.store.CreateEncargo(ctx, store.Encargo{Title: "encargo nuevo de la caja", App: "caja", CreatedBy: "p0dxD"}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if !strings.Contains(bodies["dentro"], "encargo nuevo de la caja") {
		t.Errorf("the activity for someone on the list: %q", bodies["dentro"])
	}
	if strings.Contains(bodies["fuera"], "caja") {
		t.Errorf("the activity for someone off the list: %q", bodies["fuera"])
	}

	// The administrator always enters, and sees the list.
	if r, body := e.do(t, "GET", "/api/apps/caja/members", "", me, nil); r.StatusCode != 200 || !strings.Contains(body, `"login":"dentro"`) {
		t.Fatalf("the list: %d %s", r.StatusCode, body)
	}
	if r, _ := e.do(t, "GET", "/api/apps/caja", "", me, nil); r.StatusCode != 200 {
		t.Fatalf("the administrator: %d", r.StatusCode)
	}

	// Taken out of the organization and back: she is off the list.
	if err := e.store.RemoveMember(ctx, store.DefaultOrg, "dentro"); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetMember(ctx, store.DefaultOrg, "dentro", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.do(t, "GET", "/api/apps/caja", "", dentro, nil); r.StatusCode != 404 {
		t.Errorf("back in the organization, off the list: %d, want 404", r.StatusCode)
	}

	// Taking someone off; then the list cleared lets everyone in.
	if r, _ := e.do(t, "DELETE", "/api/apps/caja/members/sube", "", me, nil); r.StatusCode != 200 {
		t.Fatalf("taking sube off: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "GET", "/api/apps/caja", "", sube, nil); r.StatusCode != 404 {
		t.Errorf("taken off the list: %d, want 404", r.StatusCode)
	}
	if r, body := e.do(t, "PUT", "/api/apps/caja/members", `{"list":false}`, me, nil); r.StatusCode != 200 || !strings.Contains(body, `"list":false`) {
		t.Fatalf("clearing the list: %d %s", r.StatusCode, body)
	}
	if r, body := e.do(t, "GET", "/api/apps/caja", "", fuera, nil); r.StatusCode != 200 || !strings.Contains(body, `"role":"collaborator"`) {
		t.Errorf("without a list, the organization's role: %d %.200s", r.StatusCode, body)
	}
}

// Every route, called by a collaborator of the platform's organization
// whom an app's list leaves out, with that app's names and numbers, finds
// nothing of it: 404, a refusal, or an answer that shows none of it. The
// routes are walked from s.routes (the platform's too: she is one of its
// people), so a route added later is checked by itself.
func TestEveryRouteKeepsToTheAppsList(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	caja, _, run := cajaConLista(t, e)
	for login, role := range map[string]string{"dentro": store.MemberGuest, "fuera": store.MemberCollaborator} {
		if err := e.store.SetMember(ctx, store.DefaultOrg, login, role, "p0dxD"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.store.SetAppMember(ctx, caja, "dentro", store.MemberGuest, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	fuera := e.session(t, "fuera")

	marks := []string{"caja", "c4j4c4j4c4"}
	fill := strings.NewReplacer("{app}", "caja", "{id}", "1", "{number}", "1", "{task}", "web", "{secret}", "caja-env",
		"{step}", "web:build", "{login}", "dentro", "{site}", "1", "{session}", "1", "{addon}", "nada", "{name}", "nada")
	var reads, writes []route
	for _, rt := range e.s.routes {
		if strings.HasPrefix(rt.pattern, "GET ") {
			reads = append(reads, rt)
		} else {
			writes = append(writes, rt)
		}
	}
	checked := 0
	for _, rt := range append(reads, writes...) {
		method, path, _ := strings.Cut(rt.pattern, " ")
		path = fill.Replace(path)
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second) // streams end here
		req, _ := http.NewRequestWithContext(cctx, method, e.srv.URL+path, strings.NewReader(`{"role":"guest"}`))
		req.AddCookie(fuera)
		status, body := 0, ""
		if res, err := http.DefaultClient.Do(req); err == nil {
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			status, body = res.StatusCode, string(b)
		}
		cancel()
		checked++
		if status >= 200 && status < 300 && strings.Contains(rt.pattern, "{app}") {
			t.Errorf("%s for someone off caja's list: %d, want 404 (%.200s)", rt.pattern, status, body)
			continue
		}
		for _, m := range marks {
			if strings.Contains(body, m) {
				t.Errorf("%s for someone off caja's list shows %q: %d %.300s", rt.pattern, m, status, body)
				break
			}
		}
	}
	if checked < 100 {
		t.Fatalf("only %d routes: are they registered?", checked)
	}
	// Nothing she did changed caja's.
	if r, err := e.store.GetRun(ctx, run.ID); err != nil || r.Status != run.Status {
		t.Errorf("caja's run after her calls: %+v %v", r, err)
	}
	if en, err := e.store.Encargo(ctx, 1); err != nil || en.Status != store.EncargoPending {
		t.Errorf("caja's encargo after her calls: %+v %v", en, err)
	}
	if l, err := e.store.AppMembers(ctx, caja); err != nil || !l.List || len(l.Members) != 1 {
		t.Errorf("caja's list after her calls: %+v %v", l, err)
	}
}

// A key limited to some apps waits only for requests about them, and
// reads and changes only their Mercado stalls.
func TestToolsKeepToTheKeysApps(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	cajaConLista(t, e) // request 1 is about caja
	token, hash, err := ruta.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.CreateAgentKey(ctx, store.DefaultOrg, "llave de bodega", hash, store.RoleCollaborator, []string{"bodega"}, "p0dxD", time.Now().Add(time.Hour)); err != nil {
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
	call := func(name string, args map[string]any) (bool, string) {
		res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		structured, _ := json.Marshal(res.StructuredContent)
		body := string(structured)
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				body += " " + tc.Text
			}
		}
		return res.IsError, body
	}
	if bad, body := call("cargo_tomar", map[string]any{"proposito": "probar la lista de la llave"}); bad {
		t.Fatalf("cargo: %s", body)
	}
	for name, cs := range map[string]struct {
		args map[string]any
		dice string
	}{
		"produccion_esperar": {map[string]any{"id": 1, "segundos": 1}, "no existe la solicitud 1"},
		"mercado_leer":       {map[string]any{"puesto": "puesto-principal"}, "no tiene acceso a la aplicación puesto-principal"},
		"mercado_editar":     {map[string]any{"puesto": "puesto-principal", "lema": "de la llave"}, "no tiene acceso a la aplicación puesto-principal"},
	} {
		if bad, body := call(name, cs.args); !bad || !strings.Contains(body, cs.dice) || strings.Contains(body, "motivo de la caja") {
			t.Errorf("%s with a key of bodega: %v %.300s, want %q", name, bad, body, cs.dice)
		}
	}
	if bad, body := call("mercado_puestos", nil); bad || strings.Contains(body, "puesto-principal") {
		t.Errorf("the stalls for a key of bodega: %v %.300s", bad, body)
	}
	if slices.Contains(hechos, "read a stall") || slices.Contains(hechos, "changed a stall") {
		t.Errorf("her calls reached the stalls: %v", hechos)
	}
}
