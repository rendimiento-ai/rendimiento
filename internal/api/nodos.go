package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// cifrasNodo is what a node did: today's steps and images, the last 7 days
// by app, and for a node «a ratos» since when it is on and its hours this
// week (its drawer on the Environment page, Ruta pendientes 199 and 201).
func (s *Server) cifrasNodo(w http.ResponseWriter, r *http.Request, _ string) {
	nodo := strings.TrimSpace(r.PathValue("node"))
	if nodo == "" || len(nodo) > 253 {
		httpError(w, http.StatusBadRequest, "which node?")
		return
	}
	c, err := s.Store.CifrasDeNodo(r.Context(), nodo, time.Now())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, c)
}

// putARatos sets what a node «a ratos» does while it is on: build the other
// half of images, run tests, run again a step it shut down under, and
// whether a build waits for it while it is off. Every process follows it
// within 15 seconds, this one at once.
func (s *Server) putARatos(w http.ResponseWriter, r *http.Request, login string) {
	nodo := strings.TrimSpace(r.PathValue("node"))
	if nodo == "" || len(nodo) > 253 {
		httpError(w, http.StatusBadRequest, "which node?")
		return
	}
	var in store.ARatosNodo
	if !readJSON(w, r, &in) {
		return
	}
	c, err := s.Store.GetCapacity(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.ARatos == nil {
		c.ARatos = map[string]store.ARatosNodo{}
	}
	if in == (store.ARatosNodo{}) {
		delete(c.ARatos, nodo)
	} else {
		c.ARatos[nodo] = in
	}
	if err := s.Store.SaveCapacity(r.Context(), c, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Capacity != nil {
		if c, err := s.Store.GetCapacity(r.Context()); err == nil {
			s.Capacity.Set(c)
		}
	}
	s.Log.Info("node «a ratos» set", "node", nodo, "sinOtraMitad", in.SinOtraMitad, "sinPruebas", in.SinPruebas, "sinReintento", in.SinReintento, "esperar", in.Esperar, "by", login)
	s.capacityView(w, r)
}
