package api

import (
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/problems"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ProblemList is the Problems page: what went wrong in the platform in the
// last 30 days, newest first, and how many problems are open.
type ProblemList struct {
	Open     int             `json:"open"`
	OpenFor  int             `json:"openForHours"` // a problem is open this long after it last happened
	Problems []store.Problem `json:"problems"`
}

// listProblems is the Problems page: warnings and errors the platform
// logged recently (an app's, or all), repeats grouped.
func (s *Server) listProblems(w http.ResponseWriter, r *http.Request, login string) {
	q := r.URL.Query()
	hidden, err := s.platformHidden(r.Context(), login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	all, err := s.Store.ListProblems(r.Context(), q.Get("app"), time.Now().Add(-problems.Keep), q.Get("dismissed") == "1", 300)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	list := []store.Problem{}
	for _, p := range all {
		if !hidden[p.App] { // an app whose list leaves the person out
			list = append(list, p)
		}
	}
	open, err := s.Store.OpenProblems(r.Context(), slices.Collect(maps.Keys(hidden))...)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, ProblemList{Open: open, OpenFor: int(store.ProblemOpenFor.Hours()), Problems: list})
}

// problemCount is the navigation badge's number.
func (s *Server) problemCount(w http.ResponseWriter, r *http.Request, login string) {
	hidden, err := s.platformHidden(r.Context(), login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	open, err := s.Store.OpenProblems(r.Context(), slices.Collect(maps.Keys(hidden))...)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]int{"open": open})
}

// dismissProblem hides a problem until it happens again.
func (s *Server) dismissProblem(w http.ResponseWriter, r *http.Request, login string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad problem id")
		return
	}
	hidden, err := s.platformHidden(r.Context(), login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if app, err := s.Store.ProblemApp(r.Context(), id); err == nil && hidden[app] {
		httpError(w, http.StatusNotFound, "no such problem")
		return
	}
	if err := s.Store.DismissProblem(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such problem")
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("problem dismissed", "id", id, "by", login)
	w.WriteHeader(http.StatusNoContent)
}
