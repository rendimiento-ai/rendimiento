package api

// What each agent may do (Ruta 184), on the AI page: every subject (the
// platform's agents and each MCP key) with its permissions in one call,
// changed one subject at a time by an administrator; the grants that still
// hold, taken back there; and the answer to what an agent asks («Para
// usted»). Members see it; only administrators change it or give a
// permission; a collaborator may say no. These routes take a person's
// session: an agent's key never reaches them, so it cannot grant itself.

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// PermisoSujeto is one subject on the page: its id (stable: other pages
// link to it), whether it is a platform's agent or an MCP key, its name
// for a key, and its permissions.
type PermisoSujeto struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"` // agent, mcp
	Name     string          `json:"name,omitempty"`
	Role     string          `json:"role,omitempty"`
	Permisos []store.Permiso `json:"permisos"`
}

// PermisosView is the Permissions section of the AI page.
type PermisosView struct {
	Sujetos []PermisoSujeto     `json:"sujetos"`
	Dados   []store.PermisoDado `json:"dados"`
	Apps    []string            `json:"apps"`
}

// sujetosDe are the organization's subjects: the platform's agents, then
// each MCP key that still works (not the platform's own keys).
func (s *Server) sujetosDe(r *http.Request) ([]PermisoSujeto, error) {
	ctx, org := r.Context(), orgOf(r)
	var out []PermisoSujeto
	for _, id := range store.SujetosPlataforma {
		ps, err := s.Store.PermisosOf(ctx, org, id)
		if err != nil {
			return nil, err
		}
		out = append(out, PermisoSujeto{ID: id, Kind: "agent", Permisos: ps})
	}
	keys, err := s.Store.ListAgentKeys(ctx, org)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if k.CreatedBy == "rendimiento" || !k.Usable(time.Now()) {
			continue
		}
		id := store.SujetoLlave(k.ID)
		ps, err := s.Store.PermisosOf(ctx, org, id)
		if err != nil {
			return nil, err
		}
		out = append(out, PermisoSujeto{ID: id, Kind: "mcp", Name: k.Name, Role: k.Role, Permisos: ps})
	}
	return out, nil
}

// permisosView is every subject with its permissions, the grants that hold
// and the apps a permission may be limited to.
func (s *Server) permisosView(w http.ResponseWriter, r *http.Request, _ string) {
	ctx, org := r.Context(), orgOf(r)
	sujetos, err := s.sujetosDe(r)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dados, err := s.Store.PermisosDados(ctx, org, "")
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v := PermisosView{Sujetos: sujetos, Dados: dados, Apps: []string{}}
	if apps, err := s.Store.ListApps(ctx); err == nil {
		acc := accessOf(r)
		for _, a := range apps {
			if a.OrgID == org && acc.Sees(a.Name) {
				v.Apps = append(v.Apps, a.Name)
			}
		}
		slices.Sort(v.Apps)
	}
	writeJSON(w, v)
}

// sujetoDeOrg says whether a subject is one of the organization's: a
// platform's agent, or one of its keys that still works.
func (s *Server) sujetoDeOrg(r *http.Request, sujeto string) bool {
	if slices.Contains(store.SujetosPlataforma, sujeto) {
		return true
	}
	id := store.LlaveDeSujeto(sujeto)
	if id == 0 {
		return false
	}
	k, err := s.Store.AgentKey(r.Context(), id)
	return err == nil && k.OrgID == orgOf(r) && k.CreatedBy != "rendimiento" && k.Usable(time.Now())
}

// putPermisos sets one subject's permissions (those sent; the others stay).
func (s *Server) putPermisos(w http.ResponseWriter, r *http.Request, login string) {
	sujeto := r.PathValue("sujeto")
	if !s.sujetoDeOrg(r, sujeto) {
		httpError(w, http.StatusNotFound, "no such agent")
		return
	}
	var in struct {
		Permisos []store.Permiso `json:"permisos"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	for _, p := range in.Permisos {
		if !store.ModoValido(p.Permiso, p.Modo) {
			httpError(w, http.StatusBadRequest, "that mode is not allowed for that permission: reading the code is always on, and actions with effect are never always")
			return
		}
		if len(p.Apps) > 100 {
			httpError(w, http.StatusBadRequest, "too many apps")
			return
		}
	}
	ctx := r.Context()
	for _, p := range in.Permisos {
		if err := s.Store.SetPermiso(ctx, orgOf(r), sujeto, p, login); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.Log.Info("agent permissions saved", "org", orgOf(r), "subject", sujeto, "by", login)
	ps, err := s.Store.PermisosOf(ctx, orgOf(r), sujeto)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, PermisoSujeto{ID: sujeto, Permisos: ps})
}

// deletePermisoDado takes a grant back.
func (s *Server) deletePermisoDado(w http.ResponseWriter, r *http.Request, login string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad grant id")
		return
	}
	if err := s.Store.QuitarPermisoDado(r.Context(), id, orgOf(r), login); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such grant")
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// answerPermiso is a person's answer to what an agent asks: no (a
// collaborator may say it), or a permission (an administrator gives it)
// once, while the encargo or the key's cargo lasts, or always for the app.
// The coding agent's encargo goes back to its queue, to go on with it.
func (s *Server) answerPermiso(w http.ResponseWriter, r *http.Request, login string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad permission request id")
		return
	}
	var in struct {
		Answer string `json:"answer"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Answer = strings.TrimSpace(in.Answer)
	if in.Answer != store.PedidoNo && roleOf(r) != store.MemberAdmin {
		httpError(w, http.StatusForbidden, "only administrators give agents permissions")
		return
	}
	ctx, org := r.Context(), orgOf(r)
	if old, err := s.Store.PermisoPedidoDe(ctx, id); err != nil || old.OrgID != org || !accessOf(r).Sees(old.App) {
		httpError(w, http.StatusNotFound, "no such permission request")
		return
	}
	p, dado, err := s.Store.ResponderPermiso(ctx, id, org, in.Answer, login)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "no such permission request")
		return
	case errors.Is(err, store.ErrPedidoRespondido):
		httpError(w, http.StatusConflict, "this request was answered already")
		return
	case errors.Is(err, store.ErrPermiso):
		httpError(w, http.StatusBadRequest, "that answer is not possible for this request: an action with effect is allowed only once")
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p.EncargoID != nil {
		what, note := store.LogPermisoNegado, s.permisoNote(p, in.Answer, false)
		if dado != nil {
			what = store.LogPermisoDado
		}
		_ = s.Store.NoteEncargoPermiso(ctx, *p.EncargoID, login, what, p.Permiso, note)
		if _, err := s.Store.AnswerEncargo(ctx, *p.EncargoID, org, login, s.permisoNote(p, in.Answer, true)); err != nil && !errors.Is(err, store.ErrEncargoState) {
			s.Log.Warn("permission answered, but the encargo did not go on", "encargo", *p.EncargoID, "err", err)
		}
	}
	writeJSON(w, map[string]any{"pedido": p, "dado": dado})
}

// permisoNote is an answer in words: for the encargo's history, or (forAgent)
// what the coding agent goes on with.
func (s *Server) permisoNote(p store.PermisoPedido, answer string, forAgent bool) string {
	es := s.AgentLang == "es"
	say := func(e, en string) string {
		if es {
			return e
		}
		return en
	}
	var reach string
	switch answer {
	case store.AlcanceOnce:
		reach = say("solo esta vez", "only this time")
	case store.AlcanceEncargo:
		reach = say("mientras dure este encargo", "while this encargo lasts")
	case store.AlcanceAlways:
		reach = say("siempre, para ", "always, for ") + p.App
	}
	if !forAgent {
		if answer == store.PedidoNo {
			return p.Que
		}
		return p.Que + " · " + reach
	}
	if answer == store.PedidoNo {
		return say("No: siga sin ese permiso y diga en su entrega qué le habría hecho falta.",
			"No: go on without that permission, and say in your delivery what it would have needed.")
	}
	return say("Permiso dado, ", "Permission given, ") + reach + say(": vuelva a llamar a la herramienta igual que antes.", ": call the tool again the same way as before.")
}
