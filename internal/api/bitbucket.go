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

	"github.com/rendimiento-ai/rendimiento/internal/bitbucket"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Bitbucket Cloud beside GitHub and GitLab (Ruta 102, step 3): a workspace
// connected on the Environment page, with an OAuth consumer of the
// workspace. As with GitLab, its connections are the platform's
// organization's (and its operators').

// BitbucketStore adapts the store's connections to the bitbucket package.
type BitbucketStore struct{ Store *store.Store }

// BitbucketConnection is one connection, bitbucket.ErrNotConnected when none.
func (b BitbucketStore) BitbucketConnection(ctx context.Context, id int64) (*bitbucket.Connection, error) {
	c, err := b.Store.BitbucketConnection(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, bitbucket.ErrNotConnected
	}
	if err != nil {
		return nil, err
	}
	return toBitbucket(c), nil
}

// BitbucketConnections lists every connection.
func (b BitbucketStore) BitbucketConnections(ctx context.Context) ([]*bitbucket.Connection, error) {
	list, err := b.Store.BitbucketConnections(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*bitbucket.Connection, len(list))
	for i, c := range list {
		out[i] = toBitbucket(c)
	}
	return out, nil
}

// LockToken holds the connection's token refresh for this process alone
// (see store.LockToken).
func (b BitbucketStore) LockToken(ctx context.Context, id int64) (func(), error) {
	return b.Store.LockToken(ctx, bitbucket.Name, id)
}

func toBitbucket(c *store.BitbucketConnection) *bitbucket.Connection {
	return &bitbucket.Connection{ID: c.ID, Workspace: c.Workspace, ConsumerKey: c.ConsumerKey, Username: c.Username}
}

// BitbucketSecrets keeps each Bitbucket connection's secret in a
// Kubernetes Secret of its own (rendimiento-bitbucket-<id>), as
// GitLabSecrets does.
type BitbucketSecrets struct {
	Client    client.Client
	Namespace string
}

func bitbucketSecretName(id int64) string { return fmt.Sprintf("rendimiento-bitbucket-%d", id) }

// Load is the connection's secret, nil when it has none.
func (b BitbucketSecrets) Load(ctx context.Context, id int64) (*bitbucket.Secret, error) {
	var sec corev1.Secret
	err := b.Client.Get(ctx, client.ObjectKey{Namespace: b.Namespace, Name: bitbucketSecretName(id)}, &sec)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s bitbucket.Secret
	if err := json.Unmarshal(sec.Data[credentialsKey], &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save creates or replaces it.
func (b BitbucketSecrets) Save(ctx context.Context, id int64, s *bitbucket.Secret) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: bitbucketSecretName(id), Namespace: b.Namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "rendimiento"}},
		Data:       map[string][]byte{credentialsKey: raw},
	}
	err = b.Client.Create(ctx, sec)
	if apierrors.IsAlreadyExists(err) {
		var old corev1.Secret
		if err := b.Client.Get(ctx, client.ObjectKeyFromObject(sec), &old); err != nil {
			return err
		}
		old.Data = sec.Data
		return b.Client.Update(ctx, &old)
	}
	return err
}

// bitbucketView is a workspace on the Environment page.
type bitbucketView struct {
	*store.BitbucketConnection
	Authorized bool `json:"authorized"`
	Apps       int  `json:"apps"`
}

// bitbucketViews lists the workspaces for listForjas; counts are the apps
// of each connection, by forja and id.
func (s *Server) bitbucketViews(ctx context.Context, counts map[string]int) ([]bitbucketView, error) {
	list, err := s.Store.BitbucketConnections(ctx)
	if err != nil {
		return nil, err
	}
	out := []bitbucketView{}
	for _, c := range list {
		out = append(out, bitbucketView{BitbucketConnection: c, Authorized: c.Username != "", Apps: counts[bitbucket.Name+"/"+strconv.FormatInt(c.ID, 10)]})
	}
	return out, nil
}

// bitbucketReady answers 503 when this platform has no Bitbucket forja.
func (s *Server) bitbucketReady(w http.ResponseWriter) bool {
	if s.Bitbucket == nil || s.Bitbucket.Keys == nil {
		httpError(w, http.StatusServiceUnavailable, i18n.M("Bitbucket cannot be connected on this platform"))
		return false
	}
	return true
}

// The cookie that carries, through Bitbucket's page, what the person went
// there for: "<state>|<what>|<connection>", what being connect, login or
// link.
const bitbucketStateCookie = "bitbucket_state"

// sendToBitbucket sets the state cookie and answers where the browser goes.
func (s *Server) sendToBitbucket(w http.ResponseWriter, c *store.BitbucketConnection, what string) string {
	state := randomToken()
	http.SetCookie(w, &http.Cookie{Name: bitbucketStateCookie, Value: state + "|" + what + "|" + strconv.FormatInt(c.ID, 10),
		Path: "/api", MaxAge: 600, HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode})
	return s.Bitbucket.AuthorizeURLFor(toBitbucket(c), state)
}

// connectBitbucket is POST /api/forjas/bitbucket: an administrator gives a
// workspace and its OAuth consumer (key and secret); it is kept, and the
// answer says where to authorize it.
func (s *Server) connectBitbucket(w http.ResponseWriter, r *http.Request, login string) {
	if !s.bitbucketReady(w) {
		return
	}
	var req struct {
		Workspace      string `json:"workspace"`
		ConsumerKey    string `json:"consumerKey"`
		ConsumerSecret string `json:"consumerSecret"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ws, ok := bitbucket.NormalizeWorkspace(req.Workspace)
	if !ok {
		httpError(w, http.StatusBadRequest, i18n.M("%s is not a Bitbucket workspace like bitbucket.org/my-team", req.Workspace))
		return
	}
	req.ConsumerKey, req.ConsumerSecret = strings.TrimSpace(req.ConsumerKey), strings.TrimSpace(req.ConsumerSecret)
	if req.ConsumerKey == "" || len(req.ConsumerKey) > 200 || strings.ContainsAny(req.ConsumerKey, " \t\n") {
		httpError(w, http.StatusBadRequest, i18n.M("the OAuth consumer's key is missing"))
		return
	}
	c, err := s.Store.AddBitbucketConnection(r.Context(), ws, req.ConsumerKey, login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	old, err := s.Bitbucket.Keys.Load(r.Context(), c.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sec := bitbucket.Secret{ConsumerSecret: req.ConsumerSecret, WebhookSecret: randomToken()}
	if old != nil {
		// The webhooks already made keep working: their secret stays.
		sec.WebhookSecret = old.WebhookSecret
		if sec.ConsumerSecret == "" {
			sec.ConsumerSecret = old.ConsumerSecret
		}
		sec.AccessToken, sec.RefreshToken, sec.Expires = old.AccessToken, old.RefreshToken, old.Expires
	}
	if sec.ConsumerSecret == "" {
		httpError(w, http.StatusBadRequest, i18n.M("the OAuth consumer's secret is missing"))
		return
	}
	if err := s.Bitbucket.Keys.Save(r.Context(), c.ID, &sec); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Bitbucket.Forget(c.ID)
	s.Log.Info("Bitbucket connection kept", "id", c.ID, "workspace", ws, "by", login)
	writeJSONStatus(w, http.StatusCreated, map[string]any{"connection": c, "authorizeUrl": s.sendToBitbucket(w, c, "connect")})
}

// bitbucketConnection is the connection {id} of the URL; nil means the
// answer was already written.
func (s *Server) bitbucketConnection(w http.ResponseWriter, r *http.Request) *store.BitbucketConnection {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad connection id")
		return nil
	}
	c, err := s.Store.BitbucketConnection(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, i18n.M("no such Bitbucket connection"))
		return nil
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil
	}
	return c
}

// reauthorizeBitbucket is POST /api/forjas/bitbucket/{id}/authorize.
func (s *Server) reauthorizeBitbucket(w http.ResponseWriter, r *http.Request, _ string) {
	if !s.bitbucketReady(w) {
		return
	}
	if c := s.bitbucketConnection(w, r); c != nil {
		writeJSON(w, map[string]string{"authorizeUrl": s.sendToBitbucket(w, c, "connect")})
	}
}

// deleteBitbucket is DELETE /api/forjas/bitbucket/{id}: forget a
// connection no app uses.
func (s *Server) deleteBitbucket(w http.ResponseWriter, r *http.Request, login string) {
	c := s.bitbucketConnection(w, r)
	if c == nil {
		return
	}
	apps, err := s.Store.ListApps(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, a := range apps {
		if a.ForjaOf() == bitbucket.Name && a.InstallationID == c.ID {
			httpError(w, http.StatusConflict, i18n.M("%s still builds from this Bitbucket workspace: delete or disconnect its apps first", a.Name))
			return
		}
	}
	if err := s.Store.DeleteBitbucketConnection(r.Context(), c.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Bitbucket != nil {
		s.Bitbucket.Forget(c.ID)
	}
	s.Log.Info("Bitbucket connection removed", "id", c.ID, "workspace", c.Workspace, "by", login)
	w.WriteHeader(http.StatusNoContent)
}

// bitbucketLogin is GET /api/auth/bitbucket/{id}: "Sign in with Bitbucket".
func (s *Server) bitbucketLogin(w http.ResponseWriter, r *http.Request) {
	if !s.bitbucketReady(w) {
		return
	}
	if c := s.bitbucketConnection(w, r); c != nil {
		http.Redirect(w, r, s.sendToBitbucket(w, c, "login"), http.StatusFound)
	}
}

// linkBitbucket is POST /api/me/identities/bitbucket/{id}: the person
// links their Bitbucket account to themselves.
func (s *Server) linkBitbucket(w http.ResponseWriter, r *http.Request, login string) {
	if !s.bitbucketReady(w) {
		return
	}
	if store.IsDemoLogin(login) {
		httpError(w, http.StatusForbidden, i18n.M("a demo link's guest links no accounts"))
		return
	}
	if c := s.bitbucketConnection(w, r); c != nil {
		writeJSON(w, map[string]string{"authorizeUrl": s.sendToBitbucket(w, c, "link")})
	}
}

// bitbucketCallback is where Bitbucket sends people back (the consumer's
// callback URL), as gitlabCallback does for GitLab.
func (s *Server) bitbucketCallback(w http.ResponseWriter, r *http.Request) {
	if !s.bitbucketReady(w) {
		return
	}
	ck, err := r.Cookie(bitbucketStateCookie)
	parts := []string{}
	if err == nil {
		parts = strings.Split(ck.Value, "|")
	}
	if len(parts) != 3 || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(r.URL.Query().Get("state"))) != 1 {
		httpError(w, http.StatusBadRequest, "login state mismatch; try again")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: bitbucketStateCookie, Value: "", Path: "/api", MaxAge: -1})
	what := parts[1]
	if r.URL.Query().Get("error") != "" {
		// The person said no on Bitbucket's page.
		http.Redirect(w, r, map[bool]string{true: "/environment", false: "/"}[what == "connect"], http.StatusFound)
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad connection id")
		return
	}
	c, err := s.Store.BitbucketConnection(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, i18n.M("no such Bitbucket connection"))
		return
	}
	sec, err := s.Bitbucket.Keys.Load(r.Context(), c.ID)
	if err != nil || sec == nil {
		httpError(w, http.StatusServiceUnavailable, i18n.M("Bitbucket cannot be connected on this platform"))
		return
	}
	user, tokens, err := s.Bitbucket.Exchange(r.Context(), toBitbucket(c), sec.ConsumerSecret, r.URL.Query().Get("code"))
	if err != nil {
		s.Log.Warn("Bitbucket sign-in failed", "workspace", c.Workspace, "err", err)
		httpError(w, http.StatusBadGateway, i18n.M("Bitbucket did not accept the sign-in: %s", bitbucketSays(err)))
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
		if err := s.Bitbucket.Authorized(r.Context(), c.ID, tokens); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.Store.SetBitbucketUsername(r.Context(), c.ID, user.Name())
		s.Log.Info("Bitbucket authorized", "workspace", c.Workspace, "as", user.Name(), "by", login)
		http.Redirect(w, r, "/environment#forjas", http.StatusFound)
	case "link":
		login, ok := s.sessionLogin(r)
		if !ok {
			httpError(w, http.StatusUnauthorized, "session expired; sign in again")
			return
		}
		err := s.Store.LinkIdentity(r.Context(), store.Identity{Login: login, Forja: bitbucket.Name, BaseURL: bitbucket.IdentityBase, ExternalID: user.ID(), Username: user.Name()})
		if errors.Is(err, store.ErrIdentityTaken) {
			httpError(w, http.StatusConflict, i18n.M("that Bitbucket account is already linked to another person"))
			return
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.Log.Info("account linked", "login", login, "bitbucket", user.Name())
		http.Redirect(w, r, "/cuentas", http.StatusFound)
	case "login":
		login, err := s.Store.IdentityLogin(r.Context(), bitbucket.Name, bitbucket.IdentityBase, user.ID())
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if login == "" {
			login = bitbucket.Login(user)
		}
		s.openSession(w, r, login)
	default:
		httpError(w, http.StatusBadRequest, "login state mismatch; try again")
	}
}

// bitbucketSays is Bitbucket's own sentence for refusing, when it gave one.
func bitbucketSays(err error) string {
	var ae *bitbucket.APIError
	if errors.As(err, &ae) {
		if says := ae.Says(); says != "" {
			return says
		}
	}
	return err.Error()
}

// bitbucketWebhook receives the workspaces' events: a delivery no
// connection's secret signed is refused.
func (s *Server) bitbucketWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Bitbucket == nil {
		httpError(w, http.StatusServiceUnavailable, bitbucket.ErrNotConnected.Error())
		return
	}
	s.webhookFrom(w, r, s.Bitbucket)
}

// bitbucketRepos is GET /api/forjas/bitbucket/{id}/repos: the repositories
// the New app wizard offers from that workspace.
func (s *Server) bitbucketRepos(w http.ResponseWriter, r *http.Request, login string) {
	if !s.bitbucketReady(w) || !s.bitbucketAllowed(w, r, login) {
		return
	}
	c := s.bitbucketConnection(w, r)
	if c == nil {
		return
	}
	list, err := s.Bitbucket.Repositories(r.Context(), c.ID)
	if err != nil {
		httpError(w, http.StatusBadGateway, bitbucketSays(err))
		return
	}
	writeJSON(w, list)
}

// bitbucketAllowed answers 403 where the organization may not use the
// platform's Bitbucket connections (any other than the platform's, for
// now).
func (s *Server) bitbucketAllowed(w http.ResponseWriter, r *http.Request, login string) bool {
	if !s.platformWide(r, login) {
		httpError(w, http.StatusForbidden, i18n.M("this organization cannot use the platform's Bitbucket connections yet"))
		return false
	}
	return true
}

// bitbucketRepoAllowed checks a Bitbucket app's connection and repository
// before proposing or creating it; false means the answer was written.
func (s *Server) bitbucketRepoAllowed(w http.ResponseWriter, r *http.Request, login string, id int64, repo string) bool {
	if !s.bitbucketReady(w) || !s.bitbucketAllowed(w, r, login) {
		return false
	}
	c, err := s.Store.BitbucketConnection(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusForbidden, i18n.M("no such Bitbucket connection"))
		return false
	}
	if !bitbucket.InWorkspace(toBitbucket(c), repo) {
		httpError(w, http.StatusForbidden, i18n.M("that repository is not in this Bitbucket workspace"))
		return false
	}
	return true
}
