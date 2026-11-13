package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// detalleKey carries, in a request's context, what its handler adds to its
// bitácora row (anotarDetalle).
type detalleKey struct{}

// anotarDetalle adds what a route did to its bitácora row, beyond what its
// path says: a decision, what it was about. Never a secret's value.
func anotarDetalle(r *http.Request, k string, v any) {
	if d, ok := r.Context().Value(detalleKey{}).(map[string]any); ok {
		d[k] = v
	}
}

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
		detalle := map[string]any{}
		r = r.WithContext(context.WithValue(r.Context(), detalleKey{}, detalle))
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
				Accion: pattern, Sobre: sobre, Resultado: resultado, Detalle: detalle})
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
	out := []store.Bitacora{}
	for _, b := range rows {
		if accessOf(r).Sees(bitacoraApp(b)) { // about an app whose list leaves the person out
			out = append(out, b)
		}
	}
	writeJSON(w, out)
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

// listAprobaciones is the organization's requests from agents: those still
// waiting with ?pendientes=1, else the last 50.
func (s *Server) listAprobaciones(w http.ResponseWriter, r *http.Request, _ string) {
	all, err := s.Store.ListarAprobaciones(r.Context(), orgOf(r), r.URL.Query().Get("pendientes") == "1")
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows := []store.Aprobacion{}
	for _, a := range all {
		if accessOf(r).Sees(a.App) {
			rows = append(rows, a)
		}
	}
	if want := r.URL.Query().Get("lang"); want != "" {
		for i := range rows {
			s.traducirMotivo(r, &rows[i], want)
		}
	}
	writeJSON(w, rows)
}

// traducirMotivo puts a request's reason in the reader's language (?lang=,
// es or en): kept from an earlier reader, or translated now by the AI and
// kept. The agent wrote it freely, in whatever language; the original
// goes along. When it cannot be translated, the reader gets it as written.
func (s *Server) traducirMotivo(r *http.Request, a *store.Aprobacion, want string) {
	text, ok := a.Traducciones[want]
	if !ok {
		if s.Translator == nil {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var err error
		if text, err = s.Translator.TranslateTo(ctx, a.OrgID, fmt.Sprintf("aprobacion/%d", a.ID), a.Motivo, want); err != nil {
			s.Log.Warn("could not translate a request's reason", "aprobacion", a.ID, "to", want, "err", err)
			return
		}
		if err := s.Store.GuardarTraduccion(context.WithoutCancel(ctx), a.ID, want, text); err != nil {
			s.Log.Warn("could not keep a request's translation", "aprobacion", a.ID, "err", err)
		}
	}
	if text != a.Motivo {
		a.MotivoOriginal, a.Motivo = a.Motivo, text
	}
}

// decidirAprobacion is an administrator approving or rejecting an agent's
// request ({"aprobar": true, "comentario": "…"}); approved, it is done at
// once in their name.
func (s *Server) decidirAprobacion(w http.ResponseWriter, r *http.Request, login string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad request id")
		return
	}
	var req struct {
		Aprobar    bool   `json:"aprobar"`
		Comentario string `json:"comentario"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	a, err := s.Platform.DecidirAprobacion(r.Context(), orgOf(r), id, req.Aprobar, login, req.Comentario)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "no such request")
	case errors.Is(err, store.ErrNoPendiente):
		httpError(w, http.StatusConflict, err.Error())
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		anotarDetalle(r, "decision", a.Estado)
		anotarDetalle(r, "app", a.App)
		anotarDetalle(r, "version", a.Version)
		anotarDetalle(r, "pedida_por", a.PedidaPor)
		if a.Comentario != "" {
			anotarDetalle(r, "comentario", a.Comentario)
		}
		if a.Resultado != "" {
			anotarDetalle(r, "resultado", a.Resultado)
		}
		writeJSON(w, a)
	}
}
