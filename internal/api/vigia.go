package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The lookout (el vigía) on the Log page, for the platform's
// administrators: its latest review and findings, a review now, its
// settings, and a person's decision on each finding. Nothing it found
// becomes an encargo or enters the Ruta without that decision.

// VigiaVista is what the Log page shows of the lookout.
type VigiaVista struct {
	Revision *store.VigiaRevision `json:"revision,omitempty"` // nil before its first review
	Ajuste   store.VigiaAjuste    `json:"ajuste"`
	// GastadoMicros is what it spent this month, of Ajuste.TopeMicros.
	GastadoMicros int64 `json:"gastadoMicros"`
	// Disponible: this platform runs the lookout (ROLE=agents, or a
	// computer's single process).
	Disponible bool `json:"disponible"`
}

func (s *Server) vigiaVista(r *http.Request) (VigiaVista, error) {
	ctx := r.Context()
	v := VigiaVista{Disponible: s.Vigia != nil}
	var err error
	if v.Ajuste, err = s.Store.GetVigia(ctx); err != nil {
		return v, err
	}
	if v.GastadoMicros, err = s.Store.AISpentBy(ctx, store.DefaultOrg, agentes.VigiaSource, time.Now()); err != nil {
		return v, err
	}
	rev, err := s.Store.UltimaRevision(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	rev.Nota = i18n.Translate(i18n.FromRequest(r), rev.Nota)
	v.Revision = &rev
	return v, nil
}

// getVigia is the lookout's latest review, its findings and its settings.
func (s *Server) getVigia(w http.ResponseWriter, r *http.Request, _ string) {
	v, err := s.vigiaVista(r)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, v)
}

// vigiaNuevos is how many findings of the latest review wait for a
// decision (the bell).
func (s *Server) vigiaNuevos(w http.ResponseWriter, r *http.Request, _ string) {
	n, err := s.Store.VigiaNuevos(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]int{"nuevos": n})
}

// revisarVigia asks the lookout for a review now, in the background.
func (s *Server) revisarVigia(w http.ResponseWriter, _ *http.Request, login string) {
	if s.Vigia == nil {
		httpError(w, http.StatusServiceUnavailable, i18n.M("the lookout does not run on this platform"))
		return
	}
	id, err := s.Vigia.Pedir(login)
	if errors.Is(err, store.ErrVigiaOcupado) {
		httpError(w, http.StatusConflict, i18n.M("the lookout is reviewing already; its findings come in a moment"))
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]int64{"id": id})
}

// putVigia turns the weekly review on or off and sets its monthly cap.
func (s *Server) putVigia(w http.ResponseWriter, r *http.Request, login string) {
	var in struct {
		Encendido   bool    `json:"encendido"`
		TopeDolares float64 `json:"topeDolares"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if math.IsNaN(in.TopeDolares) || in.TopeDolares < 0 || in.TopeDolares > 100 {
		httpError(w, http.StatusBadRequest, i18n.M("the lookout's cap is 0 to 100 dollars a month"))
		return
	}
	a := store.VigiaAjuste{Encendido: in.Encendido, TopeMicros: int64(math.Round(in.TopeDolares * 1e6))}
	if err := s.Store.SetVigia(r.Context(), a, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v, err := s.vigiaVista(r)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, v)
}

// decidirHallazgo makes a person's decision on a finding: estado is
// encargo, ruta or descartado. A finding is decided once.
func (s *Server) decidirHallazgo(estado string) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, login string) {
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, i18n.M("not a finding of the lookout"))
			return
		}
		h, err := s.Store.Hallazgo(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, i18n.M("not a finding of the lookout"))
			return
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if estado == store.HallazgoEncargo && !h.Accionable {
			httpError(w, http.StatusBadRequest, i18n.M("this finding suggests no work: note it in the Ruta or set it aside"))
			return
		}
		h, err = s.Store.DecidirHallazgo(ctx, id, estado, login)
		if errors.Is(err, store.ErrVigiaDecidido) {
			httpError(w, http.StatusConflict, i18n.M("someone decided this finding already"))
			return
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		deshacer := func(err error) {
			_ = s.Store.DeshacerHallazgo(r.Context(), id)
			httpError(w, http.StatusInternalServerError, err.Error())
		}
		switch estado {
		case store.HallazgoEncargo:
			e, err := s.Store.CreateEncargo(ctx, store.Encargo{Title: h.Titulo, Body: s.cuerpoHallazgo(h, login, true),
				App: s.appDelHallazgo(r, h), CreatedBy: login, OrgID: store.DefaultOrg})
			if err != nil {
				deshacer(err)
				return
			}
			s.Log.Info("encargo created", "id", e.ID, "app", e.App, "by", login, "from", "lookout")
			h.EncargoID = &e.ID
		case store.HallazgoRuta:
			kind, tags := store.KindNota, []string{"vigia"}
			if h.Accionable {
				kind = store.KindPendiente
			}
			if h.App != "" {
				tags = append(tags, strings.ToLower(h.App))
			}
			e, err := s.Store.AddRuta(ctx, store.RutaEntry{Kind: kind, Title: h.Titulo, Body: s.cuerpoHallazgo(h, login, false),
				Tags: tags, Author: login, OrgID: store.DefaultOrg})
			if err != nil {
				deshacer(err)
				return
			}
			h.RutaID = &e.ID
		}
		if h.EncargoID != nil || h.RutaID != nil {
			if err := s.Store.EnlazarHallazgo(ctx, id, h.EncargoID, h.RutaID); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, h)
	}
}

// appDelHallazgo is the finding's app when it is an app of the platform's
// organization the person can act on ("" otherwise: the body names it).
func (s *Server) appDelHallazgo(r *http.Request, h store.VigiaHallazgo) string {
	if h.App == "" {
		return ""
	}
	a, err := s.Store.GetApp(r.Context(), h.App)
	if err != nil || a.OrgID != store.DefaultOrg || store.RoleRank(accessOf(r).Role(a)) < store.RoleRank(store.MemberCollaborator) {
		return ""
	}
	return a.Name
}

// cuerpoHallazgo is the body of the encargo or the Ruta entry a finding
// becomes: its evidence, where it came from and who accepted it, in the
// language agents write in.
func (s *Server) cuerpoHallazgo(h store.VigiaHallazgo, login string, encargo bool) string {
	es := s.AgentLang == "es"
	say := func(spanish, english string, args ...any) string {
		if es {
			return fmt.Sprintf(spanish, args...)
		}
		return fmt.Sprintf(english, args...)
	}
	lines := []string{h.Evidencia, ""}
	if h.App != "" {
		lines = append(lines, say("Aplicación: %s.", "App: %s.", h.App))
	}
	lines = append(lines, say("Lo vio el vigía (hallazgo #%d, clave `%s`) y %s lo aceptó.", "The lookout saw it (finding #%d, key `%s`) and %s accepted it.", h.ID, h.Clave, login))
	if encargo {
		lines = append(lines, say("Compruebe el dato antes de cambiar nada (ejecucion_leer, paso_registro, la página Registro), hágalo en una rama, pruébelo ahí y entréguelo con el enlace.",
			"Check the fact before changing anything (ejecucion_leer, paso_registro, the Log page), do it on a branch, test it there and hand it over with the link."))
	}
	return strings.Join(lines, "\n")
}
