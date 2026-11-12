package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/ruta"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// listComentarios is the thread of a cargo (?cargo=) or an encargo
// (?encargo=), or with neither the organization's latest comments, oldest
// first. An agent's answer to one of the person's comments that they have
// not seen yet comes as new.
func (s *Server) listComentarios(w http.ResponseWriter, r *http.Request, login string) {
	q := r.URL.Query()
	cargo, err1 := idParam(q.Get("cargo"))
	encargo, err2 := idParam(q.Get("encargo"))
	if err1 != nil || err2 != nil {
		httpError(w, http.StatusBadRequest, i18n.M("bad cargo or encargo number"))
		return
	}
	list, err := s.Store.ListComentarios(r.Context(), orgOf(r), cargo, encargo, login, 0)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, visibleComentarios(r, list))
}

// visibleComentarios leaves out the comments about apps the person may
// not see.
func visibleComentarios(r *http.Request, list []store.Comentario) []store.Comentario {
	out := []store.Comentario{}
	for _, c := range list {
		if accessOf(r).Sees(c.App) {
			out = append(out, c)
		}
	}
	return out
}

// idParam reads an optional number from the query (0 when absent).
func idParam(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	return strconv.ParseInt(v, 10, 64)
}

// addComentario is a person's comment on an agent's cargo or on an
// encargo: it does not send anything back, and agents find it when they
// start.
func (s *Server) addComentario(w http.ResponseWriter, r *http.Request, login string) {
	var in struct {
		CargoID   int64  `json:"cargoId"`
		EncargoID int64  `json:"encargoId"`
		Body      string `json:"body"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Body = strings.TrimSpace(in.Body)
	switch {
	case (in.CargoID == 0) == (in.EncargoID == 0):
		httpError(w, http.StatusBadRequest, i18n.M("say which cargo or encargo the comment is about"))
		return
	case in.Body == "" || len([]rune(in.Body)) > 20000:
		httpError(w, http.StatusBadRequest, i18n.M("the comment must be 1 to 20,000 characters"))
		return
	case ruta.LooksSecret(in.Body):
		httpError(w, http.StatusBadRequest, i18n.M("this looks like it contains a secret; say where it lives instead of its value"))
		return
	}
	var c store.Comentario
	var err error
	if in.CargoID != 0 {
		c, err = s.Store.ComentarCargo(r.Context(), orgOf(r), in.CargoID, login, in.Body)
	} else if e, eerr := s.Store.Encargo(r.Context(), in.EncargoID); eerr == nil && !accessOf(r).Sees(e.App) {
		err = store.ErrNotFound // an encargo of an app whose list leaves the person out
	} else {
		c, err = s.Store.ComentarEncargo(r.Context(), orgOf(r), in.EncargoID, login, in.Body)
	}
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, i18n.M("no such cargo or encargo"))
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("comment", "id", c.ID, "cargo", in.CargoID, "encargo", in.EncargoID, "by", login)
	writeJSONStatus(w, http.StatusCreated, c)
}

// respuestasSinVer are agents' answers to the person's comments that they
// have not seen yet: the bell shows them.
func (s *Server) respuestasSinVer(w http.ResponseWriter, r *http.Request, login string) {
	list, err := s.Store.RespuestasSinVer(r.Context(), orgOf(r), login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, visibleComentarios(r, list))
}

// verRespuestas notes that the person saw these answers to their comments
// (the thread they are in was on their screen).
func (s *Server) verRespuestas(w http.ResponseWriter, r *http.Request, login string) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.IDs) > 0 {
		if err := s.Store.VerRespuestas(r.Context(), orgOf(r), login, in.IDs); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
