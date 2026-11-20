package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/bitbucket"
	"github.com/rendimiento-ai/rendimiento/internal/gitlab"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Each name signs in with its own forja: no "@" is GitHub's, @bitbucket.org
// Bitbucket's, any other host a GitLab's; linked accounts add theirs.
func TestClaveDeLogin(t *testing.T) {
	for login, want := range map[string]string{
		"ana":                 claveGitHub,
		"ana@bitbucket.org":   claveBitbucket,
		"ana@Bitbucket.org":   claveBitbucket,
		"ana@gitlab.com":      "gitlab:gitlab.com",
		"ana@git.example.com": "gitlab:git.example.com",
	} {
		if got := claveDeLogin(login); got != want {
			t.Errorf("%s: %s, want %s", login, got, want)
		}
	}
	if got := claveGitLab("https://Git.Example.com/gitlab"); got != "gitlab:git.example.com" {
		t.Errorf("a GitLab with a path: %s", got)
	}
	if got := claveDeIdentidad(store.Identity{Forja: gitlab.Name, BaseURL: "https://gitlab.com"}); got != "gitlab:gitlab.com" {
		t.Errorf("a linked GitLab account: %s", got)
	}
	if got := claveDeIdentidad(store.Identity{Forja: bitbucket.Name, BaseURL: bitbucket.IdentityBase}); got != claveBitbucket {
		t.Errorf("a linked Bitbucket account: %s", got)
	}
}

// Taking a forja away: who signs in only with it loses access (and the
// administrators among them are named apart); someone with another account
// that still works keeps it, and someone who could not sign in before is not
// counted.
func TestPierdenAcceso(t *testing.T) {
	accesos := []store.Acceso{
		{Login: "p0dxD", Admin: true},
		{Login: "ana@gitlab.com", Admin: true},
		{Login: "luis@gitlab.com", Identities: []store.Identity{{Forja: bitbucket.Name, BaseURL: bitbucket.IdentityBase}}},
		{Login: "marta@gitlab.com"},
		{Login: "bea", Identities: []store.Identity{{Forja: gitlab.Name, BaseURL: "https://gitlab.com"}}},
		{Login: "sol@git.example.com", Admin: true}, // that GitLab is not connected: she never could
	}
	antes := map[string]bool{claveGitHub: true, "gitlab:gitlab.com": true, claveBitbucket: true}
	despues := map[string]bool{claveGitHub: true, claveBitbucket: true}
	miembros, admins := pierdenAcceso(accesos, antes, despues)
	if !reflect.DeepEqual(miembros, []string{"ana@gitlab.com", "marta@gitlab.com"}) || !reflect.DeepEqual(admins, []string{"ana@gitlab.com"}) {
		t.Fatalf("without gitlab.com: %v, admins %v", miembros, admins)
	}
	// Without GitHub, p0dxD (an administrator with only his GitHub
	// account) and bea's own name go; bea still has her GitLab account.
	miembros, admins = pierdenAcceso(accesos, antes, map[string]bool{"gitlab:gitlab.com": true, claveBitbucket: true})
	if !reflect.DeepEqual(miembros, []string{"p0dxD"}) || !reflect.DeepEqual(admins, []string{"p0dxD"}) {
		t.Fatalf("without GitHub: %v, admins %v", miembros, admins)
	}
	if m, a := pierdenAcceso(accesos, antes, antes); len(m) != 0 || len(a) != 0 {
		t.Fatalf("nothing taken away: %v %v", m, a)
	}
}

// cookieOf is the cookie an answer set, or nil.
func cookieOf(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name && c.MaxAge >= 0 {
			return c
		}
	}
	return nil
}

// A fresh platform installed with GitLab: the setup token opens an install
// session (nothing works without it); the GitLab is connected and
// authorized from the setup page; whoever authorized it comes back signed
// in as the first administrator, though ALLOWED_USERS does not name them;
// then every door of the setup page is closed. Decision 2: the last forja
// an administrator signs in with cannot be removed.
func TestInstallWithGitLab(t *testing.T) {
	e := newEnv(t, false)
	gl := gitlabDePrueba(t)
	e.s.GitLab = &gitlab.Forja{Connections: GitLabStore{Store: e.store}, Keys: GitLabSecrets{Client: e.kube, Namespace: "rendimiento-system"}, BaseURL: e.s.BaseURL}
	connect := `{"baseUrl":"` + gl.URL + `","applicationId":"app","applicationSecret":"secreto"}`

	var st struct {
		Configured bool `json:"configured"`
		Install    *struct {
			Session   bool `json:"session"`
			CanGitLab bool `json:"canGitLab"`
		} `json:"install"`
	}
	_, body := gitlabCall(t, e, "GET", "/api/setup/status", "")
	if json.Unmarshal([]byte(body), &st) != nil || st.Configured || st.Install == nil || st.Install.Session || !st.Install.CanGitLab {
		t.Fatalf("a fresh platform: %s", body)
	}
	if r, _ := gitlabCall(t, e, "POST", "/api/setup/gitlab", connect); r.StatusCode != 403 {
		t.Fatalf("without the setup token: %d", r.StatusCode)
	}
	if r, _ := gitlabCall(t, e, "POST", "/api/setup/token", `{"token":"setup-12"}`); r.StatusCode != 403 || cookieOf(r, installCookie) != nil {
		t.Fatalf("a wrong token: %d", r.StatusCode)
	}
	forged := &http.Cookie{Name: installCookie, Value: "abc.9999999999.00"}
	if r, _ := gitlabCall(t, e, "POST", "/api/setup/gitlab", connect, forged); r.StatusCode != 403 {
		t.Fatalf("a made-up install session: %d", r.StatusCode)
	}
	r, _ := gitlabCall(t, e, "POST", "/api/setup/token", `{"token":"setup-123"}`)
	inst := cookieOf(r, installCookie)
	if r.StatusCode != 204 || inst == nil || strings.Contains(inst.Value, "setup-123") || !inst.HttpOnly {
		t.Fatalf("the setup token: %d %+v", r.StatusCode, inst)
	}
	if _, body := gitlabCall(t, e, "GET", "/api/setup/status", "", inst); !strings.Contains(body, `"session":true`) {
		t.Fatalf("the install session: %s", body)
	}

	res, body := gitlabCall(t, e, "POST", "/api/setup/gitlab", connect, inst)
	if res.StatusCode != 201 || strings.Contains(body, "secreto") {
		t.Fatalf("connect from the setup page: %d %s", res.StatusCode, body)
	}
	ck, state := stateOf(t, res)
	callback := gitlab.CallbackPath + "?state=" + state + "&code=de-ana"
	// The OAuth answer is bound to this browser's install session: without
	// it, or with another one, nobody becomes an administrator.
	if r, _ := gitlabCall(t, e, "GET", callback, "", ck); r.StatusCode != 403 {
		t.Fatalf("without the install session: %d", r.StatusCode)
	}
	r, _ = gitlabCall(t, e, "POST", "/api/setup/token", `{"token":"setup-123"}`)
	if r, _ := gitlabCall(t, e, "GET", callback, "", ck, cookieOf(r, installCookie)); r.StatusCode != 403 {
		t.Fatalf("another install session: %d", r.StatusCode)
	}
	r, _ = gitlabCall(t, e, "GET", callback, "", ck, inst)
	sess := cookieOf(r, sessionCookie)
	if r.StatusCode != 302 || r.Header.Get("Location") != "/instalacion" || sess == nil {
		t.Fatalf("authorized from the setup page: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	var out struct {
		Connection store.GitLabConnection `json:"connection"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	c, _ := e.store.GitLabConnection(t.Context(), out.Connection.ID)
	ana := gitlab.Login(toGitLab(c), "ana")
	if c.Username != "ana" {
		t.Fatalf("who rendimiento acts as: %+v", c)
	}
	if role, err := e.store.MemberRole(t.Context(), store.DefaultOrg, ana); err != nil || role != store.MemberAdmin {
		t.Fatalf("%s should be the first administrator: %q %v", ana, role, err)
	}
	if _, body := gitlabCall(t, e, "GET", "/api/me", "", sess); !strings.Contains(body, ana) {
		t.Fatalf("signed in: %s", body)
	}

	// Installed: the setup page's doors are closed, even with the token.
	if _, body := gitlabCall(t, e, "GET", "/api/setup/status", ""); !strings.Contains(body, `"configured":true`) || strings.Contains(body, `"install"`) {
		t.Fatalf("installed: %s", body)
	}
	for _, try := range []struct{ method, path, body string }{
		{"POST", "/api/setup/token", `{"token":"setup-123"}`},
		{"POST", "/api/setup/gitlab", connect},
		{"POST", "/api/setup/bitbucket", `{}`},
		{"GET", "/api/setup/github?token=setup-123", ""},
	} {
		if r, _ := gitlabCall(t, e, try.method, try.path, try.body, inst); r.StatusCode != 404 {
			t.Errorf("%s %s once installed: %d", try.method, try.path, r.StatusCode)
		}
	}
	if r, _ := gitlabCall(t, e, "GET", callback, "", ck, inst); r.StatusCode != 403 && r.StatusCode != 404 {
		t.Fatalf("the callback again once installed: %d", r.StatusCode)
	}

	// Decision 2: ana signs in only with this GitLab.
	id := strconv.FormatInt(c.ID, 10)
	if _, body := gitlabCall(t, e, "GET", "/api/forjas/gitlab/"+id+"/acceso", "", sess); !strings.Contains(body, `"admins":["`+ana+`"]`) {
		t.Fatalf("who would lose access: %s", body)
	}
	if r, body := gitlabCall(t, e, "DELETE", "/api/forjas/gitlab/"+id, "", sess); r.StatusCode != 409 || !strings.Contains(body, ana) {
		t.Fatalf("removing an administrator's last forja: %d %s", r.StatusCode, body)
	}
	if _, err := e.store.GitLabConnection(t.Context(), c.ID); err != nil {
		t.Fatalf("the GitLab must stay: %v", err)
	}
}

// With GitHub, the App's setup works with the install session, and only
// the browser that started it may hand rendimiento an App: the manifest's
// state comes back with GitHub's code. An administrator connects GitHub
// later from the Environment page, without the setup token.
func TestInstallWithGitHub(t *testing.T) {
	e := newEnv(t, false)
	r, _ := gitlabCall(t, e, "POST", "/api/setup/token", `{"token":"setup-123"}`)
	inst := cookieOf(r, installCookie)
	r, body := gitlabCall(t, e, "GET", "/api/setup/github", "", inst)
	if r.StatusCode != 200 || !strings.Contains(body, "https://github.com/settings/apps/new?state=") || cookieOf(r, setupStateCookie) == nil {
		t.Fatalf("the manifest with the install session: %d %s", r.StatusCode, body)
	}
	if r, _ := gitlabCall(t, e, "GET", "/api/setup/github/callback?code=ajeno&state=otro", "", inst, cookieOf(r, setupStateCookie)); r.StatusCode != 400 {
		t.Fatalf("someone else's code: %d", r.StatusCode)
	}
	if r, _ := gitlabCall(t, e, "GET", "/api/setup/github/callback?code=ajeno", "", inst); r.StatusCode != 400 {
		t.Fatalf("a code without the state: %d", r.StatusCode)
	}
	me := e.session(t, "p0dxD")
	if r, _ := gitlabCall(t, e, "GET", "/api/setup/github", "", me); r.StatusCode != 200 {
		t.Fatalf("an administrator connects GitHub: %d", r.StatusCode)
	}
	if r, _ := gitlabCall(t, e, "GET", "/api/installations", "", me); r.StatusCode != 200 {
		t.Fatalf("New app without GitHub: %d", r.StatusCode)
	}
}
