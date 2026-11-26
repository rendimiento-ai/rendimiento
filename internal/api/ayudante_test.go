package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// El ayudante, the whole way: the local model answers first (free), the
// platform's AI when asked; what the person asks from a run's page reaches
// it with the run's data, a run of an app they may not see does not; each
// person sees only their own conversation; an empty or too long question,
// no session and an agent key are refused.
func TestAyudante(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	m := nuevosModelos(t)
	e.s.IA = &ia.Gateway{Store: e.store, LocalURL: m.ollama.URL, LocalModel: "qwen-prueba", PlatformKey: "sk-falsa",
		PlatformModel: ia.DefaultAnthropicModel, PlatformCapMicros: 100_000_000, AnthropicURL: m.claude.URL}
	if err := e.store.SetAIAllowance(ctx, store.DefaultOrg, 5_000_000, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetMember(ctx, store.DefaultOrg, "abi-ayudante", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	app := &store.App{Name: "ayudante-app", Repo: "p0dxD/ayudante-app", DefaultBranch: "main", Spec: sp}
	if err := e.store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	run := &store.Run{AppID: app.ID, SHA: "0123456789abcdef", Branch: "arreglo", Event: "push"}
	if err := e.store.CreateRun(ctx, run, nil); err != nil {
		t.Fatal(err)
	}
	me, abi := e.session(t, "p0dxD"), e.session(t, "abi-ayudante")

	var v AyudanteView
	_, body := e.do(t, "GET", "/api/ayudante", "", me, nil)
	if err := json.Unmarshal([]byte(body), &v); err != nil || len(v.Mensajes) != 0 || v.IA != store.AILocal || len(v.IAs) != 2 || !strings.HasPrefix(v.Libro, "https://") {
		t.Fatalf("before asking: %s", body)
	}

	for name, q := range map[string]string{"empty": `{"texto": "  "}`, "too long": fmt.Sprintf(`{"texto": %q}`, strings.Repeat("a", 2001)), "a page that is not an address": `{"texto": "hola", "pagina": "https://example.com"}`, "an AI it cannot use": `{"texto": "hola", "ia": "agente"}`} {
		if r, body := e.do(t, "POST", "/api/ayudante", q, me, nil); r.StatusCode != 400 {
			t.Fatalf("%s: %d %s", name, r.StatusCode, body)
		}
	}
	tok := randomToken()
	if _, err := e.store.CreateAgentKey(ctx, store.DefaultOrg, "un agente", hashToken(tok), store.RoleCollaborator, nil, "rendimiento", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for name, hdr := range map[string]map[string]string{"no session": nil, "an agent key": {"Authorization": "Bearer " + tok}} {
		if r, body := e.do(t, "POST", "/api/ayudante", `{"texto": "hola"}`, nil, hdr); r.StatusCode != 401 {
			t.Fatalf("asked with %s: %d %s", name, r.StatusCode, body)
		}
	}

	// From a run's page: the local model answers, having read the run.
	page := fmt.Sprintf("/apps/ayudante-app/runs/%d", run.ID)
	var a AyudanteAnswer
	r, body := e.do(t, "POST", "/api/ayudante", fmt.Sprintf(`{"texto": "¿Por qué falló?", "pagina": %q}`, page), me, nil)
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &a) != nil {
		t.Fatalf("a question: %d %s", r.StatusCode, body)
	}
	if a.Respuesta.Kind != store.AyudanteRespuesta || a.Respuesta.Provider != store.AILocal || a.Respuesta.Body == "" || a.Pregunta.Body != "¿Por qué falló?" || a.Pregunta.Pagina != page {
		t.Fatalf("the answer: %+v", a)
	}
	system, user := m.last()
	if !strings.Contains(system, "helper of rendimiento") || !strings.Contains(user, fmt.Sprintf("Run %d of the app ayudante-app", run.ID)) {
		t.Fatalf("what it read: %s\n%s", system, user)
	}

	// The platform's AI, when asked; and a run of an app the person may not see says nothing of it.
	r, body = e.do(t, "POST", "/api/ayudante", `{"texto": "¿Y ahora?", "pagina": "/apps/no-es-suya/runs/1", "ia": "platform"}`, me, nil)
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &a) != nil || a.Respuesta.Provider != store.AIPlatform {
		t.Fatalf("with the platform's AI: %d %s", r.StatusCode, body)
	}
	if _, user := m.last(); strings.Contains(user, "<datos>") {
		t.Fatalf("an app it may not see: %s", user)
	}

	_, body = e.do(t, "GET", "/api/ayudante", "", me, nil)
	if err := json.Unmarshal([]byte(body), &v); err != nil || len(v.Mensajes) != 4 {
		t.Fatalf("their conversation: %s", body)
	}
	_, body = e.do(t, "GET", "/api/ayudante", "", abi, nil)
	if err := json.Unmarshal([]byte(body), &v); err != nil || len(v.Mensajes) != 0 {
		t.Fatalf("another person sees it: %s", body)
	}
}
