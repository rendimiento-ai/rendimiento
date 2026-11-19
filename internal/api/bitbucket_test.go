package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/bitbucket"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A Bitbucket as rendimiento sees it: the OAuth token for the codes
// "codigo" (the operator connecting) and "de-ana" (Ana signing in), its
// users, and one repository of the workspace «tienda».
func bitbucketDePrueba(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/site/oauth2/access_token":
			_ = r.ParseForm()
			if key, secret, _ := r.BasicAuth(); key != "llave" || secret != "secreto" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"tok-`+r.PostForm.Get("code")+`","refresh_token":"r","expires_in":7200}`)
		case r.URL.Path == "/2.0/user" && r.Header.Get("Authorization") == "Bearer tok-codigo":
			_, _ = io.WriteString(w, `{"account_id":"1","nickname":"operadora"}`)
		case r.URL.Path == "/2.0/user" && r.Header.Get("Authorization") == "Bearer tok-de-ana":
			_, _ = io.WriteString(w, `{"account_id":"42","nickname":"ana"}`)
		case r.URL.Path == "/2.0/repositories/tienda":
			_, _ = io.WriteString(w, `{"values":[{"slug":"catalogo","full_name":"tienda/catalogo","is_private":true,"mainbranch":{"name":"main"}}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// bitbucketStateOf is the bitbucket_state cookie an answer set, and the
// state Bitbucket would send back.
func bitbucketStateOf(t *testing.T, res *http.Response) (*http.Cookie, string) {
	t.Helper()
	for _, c := range res.Cookies() {
		if c.Name == bitbucketStateCookie {
			return c, strings.Split(c.Value, "|")[0]
		}
	}
	t.Fatal("no state cookie")
	return nil, ""
}

// An operator connects a workspace and authorizes it; its webhooks are
// heard only when signed with its secret; the wizard lists its
// repositories; a person links their Bitbucket account and signs in with
// it as themselves, and an account linked to no one comes in only as a
// member named nickname@bitbucket.org.
func TestBitbucketConnection(t *testing.T) {
	e := newEnv(t, true)
	bb := bitbucketDePrueba(t)
	e.s.Bitbucket = &bitbucket.Forja{Connections: BitbucketStore{Store: e.store}, Keys: BitbucketSecrets{Client: e.kube, Namespace: "rendimiento-system"},
		BaseURL: e.s.BaseURL, APIURL: bb.URL + "/2.0", SiteURL: bb.URL}
	me := e.session(t, "p0dxD")

	if r, _ := gitlabCall(t, e, "POST", "/api/forjas/bitbucket", `{"workspace":"no es","consumerKey":"llave","consumerSecret":"secreto"}`, me); r.StatusCode != 400 {
		t.Fatalf("a bad workspace: %d", r.StatusCode)
	}
	res, body := gitlabCall(t, e, "POST", "/api/forjas/bitbucket", `{"workspace":"https://bitbucket.org/Tienda/","consumerKey":"llave","consumerSecret":"secreto"}`, me)
	if res.StatusCode != 201 {
		t.Fatalf("connect: %d %s", res.StatusCode, body)
	}
	var out struct {
		Connection   store.BitbucketConnection `json:"connection"`
		AuthorizeURL string                    `json:"authorizeUrl"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	if out.Connection.Workspace != "tienda" {
		t.Fatalf("the workspace: %+v", out.Connection)
	}
	if u, _ := url.Parse(out.AuthorizeURL); u == nil || u.Path != "/site/oauth2/authorize" || u.Query().Get("client_id") != "llave" {
		t.Fatalf("authorize URL: %s", out.AuthorizeURL)
	}
	if strings.Contains(body, "secreto") {
		t.Fatal("the consumer's secret is never answered")
	}
	ck, state := bitbucketStateOf(t, res)
	if r, _ := gitlabCall(t, e, "GET", bitbucket.CallbackPath+"?state=otro&code=codigo", "", ck, me); r.StatusCode != 400 {
		t.Fatalf("another state: %d", r.StatusCode)
	}
	if r, b := gitlabCall(t, e, "GET", bitbucket.CallbackPath+"?state="+state+"&code=codigo", "", ck, me); r.StatusCode != 302 || r.Header.Get("Location") != "/environment#forjas" {
		t.Fatalf("authorized: %d %s", r.StatusCode, b)
	}
	if c, _ := e.store.BitbucketConnection(t.Context(), out.Connection.ID); c.Username != "operadora" {
		t.Fatalf("who rendimiento acts as: %+v", c)
	}
	if _, b := gitlabCall(t, e, "GET", "/api/forjas", "", me); !strings.Contains(b, `"workspace":"tienda"`) || !strings.Contains(b, `"bitbucketCallback":"`+e.s.BaseURL+bitbucket.CallbackPath) || strings.Contains(b, "tok-codigo") {
		t.Fatalf("forjas: %s", b)
	}

	// Webhooks: only a delivery signed with the workspace's secret is heard.
	push := `{"repository":{"full_name":"tienda/catalogo"},"push":{"changes":[{"new":{"type":"branch","name":"main","target":{"hash":"abc"}}}]}}`
	deliver := func(sig string) int {
		req, _ := http.NewRequest("POST", e.srv.URL+bitbucket.WebhookPath, strings.NewReader(push))
		req.Header.Set("X-Hub-Signature", sig)
		req.Header.Set("X-Event-Key", "repo:push")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if c := deliver(bitbucket.Sign("otra", []byte(push))); c != 401 {
		t.Fatalf("a wrong signature: %d", c)
	}
	sec, _ := e.s.Bitbucket.Keys.Load(t.Context(), out.Connection.ID)
	if c := deliver(bitbucket.Sign(sec.WebhookSecret, []byte(push))); c != 200 {
		t.Fatalf("its signature: %d", c)
	}

	// The wizard's repositories, with bitbucket.org in front.
	if _, b := gitlabCall(t, e, "GET", "/api/forjas/bitbucket/1/repos", "", me); !strings.Contains(b, `"repo":"bitbucket.org/tienda/catalogo"`) {
		t.Fatalf("repositories: %s", b)
	}
	// A repository of another workspace is not this connection's.
	if r, _ := gitlabCall(t, e, "POST", "/api/propose", `{"forja":"bitbucket","installation":1,"repo":"bitbucket.org/otro/catalogo","branch":"main"}`, me); r.StatusCode != 403 {
		t.Fatalf("another workspace's repository: %d", r.StatusCode)
	}

	// Ana links her Bitbucket account and signs in with it as herself.
	if err := e.store.SetMember(t.Context(), store.DefaultOrg, "ana-bb", store.MemberGuest, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	ana := e.session(t, "ana-bb")
	res, _ = gitlabCall(t, e, "POST", "/api/me/identities/bitbucket/1", "", ana)
	ck, state = bitbucketStateOf(t, res)
	if r, b := gitlabCall(t, e, "GET", bitbucket.CallbackPath+"?state="+state+"&code=de-ana", "", ck, ana); r.StatusCode != 302 {
		t.Fatalf("linked: %d %s", r.StatusCode, b)
	}
	res, _ = gitlabCall(t, e, "GET", "/api/auth/bitbucket/1", "")
	ck, state = bitbucketStateOf(t, res)
	r, _ := gitlabCall(t, e, "GET", bitbucket.CallbackPath+"?state="+state+"&code=de-ana", "", ck)
	var sess *http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == sessionCookie {
			sess = c
		}
	}
	if r.StatusCode != 302 || sess == nil {
		t.Fatalf("sign in with Bitbucket: %d", r.StatusCode)
	}
	if _, b := gitlabCall(t, e, "GET", "/api/me", "", sess); !strings.Contains(b, `"login":"ana-bb"`) {
		t.Fatalf("signed in as: %s", b)
	}
	if _, b := gitlabCall(t, e, "GET", "/api/me/identities", "", ana); !strings.Contains(b, `"username":"ana"`) || !strings.Contains(b, `"bitbucket":[{"id":1,"host":"tienda"}]`) {
		t.Fatalf("identities: %s", b)
	}

	// Unlinked, the account is ana@bitbucket.org, a member only once added.
	if r, _ := gitlabCall(t, e, "DELETE", "/api/me/identities?forja=bitbucket&baseUrl="+url.QueryEscape(bitbucket.IdentityBase)+"&externalId=42", "", ana); r.StatusCode != 204 {
		t.Fatalf("unlink: %d", r.StatusCode)
	}
	res, _ = gitlabCall(t, e, "GET", "/api/auth/bitbucket/1", "")
	ck, state = bitbucketStateOf(t, res)
	if r, b := gitlabCall(t, e, "GET", bitbucket.CallbackPath+"?state="+state+"&code=de-ana", "", ck); r.StatusCode != 403 || !strings.Contains(b, "ana@bitbucket.org") {
		t.Fatalf("an account linked to no one: %d %s", r.StatusCode, b)
	}
	if _, b := gitlabCall(t, e, "GET", "/api/setup/status", ""); !strings.Contains(b, `"forja":"bitbucket"`) {
		t.Fatalf("sign-in options: %s", b)
	}
}
