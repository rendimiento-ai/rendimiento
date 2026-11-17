package api

import (
	"net/http"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// RunNow is a run as the Apps page's "Running now" and the top bar's search
// show it: the app's name, and the run (with its steps while it goes on).
type RunNow struct {
	App string `json:"app"`
	*store.Run
}

// Corriendo is what goes on in the person's organization: the runs queued
// or running, with their steps, and the last ones that ended.
type Corriendo struct {
	Ahora     []RunNow `json:"ahora"`
	Recientes []RunNow `json:"recientes"`
}

// corriendo answers what goes on in the apps whose list lets the person in:
// one small read the Apps page repeats while it is open, and the search in
// the top bar reads for its runs.
func (s *Server) corriendo(w http.ResponseWriter, r *http.Request, _ string) {
	apps, err := s.Store.ListAppsInOrg(r.Context(), orgOf(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	names := map[int64]string{}
	ids := []int64{}
	for _, a := range apps {
		if accessOf(r).Role(a) == "" {
			continue // its list leaves the person out
		}
		names[a.ID] = a.Name
		ids = append(ids, a.ID)
	}
	ahora, err := s.Store.RunsGoingOn(r.Context(), ids, 12)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hechas, err := s.Store.RunsDone(r.Context(), ids, 30)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	vista := func(runs []*store.Run) []RunNow {
		out := make([]RunNow, 0, len(runs))
		for _, run := range runs {
			run.Message = "" // the commit's message stays on the run's page
			out = append(out, RunNow{App: names[run.AppID], Run: run})
		}
		return out
	}
	writeJSON(w, Corriendo{Ahora: vista(ahora), Recientes: vista(hechas)})
}
