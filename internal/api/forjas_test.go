package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/gitlab"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A GitLab as rendimiento sees it: the OAuth token for the codes "codigo"
// (the operator connecting) and "de-ana" (Ana signing in), its user, and
// one project.
func gitlabDePrueba(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth/token":
			_ = r.ParseForm()
			if r.Form.Get("client_secret") != "secreto" || r.Form.Get("redirect_uri") != "https://rendimiento.example/api/forjas/gitlab/callback" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"tok-`+r.Form.Get("code")+`","refresh_token":"r","expires_in":7200}`)
		case r.URL.Path == "/api/v4/user" && r.Header.Get("Authorization") == "Bearer tok-codigo":
			_, _ = io.WriteString(w, `{"id":1,"username":"operadora"}`)
		case r.URL.Path == "/api/v4/user" && r.Header.Get("Authorization") == "Bearer tok-de-ana":
			_, _ = io.WriteString(w, `{"id":42,"username":"ana"}`)
		case r.URL.Path == "/api/v4/projects":
			_, _ = io.WriteString(w, `[{"id":5,"path_with_namespace":"tienda/catalogo","default_branch":"main","visibility":"private"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// gitlabCall makes a request with the cookies given, without following
// redirects.
func gitlabCall(t *testing.T, e *testEnv, method, path, body string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

// stateOf is the gitlab_state cookie an answer set, and the state GitLab
// would send back.
func stateOf(t *testing.T, res *http.Response) (*http.Cookie, string) {
	t.Helper()
	for _, c := range res.Cookies() {
		if c.Name == gitlabStateCookie {
			return c, strings.Split(c.Value, "|")[0]
		}
	}
	t.Fatal("no state cookie")
	return nil, ""
}

// An operator connects a GitLab and authorizes it; its webhooks are heard
// only with its token; the wizard lists its projects; a person links their
// GitLab account and signs in with it as themselves, and an account linked
// to no one comes in only as a member named user@host.
func TestGitLabConnection(t *testing.T) {
	e := newEnv(t, true)
	gl := gitlabDePrueba(t)
	e.s.GitLab = &gitlab.Forja{Connections: GitLabStore{Store: e.store}, Keys: GitLabSecrets{Client: e.kube, Namespace: "rendimiento-system"}, BaseURL: e.s.BaseURL}
	me := e.session(t, "p0dxD")

	if r, _ := gitlabCall(t, e, "POST", "/api/forjas/gitlab", `{"baseUrl":"ftp://x","applicationId":"app","applicationSecret":"secreto"}`, me); r.StatusCode != 400 {
		t.Fatalf("a bad address: %d", r.StatusCode)
	}
	res, body := gitlabCall(t, e, "POST", "/api/forjas/gitlab", `{"baseUrl":"`+gl.URL+`","applicationId":"app","applicationSecret":"secreto"}`, me)
	if res.StatusCode != 201 {
		t.Fatalf("connect: %d %s", res.StatusCode, body)
	}
	var out struct {
		Connection   store.GitLabConnection `json:"connection"`
		AuthorizeURL string                 `json:"authorizeUrl"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	if u, _ := url.Parse(out.AuthorizeURL); u == nil || u.Query().Get("scope") != gitlab.ScopeConnect || u.Query().Get("client_id") != "app" {
		t.Fatalf("authorize URL: %s", out.AuthorizeURL)
	}
	if strings.Contains(body, "secreto") {
		t.Fatal("the application's secret is never answered")
	}
	ck, state := stateOf(t, res)
	if r, _ := gitlabCall(t, e, "GET", gitlab.CallbackPath+"?state=otro&code=codigo", "", ck, me); r.StatusCode != 400 {
		t.Fatalf("another state: %d", r.StatusCode)
	}
	if r, b := gitlabCall(t, e, "GET", gitlab.CallbackPath+"?state="+state+"&code=codigo", "", ck, me); r.StatusCode != 302 || r.Header.Get("Location") != "/environment#forjas" {
		t.Fatalf("authorized: %d %s", r.StatusCode, b)
	}
	if c, _ := e.store.GitLabConnection(t.Context(), out.Connection.ID); c.Username != "operadora" {
		t.Fatalf("who rendimiento acts as: %+v", c)
	}
	if _, b := gitlabCall(t, e, "GET", "/api/forjas", "", me); !strings.Contains(b, `"authorized":true`) || strings.Contains(b, "tok-codigo") {
		t.Fatalf("forjas: %s", b)
	}

	// Webhooks: only the connection's token is heard.
	push := `{"object_kind":"push","ref":"refs/heads/main","after":"abc","project":{"path_with_namespace":"tienda/catalogo"}}`
	req, _ := http.NewRequest("POST", e.srv.URL+gitlab.WebhookPath, strings.NewReader(push))
	req.Header.Set("X-Gitlab-Token", "no-es")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 401 {
		t.Fatalf("a wrong token: %d", r.StatusCode)
	}
	sec, _ := e.s.GitLab.Keys.Load(t.Context(), out.Connection.ID)
	req, _ = http.NewRequest("POST", e.srv.URL+gitlab.WebhookPath, strings.NewReader(push))
	req.Header.Set("X-Gitlab-Token", sec.WebhookToken)
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 200 {
		t.Fatalf("its token: %d", r.StatusCode)
	}

	// The wizard's projects, with their GitLab's address in front.
	host := gitlab.Host(gl.URL)
	if _, b := gitlabCall(t, e, "GET", "/api/forjas/gitlab/1/projects", "", me); !strings.Contains(b, `"repo":"`+host+`/tienda/catalogo"`) {
		t.Fatalf("projects: %s", b)
	}

	// Ana links her GitLab account and signs in with it as herself.
	if err := e.store.SetMember(t.Context(), store.DefaultOrg, "ana-gh", store.MemberGuest, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	ana := e.session(t, "ana-gh")
	res, _ = gitlabCall(t, e, "POST", "/api/me/identities/gitlab/1", "", ana)
	ck, state = stateOf(t, res)
	if r, b := gitlabCall(t, e, "GET", gitlab.CallbackPath+"?state="+state+"&code=de-ana", "", ck, ana); r.StatusCode != 302 {
		t.Fatalf("linked: %d %s", r.StatusCode, b)
	}
	res, _ = gitlabCall(t, e, "GET", "/api/auth/gitlab/1", "")
	ck, state = stateOf(t, res)
	r, _ := gitlabCall(t, e, "GET", gitlab.CallbackPath+"?state="+state+"&code=de-ana", "", ck)
	var sess *http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == sessionCookie {
			sess = c
		}
	}
	if r.StatusCode != 302 || sess == nil {
		t.Fatalf("sign in with GitLab: %d", r.StatusCode)
	}
	if _, b := gitlabCall(t, e, "GET", "/api/me", "", sess); !strings.Contains(b, `"login":"ana-gh"`) {
		t.Fatalf("signed in as: %s", b)
	}

	// Unlinked, the account is ana@host, a member only once added.
	if _, b := gitlabCall(t, e, "GET", "/api/me/identities", "", ana); !strings.Contains(b, `"username":"ana"`) {
		t.Fatalf("identities: %s", b)
	}
	if r, _ := gitlabCall(t, e, "DELETE", "/api/me/identities?forja=gitlab&baseUrl="+url.QueryEscape(gl.URL)+"&externalId=42", "", ana); r.StatusCode != 204 {
		t.Fatalf("unlink: %d", r.StatusCode)
	}
	res, _ = gitlabCall(t, e, "GET", "/api/auth/gitlab/1", "")
	ck, state = stateOf(t, res)
	if r, b := gitlabCall(t, e, "GET", gitlab.CallbackPath+"?state="+state+"&code=de-ana", "", ck); r.StatusCode != 403 || !strings.Contains(b, "ana@"+strings.TrimPrefix(gl.URL, "http://")) {
		t.Fatalf("an account linked to no one: %d %s", r.StatusCode, b)
	}
}
