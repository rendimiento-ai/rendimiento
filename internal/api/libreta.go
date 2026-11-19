package api

import (
	"net/http"

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
