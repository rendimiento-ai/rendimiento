package api

import (
	"fmt"
	"net/http"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The home page (the Apps page) is laid out by each person: which sections
// it shows and in what order (web/src/pages/Dashboard.tsx). It is kept by
// login, so the phone shows the same; who never chose sees the default an
// administrator of the platform set, or the built-in one.

// seccionesInicio are the home page's sections, in their built-in order
// and with whether they show at first. The interface knows the same list
// (web/src/inicio.ts). Visits and delivery live on the Numbers page; the
// scoreboard is folded into one line of the greeting.
var seccionesInicio = []store.SeccionInicio{
	{ID: "corriendo", On: true}, {ID: "esperan", On: true}, {ID: "ayuda", On: true}, {ID: "apps", On: true},
	{ID: "marcador", On: false}, {ID: "visitas", On: false}, {ID: "entregas", On: false}, {ID: "lluvia", On: true},
}

// InicioView is the signed-in person's home page: its sections in order,
// whether they chose it (Propias) or it is the default, the default itself,
// and whether they may set the default for everyone.
type InicioView struct {
	Secciones         []store.SeccionInicio `json:"secciones"`
	Propias           bool                  `json:"propias"`
	Predeterminado    []store.SeccionInicio `json:"predeterminado"`
	PredeterminadoPor string                `json:"predeterminadoPor,omitempty"`
	PuedeFijar        bool                  `json:"puedeFijar"`
}

// completar keeps the known sections of in, once each and in its order,
// and adds the ones it lacks as base has them: a section added in a later
// version shows up where it was never chosen.
func completar(in, base []store.SeccionInicio) []store.SeccionInicio {
	known := map[string]bool{}
	for _, s := range seccionesInicio {
		known[s.ID] = true
	}
	seen := map[string]bool{}
	out := []store.SeccionInicio{}
	for _, s := range in {
		if known[s.ID] && !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	for _, s := range base {
		if !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	return out
}

// validas says whether a list sent is made of known sections, each once.
func validas(in []store.SeccionInicio) (string, bool) {
	known := map[string]bool{}
	for _, s := range seccionesInicio {
		known[s.ID] = true
	}
	seen := map[string]bool{}
	for _, s := range in {
		if !known[s.ID] || seen[s.ID] {
			return s.ID, false
		}
		seen[s.ID] = true
	}
	return "", true
}

// verInicio is the person's home page (GET /api/inicio, any role).
func (s *Server) verInicio(w http.ResponseWriter, r *http.Request, login string) {
	ctx := r.Context()
	def, err := s.Store.GetInicioPredeterminado(ctx)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := InicioView{Predeterminado: completar(nil, seccionesInicio)}
	if def != nil {
		out.Predeterminado, out.PredeterminadoPor = completar(def.Secciones, seccionesInicio), def.UpdatedBy
	}
	own, err := s.Store.GetInicio(ctx, login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Secciones, out.Propias = out.Predeterminado, false
	if own != nil {
		out.Secciones, out.Propias = completar(own, out.Predeterminado), true
	}
	if role, err := s.Store.MemberRole(ctx, store.DefaultOrg, login); err == nil && !store.IsDemoLogin(login) {
		out.PuedeFijar = role == store.MemberAdmin
	}
	writeJSON(w, out)
}

// leerSecciones reads {"secciones": [...]} and checks it; false when it answered already.
func leerSecciones(w http.ResponseWriter, r *http.Request) ([]store.SeccionInicio, bool) {
	var req struct {
		Secciones []store.SeccionInicio `json:"secciones"`
	}
	if !readJSON(w, r, &req) {
		return nil, false
	}
	if len(req.Secciones) == 0 {
		httpError(w, http.StatusBadRequest, "say which sections the home page shows: {\"secciones\": [{\"id\": \"apps\", \"on\": true}]}")
		return nil, false
	}
	if id, ok := validas(req.Secciones); !ok {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("not a section of the home page, or twice: %s", id))
		return nil, false
	}
	return req.Secciones, true
}

// putInicio keeps the person's own home page (PUT /api/inicio, any role:
// a guest lays out their own view too). Not in a read-only demo.
func (s *Server) putInicio(w http.ResponseWriter, r *http.Request, login string) {
	if store.IsDemoLogin(login) {
		httpError(w, http.StatusForbidden, "a demo shows the usual home page; it cannot be changed")
		return
	}
	secs, ok := leerSecciones(w, r)
	if !ok {
		return
	}
	if err := s.Store.SaveInicio(r.Context(), login, secs); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.verInicio(w, r, login)
}

// borrarInicio is "Back to the usual": the person's own layout is
// forgotten, and the default shows again (DELETE /api/inicio).
func (s *Server) borrarInicio(w http.ResponseWriter, r *http.Request, login string) {
	if store.IsDemoLogin(login) {
		httpError(w, http.StatusForbidden, "a demo shows the usual home page; it cannot be changed")
		return
	}
	if err := s.Store.DeleteInicio(r.Context(), login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.verInicio(w, r, login)
}

// putInicioPredeterminado sets the home page of everyone who never chose
// theirs (PUT /api/inicio/predeterminado, the platform's administrators).
func (s *Server) putInicioPredeterminado(w http.ResponseWriter, r *http.Request, login string) {
	secs, ok := leerSecciones(w, r)
	if !ok {
		return
	}
	if err := s.Store.SaveInicioPredeterminado(r.Context(), secs, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.verInicio(w, r, login)
}

// borrarInicioPredeterminado goes back to the built-in default
// (DELETE /api/inicio/predeterminado, the platform's administrators).
func (s *Server) borrarInicioPredeterminado(w http.ResponseWriter, r *http.Request, login string) {
	if err := s.Store.DeleteInicioPredeterminado(r.Context()); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.verInicio(w, r, login)
}
