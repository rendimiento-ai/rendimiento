package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// An encargo the coding agent gave back goes again with what the person
// wants it to know: «Send it again» and «Leave it to my agent» carry a
// note, kept in its history ("told") for the agent's brief and
// encargo_leer. A body without a note still works; an agent key never
// sends one for a person.
func TestResendWithANote(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	me := e.session(t, "p0dxD")
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	if err := e.store.CreateApp(ctx, &store.App{Name: "reenvio-api", Repo: "p0dxD/reenvio-api", DefaultBranch: "main", Spec: sp}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetCodeAgent(ctx, store.DefaultOrg, store.CodeAgentAsked, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	tok := randomToken()
	k, err := e.store.CreateAgentKey(ctx, store.DefaultOrg, "agente de código", hashToken(tok), store.RoleCollaborator, nil, "rendimiento", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.store.TakeCargo(ctx, k.ID, "reenviar")
	if err != nil {
		t.Fatal(err)
	}
	givenBack := func(title string) store.Encargo {
		t.Helper()
		enc, err := e.store.CreateEncargo(ctx, store.Encargo{Title: title, App: "reenvio-api", CreatedBy: "p0dxD", OrgID: store.DefaultOrg})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.store.TakeEncargo(ctx, enc.ID, k.ID, c.ID, k.Name); err != nil {
			t.Fatal(err)
		}
		if enc, err = e.store.GiveBackEncargo(ctx, enc.ID, k.ID, k.Name, "Me detuve: la migración necesita lock_timeout y no sé si lo quiere."); err != nil {
			t.Fatal(err)
		}
		return enc
	}
	read := func(id int64) store.Encargo {
		t.Helper()
		_, body := e.do(t, "GET", fmt.Sprintf("/api/encargos/%d", id), "", me, nil)
		var out store.Encargo
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("%v: %s", err, body)
		}
		return out
	}

	enc := givenBack("Endurecer las migraciones")
	if got := read(enc.ID); !got.GivenBack {
		t.Fatalf("given back, it does not say so: %+v", got)
	}
	send := fmt.Sprintf(`{"ids":[%d],"notes":{"%d":"  Sí, haga lo de lock_timeout.  "}}`, enc.ID, enc.ID)
	for name, hdr := range map[string]map[string]string{
		"no session":   nil,
		"an agent key": {"Authorization": "Bearer " + tok},
	} {
		if r, body := e.do(t, "POST", "/api/encargos/do", send, nil, hdr); r.StatusCode != 401 {
			t.Fatalf("sent with %s: %d %s", name, r.StatusCode, body)
		}
		if r, _ := e.do(t, "POST", fmt.Sprintf("/api/encargos/%d/route", enc.ID), `{"route":"local","note":"Mire otra cosa"}`, nil, hdr); r.StatusCode != 401 {
			t.Fatalf("routed with %s: %d", name, r.StatusCode)
		}
	}
	long := fmt.Sprintf(`{"ids":[%d],"notes":{"%d":%q}}`, enc.ID, enc.ID, strings.Repeat("a", 4001))
	if r, _ := e.do(t, "POST", "/api/encargos/do", long, me, nil); r.StatusCode != 400 {
		t.Fatalf("a note too long: %d", r.StatusCode)
	}
	if r, body := e.do(t, "POST", "/api/encargos/do", send, me, nil); r.StatusCode != 200 || !strings.Contains(body, `"sent":1`) {
		t.Fatalf("sent again: %d %s", r.StatusCode, body)
	}
	got := read(enc.ID)
	if got.GivenBack || !got.DoNow {
		t.Fatalf("sent again, still given back: %+v", got)
	}
	last := got.Log[len(got.Log)-1]
	if last.What != "told" || last.Who != "p0dxD" || last.Note != "Sí, haga lo de lock_timeout." {
		t.Fatalf("the note in its history: %+v", got.Log)
	}
	if why, told := got.GaveBack(); !strings.Contains(why, "lock_timeout y no sé") || len(told) != 1 || told[0] != "Sí, haga lo de lock_timeout." {
		t.Fatalf("what the agent reads: %q %q", why, told)
	}

	// An old client: no notes, sent all the same, nothing told.
	old := givenBack("Sin nota")
	if r, body := e.do(t, "POST", "/api/encargos/do", fmt.Sprintf(`{"ids":[%d]}`, old.ID), me, nil); r.StatusCode != 200 || !strings.Contains(body, `"sent":1`) {
		t.Fatalf("sent without a note: %d %s", r.StatusCode, body)
	}
	for _, l := range read(old.ID).Log {
		if l.What == "told" {
			t.Fatalf("told with no note: %+v", l)
		}
	}

	// To the person's own agent, with what it should know.
	local := givenBack("Para mi agente")
	r, body := e.do(t, "POST", fmt.Sprintf("/api/encargos/%d/route", local.ID), `{"route":"local","note":"Mire el índice de runs en vez de la migración"}`, me, nil)
	if r.StatusCode != 200 || !strings.Contains(body, `"route":"local"`) || !strings.Contains(body, `"what":"told"`) {
		t.Fatalf("left to my agent: %d %s", r.StatusCode, body)
	}
	if _, told := read(local.ID).GaveBack(); len(told) != 1 || told[0] != "Mire el índice de runs en vez de la migración" {
		t.Fatalf("what my agent reads: %q", told)
	}
	if r, body := e.do(t, "POST", fmt.Sprintf("/api/encargos/%d/route", local.ID), `{"route":"local"}`, me, nil); r.StatusCode != 200 || strings.Count(body, `"what":"told"`) != 1 {
		t.Fatalf("routed again without a note: %d %s", r.StatusCode, body)
	}
}
