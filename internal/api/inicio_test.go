package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ids is a home page as "id:on" words, in order.
func ids(secs []store.SeccionInicio) string {
	var out []string
	for _, s := range secs {
		o := "off"
		if s.On {
			o = "on"
		}
		out = append(out, s.ID+":"+o)
	}
	return strings.Join(out, " ")
}

// A list kept from an older version gains the sections it lacks, where
// the base has them; unknown and repeated ones are dropped.
func TestCompletarInicio(t *testing.T) {
	got := completar([]store.SeccionInicio{{ID: "apps", On: true}, {ID: "vieja", On: true}, {ID: "corriendo", On: false}, {ID: "apps", On: false}}, seccionesInicio)
	want := "apps:on corriendo:off esperan:on ayuda:on marcador:off visitas:off entregas:off lluvia:on"
	if ids(got) != want {
		t.Fatalf("completar = %s, want %s", ids(got), want)
	}
	if ids(completar(nil, seccionesInicio)) != ids(seccionesInicio) {
		t.Fatal("nothing chosen is the base")
	}
}

// Each person lays out their own home page, any role; a platform
// administrator sets the default for whoever never chose; Back to the usual
// forgets one's own.
func TestInicioAPI(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	for login, role := range map[string]string{"invitada-inicio": store.MemberGuest, "colabora-inicio": store.MemberCollaborator} {
		if err := e.store.SetMember(ctx, store.DefaultOrg, login, role, "p0dxD"); err != nil {
			t.Fatal(err)
		}
	}
	admin, invitada, colabora := e.session(t, "p0dxD"), e.session(t, "invitada-inicio"), e.session(t, "colabora-inicio")
	get := func(who string) InicioView {
		t.Helper()
		cookie := map[string]*http.Cookie{"admin": admin, "invitada": invitada, "colabora": colabora}[who]
		r, body := e.do(t, "GET", "/api/inicio", "", cookie, nil)
		var v InicioView
		if r.StatusCode != 200 || json.Unmarshal([]byte(body), &v) != nil {
			t.Fatalf("%s reads: %d %s", who, r.StatusCode, body)
		}
		return v
	}
	builtin := ids(seccionesInicio)

	// Nobody chose: the built-in default, and only the administrator may set it.
	if v := get("invitada"); ids(v.Secciones) != builtin || v.Propias || v.PuedeFijar {
		t.Fatalf("a guest at first: %+v", v)
	}
	if v := get("admin"); !v.PuedeFijar {
		t.Fatalf("the platform's administrator may set the default: %+v", v)
	}

	// A guest lays out their own view; nobody else's changes.
	mio := `{"secciones":[{"id":"apps","on":true},{"id":"corriendo","on":false},{"id":"marcador","on":true}]}`
	if r, body := e.do(t, "PUT", "/api/inicio", mio, invitada, nil); r.StatusCode != 200 {
		t.Fatalf("a guest saves: %d %s", r.StatusCode, body)
	}
	v := get("invitada")
	if !v.Propias || !strings.HasPrefix(ids(v.Secciones), "apps:on corriendo:off marcador:on esperan:on") || len(v.Secciones) != len(seccionesInicio) {
		t.Fatalf("the guest's own: %s", ids(v.Secciones))
	}
	if v := get("colabora"); v.Propias || ids(v.Secciones) != builtin {
		t.Fatalf("another person's is untouched: %s", ids(v.Secciones))
	}

	// What is not a home page is refused.
	for name, b := range map[string]string{
		"nothing":         `{"secciones":[]}`,
		"unknown section": `{"secciones":[{"id":"borrar","on":true}]}`,
		"twice":           `{"secciones":[{"id":"apps","on":true},{"id":"apps","on":false}]}`,
		"not JSON":        `secciones`,
	} {
		if r, body := e.do(t, "PUT", "/api/inicio", b, invitada, nil); r.StatusCode != 400 {
			t.Errorf("%s: %d %s", name, r.StatusCode, body)
		}
	}

	// Only the platform's administrators set the default.
	def := `{"secciones":[{"id":"esperan","on":true},{"id":"apps","on":true},{"id":"corriendo","on":false},{"id":"lluvia","on":false}]}`
	for who, c := range map[string]*http.Cookie{"guest": invitada, "collaborator": colabora} {
		if r, _ := e.do(t, "PUT", "/api/inicio/predeterminado", def, c, nil); r.StatusCode != 403 {
			t.Fatalf("a %s sets the default: %d", who, r.StatusCode)
		}
		if r, _ := e.do(t, "DELETE", "/api/inicio/predeterminado", "", c, nil); r.StatusCode != 403 {
			t.Fatalf("a %s resets the default: %d", who, r.StatusCode)
		}
	}
	if r, body := e.do(t, "PUT", "/api/inicio/predeterminado", def, admin, nil); r.StatusCode != 200 {
		t.Fatalf("the administrator sets the default: %d %s", r.StatusCode, body)
	}
	if v := get("colabora"); v.Propias || !strings.HasPrefix(ids(v.Secciones), "esperan:on apps:on corriendo:off lluvia:off ayuda:on") || v.PredeterminadoPor != "p0dxD" {
		t.Fatalf("who never chose sees the new default: %s (%s)", ids(v.Secciones), v.PredeterminadoPor)
	}
	if v := get("invitada"); !strings.HasPrefix(ids(v.Secciones), "apps:on corriendo:off marcador:on") {
		t.Fatalf("who chose keeps theirs: %s", ids(v.Secciones))
	}

	// Back to the usual: the default, the administrator's.
	if r, body := e.do(t, "DELETE", "/api/inicio", "", invitada, nil); r.StatusCode != 200 {
		t.Fatalf("back to the usual: %d %s", r.StatusCode, body)
	}
	if v := get("invitada"); v.Propias || ids(v.Secciones) != ids(v.Predeterminado) || !strings.HasPrefix(ids(v.Secciones), "esperan:on") {
		t.Fatalf("after back to the usual: %+v", v)
	}

	// The administrator takes the default back to the built-in one.
	if r, body := e.do(t, "DELETE", "/api/inicio/predeterminado", "", admin, nil); r.StatusCode != 200 {
		t.Fatalf("the built-in default again: %d %s", r.StatusCode, body)
	}
	if v := get("colabora"); ids(v.Secciones) != builtin || v.PredeterminadoPor != "" {
		t.Fatalf("the built-in default: %+v", v)
	}
	// A demo's guest sees the usual one, and cannot change it.
	demo := store.DemoLogin(9301)
	if err := e.store.SetMember(ctx, store.DefaultOrg, demo, store.MemberGuest, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.do(t, "PUT", "/api/inicio", mio, e.session(t, demo), nil); r.StatusCode != 403 {
		t.Fatalf("a demo changes its home page: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "GET", "/api/inicio", "", nil, nil); r.StatusCode != 401 {
		t.Fatalf("without a session: %d", r.StatusCode)
	}
}

// The interface knows the same sections, in the same order and with the
// same first state (web/src/inicio.ts).
func TestInicioListMatchesTheUI(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "inicio.ts"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)export const SECCIONES = \[(.*?)\] as const`).FindSubmatch(b)
	if m == nil {
		t.Fatal("web/src/inicio.ts has no SECCIONES list")
	}
	var ui []store.SeccionInicio
	for _, q := range regexp.MustCompile(`\{ id: "([a-z]+)", on: (true|false) \}`).FindAllSubmatch(m[1], -1) {
		ui = append(ui, store.SeccionInicio{ID: string(q[1]), On: string(q[2]) == "true"})
	}
	if ids(ui) != ids(seccionesInicio) {
		t.Fatalf("sections:\n inicio.ts %s\n server    %s", ids(ui), ids(seccionesInicio))
	}
}
