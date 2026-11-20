package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/bitbucket"
	"github.com/rendimiento-ai/rendimiento/internal/environment"
	"github.com/rendimiento-ai/rendimiento/internal/gitlab"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Installing without GitHub: the setup page has three steps.
//
//  1. The setup token (the rendimiento-setup secret, or a file on a
//     computer) opens an install session: a cookie signed with the token,
//     good for an hour. Without it nobody picks a forja or becomes an
//     administrator.
//  2. Where the code lives: GitHub (the App's manifest, as before), a
//     GitLab or a Bitbucket workspace (the same connections the
//     Environment page makes, authorized by the install session instead of
//     an administrator's).
//  3. Who administers: the person who authorized the forja comes back
//     signed in as the first administrator, even if ALLOWED_USERS does not
//     name them (they proved the setup token), and adds the others.
//
// Every door of the setup page closes once the platform is installed.

// installCookie carries the install session: "<nonce>.<expiry>.<mac>",
// the mac an HMAC of the nonce and expiry keyed by the setup token, so the
// token itself never travels in a cookie and a session ends with it.
const (
	installCookie = "rendimiento_instalar"
	installTTL    = time.Hour
	// installWhat starts the "what" of a forja's state cookie when the
	// person went there from the setup page; the install session's nonce
	// follows, so the OAuth answer is bound to that session.
	installWhat = "install:"
	// installedBy is who added the first administrator.
	installedBy = "setup"
)

func (s *Server) installMAC(nonce string, exp int64) []byte {
	m := hmac.New(sha256.New, []byte(s.SetupToken))
	fmt.Fprintf(m, "rendimiento-instalar|%s|%d", nonce, exp)
	return m.Sum(nil)
}

// openInstall starts an install session and answers its nonce.
func (s *Server) openInstall(w http.ResponseWriter) string {
	nonce := randomToken()[:32]
	exp := time.Now().Add(installTTL).Unix()
	http.SetCookie(w, &http.Cookie{Name: installCookie, Value: nonce + "." + strconv.FormatInt(exp, 10) + "." + hex.EncodeToString(s.installMAC(nonce, exp)),
		Path: "/api", MaxAge: int(installTTL.Seconds()), HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode})
	return nonce
}

// installSession is the request's install session's nonce, if it has a
// good one.
func (s *Server) installSession(r *http.Request) (string, bool) {
	if s.SetupToken == "" {
		return "", false
	}
	c, err := r.Cookie(installCookie)
	if err != nil {
		return "", false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 || parts[0] == "" {
		return "", false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", false
	}
	mac, err := hex.DecodeString(parts[2])
	if err != nil || !hmac.Equal(mac, s.installMAC(parts[0], exp)) {
		return "", false
	}
	return parts[0], true
}

func (s *Server) closeInstall(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: installCookie, Value: "", Path: "/api", MaxAge: -1, HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode})
}

// installed says whether the platform is installed: the setup page
// finished here, or (a platform installed before it kept that) it has an
// administrator and a forja to sign in with. When the database does not
// answer it says yes, so the setup page's doors stay closed.
func (s *Server) installed(ctx context.Context) bool {
	i, err := s.Store.EstadoInstalacion(ctx)
	if err != nil {
		s.Log.Warn("installation state", "err", err)
		return true
	}
	_, gherr := s.GitHub.Get()
	return i.Marcada || (i.Administradores && (i.ForjaAutorizada || gherr == nil))
}

// setupToken is POST /api/setup/token: step 1. The token is compared in
// constant time; a good one opens the install session.
func (s *Server) setupToken(w http.ResponseWriter, r *http.Request) {
	if s.SetupToken == "" || s.installed(r.Context()) {
		httpError(w, http.StatusNotFound, i18n.M("rendimiento is already set up here"))
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(in.Token)), []byte(s.SetupToken)) != 1 {
		httpError(w, http.StatusForbidden, i18n.M("that is not the setup token"))
		return
	}
	s.openInstall(w)
	w.WriteHeader(http.StatusNoContent)
}

// installGate lets a step of the setup page through: the platform is not
// installed yet and the request has an install session. It answers the
// session's nonce; "" means the answer was written.
func (s *Server) installGate(w http.ResponseWriter, r *http.Request) string {
	if s.SetupToken == "" || s.installed(r.Context()) {
		httpError(w, http.StatusNotFound, i18n.M("rendimiento is already set up here"))
		return ""
	}
	nonce, ok := s.installSession(r)
	if !ok {
		httpError(w, http.StatusForbidden, i18n.M("the setup session expired: paste the setup token again"))
		return ""
	}
	return nonce
}

// setupGitLab is POST /api/setup/gitlab: step 2 with a GitLab. The
// connection is kept as the Environment page keeps it, and the answer says
// where to authorize it.
func (s *Server) setupGitLab(w http.ResponseWriter, r *http.Request) {
	nonce := s.installGate(w, r)
	if nonce == "" || !s.gitlabReady(w) {
		return
	}
	if c := s.keepGitLab(w, r, installedBy); c != nil {
		writeJSONStatus(w, http.StatusCreated, map[string]any{"connection": c, "authorizeUrl": s.sendToGitLab(w, c, installWhat+nonce, gitlab.ScopeConnect)})
	}
}

// setupBitbucket is POST /api/setup/bitbucket: step 2 with a Bitbucket
// workspace.
func (s *Server) setupBitbucket(w http.ResponseWriter, r *http.Request) {
	nonce := s.installGate(w, r)
	if nonce == "" || !s.bitbucketReady(w) {
		return
	}
	if c := s.keepBitbucket(w, r, installedBy); c != nil {
		writeJSONStatus(w, http.StatusCreated, map[string]any{"connection": c, "authorizeUrl": s.sendToBitbucket(w, c, installWhat+nonce)})
	}
}

// finishInstall is the forja's answer to step 2: the state cookie carried
// the install session's nonce, which must still be this browser's. The
// connection is authorized, the person who authorized it becomes the first
// administrator, and comes in signed in, on to step 3.
func (s *Server) finishInstall(w http.ResponseWriter, r *http.Request, nonce, login string, authorize func(context.Context) error) {
	ctx := r.Context()
	got, ok := s.installSession(r)
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(nonce)) != 1 {
		httpError(w, http.StatusForbidden, i18n.M("the setup session expired: paste the setup token again"))
		return
	}
	if s.installed(ctx) {
		httpError(w, http.StatusNotFound, i18n.M("rendimiento is already set up here"))
		return
	}
	if authorize != nil {
		if err := authorize(ctx); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.claimInstall(w, r, login)
}

// claimInstall makes login the first administrator, marks the platform
// installed, ends the install session and signs them in.
func (s *Server) claimInstall(w http.ResponseWriter, r *http.Request, login string) {
	ctx := r.Context()
	if err := s.Store.SetMember(ctx, store.DefaultOrg, login, store.MemberAdmin, installedBy); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.Store.MarcarInstalada(ctx, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.closeInstall(w)
	s.Log.Info("installed: the first administrator signed in", "login", login)
	s.openSessionTo(w, r, login, "/instalacion")
}

// installView is the setup page's state, in the setup status while the
// platform is not installed: whether this browser has an install session,
// which forjas this platform can connect, what is connected already, and
// the addresses an OAuth application needs.
func (s *Server) installView(r *http.Request) map[string]any {
	ctx := r.Context()
	_, session := s.installSession(r)
	_, gherr := s.GitHub.Get()
	out := map[string]any{
		"session": session, "github": gherr == nil,
		"canGitLab": s.GitLab != nil && s.GitLab.Keys != nil, "canBitbucket": s.Bitbucket != nil && s.Bitbucket.Keys != nil,
		"gitlabCallback": s.BaseURL + gitlab.CallbackPath, "bitbucketCallback": s.BaseURL + bitbucket.CallbackPath,
	}
	type conn struct {
		ID         int64  `json:"id"`
		Host       string `json:"host"`
		Authorized bool   `json:"authorized"`
	}
	gl, bb := []conn{}, []conn{}
	if list, err := s.Store.GitLabConnections(ctx); err == nil {
		for _, c := range list {
			gl = append(gl, conn{c.ID, gitlab.Host(c.BaseURL), c.Username != ""})
		}
	}
	if list, err := s.Store.BitbucketConnections(ctx); err == nil {
		for _, c := range list {
			bb = append(bb, conn{c.ID, c.Workspace, c.Username != ""})
		}
	}
	out["gitlab"], out["bitbucket"] = gl, bb
	return out
}

// ---- who signs in with which forja ----

// Forjas one signs in with, as keys: "github", "gitlab:<host>" (one per
// GitLab address) and "bitbucket" (any workspace signs in any Bitbucket
// account).
const (
	claveGitHub    = "github"
	claveBitbucket = "bitbucket"
)

// claveGitLab is the key of the GitLab at baseURL: its host, as
// gitlab.Login names its accounts (ana@gitlab.com).
func claveGitLab(baseURL string) string {
	host := gitlab.Host(baseURL)
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		host = u.Host
	}
	return "gitlab:" + strings.ToLower(host)
}

// claveDeLogin is the forja a member's own name signs in with: ana is
// GitHub's, ana@bitbucket.org Bitbucket's, ana@gitlab.com (any other host)
// a GitLab's.
func claveDeLogin(login string) string {
	at := strings.LastIndex(login, "@")
	if at < 0 {
		return claveGitHub
	}
	host := strings.ToLower(login[at+1:])
	if host == bitbucket.Host {
		return claveBitbucket
	}
	return "gitlab:" + host
}

// claveDeIdentidad is the forja a linked account signs in with.
func claveDeIdentidad(i store.Identity) string {
	switch i.Forja {
	case bitbucket.Name:
		return claveBitbucket
	case gitlab.Name:
		return claveGitLab(i.BaseURL)
	}
	return i.Forja
}

// pierdenAcceso are the people who sign in with a forja in antes and none
// in despues (everyone, and the administrators among them): who loses
// access when a forja goes. Someone who could not sign in before is not
// counted.
func pierdenAcceso(accesos []store.Acceso, antes, despues map[string]bool) (miembros, admins []string) {
	miembros, admins = []string{}, []string{}
	for _, a := range accesos {
		claves := []string{claveDeLogin(a.Login)}
		for _, i := range a.Identities {
			claves = append(claves, claveDeIdentidad(i))
		}
		tenia, tiene := false, false
		for _, k := range claves {
			tenia = tenia || antes[k]
			tiene = tiene || despues[k]
		}
		if tenia && !tiene {
			miembros = append(miembros, a.Login)
			if a.Admin {
				admins = append(admins, a.Login)
			}
		}
	}
	sort.Strings(miembros)
	sort.Strings(admins)
	return miembros, admins
}

// forjasDeEntrada are the forjas one signs in with here, leaving out the
// connection sin ("gitlab/<id>" or "bitbucket/<id>"; "" leaves none out).
func (s *Server) forjasDeEntrada(ctx context.Context, sin string) (map[string]bool, error) {
	out := map[string]bool{}
	if _, err := s.GitHub.Get(); err == nil {
		out[claveGitHub] = true
	}
	gl, err := s.Store.GitLabConnections(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range gl {
		if sin != gitlab.Name+"/"+strconv.FormatInt(c.ID, 10) {
			out[claveGitLab(c.BaseURL)] = true
		}
	}
	bb, err := s.Store.BitbucketConnections(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range bb {
		if sin != bitbucket.Name+"/"+strconv.FormatInt(c.ID, 10) {
			out[claveBitbucket] = true
		}
	}
	return out, nil
}

// quienesPierden are who would lose access if the connection sin went.
func (s *Server) quienesPierden(ctx context.Context, sin string) (miembros, admins []string, err error) {
	antes, err := s.forjasDeEntrada(ctx, "")
	if err != nil {
		return nil, nil, err
	}
	despues, err := s.forjasDeEntrada(ctx, sin)
	if err != nil {
		return nil, nil, err
	}
	accesos, err := s.Store.Accesos(ctx)
	if err != nil {
		return nil, nil, err
	}
	miembros, admins = pierdenAcceso(accesos, antes, despues)
	return miembros, admins, nil
}

// lastForjaOfAnAdmin answers 409 when taking the connection sin away
// would leave an administrator with no forja to sign in with (decision 2);
// false means the answer was written.
func (s *Server) lastForjaOfAnAdmin(w http.ResponseWriter, r *http.Request, sin string) bool {
	_, admins, err := s.quienesPierden(r.Context(), sin)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if len(admins) > 0 {
		httpError(w, http.StatusConflict, i18n.M("%s administers here and signs in only with this forja: link another account to them (Your accounts) or take away their role first", strings.Join(admins, ", ")))
		return false
	}
	return true
}

// forjaAcceso is GET /api/forjas/{gitlab|bitbucket}/{id}/acceso: who would
// lose access if the connection went, for the warning before removing it.
func (s *Server) forjaAcceso(forja string) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "bad connection id")
			return
		}
		miembros, admins, err := s.quienesPierden(r.Context(), forja+"/"+strconv.FormatInt(id, 10))
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string][]string{"members": miembros, "admins": admins})
	}
}

// ForjasConectadas lists, for the environment check, the GitLabs and
// Bitbucket workspaces connected and authorized here.
func ForjasConectadas(st *store.Store) func(context.Context) []environment.Forja {
	return func(ctx context.Context) []environment.Forja {
		out := []environment.Forja{}
		if list, err := st.GitLabConnections(ctx); err == nil {
			for _, c := range list {
				if c.Username != "" {
					out = append(out, environment.Forja{ID: gitlab.Name, Name: "GitLab " + gitlab.Host(c.BaseURL)})
				}
			}
		}
		if list, err := st.BitbucketConnections(ctx); err == nil {
			for _, c := range list {
				if c.Username != "" {
					out = append(out, environment.Forja{ID: bitbucket.Name, Name: "Bitbucket " + c.Workspace})
				}
			}
		}
		return out
	}
}
