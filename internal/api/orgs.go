package api

import (
	"errors"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// orgSlug is an organization's address: lowercase letters, digits and
// dashes, 2 to 40 characters.
var orgSlug = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])$`)

// Me is who is signed in: their role in the organization they work in, and
// whether they also run the platform (its organization's administrators).
type Me struct {
	Login    string    `json:"login"`
	Role     string    `json:"role"`
	Org      store.Org `json:"org"`
	Operator bool      `json:"operator"`
	Cluster  bool      `json:"cluster"`        // false on a computer without Kubernetes: no Services or Add-ons, and Environment shows only its version and settings
	Demo     bool      `json:"demo,omitempty"` // signed in by a demo link: read-only
	// Mercado is whether the Mercado (pages without code) is set up here
	// (PAGES_ORG): it is reached from the Apps page, not the menu.
	Mercado bool `json:"mercado,omitempty"`
	// AppPrefix is what the names of this organization's apps begin with
	// (its address and a dash); empty in the platform's organization, whose
	// apps keep the name they are given.
	AppPrefix string `json:"appPrefix,omitempty"`
	// Version is the rendimiento that answers (a release, or the commit it
	// was built from), Modo how it runs ("local": a computer; "kubernetes":
	// a cluster), and OS and Arch the system its program was built for. The
	// foot of every page shows them, so anyone can say what they run.
	Version string `json:"version,omitempty"`
	Modo    string `json:"modo,omitempty"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	// Buzon: this platform receives the comments people write to the team,
	// and the person answers them (its operators): the Comments page.
	Buzon bool `json:"buzon,omitempty"`
	// Uso: the interface notes how it is used (totals only); off, it sends
	// nothing.
	Uso bool `json:"uso,omitempty"`
}

// me is the signed-in person, for the top bar.
func (s *Server) me(w http.ResponseWriter, r *http.Request, login string) {
	out := Me{Login: login, Role: roleOf(r), Demo: store.IsDemoLogin(login), Cluster: s.Kube != nil, Mercado: s.Mercado != nil,
		Version: s.Version, Modo: s.Modo, OS: runtime.GOOS, Arch: runtime.GOARCH}
	o, err := s.Store.GetOrg(r.Context(), orgOf(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Org = o
	if o.ID != store.DefaultOrg {
		out.AppPrefix = o.Slug + "-"
	}
	if role, err := s.Store.MemberRole(r.Context(), store.DefaultOrg, login); err == nil {
		out.Operator = role == store.MemberAdmin
	}
	out.Buzon = out.Operator && s.SenalesRecibir
	if u, err := s.Store.GetUso(r.Context()); err == nil {
		out.Uso = u.Encendido && !out.Demo
	}
	writeJSON(w, out)
}

// listOrgs lists the organizations the person belongs to, with their role.
func (s *Server) listOrgs(w http.ResponseWriter, r *http.Request, login string) {
	orgs, err := s.Store.OrgsOf(r.Context(), login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, orgs)
}

// createOrg makes an organization with the person as its administrator
// (the platform's operators only, while organizations are by invitation).
func (s *Server) createOrg(w http.ResponseWriter, r *http.Request, login string) {
	var in struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Slug, in.Name = strings.ToLower(strings.TrimSpace(in.Slug)), strings.TrimSpace(in.Name)
	if !orgSlug.MatchString(in.Slug) || in.Name == "" || len([]rune(in.Name)) > 80 {
		httpError(w, http.StatusBadRequest, "an organization needs a name and an address of 2 to 40 lowercase letters, digits and dashes")
		return
	}
	o, err := s.Store.CreateOrg(r.Context(), in.Slug, in.Name, login)
	if errors.Is(err, store.ErrSlugTaken) {
		httpError(w, http.StatusConflict, "that address is taken by another organization")
		return
	} else if errors.Is(err, store.ErrSlugPrefix) {
		httpError(w, http.StatusConflict, "that address begins the name of an app, or of another organization's address: an organization's apps carry its address in front, and their names would mix; choose another")
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("organization created", "org", o.Slug, "by", login)
	writeJSONStatus(w, http.StatusCreated, o)
}

// switchOrg moves the session to another organization the person belongs to.
func (s *Server) switchOrg(w http.ResponseWriter, r *http.Request, login string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad organization id")
		return
	}
	if _, err := s.Store.MemberRole(r.Context(), id, login); err != nil {
		httpError(w, http.StatusNotFound, "no such organization")
		return
	}
	c, _ := r.Cookie(sessionCookie)
	if err := s.Store.SetSessionOrg(r.Context(), hashToken(c.Value), id); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
