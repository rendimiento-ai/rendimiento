package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ---- Poner al día (decision 191) ----
//
// Whoever can see an app reads the reports on its dependency updates (its
// Caminos tab); a person who may merge puts it up to date with one, which
// opens the encargo for the agent the organization chose. The choices are
// on the AI page: anyone reads them, an administrator changes them.

// AlDiaView is what an app's Caminos tab shows: its updates' reports, and
// who puts it up to date.
type AlDiaView struct {
	Informes []store.AlDia      `json:"informes"`
	Ajustes  store.AlDiaAjustes `json:"ajustes"`
	// CoderOff: the platform's coding agent is off, so an encargo for it
	// waits until it is turned on (AI page).
	CoderOff bool `json:"coderOff,omitempty"`
}

// alDiaRoutes adds the routes of the reports and their choices.
func (s *Server) alDiaRoutes(auth func(role, pattern string, h func(http.ResponseWriter, *http.Request, string))) {
	auth(store.MemberGuest, "GET /api/apps/{app}/al-dia", s.appAlDia)
	auth(store.MemberCollaborator, "POST /api/apps/{app}/al-dia/{id}/poner", s.ponerAlDia)
	auth(store.MemberGuest, "GET /api/ia/al-dia", s.getAlDiaAjustes)
	auth(store.MemberAdmin, "PUT /api/ia/al-dia", s.putAlDiaAjustes)
}

func (s *Server) appAlDia(w http.ResponseWriter, r *http.Request, _ string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	informes, err := s.Store.AlDiaDeApp(r.Context(), a.ID, 20)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v := AlDiaView{Informes: informes}
	if v.Ajustes, err = s.Store.AlDiaAjustesDe(r.Context(), a.OrgID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if set, err := s.Store.AISettingsOf(r.Context(), a.OrgID); err == nil {
		v.CoderOff = set.CodeAgent == store.CodeAgentOff
	}
	writeJSON(w, v)
}

func (s *Server) ponerAlDia(w http.ResponseWriter, r *http.Request, login string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpError(w, http.StatusBadRequest, i18n.M("bad report number"))
		return
	}
	rep, err := s.Store.AlDiaPorID(r.Context(), id)
	if err != nil || rep.AppID != a.ID {
		httpError(w, http.StatusNotFound, i18n.M("this app has no such report"))
		return
	}
	enc, err := agentes.PonerAlDia(r.Context(), s.Store, a, rep, login, idiomaDe(r))
	switch {
	case errors.Is(err, store.ErrEncargoState):
		httpError(w, http.StatusConflict, i18n.M("this report is already being put to work, or has nothing to change"))
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anotarDetalle(r, "encargo", enc.ID)
	writeJSON(w, map[string]any{"encargo": enc.ID})
}

func (s *Server) getAlDiaAjustes(w http.ResponseWriter, r *http.Request, _ string) {
	set, err := s.Store.AlDiaAjustesDe(r.Context(), orgOf(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, set)
}

// alDiaTopeMax is the most a report's work may cost to start without asking.
const alDiaTopeMax = 5_000_000

func (s *Server) putAlDiaAjustes(w http.ResponseWriter, r *http.Request, login string) {
	var in store.AlDiaAjustes
	if !readJSON(w, r, &in) {
		return
	}
	switch {
	case in.Quien != store.AlDiaQuienPlataforma && in.Quien != store.AlDiaQuienPersona:
		httpError(w, http.StatusBadRequest, i18n.M("who puts an app up to date must be platform or local"))
		return
	case in.Cuando != store.AlDiaCuandoToda && in.Cuando != store.AlDiaCuandoFalla:
		httpError(w, http.StatusBadRequest, i18n.M("when a report is made must be toda or falla"))
		return
	case in.TopeMicros < 0 || in.TopeMicros > alDiaTopeMax:
		httpError(w, http.StatusBadRequest, i18n.M("the amount to start without asking must be between $0 and $5"))
		return
	}
	if err := s.Store.GuardarAlDiaAjustes(r.Context(), orgOf(r), in, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anotarDetalle(r, "quien", in.Quien)
	s.getAlDiaAjustes(w, r, login)
}
