package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rendimiento-ai/rendimiento/internal/bitbucket"
	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/gitlab"
	"github.com/rendimiento-ai/rendimiento/internal/platform"
)

// Dormir and Despertar (Ruta 92): rendimiento on a computer sleeps and
// wakes from its top bar. Everyone sees whether it sleeps; only the
// platform's administrators put it to sleep, wake it or change its
// options, and only from a session (agent keys reach /mcp, never these).
// In a cluster there is no Sueno, and every route answers 409.

// conSueno answers 409 where rendimiento cannot sleep (a cluster); false
// means the answer was written.
func (s *Server) conSueno(w http.ResponseWriter) bool {
	if s.Sueno == nil {
		httpError(w, http.StatusConflict, platform.ErrSoloComputadora.Error())
		return false
	}
	return true
}

// getDormir is GET /api/dormir: where the sleep stands, with the publishes
// it would wait for and the webhooks waiting for the wake.
func (s *Server) getDormir(w http.ResponseWriter, r *http.Request, _ string) {
	if !s.conSueno(w) {
		return
	}
	st, err := s.Sueno.Estado(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, st)
}

// postDormir is POST /api/dormir {ya, apagar}: sleep once the publish going
// on ends (ya: now, cutting it), and with apagar stop rendimiento too.
func (s *Server) postDormir(w http.ResponseWriter, r *http.Request, login string) {
	if !s.conSueno(w) {
		return
	}
	var req struct {
		Ya     bool `json:"ya"`
		Apagar bool `json:"apagar"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	anotarDetalle(r, "ya", req.Ya)
	anotarDetalle(r, "apagar", req.Apagar)
	st, err := s.Sueno.Dormir(context.WithoutCancel(r.Context()), login, req.Ya, req.Apagar)
	s.respuestaSueno(w, st, err)
}

// postDespertar is POST /api/despertar: wake now.
func (s *Server) postDespertar(w http.ResponseWriter, r *http.Request, login string) {
	if !s.conSueno(w) {
		return
	}
	st, err := s.Sueno.Despertar(context.WithoutCancel(r.Context()), login)
	s.respuestaSueno(w, st, err)
}

// putDormirOpciones is PUT /api/dormir/opciones {despertarA, tapa}: the hour
// to wake at by itself ("" off) and «Dormir al cerrar la tapa».
func (s *Server) putDormirOpciones(w http.ResponseWriter, r *http.Request, _ string) {
	if !s.conSueno(w) {
		return
	}
	var req struct {
		DespertarA string `json:"despertarA"`
		Tapa       bool   `json:"tapa"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	anotarDetalle(r, "despertarA", req.DespertarA)
	st, err := s.Sueno.Opciones(r.Context(), req.DespertarA, req.Tapa)
	s.respuestaSueno(w, st, err)
}

func (s *Server) respuestaSueno(w http.ResponseWriter, st platform.EstadoSueno, err error) {
	switch {
	case errors.Is(err, platform.ErrSoloComputadora):
		httpError(w, http.StatusConflict, err.Error())
	case errors.Is(err, platform.ErrHoraInvalida):
		httpError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, st)
	}
}

// AtenderAvisos handles the webhooks kept while rendimiento slept, in the
// order they came, as if they arrived now (platform.Sueno.Avisos). Each is
// forgotten once handled; one that cannot be (its forja is gone, or a
// push could not be queued) is counted and forgotten too, so the wake
// never stops on it.
func (s *Server) AtenderAvisos(ctx context.Context) (hechos, fallidos int) {
	avisos, err := s.Store.AvisosGuardados(ctx)
	if err != nil {
		s.Log.Error("wake: could not read the kept webhooks", "err", err)
		return 0, 0
	}
	for _, a := range avisos {
		f := s.forjaLlamada(a.Forja)
		var ev forja.Event
		switch {
		case f == nil || f.Ready() != nil:
			s.Log.Warn("wake: a kept webhook's forja is not connected", "forja", a.Forja)
			fallidos++
		case json.Unmarshal(a.Evento, &ev) != nil:
			s.Log.Warn("wake: a kept webhook could not be read", "forja", a.Forja)
			fallidos++
		default:
			if _, _, err := s.atenderEvento(ctx, f, ev); err != nil {
				fallidos++
			} else {
				hechos++
			}
		}
		if err := s.Store.BorrarAviso(ctx, a.ID); err != nil {
			s.Log.Error("wake: could not forget a kept webhook", "err", err)
		}
	}
	return hechos, fallidos
}

// forjaLlamada is the forja by its name ("github", "gitlab", "bitbucket"),
// or nil when it is not here.
func (s *Server) forjaLlamada(name string) forja.Forja {
	switch name {
	case "github":
		if s.GitHub != nil {
			return s.GitHub
		}
	case gitlab.Name:
		if s.GitLab != nil {
			return s.GitLab
		}
	case bitbucket.Name:
		if s.Bitbucket != nil {
			return s.Bitbucket
		}
	}
	return nil
}
