package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// How rendimiento's own interface is used (POST /api/uso, the Use page).
// Only totals: the page's route pattern, the one it came from, the main
// actions by a fixed name and how long pages took to show their data.
// Never who, never an address with a name or number in it, never text.

// rutasUso are the interface's route patterns: the only pages counted. They
// are the routes of web/src/main.tsx, and web/src/uso.ts keeps the same list
// (TestUsoListsMatchTheUI checks the three agree).
var rutasUso = map[string]bool{
	"/": true, "/new": true, "/mercado": true, "/mercado/nuevo": true, "/mercado/:app": true,
	"/environment": true, "/universe": true, "/universe/requests/:id": true, "/services": true,
	"/addons": true, "/problems": true, "/ruta": true, "/bitacora": true, "/senales": true,
	"/ia": true, "/buzon": true, "/escribir": true, "/members": true, "/addons/:name": true,
	"/services/:ns/:name": true, "/apps/:name": true, "/apps/:name/:tab": true,
	"/apps/:name/runs/:id": true, "/uso": true,
	rutaDesconocida: true,
}

// rutaDesconocida is a page the list does not know (an old link, a typo).
const rutaDesconocida = "(unknown)"

// accionesUso is the closed list of actions noted, by their short name
// (web/src/uso.ts has the same list, to extend both at once).
var accionesUso = map[string]bool{
	"build_now": true, "retry": true, "cancel_run": true, "merge": true, "dismiss_problem": true,
	"ask_agent": true, "select_step": true, "switch_theme": true, "switch_language": true, "rollback": true,
}

// Limits of one request: at most this many events, of this many bytes.
const (
	usoMaxEventos = 100
	usoMaxBytes   = 32 << 10
)

// eventoUso is one event as the interface sends it: a page shown ("v", with
// where from and, when measured, its time to data) or an action ("a").
type eventoUso struct {
	T      string `json:"t"`
	Ruta   string `json:"r"`
	Desde  string `json:"d"`
	Ms     *int   `json:"ms"`
	Accion string `json:"a"`
}

// anotarUso adds a batch of the interface's use to the day's totals. Off,
// or from a demo link, it keeps nothing and answers the same 204. Anything
// that is not exactly what the interface sends is refused, whole.
func (s *Server) anotarUso(w http.ResponseWriter, r *http.Request, login string) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, usoMaxBytes+1))
	if err != nil || len(raw) > usoMaxBytes {
		httpError(w, http.StatusRequestEntityTooLarge, "too much at once")
		return
	}
	var in struct {
		E []eventoUso `json:"e"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(in.E) > usoMaxEventos {
		httpError(w, http.StatusRequestEntityTooLarge, "too much at once")
		return
	}
	var vistas []store.VistaUso
	var acciones []store.AccionUso
	for _, e := range in.E {
		if !rutasUso[e.Ruta] {
			httpError(w, http.StatusBadRequest, "not a page of the interface")
			return
		}
		switch e.T {
		case "v":
			if e.Desde != "" && !rutasUso[e.Desde] || e.Accion != "" {
				httpError(w, http.StatusBadRequest, "not a page of the interface")
				return
			}
			if e.Ms != nil && (*e.Ms < 0 || *e.Ms > store.UsoMaxMs) {
				httpError(w, http.StatusBadRequest, "a time to data is from 0 to 60000 ms")
				return
			}
			vistas = append(vistas, store.VistaUso{Ruta: e.Ruta, Desde: e.Desde, Ms: e.Ms})
		case "a":
			if !accionesUso[e.Accion] || e.Desde != "" || e.Ms != nil {
				httpError(w, http.StatusBadRequest, "not an action the interface notes")
				return
			}
			acciones = append(acciones, store.AccionUso{Ruta: e.Ruta, Accion: e.Accion})
		default:
			httpError(w, http.StatusBadRequest, "not an action the interface notes")
			return
		}
	}
	if store.IsDemoLogin(login) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	a, err := s.Store.GetUso(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a.Encendido {
		if err := s.Store.AnotarUso(r.Context(), time.Now(), vistas, acciones); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// UsoView is the Use page: the switch and the totals of a period.
type UsoView struct {
	store.UsoAjuste
	Dias int `json:"dias"`
	store.ResumenUso
}

// verUso is the use of the last 7, 14 or 30 days (?dias=, 14 by default),
// for the platform's administrators.
func (s *Server) verUso(w http.ResponseWriter, r *http.Request, _ string) {
	dias := 14
	if q := r.URL.Query().Get("dias"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || (n != 7 && n != 14 && n != 30) {
			httpError(w, http.StatusBadRequest, "the period is 7, 14 or 30 days")
			return
		}
		dias = n
	}
	a, err := s.Store.GetUso(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Today counts as one of the days.
	res, err := s.Store.LeerUso(r.Context(), time.Now().UTC().AddDate(0, 0, -(dias-1)))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, UsoView{UsoAjuste: a, Dias: dias, ResumenUso: res})
}

// putUso turns measuring on or off ({"encendido": false}).
func (s *Server) putUso(w http.ResponseWriter, r *http.Request, login string) {
	var req struct {
		Encendido *bool `json:"encendido"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Encendido == nil {
		httpError(w, http.StatusBadRequest, "say whether it is on: {\"encendido\": true}")
		return
	}
	if err := s.Store.SetUso(r.Context(), *req.Encendido, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anotarDetalle(r, "encendido", *req.Encendido)
	s.verUso(w, r, login)
}
