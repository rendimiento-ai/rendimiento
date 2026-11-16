package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/notify"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The comments endpoint answers only where señales are received, keeps a
// comment from the form (with its email) and one from an installation
// (with only the data its person saw), turns away the malformed, drops
// what robots send as if kept, and holds a sender to three an hour.
func TestRecibirOpinion(t *testing.T) {
	e := newEnv(t, false)
	mandar := func(body string, hdr map[string]string) (int, string) {
		t.Helper()
		r, b := e.do(t, "POST", "/api/senales/comentarios", body, nil, hdr)
		return r.StatusCode, b
	}
	forma := `{"texto":"¿Se puede con GitLab?","idioma":"es","nombre":"Ana","correo":"ana@example.com"}`
	if code, _ := mandar(forma, nil); code != 404 {
		t.Fatalf("not receiving: %d", code)
	}
	e.s.SenalesRecibir = true
	desde := map[string]string{"CF-Connecting-IP": "203.0.113.7"}
	if code, b := mandar(forma, desde); code != 201 || !strings.Contains(b, `"id":`) {
		t.Fatalf("from the form: %d %s", code, b)
	}
	ficha := strings.Repeat("ab", 32)
	app := `{"texto":"Me encanta el pie de página","datos":{"version":"0.2.2","modo":"local","motor":"docker","so":"linux","apps":["secreta"]},"ficha":"` + ficha + `"}`
	if code, b := mandar(app, map[string]string{"CF-Connecting-IP": "198.51.100.2"}); code != 201 {
		t.Fatalf("from an installation: %d %s", code, b)
	}
	for _, malo := range []string{
		`{"texto":"  "}`,
		`{"texto":"sin a quién responder"}`,
		`{"texto":"hola","correo":"no es un correo"}`,
		`{"texto":"hola","ficha":"corta"}`,
		`{"texto":"` + strings.Repeat("a", MaxOpinion+1) + `","correo":"ana@example.com"}`,
		`no es json`,
	} {
		if code, _ := mandar(malo, map[string]string{"CF-Connecting-IP": "192.0.2.9"}); code != 400 {
			t.Errorf("%.40s: %d, want 400", malo, code)
		}
	}
	if code, _ := mandar(`{"texto":"compre ya","correo":"bot@example.com","sitio":"https://spam.example"}`, desde); code != 204 {
		t.Fatalf("a robot: %d", code)
	}
	mandar(forma, desde)
	mandar(forma, desde)
	if code, _ := mandar(forma, desde); code != 429 {
		t.Fatalf("a fourth in the hour from one sender: %d", code)
	}

	got, err := e.store.Opiniones(context.Background(), 10)
	if err != nil || len(got) != 4 {
		t.Fatalf("kept: %d %v", len(got), err)
	}
	var deApp bool
	for _, c := range got {
		if c.Origen == "app" {
			deApp = true
			var d map[string]any
			if err := json.Unmarshal(c.Datos, &d); err != nil || d["version"] != "0.2.2" || d["so"] != "linux" || len(d) != 4 {
				t.Fatalf("an installation's data, only the known fields: %s", c.Datos)
			}
		}
		if strings.Contains(c.Texto, "compre") {
			t.Fatal("a robot's comment was kept")
		}
	}
	if !deApp {
		t.Fatal("the installation's comment is missing")
	}
}

// correos keeps the emails it was asked to send.
type correos struct {
	mu   sync.Mutex
	sent []string // to|subject|text
}

func (c *correos) Send(_ context.Context, to []string, subject, _, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, strings.Join(to, ",")+"|"+subject+"|"+text)
	return nil
}

// The team's page: the comments are listed without emails, one is read
// with its email, changed, answered once by email with the person's name,
// and set aside; the bell counts those waiting. Where comments are not
// received, none of it answers.
func TestBuzonAPI(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	me := e.session(t, "p0dxD")
	if r, _ := e.do(t, "GET", "/api/opiniones", "", me, nil); r.StatusCode != 404 {
		t.Fatalf("not receiving: %d", r.StatusCode)
	}
	e.s.SenalesRecibir = true
	enviados := &correos{}
	e.s.Notify = &notify.Notifier{Sender: enviados}
	a, _ := e.store.GuardarOpinion(ctx, store.NuevaOpinion{Origen: store.OpinionFormulario, Texto: "¿Y GitLab?", Idioma: "es", Nombre: "Ana", Correo: "ana@example.com", Remitente: "192.0.2.31"})
	b, _ := e.store.GuardarOpinion(ctx, store.NuevaOpinion{Origen: store.OpinionApp, Texto: "Me gusta", Ficha: strings.Repeat("cd", 32), Remitente: "192.0.2.32"})
	_ = e.store.GuardarClasificacion(ctx, a, store.Clasificacion{Clase: store.ClaseNuevo, Resumen: "GitLab support", Borrador: "Gracias, quedó anotado."})

	_, body := e.do(t, "GET", "/api/opiniones", "", me, nil)
	if strings.Contains(body, "ana@example.com") || !strings.Contains(body, "GitLab support") {
		t.Fatalf("the list: %s", body)
	}
	if _, body := e.do(t, "GET", "/api/opiniones/cuenta", "", me, nil); !strings.Contains(body, `"porRevisar":2`) {
		t.Fatalf("count: %s", body)
	}
	if _, body := e.do(t, "GET", fmt.Sprintf("/api/opiniones/%d", a), "", me, nil); !strings.Contains(body, `"correo":"ana@example.com"`) {
		t.Fatalf("one: %s", body)
	}
	if r, _ := e.do(t, "PUT", fmt.Sprintf("/api/opiniones/%d", a), `{"clase":"otra"}`, me, nil); r.StatusCode != 400 {
		t.Fatalf("a bad class: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "PUT", fmt.Sprintf("/api/opiniones/%d", a), `{"estado":"respondido"}`, me, nil); r.StatusCode != 400 {
		t.Fatalf("answered by PUT: %d", r.StatusCode)
	}
	if r, body := e.do(t, "PUT", fmt.Sprintf("/api/opiniones/%d", a), `{"clase":"ya_existe","borrador":"Ya existe"}`, me, nil); r.StatusCode != 200 || !strings.Contains(body, `"clase":"ya_existe"`) {
		t.Fatalf("change: %d %s", r.StatusCode, body)
	}
	r, body := e.do(t, "POST", fmt.Sprintf("/api/opiniones/%d/respuesta", a), `{"respuesta":"Hola Ana: ya se puede, vea el libro."}`, me, nil)
	if r.StatusCode != 200 || !strings.Contains(body, `"estado":"respondido"`) || !strings.Contains(body, `"respondidoPor":"p0dxD"`) {
		t.Fatalf("answer: %d %s", r.StatusCode, body)
	}
	if len(enviados.sent) != 1 || !strings.HasPrefix(enviados.sent[0], "ana@example.com|Su comentario a rendimiento|Hola Ana,") ||
		!strings.Contains(enviados.sent[0], "— p0dxD") || !strings.Contains(enviados.sent[0], "> ¿Y GitLab?") {
		t.Fatalf("sent: %q", enviados.sent)
	}
	if r, _ := e.do(t, "POST", fmt.Sprintf("/api/opiniones/%d/respuesta", a), `{"respuesta":"otra vez"}`, me, nil); r.StatusCode != 409 {
		t.Fatalf("answered twice: %d", r.StatusCode)
	}
	if r, body := e.do(t, "POST", fmt.Sprintf("/api/opiniones/%d/respuesta", b), `{"respuesta":"¡Gracias!"}`, me, nil); r.StatusCode != 200 || len(enviados.sent) != 1 {
		t.Fatalf("an installation's comment is answered without email: %d %s %q", r.StatusCode, body, enviados.sent)
	}
	if _, body := e.do(t, "GET", "/api/opiniones/cuenta", "", me, nil); !strings.Contains(body, `"porRevisar":0`) {
		t.Fatalf("count after: %s", body)
	}
	if r, _ := e.do(t, "POST", fmt.Sprintf("/api/opiniones/%d/reclasificar", a), `{}`, me, nil); r.StatusCode != 404 {
		t.Fatalf("an answered one is not sorted again: %d", r.StatusCode)
	}
}
