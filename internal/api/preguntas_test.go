package api

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// What the coding agent asks shows first on «Para usted»; a person answers
// it (back to the agent's queue) or passes it to their own agent. An agent
// key never answers for a person, and an encargo that asks nothing is not
// answered.
func TestAnswerTheCodingAgent(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	me := e.session(t, "p0dxD")
	tok := randomToken()
	k, err := e.store.CreateAgentKey(ctx, store.DefaultOrg, "agente de código", hashToken(tok), store.RoleCollaborator, nil, "rendimiento", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	ask := func(title, question string, options ...string) store.Encargo {
		t.Helper()
		enc, err := e.store.CreateEncargo(ctx, store.Encargo{Title: title, CreatedBy: "p0dxD"})
		if err != nil {
			t.Fatal(err)
		}
		c, err := e.store.TakeCargo(ctx, k.ID, title)
		if err != nil {
			c, err = e.store.OpenCargo(ctx, k.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.store.TakeEncargo(ctx, enc.ID, k.ID, c.ID, k.Name); err != nil {
			t.Fatal(err)
		}
		if enc, err = e.store.AskEncargo(ctx, enc.ID, k.ID, k.Name, store.EncargoPregunta{Question: question, Options: options}); err != nil {
			t.Fatal(err)
		}
		return enc
	}
	enc := ask("Comentarios al agente", "¿Botón o plática?", "Un botón", "Una plática")
	path := fmt.Sprintf("/api/encargos/%d", enc.ID)

	if _, body := e.do(t, "GET", "/api/ruta/portada", "", me, nil); !strings.Contains(body, `"asking":[{`) || !strings.Contains(body, "¿Botón o plática?") {
		t.Fatalf("«Para usted» without the question: %s", body)
	}
	for name, hdr := range map[string]map[string]string{
		"no session":   nil,
		"an agent key": {"Authorization": "Bearer " + tok},
	} {
		if r, body := e.do(t, "POST", path+"/answer", `{"answer":"Un botón"}`, nil, hdr); r.StatusCode != 401 {
			t.Fatalf("answered with %s: %d %s", name, r.StatusCode, body)
		}
		if r, _ := e.do(t, "POST", path+"/pass", "", nil, hdr); r.StatusCode != 401 {
			t.Fatalf("passed with %s: %d", name, r.StatusCode)
		}
	}
	if r, _ := e.do(t, "POST", path+"/answer", `{"answer":"  "}`, me, nil); r.StatusCode != 400 {
		t.Fatalf("an empty answer: %d", r.StatusCode)
	}
	r, body := e.do(t, "POST", path+"/answer", `{"answer":"Un botón, y que se vea en el encargo"}`, me, nil)
	if r.StatusCode != 200 || !strings.Contains(body, `"status":"pending"`) || !strings.Contains(body, `"doNow":true`) {
		t.Fatalf("answered: %d %s", r.StatusCode, body)
	}
	if r, _ := e.do(t, "POST", path+"/answer", `{"answer":"otra vez"}`, me, nil); r.StatusCode != 409 {
		t.Fatalf("answered twice: %d", r.StatusCode)
	}
	if p, err := e.store.ResumeAnswer(ctx, enc.ID); err != nil || p.Answer != "Un botón, y que se vea en el encargo" || p.AnsweredBy != "p0dxD" {
		t.Fatalf("the answer the agent gets: %+v (%v)", p, err)
	}

	other := ask("Otra", "¿Sigo?")
	opath := fmt.Sprintf("/api/encargos/%d", other.ID)
	r, body = e.do(t, "POST", opath+"/pass", "", me, nil)
	if r.StatusCode != 200 || !strings.Contains(body, `"status":"pending"`) || !strings.Contains(body, `"route":"local"`) {
		t.Fatalf("passed: %d %s", r.StatusCode, body)
	}
	if _, body := e.do(t, "GET", opath, "", me, nil); !strings.Contains(body, "¿Sigo?") || !strings.Contains(body, `"what":"released"`) {
		t.Fatalf("the question is not in its history: %s", body)
	}
	if r, _ := e.do(t, "POST", opath+"/pass", "", me, nil); r.StatusCode != 409 {
		t.Fatalf("passed twice: %d", r.StatusCode)
	}
}
