package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"

	"github.com/rendimiento-ai/rendimiento/internal/senales"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// instalacionID is a señal's identity: 16 random bytes in hex.
var instalacionID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// recibirSenal keeps a señal de vida from an installation (POST
// /api/senales, no session: installations have none here). Only where
// SenalesRecibir is on (rendimiento.ai). The sender's address is never
// read nor kept; a señal is at most 4 KB of the known fields.
func (s *Server) recibirSenal(w http.ResponseWriter, r *http.Request) {
	if !s.SenalesRecibir {
		http.NotFound(w, r)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		httpError(w, http.StatusBadRequest, "unreadable")
		return
	}
	var sen senales.Senal
	if err := json.Unmarshal(raw, &sen); err != nil || !instalacionID.MatchString(sen.Instalacion) {
		httpError(w, http.StatusBadRequest, "not a señal")
		return
	}
	// Kept as the known fields only, whatever else came.
	limpio, _ := json.Marshal(sen)
	if err := s.Store.RecibirSenal(r.Context(), sen.Instalacion, sen.Equipo, limpio); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SenalesView is what the operators see of this installation's señales:
// whether they go (and why not), when the last went, whether they saw the
// notice, and the next one exactly as it would go.
type SenalesView struct {
	Encendidas  bool                  `json:"encendidas"`
	Porque      string                `json:"porque,omitempty"`
	AvisoVisto  bool                  `json:"avisoVisto"`
	UltimoEnvio *string               `json:"ultimoEnvio,omitempty"`
	Proxima     senales.Senal         `json:"proxima"`
	// Enviadas are the last señales sent from here, as they went.
	Enviadas []store.SenalEnviada `json:"enviadas"`
	Resumen     *store.ResumenSenales `json:"resumen,omitempty"` // where señales are received
}

// verSenales is the operators' view of the señales (and, where they are
// received, what they say).
func (s *Server) verSenales(w http.ResponseWriter, r *http.Request, _ string) {
	if s.Senales == nil {
		httpError(w, http.StatusServiceUnavailable, "señales are not set up here")
		return
	}
	ctx := r.Context()
	on, porque, err := s.Senales.Encendidas(ctx)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a, err := s.Store.GetSenales(ctx)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	prox, err := s.Senales.Proxima(ctx)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v := SenalesView{Encendidas: on, Porque: porque, AvisoVisto: a.AvisoVisto, Proxima: prox, Enviadas: a.Enviadas}
	if v.Enviadas == nil {
		v.Enviadas = []store.SenalEnviada{}
	}
	if a.UltimoEnvio != nil {
		t := a.UltimoEnvio.UTC().Format("2006-01-02T15:04:05Z")
		v.UltimoEnvio = &t
	}
	if s.SenalesRecibir {
		res, err := s.Store.Resumen(ctx)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		v.Resumen = &res
	}
	writeJSON(w, v)
}

// putSenales turns the señales on or off, or records that the operators
// saw the notice ({"encendidas": false} or {"avisoVisto": true}).
func (s *Server) putSenales(w http.ResponseWriter, r *http.Request, login string) {
	var req struct {
		Encendidas *bool `json:"encendidas"`
		AvisoVisto *bool `json:"avisoVisto"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	a, err := s.Store.GetSenales(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Encendidas != nil {
		a.Apagadas = !*req.Encendidas
		anotarDetalle(r, "encendidas", *req.Encendidas)
	}
	if req.AvisoVisto != nil {
		a.AvisoVisto = *req.AvisoVisto
	}
	if err := s.Store.SaveSenales(r.Context(), a, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.verSenales(w, r, login)
}

// listarInstalaciones are the installations whose señales came here (where
// SenalesRecibir is on), the most recent first.
func (s *Server) listarInstalaciones(w http.ResponseWriter, r *http.Request, _ string) {
	if !s.SenalesRecibir {
		httpError(w, http.StatusNotFound, "this platform does not receive señales")
		return
	}
	rows, err := s.Store.ListarInstalaciones(r.Context(), 500)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, rows)
}

// marcarEquipo marks an installation as the team's own, or not
// ({"equipo": true}): counted apart, never in the numbers given out.
func (s *Server) marcarEquipo(w http.ResponseWriter, r *http.Request, _ string) {
	if !s.SenalesRecibir {
		httpError(w, http.StatusNotFound, "this platform does not receive señales")
		return
	}
	var req struct {
		Equipo bool `json:"equipo"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	switch err := s.Store.MarcarEquipo(r.Context(), id, req.Equipo); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "no señal came from that installation")
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		anotarDetalle(r, "equipo", req.Equipo)
		w.WriteHeader(http.StatusNoContent)
	}
}
