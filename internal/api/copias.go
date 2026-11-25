package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/platform"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ---- an app's live copies, on its Caminos tab (Ruta 180) ----
//
// Whoever can see the app reads its copies; a person who may merge (a
// collaborator or an administrator of the app) makes one again, keeps it
// longer or deletes it. Like every route here they take a session, so an
// agent's key never can; each action is in the bitácora.

// copiasRoutes adds the copies' routes.
func (s *Server) copiasRoutes(auth func(role, pattern string, h func(http.ResponseWriter, *http.Request, string))) {
	auth(store.MemberGuest, "GET /api/apps/{app}/copias", s.appCopias)
	auth(store.MemberCollaborator, "POST /api/apps/{app}/copias/{n}/rehacer", s.rehacerCopia)
	auth(store.MemberCollaborator, "POST /api/apps/{app}/copias/{n}/extender", s.extenderCopia)
	auth(store.MemberCollaborator, "DELETE /api/apps/{app}/copias/{n}", s.borrarCopia)
}

// appCopias is what the Caminos tab shows of the app's copies.
func (s *Server) appCopias(w http.ResponseWriter, r *http.Request, _ string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	v, err := s.Platform.CopiasOf(r.Context(), a)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, v)
}

// copiaTarget is the app and the pull request number in the URL (nil after
// writing the error).
func (s *Server) copiaTarget(w http.ResponseWriter, r *http.Request) (*store.App, int) {
	a := s.app(w, r)
	if a == nil {
		return nil, 0
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n <= 0 {
		httpError(w, http.StatusBadRequest, i18n.M("bad pull request number"))
		return nil, 0
	}
	return a, n
}

// copiaError answers an action on a copy that failed.
func copiaError(w http.ResponseWriter, err error) {
	var no *platform.CopiaNo
	switch {
	case errors.As(err, &no):
		httpError(w, http.StatusConflict, no.Why)
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, i18n.M("this pull request has no live copy"))
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
	}
}

// rehacerCopia makes the copy again with its branch's last commit whose
// run passed.
func (s *Server) rehacerCopia(w http.ResponseWriter, r *http.Request, login string) {
	a, n := s.copiaTarget(w, r)
	if a == nil {
		return
	}
	got, err := s.Platform.RemakeCopy(r.Context(), a, n, login)
	if err != nil {
		copiaError(w, err)
		return
	}
	anotarDetalle(r, "run", got.Run)
	writeJSON(w, got)
}

// extenderCopia keeps the copy 3 more days, up to previews' most.
func (s *Server) extenderCopia(w http.ResponseWriter, r *http.Request, login string) {
	a, n := s.copiaTarget(w, r)
	if a == nil {
		return
	}
	until, err := s.Platform.ExtendCopy(r.Context(), a, n, login)
	if err != nil {
		copiaError(w, err)
		return
	}
	anotarDetalle(r, "until", until)
	writeJSON(w, map[string]any{"deletesOn": until})
}

// borrarCopia deletes the copy now; the next push to its branch makes it
// again.
func (s *Server) borrarCopia(w http.ResponseWriter, r *http.Request, login string) {
	a, n := s.copiaTarget(w, r)
	if a == nil {
		return
	}
	if err := s.Platform.DeleteCopy(r.Context(), a, n, login); err != nil {
		copiaError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
