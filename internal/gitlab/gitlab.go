// Package gitlab is GitLab's forja: gitlab.com or a GitLab of one's own,
// through its REST API (v4).
//
// A person connects a GitLab once, from the Environment page: its address
// and an OAuth application of theirs (its id and secret). They authorize it
// on GitLab, and rendimiento keeps the tokens it got and acts with them
// (refreshing them as they expire, every two hours). On each app it creates
// from there, rendimiento adds the project's webhook, with a secret token
// of the connection's that each delivery carries in X-Gitlab-Token.
//
// Inside rendimiento, a GitLab app's InstallationID is its connection's id
// and its Repo starts with the connection's address without the scheme:
// git.example.com/group/project. So two forjas never mix their
// repositories, and the address says which GitLab to ask.
package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// Name is the forja's short name, as apps.forja says it.
const Name = "gitlab"

// The scopes rendimiento asks for: api to read code, write statuses, open
// and merge merge requests and add webhooks; read_user to know who
// authorized it. Signing in asks only for read_user.
const (
	ScopeConnect = "api read_user"
	ScopeSignIn  = "read_user"
)

// Secret is what a connection keeps out of the database: the OAuth
// application's secret, the webhooks' token, and the tokens rendimiento
// acts with.
type Secret struct {
	ApplicationSecret string    `json:"application_secret"`
	WebhookToken      string    `json:"webhook_token"`
	AccessToken       string    `json:"access_token,omitempty"`
	RefreshToken      string    `json:"refresh_token,omitempty"`
	Expires           time.Time `json:"expires,omitempty"`
}

// Keys keeps each connection's Secret: in a Kubernetes Secret, or sealed
// in the Caja (see api.GitLabSecrets and secretos.GitLab).
type Keys interface {
	// Load is the connection's secret, nil when it has none.
	Load(ctx context.Context, id int64) (*Secret, error)
	// Save creates or replaces it.
	Save(ctx context.Context, id int64, s *Secret) error
}

// Connection is one GitLab rendimiento works with (a row of
// gitlab_connections, store.GitLabConnection).
type Connection struct {
	ID            int64
	BaseURL       string // https://gitlab.com
	ApplicationID string // the OAuth application's id
	Username      string // who rendimiento acts as ("" until authorized)
}

// Connections is where the connections are listed (the store): a
// connection that is not there is ErrNotConnected.
type Connections interface {
	GitLabConnection(ctx context.Context, id int64) (*Connection, error)
	GitLabConnections(ctx context.Context) ([]*Connection, error)
}

// ErrNotConnected is a GitLab app without its connection.
var ErrNotConnected = errors.New("GitLab is not connected yet: connect it on the Environment page")

// Forja is GitLab's forja, for every connection.
type Forja struct {
	Connections Connections
	Keys        Keys
	// BaseURL is rendimiento's own address: GitLab sends people back to
	// BaseURL + CallbackPath, and webhooks to BaseURL + WebhookPath.
	BaseURL string
	HTTP    *http.Client

	mu sync.Mutex
	// secrets caches each connection's secret, so a token is not read
	// from Kubernetes on every call.
	secrets map[int64]*Secret
}

// CallbackPath is where GitLab sends people back after authorizing (the
// OAuth application's redirect URI), and WebhookPath where it delivers.
const (
	CallbackPath = "/api/forjas/gitlab/callback"
	WebhookPath  = "/api/webhooks/gitlab"
)

var _ forja.Forja = (*Forja)(nil)

// Name is the forja's short name.
func (f *Forja) Name() string { return Name }

// Ready is nil once a GitLab is connected.
func (f *Forja) Ready() error {
	if f == nil || f.Connections == nil {
		return ErrNotConnected
	}
	list, err := f.Connections.GitLabConnections(context.Background())
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return ErrNotConnected
	}
	return nil
}

// Allows: a GitLab is connected on purpose, and only its projects that an
// administrator made apps of are built, so every account counts.
func (f *Forja) Allows(string) bool { return true }

// client is the HTTP client (30 seconds at most per request by default).
func (f *Forja) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Host is the connection's address without its scheme, the start of its
// apps' Repo: "gitlab.com", "git.example.com:8443/gitlab".
func Host(baseURL string) string {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Host == "" {
		return strings.TrimRight(baseURL, "/")
	}
	return u.Host + strings.TrimRight(u.Path, "/")
}

// NormalizeURL checks a GitLab's address and writes it one way:
// https://git.example.com, without a trailing slash. Plain http is only
// for an address on this network (a name without dots, .lan, .local,
// .internal, localhost or an IP), where there may be no certificate.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "https://gitlab.com", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q is not a GitLab address like https://git.example.com", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !local(u.Hostname()) {
			return "", fmt.Errorf("%s: a GitLab on the internet is reached with https", raw)
		}
	default:
		return "", fmt.Errorf("%q is not a GitLab address like https://git.example.com", raw)
	}
	return u.Scheme + "://" + strings.ToLower(u.Host) + strings.TrimRight(u.Path, "/"), nil
}

// local says whether a host name is on this network.
func local(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || !strings.Contains(host, ".") || strings.Count(host, ":") > 1 {
		return true
	}
	for _, s := range []string{".lan", ".local", ".internal", ".home.arpa"} {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return strings.Trim(host, "0123456789.") == ""
}

// connection is a connection and its secret.
func (f *Forja) connection(ctx context.Context, id int64) (*Connection, *Secret, error) {
	if f == nil || f.Connections == nil {
		return nil, nil, ErrNotConnected
	}
	c, err := f.Connections.GitLabConnection(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	sec, err := f.secret(ctx, id, false)
	if err != nil {
		return nil, nil, err
	}
	return c, sec, nil
}

// secret is the connection's secret, cached; fresh reads it again.
func (f *Forja) secret(ctx context.Context, id int64, fresh bool) (*Secret, error) {
	f.mu.Lock()
	cached := f.secrets[id]
	f.mu.Unlock()
	if cached != nil && !fresh {
		return cached, nil
	}
	if f.Keys == nil {
		return nil, ErrNotConnected
	}
	sec, err := f.Keys.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	if sec == nil {
		return nil, ErrNotConnected
	}
	f.remember(id, sec)
	return sec, nil
}

// remember caches a connection's secret.
func (f *Forja) remember(id int64, sec *Secret) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.secrets == nil {
		f.secrets = map[int64]*Secret{}
	}
	f.secrets[id] = sec
}

// Forget drops a connection's cached secret (it was connected again or
// removed).
func (f *Forja) Forget(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.secrets, id)
}

// project is the project's path on its GitLab: repo without the
// connection's address.
func project(c *Connection, repo string) string {
	return strings.TrimPrefix(repo, Host(c.BaseURL)+"/")
}

// RepoOf is an app's Repo for a project of the connection.
func RepoOf(c *Connection, path string) string {
	return Host(c.BaseURL) + "/" + strings.Trim(path, "/")
}

// CloneURL is where build pods clone a GitLab app's repository from.
func (f *Forja) CloneURL(ctx context.Context, installation int64, repo string) (string, error) {
	if f == nil || f.Connections == nil {
		return "", ErrNotConnected
	}
	c, err := f.Connections.GitLabConnection(ctx, installation)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(c.BaseURL, "/") + "/" + project(c, repo) + ".git", nil
}
