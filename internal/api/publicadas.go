package api

import (
	"net/http"
	"strings"
	"time"

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
	// CanInstall: this computer can install Latest itself (it is newer and
	// signed). Previous is the version kept to go back to ("" when none),
	// and LastChange what the last update did.
	CanInstall bool              `json:"canInstall"`
	Previous   string            `json:"previous,omitempty"`
	LastChange *actualizar.Marca `json:"lastChange,omitempty"`
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
	if s.Programa != nil {
		out.CanInstall = out.Newer && latest.Firmada
		out.Previous, out.LastChange = s.Programa.Anterior(), s.UltimoCambio
	}
	writeJSON(w, out)
}

// platformInstall installs the latest published version on this computer
// and restarts rendimiento in it: only that version, newer and signed.
func (s *Server) platformInstall(w http.ResponseWriter, r *http.Request, login string) {
	if s.Programa == nil || s.Publicadas == nil {
		httpError(w, http.StatusNotFound, "rendimiento updates its own program only on a computer")
		return
	}
	var in struct {
		Version string `json:"version"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	latest, err := s.Publicadas.Ultima(r.Context(), false)
	if err != nil || latest.Version != strings.TrimPrefix(in.Version, "v") {
		httpError(w, http.StatusConflict, "only the latest published version is installed")
		return
	}
	if err := s.Programa.Instalar(r.Context(), s.Version, latest.Version, login); err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	s.Log.Info("rendimiento updated: restarting in the new version", "from", s.Version, "to", latest.Version, "by", login)
	s.restartSoon(w)
}

// platformBack puts the previous version back, only a lower one.
func (s *Server) platformBack(w http.ResponseWriter, r *http.Request, login string) {
	if s.Programa == nil {
		httpError(w, http.StatusNotFound, "rendimiento changes its own program only on a computer")
		return
	}
	if err := s.Programa.Regresar(s.Version, login); err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	s.Log.Info("rendimiento went back to its previous version: restarting", "from", s.Version, "by", login)
	s.restartSoon(w)
}

// restartSoon answers, then ends the process a moment later so the service
// starts the program now in place.
func (s *Server) restartSoon(w http.ResponseWriter) {
	w.WriteHeader(http.StatusAccepted)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		if s.Programa.Salir != nil {
			s.Programa.Salir(actualizar.CodigoReinicio)
		}
	}()
}
