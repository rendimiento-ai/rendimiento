package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/gitlab"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Where an app's code lives (Ruta 102): GitHub, through its App, and the
// GitLabs connected on the Environment page. A GitLab connection is used by
// the platform's organization (and its operators), as the GitHub App's
// installations are before an operator gives one to another organization.

// GitLabStore adapts the store's connections to the gitlab package.
type GitLabStore struct{ Store *store.Store }

// GitLabConnection is one connection, gitlab.ErrNotConnected when none.
func (g GitLabStore) GitLabConnection(ctx context.Context, id int64) (*gitlab.Connection, error) {
	c, err := g.Store.GitLabConnection(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, gitlab.ErrNotConnected
	}
	if err != nil {
		return nil, err
	}
	return toGitLab(c), nil
}

// GitLabConnections lists every connection.
func (g GitLabStore) GitLabConnections(ctx context.Context) ([]*gitlab.Connection, error) {
	list, err := g.Store.GitLabConnections(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*gitlab.Connection, len(list))
	for i, c := range list {
		out[i] = toGitLab(c)
	}
	return out, nil
}

func toGitLab(c *store.GitLabConnection) *gitlab.Connection {
	return &gitlab.Connection{ID: c.ID, BaseURL: c.BaseURL, ApplicationID: c.ApplicationID, Username: c.Username}
}

// GitLabSecrets keeps each GitLab connection's secret in a Kubernetes
// Secret of its own (rendimiento-gitlab-<id>), as SecretCredentials keeps
// the GitHub App's.
type GitLabSecrets struct {
	Client    client.Client
	Namespace string
}

func gitlabSecretName(id int64) string { return fmt.Sprintf("rendimiento-gitlab-%d", id) }

// Load is the connection's secret, nil when it has none.
func (g GitLabSecrets) Load(ctx context.Context, id int64) (*gitlab.Secret, error) {
	var sec corev1.Secret
	err := g.Client.Get(ctx, client.ObjectKey{Namespace: g.Namespace, Name: gitlabSecretName(id)}, &sec)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s gitlab.Secret
	if err := json.Unmarshal(sec.Data[credentialsKey], &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save creates or replaces it.
func (g GitLabSecrets) Save(ctx context.Context, id int64, s *gitlab.Secret) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gitlabSecretName(id), Namespace: g.Namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "rendimiento"}},
		Data:       map[string][]byte{credentialsKey: raw},
	}
	err = g.Client.Create(ctx, sec)
	if apierrors.IsAlreadyExists(err) {
		var old corev1.Secret
		if err := g.Client.Get(ctx, client.ObjectKeyFromObject(sec), &old); err != nil {
			return err
		}
		old.Data = sec.Data
		return g.Client.Update(ctx, &old)
	}
	return err
}

// forjaView is the Environment page's «Where your code lives».
type forjaView struct {
	GitHub struct {
		Configured bool   `json:"configured"`
		Slug       string `json:"slug,omitempty"`
		HTMLURL    string `json:"htmlUrl,omitempty"`
		Apps       int    `json:"apps"`
	} `json:"github"`
	GitLab []gitlabView `json:"gitlab"`
	// Callback is the redirect URI a GitLab OAuth application needs, and
	// Webhook where GitLab delivers.
	Callback string `json:"callback"`
	Webhook  string `json:"webhook"`
}

type gitlabView struct {
	*store.GitLabConnection
	Host       string `json:"host"`
	Authorized bool   `json:"authorized"`
	Apps       int    `json:"apps"`
}

// listForjas is GET /api/forjas: the forjas connected here.
func (s *Server) listForjas(w http.ResponseWriter, r *http.Request, _ string) {
	var v forjaView
	v.Callback, v.Webhook = s.BaseURL+gitlab.CallbackPath, s.BaseURL+gitlab.WebhookPath
	if app, err := s.GitHub.Get(); err == nil {
		c := app.Credentials()
		v.GitHub.Configured, v.GitHub.Slug, v.GitHub.HTMLURL = true, c.Slug, c.HTMLURL
	}
	apps, err := s.Store.ListApps(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	counts := map[string]int{}
	for _, a := range apps {
		if a.ForjaOf() == store.ForjaGitHub {
			v.GitHub.Apps++
		} else {
			counts[fmt.Sprint(a.InstallationID)]++
		}
	}
	list, err := s.Store.GitLabConnections(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v.GitLab = []gitlabView{}
	for _, c := range list {
		gv := gitlabView{GitLabConnection: c, Host: gitlab.Host(c.BaseURL), Authorized: c.Username != "", Apps: counts[fmt.Sprint(c.ID)]}
		v.GitLab = append(v.GitLab, gv)
	}
	writeJSON(w, v)
}

// gitlabReady answers 503 when this platform has no GitLab forja.
func (s *Server) gitlabReady(w http.ResponseWriter) bool {
	if s.GitLab == nil || s.GitLab.Keys == nil {
		httpError(w, http.StatusServiceUnavailable, i18n.M("GitLab cannot be connected on this platform"))
		return false
	}
	return true
}

// The cookie that carries, through GitLab's page, what the person went
// there for: "<state>|<what>|<connection>", what being connect, login or
// link.
const gitlabStateCookie = "gitlab_state"

// toGitLab sets the state cookie and answers where the browser goes.
func (s *Server) sendToGitLab(w http.ResponseWriter, c *store.GitLabConnection, what, scope string) string {
	state := randomToken()
	http.SetCookie(w, &http.Cookie{Name: gitlabStateCookie, Value: state + "|" + what + "|" + strconv.FormatInt(c.ID, 10),
		Path: "/api", MaxAge: 600, HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode})
	return gitlab.AuthorizeURLFor(toGitLab(c), state, s.BaseURL+gitlab.CallbackPath, scope)
}

// connectGitLab is POST /api/forjas/gitlab: an administrator gives a
// GitLab's address and their OAuth application (id and secret); it is kept,
// and the answer says where to authorize it.
func (s *Server) connectGitLab(w http.ResponseWriter, r *http.Request, login string) {
	if !s.gitlabReady(w) {
		return
	}
	var req struct {
		BaseURL           string `json:"baseUrl"`
		ApplicationID     string `json:"applicationId"`
		ApplicationSecret string `json:"applicationSecret"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	base, err := gitlab.NormalizeURL(req.BaseURL)
	if err != nil {
		httpError(w, http.StatusBadRequest, i18n.M("%s is not a GitLab address like https://git.example.com", req.BaseURL))
		return
	}
	req.ApplicationID, req.ApplicationSecret = strings.TrimSpace(req.ApplicationID), strings.TrimSpace(req.ApplicationSecret)
	if req.ApplicationID == "" || len(req.ApplicationID) > 200 || strings.ContainsAny(req.ApplicationID, " \t\n") {
		httpError(w, http.StatusBadRequest, i18n.M("the OAuth application's id is missing"))
		return
	}
	c, err := s.Store.AddGitLabConnection(r.Context(), base, req.ApplicationID, login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	old, err := s.GitLab.Keys.Load(r.Context(), c.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sec := gitlab.Secret{ApplicationSecret: req.ApplicationSecret, WebhookToken: randomToken()}
	if old != nil {
		// The webhooks already made keep working: their token stays.
		sec.WebhookToken = old.WebhookToken
		if sec.ApplicationSecret == "" {
			sec.ApplicationSecret = old.ApplicationSecret
		}
		sec.AccessToken, sec.RefreshToken, sec.Expires = old.AccessToken, old.RefreshToken, old.Expires
	}
	if sec.ApplicationSecret == "" {
		httpError(w, http.StatusBadRequest, i18n.M("the OAuth application's secret is missing"))
		return
	}
	if err := s.GitLab.Keys.Save(r.Context(), c.ID, &sec); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.GitLab.Forget(c.ID)
	s.Log.Info("GitLab connection kept", "id", c.ID, "base", base, "by", login)
	writeJSONStatus(w, http.StatusCreated, map[string]any{"connection": c, "authorizeUrl": s.sendToGitLab(w, c, "connect", gitlab.ScopeConnect)})
}

// gitlabConnection is the connection {id} of the URL; nil means the answer
// was already written.
func (s *Server) gitlabConnection(w http.ResponseWriter, r *http.Request) *store.GitLabConnection {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad connection id")
		return nil
	}
	c, err := s.Store.GitLabConnection(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, i18n.M("no such GitLab connection"))
		return nil
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil
	}
	return c
}

// reauthorizeGitLab is POST /api/forjas/gitlab/{id}/authorize: authorize
// the connection again (another person, or a token GitLab revoked).
func (s *Server) reauthorizeGitLab(w http.ResponseWriter, r *http.Request, _ string) {
	if !s.gitlabReady(w) {
		return
	}
	if c := s.gitlabConnection(w, r); c != nil {
		writeJSON(w, map[string]string{"authorizeUrl": s.sendToGitLab(w, c, "connect", gitlab.ScopeConnect)})
	}
}

// deleteGitLab is DELETE /api/forjas/gitlab/{id}: forget a connection no
// app uses.
func (s *Server) deleteGitLab(w http.ResponseWriter, r *http.Request, login string) {
	c := s.gitlabConnection(w, r)
	if c == nil {
		return
	}
	apps, err := s.Store.ListApps(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, a := range apps {
		if a.ForjaOf() == gitlab.Name && a.InstallationID == c.ID {
			httpError(w, http.StatusConflict, i18n.M("%s still builds from this GitLab: delete or disconnect its apps first", a.Name))
			return
		}
	}
	if err := s.Store.DeleteGitLabConnection(r.Context(), c.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.GitLab != nil {
		s.GitLab.Forget(c.ID)
	}
	s.Log.Info("GitLab connection removed", "id", c.ID, "base", c.BaseURL, "by", login)
	w.WriteHeader(http.StatusNoContent)
}

// gitlabLogin is GET /api/auth/gitlab/{id}: "Sign in with GitLab".
func (s *Server) gitlabLogin(w http.ResponseWriter, r *http.Request) {
	if !s.gitlabReady(w) {
		return
	}
	if c := s.gitlabConnection(w, r); c != nil {
		http.Redirect(w, r, s.sendToGitLab(w, c, "login", gitlab.ScopeSignIn), http.StatusFound)
	}
}

// linkGitLab is POST /api/me/identities/gitlab/{id}: the person links
// their account on that GitLab to themselves; the answer says where to
// authorize it.
func (s *Server) linkGitLab(w http.ResponseWriter, r *http.Request, login string) {
	if !s.gitlabReady(w) {
		return
	}
	if store.IsDemoLogin(login) {
		httpError(w, http.StatusForbidden, i18n.M("a demo link's guest links no accounts"))
		return
	}
	if c := s.gitlabConnection(w, r); c != nil {
		writeJSON(w, map[string]string{"authorizeUrl": s.sendToGitLab(w, c, "link", gitlab.ScopeSignIn)})
	}
}

// myIdentities is GET /api/me/identities: the person's linked accounts,
// and the GitLabs they could link one of.
func (s *Server) myIdentities(w http.ResponseWriter, r *http.Request, login string) {
	ids, err := s.Store.Identities(r.Context(), login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	list, err := s.Store.GitLabConnections(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type option struct {
		ID   int64  `json:"id"`
		Host string `json:"host"`
	}
	opts := []option{}
	for _, c := range list {
		opts = append(opts, option{c.ID, gitlab.Host(c.BaseURL)})
	}
	writeJSON(w, map[string]any{"login": login, "identities": ids, "gitlab": opts})
}

// unlinkIdentity is DELETE /api/me/identities: one of the person's own
// accounts is no longer them (?forja, ?baseUrl, ?externalId).
func (s *Server) unlinkIdentity(w http.ResponseWriter, r *http.Request, login string) {
	q := r.URL.Query()
	if err := s.Store.UnlinkIdentity(r.Context(), login, q.Get("forja"), q.Get("baseUrl"), q.Get("externalId")); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// gitlabCallback is where GitLab sends people back: the state is checked
// against the cookie, the code traded for the account, and then what they
// went for is done. Connecting keeps the tokens (rendimiento acts with
// them); signing in opens a session as the person the account is linked to,
// or as user@host when it is linked to nobody (an administrator adds that
// name to Members); linking ties the account to who is signed in.
func (s *Server) gitlabCallback(w http.ResponseWriter, r *http.Request) {
	if !s.gitlabReady(w) {
		return
	}
	ck, err := r.Cookie(gitlabStateCookie)
	parts := []string{}
	if err == nil {
		parts = strings.Split(ck.Value, "|")
	}
	if len(parts) != 3 || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(r.URL.Query().Get("state"))) != 1 {
		httpError(w, http.StatusBadRequest, "login state mismatch; try again")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: gitlabStateCookie, Value: "", Path: "/api", MaxAge: -1})
	what := parts[1]
	if r.URL.Query().Get("error") != "" {
		// The person said no on GitLab's page.
		http.Redirect(w, r, map[bool]string{true: "/environment", false: "/"}[what == "connect"], http.StatusFound)
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad connection id")
		return
	}
	c, err := s.Store.GitLabConnection(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, i18n.M("no such GitLab connection"))
		return
	}
	sec, err := s.GitLab.Keys.Load(r.Context(), c.ID)
	if err != nil || sec == nil {
		httpError(w, http.StatusServiceUnavailable, i18n.M("GitLab cannot be connected on this platform"))
		return
	}
	user, tokens, err := s.GitLab.Exchange(r.Context(), toGitLab(c), sec.ApplicationSecret, r.URL.Query().Get("code"), s.BaseURL+gitlab.CallbackPath)
	if err != nil {
		s.Log.Warn("GitLab sign-in failed", "base", c.BaseURL, "err", err)
		httpError(w, http.StatusBadGateway, i18n.M("GitLab did not accept the sign-in: %s", gitlabSays(err)))
		return
	}
	switch what {
	case "connect":
		// Only an administrator of the platform connects: who started it
		// must still be signed in as one.
		login, ok := s.sessionLogin(r)
		if role, err := s.Store.MemberRole(r.Context(), store.DefaultOrg, login); !ok || err != nil || role != store.MemberAdmin {
			httpError(w, http.StatusForbidden, "this belongs to the platform's operators")
			return
		}
		if err := s.GitLab.Authorized(r.Context(), c.ID, tokens); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.Store.SetGitLabUsername(r.Context(), c.ID, user.Username)
		s.Log.Info("GitLab authorized", "base", c.BaseURL, "as", user.Username, "by", login)
		http.Redirect(w, r, "/environment#forjas", http.StatusFound)
	case "link":
		login, ok := s.sessionLogin(r)
		if !ok {
			httpError(w, http.StatusUnauthorized, "session expired; sign in again")
			return
		}
		err := s.Store.LinkIdentity(r.Context(), store.Identity{Login: login, Forja: gitlab.Name, BaseURL: c.BaseURL, ExternalID: strconv.FormatInt(user.ID, 10), Username: user.Username})
		if errors.Is(err, store.ErrIdentityTaken) {
			httpError(w, http.StatusConflict, i18n.M("that GitLab account is already linked to another person"))
			return
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.Log.Info("account linked", "login", login, "gitlab", user.Username, "base", c.BaseURL)
		http.Redirect(w, r, "/cuentas", http.StatusFound)
	case "login":
		login, err := s.Store.IdentityLogin(r.Context(), gitlab.Name, c.BaseURL, strconv.FormatInt(user.ID, 10))
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if login == "" {
			login = gitlab.Login(toGitLab(c), user.Username)
		}
		s.openSession(w, r, login)
	default:
		httpError(w, http.StatusBadRequest, "login state mismatch; try again")
	}
}

// gitlabSays is GitLab's own sentence for refusing, when it gave one.
func gitlabSays(err error) string {
	var ae *gitlab.APIError
	if errors.As(err, &ae) {
		if says := ae.Says(); says != "" {
			return says
		}
	}
	return err.Error()
}

// sessionLogin is who the request's session belongs to.
func (s *Server) sessionLogin(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	login, _, err := s.Store.SessionInfo(r.Context(), hashToken(c.Value))
	return login, err == nil && login != ""
}

// gitlabProjects is GET /api/forjas/gitlab/{id}/projects: the projects the
// New app wizard offers from that GitLab.
func (s *Server) gitlabProjects(w http.ResponseWriter, r *http.Request, login string) {
	if !s.gitlabReady(w) || !s.gitlabAllowed(w, r, login) {
		return
	}
	c := s.gitlabConnection(w, r)
	if c == nil {
		return
	}
	list, err := s.GitLab.Projects(r.Context(), c.ID)
	if err != nil {
		httpError(w, http.StatusBadGateway, gitlabSays(err))
		return
	}
	writeJSON(w, list)
}

// gitlabAllowed answers 403 where the organization may not use GitLab
// connections (any other than the platform's, for now).
func (s *Server) gitlabAllowed(w http.ResponseWriter, r *http.Request, login string) bool {
	if !s.platformWide(r, login) {
		httpError(w, http.StatusForbidden, i18n.M("this organization cannot use the platform's GitLab connections yet"))
		return false
	}
	return true
}

// gitlabRepoAllowed checks a GitLab app's connection and repository before
// proposing or creating it; false means the answer was written.
func (s *Server) gitlabRepoAllowed(w http.ResponseWriter, r *http.Request, login string, id int64, repo string) bool {
	if !s.gitlabReady(w) || !s.gitlabAllowed(w, r, login) {
		return false
	}
	c, err := s.Store.GitLabConnection(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusForbidden, i18n.M("no such GitLab connection"))
		return false
	}
	if !strings.HasPrefix(repo, gitlab.Host(c.BaseURL)+"/") {
		httpError(w, http.StatusForbidden, i18n.M("that repository is not on this GitLab"))
		return false
	}
	return true
}
