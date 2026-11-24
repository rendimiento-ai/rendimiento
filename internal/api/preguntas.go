package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The coding agent's questions (Ruta 178): a person answers one, and the
// agent goes on with the answer, or passes the encargo to their own agent.
// Both take a person's session: an agent's key never answers for them.

// answerEncargo is a person's answer to what the coding agent asks about
// an encargo: it goes back to the agent's queue, to go on on its branch.
func (s *Server) answerEncargo(w http.ResponseWriter, r *http.Request, login string) {
	id, ok := s.encargoID(w, r)
	if !ok {
		return
	}
	var in struct {
		Answer string `json:"answer"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Answer = strings.TrimSpace(in.Answer)
	switch {
	case in.Answer == "":
		httpError(w, http.StatusBadRequest, "write an answer, or choose one of the options")
		return
	case len([]rune(in.Answer)) > 4000:
		httpError(w, http.StatusBadRequest, "the answer is too long: up to 4000 characters")
		return
	}
	e, err := s.Store.AnswerEncargo(r.Context(), id, orgOf(r), login, in.Answer)
	if errors.Is(err, store.ErrEncargoState) {
		httpError(w, http.StatusConflict, "the coding agent asks nothing about this encargo now")
		return
	}
	s.encargoAnswer(w, e, err)
}

// passEncargo gives an encargo the coding agent asks about to the person's
// own agent, with the question in its history.
func (s *Server) passEncargo(w http.ResponseWriter, r *http.Request, login string) {
	id, ok := s.encargoID(w, r)
	if !ok {
		return
	}
	q, err := s.Store.OpenQuestion(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusConflict, "the coding agent asks nothing about this encargo now")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	e, err := s.Store.PassAskedEncargo(r.Context(), id, orgOf(r), login, agentes.PassNote(s.AgentLang, q, false))
	if errors.Is(err, store.ErrEncargoState) {
		httpError(w, http.StatusConflict, "the coding agent asks nothing about this encargo now")
		return
	}
	s.encargoAnswer(w, e, err)
}
