package api

import (
	"net/http"
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
	out := []MesaCount{}
	for _, a := range apps {
		if accessOf(r).Role(a) == "" {
			continue
		}
		b, err := s.Platform.Mesa(r.Context(), a)
		if err != nil {
			s.Log.Warn("work board", "app", a.Name, "err", err) // the others still count
			continue
		}
		if n := b.You(); n > 0 {
			out = append(out, MesaCount{App: a.Name, You: n})
		}
	}
	writeJSON(w, out)
}
