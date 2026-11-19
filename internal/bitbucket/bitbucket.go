// Package bitbucket is Bitbucket Cloud's forja, through its REST API 2.0
// (Bitbucket Data Center comes later).
//
// A person connects a Bitbucket workspace once, from the Environment page:
// its slug and an OAuth consumer of the workspace (its key and secret).
// They authorize it on Bitbucket, and rendimiento keeps the tokens it got
// and acts with them, refreshing them as they expire (every two hours). On
// each app it creates from there, rendimiento adds the repository's
// webhook with a secret of the connection's: Bitbucket signs each delivery
// with it (HMAC-SHA256, in X-Hub-Signature).
//
// Inside rendimiento, a Bitbucket app's InstallationID is its connection's
// id and its Repo is bitbucket.org/<workspace>/<repository>. So Bitbucket's
// repositories never mix with GitHub's or a GitLab's.
package bitbucket

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// Name is the forja's short name, as apps.forja says it.
const Name = "bitbucket"

// Host starts every Bitbucket app's Repo: bitbucket.org/workspace/repo.
const Host = "bitbucket.org"

// The addresses of Bitbucket Cloud.
const (
	DefaultAPIURL  = "https://api.bitbucket.org/2.0"
	DefaultSiteURL = "https://bitbucket.org"
)

// Secret is what a connection keeps out of the database: the OAuth
// consumer's secret, the webhooks' secret, and the tokens rendimiento acts
// with.
type Secret struct {
	ConsumerSecret string    `json:"consumer_secret"`
	WebhookSecret  string    `json:"webhook_secret"`
	AccessToken    string    `json:"access_token,omitempty"`
	RefreshToken   string    `json:"refresh_token,omitempty"`
	Expires        time.Time `json:"expires,omitempty"`
}

// Keys keeps each connection's Secret: in a Kubernetes Secret, or sealed
// in the Caja (see api.BitbucketSecrets and secretos.Bitbucket).
type Keys interface {
	// Load is the connection's secret, nil when it has none.
	Load(ctx context.Context, id int64) (*Secret, error)
	// Save creates or replaces it.
	Save(ctx context.Context, id int64, s *Secret) error
}

// Connection is one workspace rendimiento works with (a row of
// bitbucket_connections, store.BitbucketConnection).
type Connection struct {
	ID          int64
	Workspace   string // the workspace's slug
	ConsumerKey string // the OAuth consumer's key
	Username    string // who rendimiento acts as ("" until authorized)
}

// Connections is where the connections are listed (the store): a
// connection that is not there is ErrNotConnected.
type Connections interface {
	BitbucketConnection(ctx context.Context, id int64) (*Connection, error)
	BitbucketConnections(ctx context.Context) ([]*Connection, error)
}

// ErrNotConnected is a Bitbucket app without its connection.
var ErrNotConnected = errors.New("Bitbucket is not connected yet: connect it on the Environment page")

// Forja is Bitbucket Cloud's forja, for every workspace connected.
type Forja struct {
	Connections Connections
	Keys        Keys
	// BaseURL is rendimiento's own address: Bitbucket sends people back to
	// BaseURL + CallbackPath (the consumer's callback URL), and webhooks to
	// BaseURL + WebhookPath.
	BaseURL string
	// APIURL and SiteURL are Bitbucket's (DefaultAPIURL and DefaultSiteURL
	// when empty; tests point them at a fake).
	APIURL, SiteURL string
	HTTP            *http.Client

	mu sync.Mutex
	// secrets caches each connection's secret, so a token is not read
	// from Kubernetes on every call.
	secrets map[int64]*Secret
}

// CallbackPath is where Bitbucket sends people back after authorizing (the
// consumer's callback URL), and WebhookPath where it delivers.
const (
	CallbackPath = "/api/forjas/bitbucket/callback"
	WebhookPath  = "/api/webhooks/bitbucket"
)

var _ forja.Forja = (*Forja)(nil)

// Name is the forja's short name.
func (f *Forja) Name() string { return Name }

// Ready is nil once a workspace is connected.
func (f *Forja) Ready() error {
	if f == nil || f.Connections == nil {
		return ErrNotConnected
	}
	list, err := f.Connections.BitbucketConnections(context.Background())
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return ErrNotConnected
	}
	return nil
}

// Allows: a workspace is connected on purpose, and only its repositories
// that an administrator made apps of are built, so every account counts.
func (f *Forja) Allows(string) bool { return true }

// client is the HTTP client (30 seconds at most per request by default).
func (f *Forja) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (f *Forja) apiURL() string {
	if f.APIURL != "" {
		return strings.TrimRight(f.APIURL, "/")
	}
	return DefaultAPIURL
}

func (f *Forja) siteURL() string {
	if f.SiteURL != "" {
		return strings.TrimRight(f.SiteURL, "/")
	}
	return DefaultSiteURL
}

// workspaceSlug is what a workspace's slug may be: Bitbucket's own rule
// (letters, digits, dashes and underscores, lower case), with a dot too.
var workspaceSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,61}$`)

// NormalizeWorkspace checks a workspace's slug and writes it one way: the
// person may paste its address (https://bitbucket.org/tienda/) too.
func NormalizeWorkspace(raw string) (string, bool) {
	ws := strings.ToLower(strings.TrimSpace(raw))
	for _, p := range []string{"https://", "http://", "bitbucket.org/", "www."} {
		ws = strings.TrimPrefix(ws, p)
	}
	ws = strings.TrimPrefix(ws, "bitbucket.org/")
	ws = strings.Trim(ws, "/")
	if i := strings.Index(ws, "/"); i >= 0 {
		ws = ws[:i]
	}
	return ws, workspaceSlug.MatchString(ws)
}

// connection is a connection and its secret.
func (f *Forja) connection(ctx context.Context, id int64) (*Connection, *Secret, error) {
	if f == nil || f.Connections == nil {
		return nil, nil, ErrNotConnected
	}
	c, err := f.Connections.BitbucketConnection(ctx, id)
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
	f.cache(id, sec)
}

// cache is remember with f.mu already held.
func (f *Forja) cache(id int64, sec *Secret) {
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

// fullName is the repository's workspace/slug: repo without bitbucket.org/.
func fullName(repo string) string { return strings.TrimPrefix(repo, Host+"/") }

// RepoOf is an app's Repo for a repository of Bitbucket (workspace/slug).
func RepoOf(full string) string { return Host + "/" + strings.Trim(full, "/") }

// InWorkspace says whether repo is one of the connection's workspace.
func InWorkspace(c *Connection, repo string) bool {
	ws, rest, ok := strings.Cut(fullName(repo), "/")
	return ok && strings.HasPrefix(repo, Host+"/") && strings.EqualFold(ws, c.Workspace) && rest != "" && !strings.Contains(rest, "/")
}

// CloneURL is where build pods clone a Bitbucket app's repository from.
func (f *Forja) CloneURL(_ context.Context, _ int64, repo string) (string, error) {
	return f.siteURL() + "/" + fullName(repo) + ".git", nil
}
