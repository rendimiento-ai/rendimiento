package api

import (
	"net/http"

	"github.com/rendimiento-ai/rendimiento/internal/actualizar"
)

// Published is what the update card shows to the platform's administrators:
// the version running here, the latest published one, whether it is newer,
// whether it only brings fixes, and how this install takes it.
type Published struct {
	Version string `json:"version"`
	// How is how this install is updated: computer (rendimiento changes its
	// own program), self (a cluster that builds rendimiento: Update), or
	// helm (a cluster installed with the chart: the card shows the command).
	How     string                `json:"how"`
	Latest  *actualizar.Publicada `json:"latest,omitempty"`
	Newer   bool                  `json:"newer"`
	OnlyFix bool                  `json:"onlyFix"`
	Error   string                `json:"error,omitempty"`
}

// platformPublished is the update card: only for the platform's own
// administrators (decision 138), since a version changes every
// organization's rendimiento.
func (s *Server) platformPublished(w http.ResponseWriter, r *http.Request, _ string) {
	out := Published{Version: s.Version, How: "helm"}
	switch {
	case s.Modo == "local":
		out.How = "computer"
	case s.Update != nil:
		out.How = "self"
	}
	if s.Publicadas == nil {
		out.Error = "this rendimiento does not ask which version is published"
		writeJSON(w, out)
		return
	}
	latest, err := s.Publicadas.Ultima(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		out.Error = err.Error()
		writeJSON(w, out)
		return
	}
	out.Latest = latest
	out.Newer, out.OnlyFix = actualizar.Comparar(s.Version, latest.Version)
	writeJSON(w, out)
}
