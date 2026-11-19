package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/ruta"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// appLibreta is the Libreta tab (Ruta 150): what was asked for the app and
// whether it is done, each line with its state computed from its branches.
func (s *Server) appLibreta(w http.ResponseWriter, r *http.Request, _ string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	l, err := s.Platform.Libreta(r.Context(), a)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, l)
}

// PedidoInput is a request written on the Libreta tab.
type PedidoInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// anotarPedido («Anotar un pedido») writes a to-do in the Ruta, about the
// app and signed by the person: the libreta only shows what the Ruta has.
func (s *Server) anotarPedido(w http.ResponseWriter, r *http.Request, login string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	var p PedidoInput
	if !readJSON(w, r, &p) {
		return
	}
	in := RutaInput{Kind: store.KindPendiente, Title: p.Title, Body: p.Body, Tags: []string{}}
	if msg := in.check(); msg != "" {
		httpError(w, http.StatusBadRequest, msg)
		return
	}
	e, err := s.Store.AddRuta(r.Context(), store.RutaEntry{Kind: in.Kind, Title: in.Title, Body: in.Body, Tags: in.Tags,
		Author: login, OrgID: a.OrgID, App: a.Name})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("libreta: request noted", "app", a.Name, "entry", e.ID, "by", login)
	writeJSONStatus(w, http.StatusCreated, e)
}

// DescarteInput is why a request of the libreta is set aside ("" takes it
// up again).
type DescarteInput struct {
	Reason string `json:"reason"`
}

// descartarPedido («Descartar», «Retomar») sets a Ruta to-do or decision
// about the app aside with its reason, or takes it up again, keeping the
// version before in the Ruta's history. Its state in the libreta follows.
func (s *Server) descartarPedido(w http.ResponseWriter, r *http.Request, login string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	id, ok := rutaID(w, r)
	if !ok {
		return
	}
	var in DescarteInput
	if !readJSON(w, r, &in) {
		return
	}
	reason := strings.TrimSpace(in.Reason)
	if len([]rune(reason)) > 1000 || ruta.LooksSecret(reason) {
		httpError(w, http.StatusBadRequest, "why it was set aside must be up to 1,000 characters, with no secrets")
		return
	}
	e, err := s.Store.Ruta(r.Context(), id)
	if err == nil && (e.OrgID != a.OrgID || e.App != a.Name || e.ArchivedAt != nil) {
		err = store.ErrNotFound
	}
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such entry")
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if e.Kind != store.KindPendiente && e.Kind != store.KindDecision {
		httpError(w, http.StatusBadRequest, "only a to-do or a decision is set aside here")
		return
	}
	e.Discarded = reason
	if e, err = s.Store.UpdateRuta(r.Context(), e, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("libreta: request set aside or taken up", "app", a.Name, "entry", e.ID, "discarded", reason != "", "by", login)
	writeJSON(w, e)
}
