package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/notify"
	"github.com/rendimiento-ai/rendimiento/internal/senales"
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

// From an installation to the team and back: the page shows exactly what
// would go; Send keeps the comment here and there; the answer a person
// sends there is fetched with the token, lands here, counts in the bell
// until seen. Off, nothing leaves; without the token, no answer.
func TestAlEquipoYDeVuelta(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	me := e.session(t, "p0dxD")
	e.s.SenalesRecibir = true // this platform is also where comments go, as rendimiento.ai is
	e.s.Senales = &senales.Enviador{Store: e.store, URL: e.srv.URL + "/api/senales", Version: "0.2.2", Modo: "kubernetes", Motor: "kubernetes"}
	e.s.Notify = &notify.Notifier{Sender: &correos{}}

	_, body := e.do(t, "GET", "/api/al-equipo", "", me, nil)
	if !strings.Contains(body, `"encendidos":true`) || !strings.Contains(body, `"version":"0.2.2"`) || !strings.Contains(body, "sha256 of a random token") {
		t.Fatalf("the page: %s", body)
	}
	if r, body := e.do(t, "POST", "/api/al-equipo", `{"texto":"Me gustaría ver la versión en el pie"}`, me, nil); r.StatusCode != 200 || !strings.Contains(body, "versión en el pie") {
		t.Fatalf("send: %d %s", r.StatusCode, body)
	}
	llegadas, _ := e.store.Opiniones(ctx, 10)
	if len(llegadas) != 1 || llegadas[0].Origen != store.OpinionApp || !strings.Contains(string(llegadas[0].Datos), "0.2.2") {
		t.Fatalf("arrived: %+v", llegadas)
	}
	mios, _ := e.store.MisComentarios(ctx, "p0dxD")
	if len(mios) != 1 || mios[0].Remoto != llegadas[0].ID || len(mios[0].Ficha) != 64 {
		t.Fatalf("kept here: %+v", mios)
	}
	if r, _ := e.do(t, "GET", fmt.Sprintf("/api/senales/comentarios/%d/respuesta", mios[0].Remoto), "", nil, map[string]string{"X-Ficha": mios[0].Ficha}); r.StatusCode != 204 {
		t.Fatalf("no answer yet: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "GET", fmt.Sprintf("/api/senales/comentarios/%d/respuesta", mios[0].Remoto), "", nil, map[string]string{"X-Ficha": strings.Repeat("0", 64)}); r.StatusCode != 404 {
		t.Fatalf("another token: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "POST", fmt.Sprintf("/api/opiniones/%d/respuesta", mios[0].Remoto), `{"respuesta":"¡Ya está! Véalo al pie."}`, me, nil); r.StatusCode != 200 {
		t.Fatalf("answer: %d", r.StatusCode)
	}
	// Opening the page asks right then: the answer is there without waiting for the next round.
	if _, body := e.do(t, "GET", "/api/al-equipo", "", me, nil); !strings.Contains(body, "¡Ya está! Véalo al pie.") || !strings.Contains(body, `"respondidoPor":"p0dxD"`) {
		t.Fatalf("the answer here: %s", body)
	}
	if _, body := e.do(t, "GET", "/api/al-equipo/sin-ver", "", me, nil); !strings.Contains(body, `"sinVer":0`) {
		t.Fatalf("seen: %s", body)
	}
	// One that comes in the background counts in the bell until the page is opened.
	ficha2 := strings.Repeat("ab", 32)
	h2 := sha256.Sum256([]byte(ficha2))
	remoto2, err := e.store.GuardarOpinion(ctx, store.NuevaOpinion{Origen: store.OpinionApp, Texto: "otra idea", Idioma: "es", Ficha: hex.EncodeToString(h2[:]), Remitente: "192.0.2.42"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.GuardarMiComentario(ctx, "p0dxD", "otra idea", remoto2, ficha2); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.do(t, "POST", fmt.Sprintf("/api/opiniones/%d/respuesta", remoto2), `{"respuesta":"Gracias por la idea."}`, me, nil); r.StatusCode != 200 {
		t.Fatalf("answer: %d", r.StatusCode)
	}
	e.s.Senales.Recoger(ctx, e.store)
	if _, body := e.do(t, "GET", "/api/al-equipo/sin-ver", "", me, nil); !strings.Contains(body, `"sinVer":1`) {
		t.Fatalf("the bell: %s", body)
	}
	if _, body := e.do(t, "GET", "/api/al-equipo", "", me, nil); !strings.Contains(body, "Gracias por la idea.") {
		t.Fatalf("the second answer here: %s", body)
	}
	e.s.Senales.Off = true
	if r, _ := e.do(t, "POST", "/api/al-equipo", `{"texto":"otro"}`, me, nil); r.StatusCode != 409 {
		t.Fatalf("señales off: %d", r.StatusCode)
	}
}

// A person passes a comment to the Ruta: it becomes an idea with their
// name and the comment's words quoted, the comment links to it and goes
// back for the AI to draft its answer again; once only. An answered one
// may be passed too, and keeps what was sent.
func TestOpinionALaRuta(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	me := e.session(t, "p0dxD")
	e.s.SenalesRecibir = true
	a, _ := e.store.GuardarOpinion(ctx, store.NuevaOpinion{Origen: store.OpinionApp, Texto: "More color themes, please", Idioma: "en", Ficha: strings.Repeat("ef", 32), Remitente: "192.0.2.41"})
	_ = e.store.GuardarClasificacion(ctx, a, store.Clasificacion{Clase: store.ClaseNuevo, Resumen: "More color themes", Borrador: "Thanks."})

	r, body := e.do(t, "POST", fmt.Sprintf("/api/opiniones/%d/ruta", a), `{}`, me, nil)
	if r.StatusCode != 200 || !strings.Contains(body, `"rutaId":`) || strings.Contains(body, `"clasificadaAt"`) {
		t.Fatalf("pass: %d %s", r.StatusCode, body)
	}
	d, _ := e.store.GetOpinion(ctx, a)
	entrada, err := e.store.Ruta(ctx, *d.RutaID)
	if err != nil {
		t.Fatal(err)
	}
	if entrada.Kind != store.KindPendiente || entrada.Author != "p0dxD" || entrada.ByAgent || entrada.Done ||
		entrada.Title != "Idea de un comentario: More color themes, please" || strings.Join(entrada.Tags, ",") != "idea,comentario" ||
		!strings.Contains(entrada.Body, "> More color themes, please") || !strings.Contains(entrada.Body, "More color themes\n") {
		t.Fatalf("the entry: %+v", entrada)
	}
	if sin, _ := e.store.OpinionesSinClasificar(ctx, 10); len(sin) != 1 || sin[0].ID != a || sin[0].RutaID == nil {
		t.Fatalf("not back for its draft: %+v", sin)
	}
	if r, _ := e.do(t, "POST", fmt.Sprintf("/api/opiniones/%d/ruta", a), `{}`, me, nil); r.StatusCode != 409 {
		t.Fatalf("passed twice: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "POST", "/api/opiniones/999999/ruta", `{}`, me, nil); r.StatusCode != 404 {
		t.Fatalf("no such comment: %d", r.StatusCode)
	}

	b, _ := e.store.GuardarOpinion(ctx, store.NuevaOpinion{Origen: store.OpinionApp, Texto: "Dark mode on the map", Ficha: strings.Repeat("ab", 32), Remitente: "192.0.2.42"})
	_ = e.store.GuardarClasificacion(ctx, b, store.Clasificacion{Clase: store.ClaseNuevo, Resumen: "Dark map", Borrador: "Thanks."})
	_ = e.store.ResponderOpinion(ctx, b, "Thanks, noted.", "p0dxD")
	if r, body := e.do(t, "POST", fmt.Sprintf("/api/opiniones/%d/ruta", b), `{}`, me, nil); r.StatusCode != 200 || !strings.Contains(body, `"clasificadaAt"`) || !strings.Contains(body, `"respuesta":"Thanks, noted."`) {
		t.Fatalf("an answered one: %d %s", r.StatusCode, body)
	}
}

// An installation asking for an answer leaves a line in the log with the
// comment's number and what it got, never the token.
func TestNotaRespuesta(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{Log: slog.New(slog.NewJSONHandler(&buf, nil))}
	s.notaRespuesta(42, http.StatusNoContent)
	var l map[string]any
	if err := json.Unmarshal(buf.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	if l["level"] != "INFO" || l["comment"] != float64(42) || l["status"] != float64(204) || l["component"] != "comentarios" || len(l) != 6 {
		t.Fatalf("the line: %s", buf.String())
	}
}
