package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	gh "github.com/rendimiento-ai/rendimiento/internal/github"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Which GitHub installations an organization may create apps from. The
// platform's organization, and its operators wherever they work, use every
// installation the App allows (GITHUB_ACCOUNTS), as before there were
// organizations. Any other organization uses only those given to it
// (org_installations): by an operator, or by its administrator connecting
// the App installed on their own GitHub account, which their sign-in with
// GitHub proves is theirs. A GitHub organization's installation has no such
// proof without asking GitHub for more, so an operator gives it.

// platformWide reports whether the person, where they work now, uses every
// installation: in the platform's organization, or as one of its operators.
func (s *Server) platformWide(r *http.Request, login string) bool {
	if orgOf(r) == store.DefaultOrg {
		return true
	}
	if store.IsDemoLogin(login) {
		return false
	}
	role, err := s.Store.MemberRole(r.Context(), store.DefaultOrg, login)
	return err == nil && role == store.MemberAdmin
}

// usable lists the installations the organization may create apps from.
func (s *Server) usable(r *http.Request, app *gh.App, login string) ([]gh.Installation, error) {
	all, err := app.Installations(r.Context())
	if err != nil {
		return nil, err
	}
	if s.platformWide(r, login) {
		return all, nil
	}
	given, err := s.Store.OrgInstallations(r.Context(), orgOf(r))
	if err != nil {
		return nil, err
	}
	out := []gh.Installation{}
	for _, i := range all {
		for _, g := range given {
			if g.InstallationID == i.ID {
				out = append(out, i)
				break
			}
		}
	}
	return out, nil
}

// connectable lists the installations the person could connect to their
// organization themselves: the App on their own GitHub account, not yet
// connected here.
func (s *Server) connectable(r *http.Request, app *gh.App, login string) ([]gh.Installation, error) {
	out := []gh.Installation{}
	if s.platformWide(r, login) || store.IsDemoLogin(login) {
		return out, nil
	}
	all, err := app.Installations(r.Context())
	if err != nil {
		return nil, err
	}
	for _, i := range all {
		if !strings.EqualFold(i.Account.Login, login) {
			continue
		}
		has, err := s.Store.OrgHasInstallation(r.Context(), orgOf(r), i.ID)
		if err != nil {
			return nil, err
		}
		if !has {
			out = append(out, i)
		}
	}
	return out, nil
}

// connectInstallation gives the person's organization the App installed on
// their own GitHub account (its login is the one they signed in with).
func (s *Server) connectInstallation(w http.ResponseWriter, r *http.Request, login string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad installation id")
		return
	}
	if orgOf(r) == store.DefaultOrg {
		httpError(w, http.StatusConflict, "the platform's organization already uses every installation of the GitHub App")
		return
	}
	app, err := s.GitHub.Get()
	if err != nil {
		httpError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	all, err := app.Installations(r.Context())
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	var inst *gh.Installation
	for i := range all {
		if all[i].ID == id {
			inst = &all[i]
		}
	}
	if inst == nil || store.IsDemoLogin(login) || !strings.EqualFold(inst.Account.Login, login) {
		httpError(w, http.StatusForbidden, "you can connect only the GitHub App installed on your own GitHub account; for a GitHub organization's, ask the platform's operators")
		return
	}
	if err := s.Store.AddOrgInstallation(r.Context(), orgOf(r), inst.ID, inst.Account.Login, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("installation connected", "org", orgOf(r), "installation", inst.ID, "account", inst.Account.Login, "by", login)
	writeJSON(w, inst)
}

// disconnectInstallation stops the organization creating apps from an
// installation; its apps keep building from it.
func (s *Server) disconnectInstallation(w http.ResponseWriter, r *http.Request, login string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad installation id")
		return
	}
	if err := s.Store.RemoveOrgInstallation(r.Context(), orgOf(r), id); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "this organization does not have that installation")
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("installation disconnected", "org", orgOf(r), "installation", id, "by", login)
	w.WriteHeader(http.StatusNoContent)
}

// orgInstallations is, for the operators, every installation of the App,
// the organizations each is given to, and the organizations to give them to.
func (s *Server) orgInstallations(w http.ResponseWriter, r *http.Request, _ string) {
	app, err := s.GitHub.Get()
	if err != nil {
		httpError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	all, err := app.Installations(r.Context())
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	given, err := s.Store.OrgInstallations(r.Context(), 0)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if given == nil {
		given = []store.OrgInstallation{}
	}
	orgs, err := s.Store.AllOrgs(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	others := []store.Org{} // the platform's uses every installation
	for _, o := range orgs {
		if o.ID != store.DefaultOrg {
			others = append(others, o)
		}
	}
	writeJSON(w, map[string]any{"installations": all, "given": given, "orgs": others, "installURL": app.Credentials().HTMLURL + "/installations/new"})
}

// orgAndInstallation reads an operator's request about installation {id}
// and an organization (other than the platform's); ok false means the
// answer was already written.
func (s *Server) orgAndInstallation(w http.ResponseWriter, r *http.Request, org int64) (int64, *gh.Installation, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad installation id")
		return 0, nil, false
	}
	if org == store.DefaultOrg {
		httpError(w, http.StatusConflict, "the platform's organization already uses every installation of the GitHub App")
		return 0, nil, false
	}
	if _, err := s.Store.GetOrg(r.Context(), org); err != nil {
		httpError(w, http.StatusNotFound, "no such organization")
		return 0, nil, false
	}
	app, err := s.GitHub.Get()
	if err != nil {
		httpError(w, http.StatusServiceUnavailable, err.Error())
		return 0, nil, false
	}
	inst, err := app.Installation(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return 0, nil, false
	}
	return org, inst, true
}

// giveInstallation lets an organization create apps from an installation
// (an operator's decision, for a GitHub organization's account).
func (s *Server) giveInstallation(w http.ResponseWriter, r *http.Request, login string) {
	var in struct {
		Org int64 `json:"org"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	org, inst, ok := s.orgAndInstallation(w, r, in.Org)
	if !ok {
		return
	}
	if err := s.Store.AddOrgInstallation(r.Context(), org, inst.ID, inst.Account.Login, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("installation given", "org", org, "installation", inst.ID, "account", inst.Account.Login, "by", login)
	w.WriteHeader(http.StatusNoContent)
}

// takeInstallation takes an installation away from an organization
// (?org=); its apps keep building from it.
func (s *Server) takeInstallation(w http.ResponseWriter, r *http.Request, login string) {
	org, err := strconv.ParseInt(r.URL.Query().Get("org"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad organization id")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad installation id")
		return
	}
	if err := s.Store.RemoveOrgInstallation(r.Context(), org, id); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "this organization does not have that installation")
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("installation taken away", "org", org, "installation", id, "by", login)
	w.WriteHeader(http.StatusNoContent)
}
