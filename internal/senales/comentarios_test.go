package senales

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A comment goes with only its text, language and the installation's four
// data, and the hash of a token that stays; the answer is asked for with
// the token, and 429 says to wait.
func TestComentario(t *testing.T) {
	var llego ComentarioSaliente
	var raw string
	var ficha string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/senales/comentarios":
			b, _ := io.ReadAll(r.Body)
			raw = string(b)
			_ = json.Unmarshal(b, &llego)
			if strings.Contains(llego.Texto, "muchos") {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"id":42}`)
		case r.URL.Path == "/api/senales/comentarios/42/respuesta":
			h := sha256.Sum256([]byte(r.Header.Get("X-Ficha")))
			if hex.EncodeToString(h[:]) != ficha {
				http.NotFound(w, r)
				return
			}
			io.WriteString(w, `{"respuesta":"Gracias","respondidoPor":"p0dxD","respondidoAt":"2026-11-16T10:00:00Z"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	e := &Enviador{URL: srv.URL + "/api/senales", Version: "0.2.2", Modo: "local", Motor: "docker"}
	if v := e.Comentario("hola", "es"); v.Ficha != FichaVista || v.Datos.Version != "0.2.2" || v.Datos.SO == "" {
		t.Fatalf("preview %+v", v)
	}
	id, token, err := e.EnviarComentario(context.Background(), "hola", "es")
	if err != nil || id != 42 || len(token) != 64 {
		t.Fatalf("sent %d %q %v", id, token, err)
	}
	ficha = llego.Ficha
	h := sha256.Sum256([]byte(token))
	if ficha != hex.EncodeToString(h[:]) || strings.Contains(raw, token) || strings.Contains(raw, "instalacion") {
		t.Fatalf("what went: %s", raw)
	}
	r, ok, err := e.PedirRespuesta(context.Background(), 42, token)
	if err != nil || !ok || r.Respuesta != "Gracias" || r.RespondidoPor != "p0dxD" {
		t.Fatalf("answer %+v %v %v", r, ok, err)
	}
	if _, _, err := e.EnviarComentario(context.Background(), "muchos", "es"); err != ErrDemasiados {
		t.Fatalf("429: %v", err)
	}
}

// fakeComentarios is a store with comments that wait, and the answers kept.
type fakeComentarios struct {
	mu       sync.Mutex
	espera   []store.MiComentario
	guardado map[int64]string
}

func (f *fakeComentarios) ComentariosSinRespuesta(context.Context) ([]store.MiComentario, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.MiComentario(nil), f.espera...), nil
}

func (f *fakeComentarios) GuardarRespuestaRecibida(_ context.Context, id int64, respuesta, _ string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.guardado == nil {
		f.guardado = map[int64]string{}
	}
	f.guardado[id] = respuesta
	return nil
}

// contador is rendimiento.ai answering 204 (or status) and counting the asks per comment.
type contador struct {
	mu     sync.Mutex
	veces  map[string]int
	status int
}

func (c *contador) servir() *httptest.Server {
	c.veces = map[string]int{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.veces[r.URL.Path]++
		st := c.status
		c.mu.Unlock()
		if st == 0 {
			st = http.StatusNoContent
		}
		if st == http.StatusOK {
			io.WriteString(w, `{"respuesta":"Ya quedó","respondidoPor":"p0dxD","respondidoAt":"2026-11-16T10:00:00Z"}`)
			return
		}
		w.WriteHeader(st)
	}))
}

func (c *contador) n(remoto int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.veces["/api/senales/comentarios/"+strconv.Itoa(remoto)+"/respuesta"]
}

// A comment is asked about the first time always, then every 30 minutes;
// once it has waited a day, every 3 hours.
func TestRecogidaToca(t *testing.T) {
	t0 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	r := &Recogida{}
	nuevo := store.MiComentario{ID: 1, CreatedAt: t0.Add(-time.Hour)}
	viejo := store.MiComentario{ID: 2, CreatedAt: t0.Add(-48 * time.Hour)}
	if !r.Toca(nuevo, t0) || !r.Toca(viejo, t0) {
		t.Fatal("the first ask since start is always")
	}
	r.anotar(1, t0)
	r.anotar(2, t0)
	for _, c := range []struct {
		c    store.MiComentario
		tras time.Duration
		toca bool
	}{
		{nuevo, 10 * time.Minute, false}, {nuevo, 30*time.Minute - 20*time.Second, true}, {nuevo, 30 * time.Minute, true},
		{viejo, 30 * time.Minute, false}, {viejo, 2 * time.Hour, false}, {viejo, 3 * time.Hour, true},
	} {
		if got := r.Toca(c.c, t0.Add(c.tras)); got != c.toca {
			t.Errorf("comment %d after %s: %v, want %v", c.c.ID, c.tras, got, c.toca)
		}
	}
}

// The first ask comes soon after start (not after hours), the next ones
// every Cada; a failure is retried next time, and an answer is kept.
func TestRecogerRespuestasPronto(t *testing.T) {
	var c contador
	c.status = http.StatusBadGateway
	srv := c.servir()
	defer srv.Close()
	st := &fakeComentarios{espera: []store.MiComentario{{ID: 1, Remoto: 7, Ficha: "f", CreatedAt: time.Now()}}}
	e := &Enviador{URL: srv.URL + "/api/senales", Respuestas: Recogida{Primera: 30 * time.Millisecond, Cada: 60 * time.Millisecond}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inicio := time.Now()
	go e.RecogerRespuestas(ctx, st)
	esperar := func(n int) {
		t.Helper()
		for limite := time.Now().Add(5 * time.Second); c.n(7) < n; {
			if time.Now().After(limite) {
				t.Fatalf("%d asks, want %d", c.n(7), n)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	esperar(1)
	if d := time.Since(inicio); d < 30*time.Millisecond {
		t.Fatalf("asked %s after start, before Primera", d)
	}
	c.mu.Lock()
	c.status = http.StatusOK // the failure is asked again, and now the answer came
	c.mu.Unlock()
	esperar(2)
	for limite := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		st.mu.Lock()
		got := st.guardado[1]
		st.mu.Unlock()
		if got == "Ya quedó" {
			break
		}
		if time.Now().After(limite) {
			t.Fatal("the answer was not kept")
		}
	}
}

// With the clock: a comment that waited a day is asked about every 3
// hours, not every 30 minutes.
func TestRecogerAtrasado(t *testing.T) {
	var c contador
	srv := c.servir()
	defer srv.Close()
	ahora := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	st := &fakeComentarios{espera: []store.MiComentario{
		{ID: 1, Remoto: 7, Ficha: "f", CreatedAt: ahora.Add(-time.Hour)},
		{ID: 2, Remoto: 8, Ficha: "f", CreatedAt: ahora.Add(-72 * time.Hour)},
	}}
	e := &Enviador{URL: srv.URL + "/api/senales", Respuestas: Recogida{Ahora: func() time.Time { return ahora }}}
	ctx := context.Background()
	for i := 0; i < 7; i++ { // every 30 minutes for 3 hours
		e.recoger(ctx, st, false)
		ahora = ahora.Add(RespuestasCada)
	}
	if c.n(7) != 7 || c.n(8) != 2 {
		t.Fatalf("asked %d (new) and %d (old) times, want 7 and 2", c.n(7), c.n(8))
	}
}

// Opening the page asks at once, at most once a minute; without comments
// that wait, it neither asks nor uses up the minute.
func TestRecogerAlVer(t *testing.T) {
	var c contador
	srv := c.servir()
	defer srv.Close()
	ahora := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	st := &fakeComentarios{}
	e := &Enviador{URL: srv.URL + "/api/senales", Respuestas: Recogida{Ahora: func() time.Time { return ahora }}}
	ctx := context.Background()
	e.RecogerAlVer(ctx, st, time.Second)
	st.espera = []store.MiComentario{{ID: 1, Remoto: 7, Ficha: "f", CreatedAt: ahora}}
	e.RecogerAlVer(ctx, st, time.Second)
	if c.n(7) != 1 {
		t.Fatalf("the page asked %d times, want 1", c.n(7))
	}
	ahora = ahora.Add(30 * time.Second)
	e.RecogerAlVer(ctx, st, time.Second)
	if c.n(7) != 1 {
		t.Fatalf("within the minute: %d asks", c.n(7))
	}
	ahora = ahora.Add(31 * time.Second)
	e.RecogerAlVer(ctx, st, time.Second)
	if c.n(7) != 2 {
		t.Fatalf("after the minute: %d asks", c.n(7))
	}
}
