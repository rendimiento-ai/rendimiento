package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/platform"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// appMesa is the app's work board: what is open on each branch and who it
// waits for.
func (s *Server) appMesa(w http.ResponseWriter, r *http.Request, _ string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	b, err := s.Platform.Mesa(r.Context(), a)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, b)
}

// MesaCount is how many lines of an app's board wait for a person.
type MesaCount struct {
	App string `json:"app"`
	You int    `json:"you"`
}

// mesaCounts is, for the home page, how many things wait for a person in
// each of the organization's apps (those with none are left out).
func (s *Server) mesaCounts(w http.ResponseWriter, r *http.Request, _ string) {
	apps, err := s.Store.ListAppsInOrg(r.Context(), orgOf(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var mine []*store.App
	for _, a := range apps {
		if accessOf(r).Role(a) != "" {
			mine = append(mine, a)
		}
	}
	// Kept counts, made again behind the page: making every board now asks
	// GitHub about each app, and took up to 17 s.
	waiting := s.Platform.MesaWaiting(r.Context(), mine)
	out := []MesaCount{}
	for _, a := range mine {
		if n := waiting[a.ID]; n > 0 {
			out = append(out, MesaCount{App: a.Name, You: n})
		}
	}
	writeJSON(w, out)
}

// closeSpare closes a branch the work board calls spare (a copy, or
// forgotten): its pull request without merging, and the branch itself.
func (s *Server) closeSpare(w http.ResponseWriter, r *http.Request, login string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	var in struct {
		Branch string `json:"branch"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := s.Platform.CloseSpare(r.Context(), a, strings.TrimSpace(in.Branch), login)
	var no *platform.NotApprovable
	switch {
	case errors.As(err, &no):
		httpError(w, http.StatusConflict, no.Why)
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
