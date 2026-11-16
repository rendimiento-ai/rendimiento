package senales

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
