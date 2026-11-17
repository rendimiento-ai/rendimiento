package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The interface's use: only route patterns from the list and actions from
// the closed list come in, nothing says who, off keeps nothing, and only the
// platform's administrators read it.
func TestUsoAPI(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	if err := e.store.SetMember(ctx, store.DefaultOrg, "colabora-uso", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	admin, colabora := e.session(t, "p0dxD"), e.session(t, "colabora-uso")

	if _, body := e.do(t, "GET", "/api/me", "", colabora, nil); !strings.Contains(body, `"uso":true`) {
		t.Fatalf("on by default, and the interface is told: %s", body)
	}
	lote := `{"e":[{"t":"v","r":"/","d":""},{"t":"v","r":"/apps/:name","d":"/","ms":420},
		{"t":"v","r":"/apps/:name/runs/:id","d":"/apps/:name","ms":2600},{"t":"a","r":"/apps/:name","a":"build_now"}]}`
	if r, body := e.do(t, "POST", "/api/uso", lote, colabora, nil); r.StatusCode != 204 {
		t.Fatalf("a batch: %d %s", r.StatusCode, body)
	}

	// Refused, whole: concrete addresses, unknown actions, times out of
	// bounds, too many events, and no session at all.
	muchos := strings.TrimSuffix(strings.Repeat(`{"t":"v","r":"/"},`, usoMaxEventos+1), ",")
	for name, b := range map[string]string{
		"an app's name":     `{"e":[{"t":"v","r":"/apps/propinas"}]}`,
		"a run's number":    `{"e":[{"t":"v","r":"/apps/:name/runs/42"}]}`,
		"a concrete origin": `{"e":[{"t":"v","r":"/","d":"/apps/propinas"}]}`,
		"a query":           `{"e":[{"t":"v","r":"/?q=secreto"}]}`,
		"an unknown action": `{"e":[{"t":"a","r":"/","a":"borrar_todo"}]}`,
		"typed text":        `{"e":[{"t":"a","r":"/","a":"hola, soy Ana"}]}`,
		"a negative time":   `{"e":[{"t":"v","r":"/","ms":-1}]}`,
		"too long a time":   `{"e":[{"t":"v","r":"/","ms":60001}]}`,
		"an unknown kind":   `{"e":[{"t":"x","r":"/"}]}`,
		"too many":          `{"e":[` + muchos + `]}`,
	} {
		if r, body := e.do(t, "POST", "/api/uso", b, colabora, nil); r.StatusCode != 400 && r.StatusCode != 413 {
			t.Errorf("%s: %d %s", name, r.StatusCode, body)
		}
	}
	if r, _ := e.do(t, "POST", "/api/uso", lote, nil, nil); r.StatusCode != 401 {
		t.Fatalf("without a session: %d", r.StatusCode)
	}

	// Only the platform's administrators read it, or turn it off.
	if r, _ := e.do(t, "GET", "/api/uso", "", colabora, nil); r.StatusCode != 403 {
		t.Fatalf("a collaborator reads: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "PUT", "/api/uso", `{"encendido":false}`, colabora, nil); r.StatusCode != 403 {
		t.Fatalf("a collaborator turns it off: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "GET", "/api/uso?dias=3", "", admin, nil); r.StatusCode != 400 {
		t.Fatalf("a period not offered: %d", r.StatusCode)
	}
	r, body := e.do(t, "GET", "/api/uso?dias=7", "", admin, nil)
	var v UsoView
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &v) != nil {
		t.Fatalf("read: %d %s", r.StatusCode, body)
	}
	if !v.Encendido || v.Dias != 7 || len(v.Paginas) != 3 || len(v.Pasos) != 3 || len(v.Acciones) != 1 {
		t.Fatalf("view = %+v", v)
	}
	for _, p := range v.Paginas {
		if p.Ruta == "/apps/:name/runs/:id" && (p.Medidas != 1 || p.MsTotal != 2600 || p.Cubetas[3] != 1) {
			t.Fatalf("the run's page = %+v", p)
		}
	}

	// Nothing that says who: no login in what is kept, and the batches are
	// not in the bitácora.
	if strings.Contains(body, "colabora") || strings.Contains(body, "p0dxD") {
		t.Fatalf("the use names a person: %s", body)
	}
	for _, p := range v.Pasos {
		if !rutasUso[p.Hacia] || (p.Desde != "" && !rutasUso[p.Desde]) {
			t.Fatalf("kept a page not in the list: %+v", p)
		}
	}
	rows, err := e.store.ListarBitacora(ctx, store.DefaultOrg, store.FiltroBitacora{})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range rows {
		if b.Accion == "POST /api/uso" {
			t.Fatalf("a batch in the bitácora: %+v", b)
		}
	}

	// Off: the interface is told, and batches answer 204 and keep nothing.
	if r, body := e.do(t, "PUT", "/api/uso", `{"encendido":false}`, admin, nil); r.StatusCode != 200 || !strings.Contains(body, `"encendido":false`) {
		t.Fatalf("turn off: %d %s", r.StatusCode, body)
	}
	if _, body := e.do(t, "GET", "/api/me", "", colabora, nil); strings.Contains(body, `"uso":true`) {
		t.Fatalf("still on for the interface: %s", body)
	}
	if r, _ := e.do(t, "POST", "/api/uso", lote, colabora, nil); r.StatusCode != 204 {
		t.Fatalf("a batch while off: %d", r.StatusCode)
	}
	_, body = e.do(t, "GET", "/api/uso?dias=7", "", admin, nil)
	_ = json.Unmarshal([]byte(body), &v)
	for _, p := range v.Paginas {
		if p.Ruta == "/" && p.Vistas != 1 {
			t.Fatalf("counted while off: %+v", p)
		}
	}
	if r, _ := e.do(t, "PUT", "/api/uso", `{"encendido":true}`, admin, nil); r.StatusCode != 200 {
		t.Fatalf("turn on: %d", r.StatusCode)
	}

	// A read-only demo session is never counted.
	if r, _ := e.do(t, "POST", "/api/orgs", `{"slug":"demo-uso","name":"Demo uso"}`, admin, nil); r.StatusCode != 201 {
		t.Fatalf("org: %d", r.StatusCode)
	}
	orgs, _ := e.store.OrgsOf(ctx, "p0dxD")
	var demo int64
	for _, o := range orgs {
		if o.Slug == "demo-uso" {
			demo = o.ID
		}
	}
	if r, _ := e.do(t, "POST", fmt.Sprintf("/api/orgs/%d/switch", demo), "", admin, nil); r.StatusCode != 204 {
		t.Fatalf("switch: %d", r.StatusCode)
	}
	r, body = e.do(t, "POST", "/api/demo-links", `{"days":1}`, admin, nil)
	var link struct {
		URL string `json:"url"`
	}
	if r.StatusCode != 201 || json.Unmarshal([]byte(body), &link) != nil {
		t.Fatalf("demo link: %d %s", r.StatusCode, body)
	}
	r, _ = e.do(t, "GET", strings.TrimPrefix(link.URL, "https://rendimiento.example"), "", nil, nil)
	var guest *http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == sessionCookie {
			guest = c
		}
	}
	if guest == nil {
		t.Fatal("no demo session")
	}
	if _, body := e.do(t, "GET", "/api/me", "", guest, nil); strings.Contains(body, `"uso":true`) {
		t.Fatalf("a demo is told to measure: %s", body)
	}
	if r, _ := e.do(t, "POST", "/api/uso", `{"e":[{"t":"v","r":"/ruta","d":""}]}`, guest, nil); r.StatusCode != 204 {
		t.Fatalf("a demo's batch: %d", r.StatusCode)
	}
	res, _ := e.store.LeerUso(ctx, time.Now().AddDate(0, 0, -1))
	for _, p := range res.Paginas {
		if p.Ruta == "/ruta" {
			t.Fatalf("a demo was counted: %+v", p)
		}
	}
}

// The pages and actions the server accepts are the interface's: the routes
// of web/src/main.tsx, and the lists of web/src/uso.ts.
func TestUsoListsMatchTheUI(t *testing.T) {
	read := func(p string) string {
		b, err := os.ReadFile(filepath.Join("..", "..", "web", "src", p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	keys := func(m map[string]bool) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	list := func(src, name string) []string {
		m := regexp.MustCompile(`(?s)export const ` + name + ` = \[(.*?)\] as const`).FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("web/src/uso.ts has no %s list", name)
		}
		var out []string
		for _, q := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(m[1], -1) {
			out = append(out, q[1])
		}
		sort.Strings(out)
		return out
	}
	uso := read("uso.ts")
	rutas := map[string]bool{}
	for k := range rutasUso {
		if k != rutaDesconocida {
			rutas[k] = true
		}
	}
	if got, want := strings.Join(list(uso, "RUTAS"), " "), strings.Join(keys(rutas), " "); got != want {
		t.Errorf("route patterns:\n uso.ts %s\n server %s", got, want)
	}
	if got, want := strings.Join(list(uso, "ACCIONES"), " "), strings.Join(keys(accionesUso), " "); got != want {
		t.Errorf("actions:\n uso.ts %s\n server %s", got, want)
	}
	// Every page route of the interface is a pattern that counts.
	for _, m := range regexp.MustCompile(`path[=:]\s*["']([^"']+)["']`).FindAllStringSubmatch(read("main.tsx"), -1) {
		if p := m[1]; p != "*" && p != "/setup" && !rutasUso[p] {
			t.Errorf("main.tsx has the route %s, which the use does not count", p)
		}
	}
}
