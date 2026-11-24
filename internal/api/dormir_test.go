package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/destino"
	"github.com/rendimiento-ai/rendimiento/internal/platform"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// destinoComputadora stands for the local destination: rendimiento may sleep.
type destinoComputadora struct{ destino.Ninguno }

func (destinoComputadora) Nombre() string { return "local" }

type camaVacia struct{}

func (camaVacia) Dormir(context.Context) ([]string, error) { return []string{"tienda-web-1"}, nil }
func (camaVacia) Despertar(context.Context, []string, []*store.App) ([]string, error) {
	return []string{"tienda"}, nil
}

// In a cluster the sleep's routes answer 409; on a computer only an
// administrator puts it to sleep, the webhooks that arrive meanwhile are
// kept (not acted on), and waking handles them in order and says so.
func TestDormirRoutes(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	admin := e.session(t, "p0dxD")
	if err := e.store.SetMember(ctx, store.DefaultOrg, "colabora", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if r, body := e.do(t, "POST", "/api/dormir", `{}`, admin, nil); r.StatusCode != http.StatusConflict || !strings.Contains(body, "cluster stays awake") {
		t.Fatalf("sleeping in a cluster: %d %s", r.StatusCode, body)
	}

	e.s.Platform.Destino = destinoComputadora{}
	z := &platform.Sueno{P: e.s.Platform, Cama: camaVacia{}, Cada: 20 * time.Millisecond, Avisos: e.s.AtenderAvisos}
	e.s.Sueno = z
	if r, _ := e.do(t, "POST", "/api/dormir", `{}`, e.session(t, "colabora"), nil); r.StatusCode != http.StatusForbidden {
		t.Fatalf("a collaborator put it to sleep: %d", r.StatusCode)
	}
	if r, body := e.do(t, "PUT", "/api/dormir/opciones", `{"despertarA":"8 am"}`, admin, nil); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("a bad hour: %d %s", r.StatusCode, body)
	}
	if r, body := e.do(t, "POST", "/api/dormir", `{"ya":false}`, admin, nil); r.StatusCode != 200 || !strings.Contains(body, `"estado":"durmiendose"`) {
		t.Fatalf("sleep: %d %s", r.StatusCode, body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if st, _ := e.store.GetSueno(ctx); st.Estado == store.Dormido {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("it did not fall asleep")
		}
		time.Sleep(20 * time.Millisecond)
	}

	for _, repo := range []string{"p0dxD/uno", "p0dxD/dos"} {
		body := `{"ref":"refs/heads/main","after":"` + strings.Repeat("a", 40) + `","repository":{"full_name":"` + repo + `"},"installation":{"id":1}}`
		if r, out := e.do(t, "POST", "/api/webhooks/github", body, nil, map[string]string{"X-GitHub-Event": "push", "X-Hub-Signature-256": sign("whsec", body)}); r.StatusCode != http.StatusAccepted {
			t.Fatalf("a webhook while asleep: %d %s", r.StatusCode, out)
		}
	}
	avisos, err := e.store.AvisosGuardados(ctx)
	if err != nil || len(avisos) != 2 || avisos[0].Forja != "github" || !strings.Contains(string(avisos[0].Evento), "p0dxD/uno") || !strings.Contains(string(avisos[1].Evento), "p0dxD/dos") {
		t.Fatalf("kept: %+v %v", avisos, err)
	}
	if _, body := e.do(t, "GET", "/api/dormir", "", admin, nil); !strings.Contains(body, `"avisos":2`) || !strings.Contains(body, `"estado":"dormido"`) {
		t.Fatalf("asleep: %s", body)
	}

	r, body := e.do(t, "POST", "/api/despertar", "", admin, nil)
	if r.StatusCode != 200 || !strings.Contains(body, `"estado":"despierto"`) || !strings.Contains(body, `"avisos":0`) || !strings.Contains(body, `"apps":["tienda"]`) {
		t.Fatalf("wake: %d %s", r.StatusCode, body)
	}
	if st, _ := e.store.GetSueno(ctx); st.Resumen == nil || st.Resumen.Avisos != 2 || st.Resumen.Por != "p0dxD" {
		t.Fatalf("the wake's summary: %+v", st.Resumen)
	}
	// Awake, a webhook is acted on again.
	body = `{"ref":"refs/heads/main","after":"` + strings.Repeat("b", 40) + `","repository":{"full_name":"p0dxD/tres"},"installation":{"id":1}}`
	if r, out := e.do(t, "POST", "/api/webhooks/github", body, nil, map[string]string{"X-GitHub-Event": "push", "X-Hub-Signature-256": sign("whsec", body)}); r.StatusCode != 200 || !strings.Contains(out, `"queued":0`) {
		t.Fatalf("a webhook once awake: %d %s", r.StatusCode, out)
	}
}
