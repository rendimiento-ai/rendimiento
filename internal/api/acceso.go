package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/catalog"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/store"
	"github.com/rendimiento-ai/rendimiento/internal/universe"
)

// Who enters an app. A person's role in the organization holds on every
// app of it, unless the app has a list: then each person on the list has
// their role for that app, people not on it do not see it (it answers 404,
// like another organization's app), and the organization's administrators
// always enter. store.AppAccess.Role decides; requireRole asks it for the
// app a route is about, and every list of things of apps asks it too.

type accessKey struct{}

// accessOf is what the signed-in person may do on their organization's
// apps (set by requireRole; without it, no app is theirs).
func accessOf(r *http.Request) store.AppAccess {
	acc, _ := r.Context().Value(accessKey{}).(store.AppAccess)
	return acc
}

// routeApp is the app a request is about, from its path: {app}, a run's
// ({id} under /api/runs/), or an encargo's ({id} under /api/encargos/,
// when it names an app). ok is false for routes about no single app; app
// is nil when the route names one that does not exist.
func (s *Server) routeApp(r *http.Request) (app *store.App, ok bool) {
	ctx := r.Context()
	if name := r.PathValue("app"); name != "" {
		a, err := s.Store.GetApp(ctx, name)
		if err != nil {
			return nil, true
		}
		return a, true
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, false
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/runs/"):
		run, err := s.Store.GetRun(ctx, id)
		if err != nil {
			return nil, true
		}
		a, err := s.Store.GetAppByID(ctx, run.AppID)
		if err != nil {
			return nil, true
		}
		return a, true
	case strings.HasPrefix(r.URL.Path, "/api/encargos/"):
		e, err := s.Store.Encargo(ctx, id)
		if err != nil || e.App == "" {
			return nil, false // about no app: the organization's role
		}
		a, err := s.Store.GetApp(ctx, e.App)
		if err != nil {
			return nil, false
		}
		return a, true
	}
	return nil, false
}

// platformHidden are the platform organization's apps login may not see,
// for the platform's pages (Problems, Mercado, add-ons): those are the
// operators' view of the whole installation, and there an app's list
// leaves out the operators it leaves out.
func (s *Server) platformHidden(ctx context.Context, login string) (map[string]bool, error) {
	role, err := s.Store.MemberRole(ctx, store.DefaultOrg, login)
	if errors.Is(err, store.ErrNotFound) {
		role, err = "", nil
	}
	if err != nil {
		return nil, err
	}
	acc, err := s.Store.AppAccessOf(ctx, store.DefaultOrg, login, role)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, n := range acc.Hidden() {
		out[n] = true
	}
	return out, nil
}

// hiddenApps are the apps of the whole installation the person may not
// see (other organizations' and those whose list leaves them out), for
// what mixes every app (the cluster's catalog, traces).
func (s *Server) hiddenApps(r *http.Request) (map[string]bool, error) {
	apps, err := s.Store.ListApps(r.Context())
	if err != nil {
		return nil, err
	}
	acc, out := accessOf(r), map[string]bool{}
	for _, a := range apps {
		if acc.Role(a) == "" {
			out[a.Name] = true
		}
	}
	return out, nil
}

// visibleUniverse leaves out of the Universe's inputs what belongs to apps
// the person may not see: their workloads in the cluster's catalog and
// their traced calls. The cached catalog is copied, never changed.
func visibleUniverse(in *universe.Input, hidden map[string]bool) {
	if in.Catalog != nil {
		c := *in.Catalog
		c.Entries = make([]catalog.Entry, 0, len(in.Catalog.Entries))
		for _, e := range in.Catalog.Entries {
			if hidden[e.Origin.App] || hidden[e.Namespace] {
				continue
			}
			users := make([]catalog.Consumer, 0, len(e.UsedBy))
			for _, u := range e.UsedBy {
				if !hidden[u.App] && !hidden[u.Namespace] {
					users = append(users, u)
				}
			}
			e.UsedBy = users
			c.Entries = append(c.Entries, e)
		}
		in.Catalog = &c
	}
	if in.Traced != nil {
		traced := map[[2]string]universe.EdgeMetrics{}
		for k, m := range in.Traced {
			from, _, _ := strings.Cut(k[0], "/")
			to, _, _ := strings.Cut(k[1], "/")
			if !hidden[from] && !hidden[to] {
				traced[k] = m
			}
		}
		in.Traced = traced
	}
}

// bitacoraApp is the app a bitácora row is about, as far as it says: the
// app in its path, in its detail, or in an agent's arguments.
func bitacoraApp(b store.Bitacora) string {
	if a := b.Sobre["app"]; a != "" {
		return a
	}
	if a, ok := b.Detalle["app"].(string); ok && a != "" {
		return a
	}
	if raw, ok := b.Detalle["argumentos"].(string); ok {
		return argsApp(raw)
	}
	return ""
}

// argsApp is the app an agent's tool call names in its arguments (JSON),
// or "".
func argsApp(raw string) string {
	var args map[string]any
	if json.Unmarshal([]byte(raw), &args) == nil {
		for _, k := range []string{"app", "puesto"} {
			if a, ok := args[k].(string); ok && a != "" {
				return a
			}
		}
	}
	return ""
}

// rutaAboutHidden says whether a Ruta entry is tagged with an app the
// person may not see (a known failure of it, say): the Ruta is the
// organization's, but what it says of an app is that app's.
func rutaAboutHidden(acc store.AppAccess, e store.RutaEntry) bool {
	for _, n := range acc.Hidden() {
		for _, t := range e.Tags {
			if strings.EqualFold(t, n) {
				return true
			}
		}
	}
	return false
}

// ---- who enters an app (its list) ----

// AppAccessView is the app page's «Who enters» section: whether the app
// has a list, who is on it, and the organization's people (who can be
// added; administrators always enter).
type AppAccessView struct {
	store.AppList
	People []store.Member `json:"people"`
}

// orgAdminOnly answers 403 unless the person is an administrator of the
// organization: an app's administrator does not choose who enters it.
func orgAdminOnly(w http.ResponseWriter, r *http.Request) bool {
	if roleOf(r) != store.MemberAdmin {
		httpError(w, http.StatusForbidden, i18n.M("only the organization's administrators choose who enters an app"))
		return false
	}
	return true
}

// appAccessView answers the app's list as the section shows it.
func (s *Server) appAccessView(w http.ResponseWriter, r *http.Request, a *store.App) {
	list, err := s.Store.AppMembers(r.Context(), a)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	people, err := s.Store.Members(r.Context(), a.OrgID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	view := AppAccessView{AppList: list, People: []store.Member{}}
	for _, m := range people {
		if !store.IsDemoLogin(m.Login) { // a demo link's guest only ever reads
			view.People = append(view.People, m)
		}
	}
	writeJSON(w, view)
}

// getAppMembers is who enters the app.
func (s *Server) getAppMembers(w http.ResponseWriter, r *http.Request, _ string) {
	if a := s.app(w, r); a != nil && orgAdminOnly(w, r) {
		s.appAccessView(w, r, a)
	}
}

// putAppList turns the app's list on ({"list": true}: only the
// organization's administrators enter until people are added) or off
// (everyone of the organization enters with their role).
func (s *Server) putAppList(w http.ResponseWriter, r *http.Request, _ string) {
	a := s.app(w, r)
	if a == nil || !orgAdminOnly(w, r) {
		return
	}
	var in struct {
		List bool `json:"list"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.Store.SetAppList(r.Context(), a.ID, in.List); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anotarDetalle(r, "list", in.List)
	s.appAccessView(w, r, a)
}

// putAppMember gives a person of the organization a role on the app
// ({"role": "guest"|"collaborator"|"admin"}), turning its list on.
func (s *Server) putAppMember(w http.ResponseWriter, r *http.Request, login string) {
	a := s.app(w, r)
	if a == nil || !orgAdminOnly(w, r) {
		return
	}
	who := r.PathValue("login")
	var in struct {
		Role string `json:"role"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !githubLogin.MatchString(who) {
		httpError(w, http.StatusBadRequest, "that is not a GitHub username")
		return
	}
	if store.RoleRank(in.Role) == 0 {
		httpError(w, http.StatusBadRequest, "role must be guest, collaborator or admin")
		return
	}
	_, err := s.Store.SetAppMember(r.Context(), a, who, in.Role, login)
	switch {
	case errors.Is(err, store.ErrNotInOrg):
		httpError(w, http.StatusBadRequest, i18n.M("that person is not in this organization; add them on the Members page first"))
		return
	case errors.Is(err, store.ErrOrgAdmin):
		httpError(w, http.StatusBadRequest, i18n.M("the organization's administrators always enter every app"))
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anotarDetalle(r, "role", in.Role)
	s.appAccessView(w, r, a)
}

// deleteAppMember takes a person off the app's list: they no longer see it.
func (s *Server) deleteAppMember(w http.ResponseWriter, r *http.Request, _ string) {
	a := s.app(w, r)
	if a == nil || !orgAdminOnly(w, r) {
		return
	}
	switch err := s.Store.RemoveAppMember(r.Context(), a.ID, r.PathValue("login")); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, i18n.M("that person is not on this app's list"))
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.appAccessView(w, r, a)
}
