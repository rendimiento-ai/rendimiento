package api

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/probados"
)

// AjusteValor is one variable of a setting tested before it changes: the
// value in use, the ConfigMap's, and who saved the one in use here, if
// someone did.
type AjusteValor struct {
	Key       string     `json:"key"`
	Value     string     `json:"value"`
	Env       string     `json:"env"`
	SavedBy   string     `json:"savedBy,omitempty"`
	SavedAt   *time.Time `json:"savedAt,omitempty"`
	Pendiente string     `json:"pendiente,omitempty"` // saved, but the platform has not restarted with it yet
}

// AjusteProbadoView is one setting tested before it changes, on the Keys
// and settings page.
type AjusteProbadoView struct {
	ID        string        `json:"id"`
	Valores   []AjusteValor `json:"valores"`
	Cambiable bool          `json:"cambiable"`
	Reason    string        `json:"reason,omitempty"`
	// Guardado: a value saved here wins over the ConfigMap's (there is a
	// way back).
	Guardado bool `json:"guardado,omitempty"`
}

// ajustesProbadosView are the settings tested before they change.
func (s *Server) ajustesProbadosView(r *http.Request) []AjusteProbadoView {
	saved := map[string]struct {
		v, by string
		at    time.Time
	}{}
	if rows, err := s.Store.GetAjustes(r.Context()); err == nil {
		for k, a := range rows {
			saved[k] = struct {
				v, by string
				at    time.Time
			}{a.Value, a.UpdatedBy, a.UpdatedAt}
		}
	}
	var out []AjusteProbadoView
	for _, a := range probados.Ajustes {
		v := AjusteProbadoView{ID: a.ID, Valores: []AjusteValor{}}
		for _, k := range a.Keys {
			val := os.Getenv(k)
			if k == probados.BaseURL && s.BaseURL != "" {
				val = s.BaseURL
			}
			av := AjusteValor{Key: k, Value: val, Env: val}
			if s.Probados != nil {
				av.Env = s.Probados.Entorno[k]
			}
			if x, ok := saved[k]; ok {
				v.Guardado = true
				at := x.at
				av.SavedBy, av.SavedAt = x.by, &at
				if x.v != val {
					av.Pendiente = x.v
				}
			}
			v.Valores = append(v.Valores, av)
		}
		switch {
		case s.Kube == nil:
			v.Reason = i18n.M("on a computer these settings are in its settings file")
		case s.Probados == nil:
			v.Reason = i18n.M("these settings do not change in this process")
		}
		v.Cambiable = v.Reason == ""
		out = append(out, v)
	}
	return out
}

// probadosListo answers why the settings cannot change in this process,
// or false when they can.
func (s *Server) probadosListo(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("ajuste")
	if _, ok := probados.Buscar(id); !ok {
		httpError(w, http.StatusNotFound, i18n.M("no such setting changes here"))
		return "", false
	}
	if s.Kube == nil || s.Probados == nil {
		httpError(w, http.StatusConflict, i18n.M("these settings change here only on a cluster; on a computer they are in its settings file"))
		return "", false
	}
	return id, true
}

// probarAjuste tests new values of a setting, against the cluster and the
// services themselves. Nothing changes: a test that passed hands back a
// ficha, for cambiarAjuste.
func (s *Server) probarAjuste(w http.ResponseWriter, r *http.Request, login string) {
	id, ok := s.probadosListo(w, r)
	if !ok {
		return
	}
	var in struct {
		Values map[string]string `json:"values"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	anotarDetalle(r, "ajuste", id)
	res, err := s.Probados.Probar(r.Context(), id, in.Values)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anotarDetalle(r, "pasa", res.Pasa)
	for _, p := range res.Pasos {
		if !p.OK {
			anotarDetalle(r, "fallo", p.ID)
		}
	}
	writeJSON(w, res)
}

// cambiarAjuste saves the values a test passed with and restarts the
// platform so it takes them.
func (s *Server) cambiarAjuste(w http.ResponseWriter, r *http.Request, login string) {
	id, ok := s.probadosListo(w, r)
	if !ok {
		return
	}
	var in struct {
		Values map[string]string `json:"values"`
		Ficha  string            `json:"ficha"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	anotarDetalle(r, "ajuste", id)
	res, err := s.Probados.Cambiar(r.Context(), id, in.Values, strings.TrimSpace(in.Ficha), login)
	if errors.Is(err, probados.ErrSinPrueba) {
		httpError(w, http.StatusConflict, i18n.M("test it first: a change is saved only with a test that passed, for the same values, in the last 15 minutes"))
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for k, v := range res.Valores {
		anotarDetalle(r, k, v)
	}
	anotarDetalle(r, "guardado", res.Guardado)
	writeJSON(w, res)
}

// volverAjuste forgets the value saved here: the ConfigMap's holds again,
// after a restart.
func (s *Server) volverAjuste(w http.ResponseWriter, r *http.Request, login string) {
	id, ok := s.probadosListo(w, r)
	if !ok {
		return
	}
	anotarDetalle(r, "ajuste", id)
	res, err := s.Probados.Volver(r.Context(), id, login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, res)
}

// identidad answers who this platform is: the nonce signed with the key
// only its processes have. A new address must answer it before it is
// saved, so a typo cannot lock people out.
func (s *Server) identidad(w http.ResponseWriter, r *http.Request) {
	n := r.URL.Query().Get("n")
	if len(n) < 16 || len(n) > 128 || len(s.Identidad) == 0 {
		httpError(w, http.StatusBadRequest, "a nonce of 16 to 128 characters")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]string{"firma": probados.Firmar(s.Identidad, n)})
}
