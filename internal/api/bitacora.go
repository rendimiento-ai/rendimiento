package api

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// pathParams are the {names} in a route pattern.
var pathParams = regexp.MustCompile(`\{([a-z]+)\}`)

// anotando records every call of a route that changes something (all but
// GET) in the bitácora: who (the person signed in), the route, what it
// was on (its path values: the app, the run, the secret's name, never a
// value) and how it ended (the HTTP status). Bodies are never kept: they
// can hold secrets. A platform route goes to the platform's organization.
func (s *Server) anotando(pattern string, platform bool, h func(http.ResponseWriter, *http.Request, string)) func(http.ResponseWriter, *http.Request, string) {
	if strings.HasPrefix(pattern, "GET ") {
		return h
	}
	var names []string
	for _, m := range pathParams.FindAllStringSubmatch(pattern, -1) {
		names = append(names, m[1])
	}
	return func(w http.ResponseWriter, r *http.Request, login string) {
		rec := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		// Written even when the handler panics: what was tried is recorded
		// too, and the panic goes on to the server.
		defer func() {
			resultado := strconv.Itoa(rec.status)
			p := recover()
			if p != nil {
				resultado = "panic"
			}
			org := orgOf(r)
			if platform {
				org = store.DefaultOrg
			}
			sobre := map[string]string{}
			for _, n := range names {
				sobre[n] = r.PathValue(n)
			}
			s.anotar(r.Context(), store.Bitacora{OrgID: org, QuienTipo: store.QuienPersona, Quien: login, Por: "interfaz",
				Accion: pattern, Sobre: sobre, Resultado: resultado})
			if p != nil {
				panic(p)
			}
		}()
		h(rec, r, login)
	}
}

// anotar writes to the bitácora even when the request was cancelled; a
// failure is logged, never shown to the person (their action is done).
func (s *Server) anotar(ctx context.Context, b store.Bitacora) {
	if _, err := s.Store.AnotarBitacora(context.WithoutCancel(ctx), b); err != nil {
		s.Log.Error("bitácora", "accion", b.Accion, "err", err)
	}
}

// statusWriter remembers the status a handler answered with.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status, w.wrote = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

// Flush lets a streaming answer through.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap gives http.ResponseController the writer underneath.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// listBitacora is the organization's bitácora, newest first, filtered by
// ?quien=, ?accion=, ?app= and paged with ?antes= (an id).
func (s *Server) listBitacora(w http.ResponseWriter, r *http.Request, _ string) {
	q := r.URL.Query()
	antes, _ := strconv.ParseInt(q.Get("antes"), 10, 64)
	limite, _ := strconv.Atoi(q.Get("limite"))
	rows, err := s.Store.ListarBitacora(r.Context(), orgOf(r), store.FiltroBitacora{
		Quien: q.Get("quien"), Accion: q.Get("accion"), App: q.Get("app"), Antes: antes, Limite: limite})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, rows)
}

// verifyBitacora walks the organization's chain and says whether it holds.
func (s *Server) verifyBitacora(w http.ResponseWriter, r *http.Request, _ string) {
	v, err := s.Store.VerificarBitacora(r.Context(), orgOf(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, v)
}
