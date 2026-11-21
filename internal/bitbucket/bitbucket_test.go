package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// conexiones is a store with the connections given.
type conexiones []*Connection

func (c conexiones) BitbucketConnection(_ context.Context, id int64) (*Connection, error) {
	for _, x := range c {
		if x.ID == id {
			return x, nil
		}
	}
	return nil, ErrNotConnected
}

func (c conexiones) BitbucketConnections(context.Context) ([]*Connection, error) { return c, nil }

// llavero keeps secrets in memory, and counts how often they were saved.
type llavero struct {
	mu    sync.Mutex
	m     map[int64]Secret
	saves int
}

func (l *llavero) Load(_ context.Context, id int64) (*Secret, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.m[id]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (l *llavero) Save(_ context.Context, id int64, s *Secret) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[id] = *s
	l.saves++
	return nil
}

// A forja with two workspaces (1 «tienda», 2 «taller»), each with its
// webhook secret, against a fake Bitbucket at base (its API under /2.0).
func nueva(base string) (*Forja, *llavero) {
	keys := &llavero{m: map[int64]Secret{
		1: {ConsumerSecret: "secreto-1", WebhookSecret: "firma-1", AccessToken: "acceso-1", RefreshToken: "renueva-1", Expires: time.Now().Add(time.Hour)},
		2: {ConsumerSecret: "secreto-2", WebhookSecret: "firma-2"},
	}}
	return &Forja{
		Connections: conexiones{
			{ID: 1, Workspace: "tienda", ConsumerKey: "llave-1"},
			{ID: 2, Workspace: "taller", ConsumerKey: "llave-2"},
		},
		Keys: keys, BaseURL: "https://rendimiento.example.com", APIURL: base + "/2.0", SiteURL: base,
	}, keys
}

func TestWebhookSignature(t *testing.T) {
	f, _ := nueva("https://bitbucket.example")
	push := []byte(`{"repository":{"full_name":"tienda/catalogo"},"push":{"changes":[{"new":{"type":"branch","name":"main","target":{"hash":"aaa","message":"dos [skip ci]"}},
		"commits":[{"hash":"aaa","message":"dos [skip ci]"},{"hash":"bbb","message":"uno"}]}]}}`)
	for _, tc := range []struct {
		sig  string
		want error
	}{
		{Sign("firma-1", push), nil},
		{Sign("firma-2", push), nil}, // signed by taller's: but the repository is tienda's
		{Sign("otra", push), forja.ErrBadSignature},
		{"", forja.ErrBadSignature},
		{"sha256=", forja.ErrBadSignature},
		{strings.TrimPrefix(Sign("firma-1", push), "sha256="), forja.ErrBadSignature},
	} {
		h := http.Header{"X-Hub-Signature": {tc.sig}, "X-Event-Key": {"repo:push"}}
		ev, err := f.ParseWebhook(h, push)
		if !errors.Is(err, tc.want) {
			t.Fatalf("signature %q: err %v, want %v", tc.sig, err, tc.want)
		}
		if tc.want != nil {
			continue
		}
		if tc.sig == Sign("firma-2", push) {
			if ev.Push != nil {
				t.Fatal("a workspace's secret builds no other workspace's repository")
			}
			continue
		}
		p := ev.Push
		if p == nil || p.Installation != 1 || p.Repo != "bitbucket.org/tienda/catalogo" || p.Branch != "main" || p.SHA != "aaa" || !p.SkipCI ||
			len(p.Messages) != 2 || p.Messages[0] != "uno" || p.Deleted {
			t.Fatalf("push %+v", p)
		}
	}
	if _, err := f.ParseWebhook(http.Header{"X-Hub-Signature": {Sign("firma-1", []byte("{"))}}, []byte("{")); !errors.Is(err, forja.ErrBadPayload) {
		t.Fatalf("bad JSON: %v", err)
	}
	if _, err := (&Forja{Connections: conexiones{}}).ParseWebhook(http.Header{"X-Hub-Signature": {"x"}}, push); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("no connection: %v", err)
	}
}

func TestWebhookEvents(t *testing.T) {
	c := &Connection{ID: 3, Workspace: "tienda"}
	del, _ := parseWebhook(c, "repo:push", []byte(`{"repository":{"full_name":"tienda/a"},"push":{"changes":[{"new":null,"old":{"type":"branch","name":"vieja","target":{"hash":"abc"}}}]}}`))
	if del.Push == nil || !del.Push.Deleted || del.Push.Branch != "vieja" || del.Push.Repo != "bitbucket.org/tienda/a" {
		t.Fatalf("deleted branch: %+v", del.Push)
	}
	tag, _ := parseWebhook(c, "repo:push", []byte(`{"repository":{"full_name":"tienda/a"},"push":{"changes":[{"new":{"type":"tag","name":"v1","target":{"hash":"abc"}}}]}}`))
	if tag.Push != nil || tag.Change != nil || tag.Kind != "repo:push" {
		t.Fatalf("a tag is no branch push: %+v", tag)
	}
	pr := func(kind string) *forja.ChangeEvent {
		ev, err := parseWebhook(c, kind, []byte(`{"repository":{"full_name":"tienda/a"},"pullrequest":{"id":7,"state":"MERGED","updated_on":"2026-11-19T10:00:00.123456+00:00",
			"source":{"branch":{"name":"rama"},"commit":{"hash":"0123456789ab"},"repository":{"full_name":"tienda/a"}},
			"destination":{"branch":{"name":"main"},"repository":{"full_name":"tienda/a"}}}}`))
		if err != nil {
			t.Fatal(err)
		}
		return ev.Change
	}
	if ch := pr("pullrequest:created"); ch == nil || ch.Action != "opened" || ch.Number != 7 || ch.Repo != "bitbucket.org/tienda/a" || ch.HeadRef != "rama" || ch.HeadRepo != ch.Repo {
		t.Fatalf("opened: %+v", ch)
	}
	if ch := pr("pullrequest:fulfilled"); ch.Action != "closed" || !ch.Merged || ch.MergedAt == nil || ch.MergedAt.Hour() != 10 {
		t.Fatalf("merged: %+v", ch)
	}
	if ch := pr("pullrequest:rejected"); ch.Action != "closed" || ch.Merged {
		t.Fatalf("declined: %+v", ch)
	}
	if ch := pr("pullrequest:updated"); ch.Action != "updated" {
		t.Fatalf("updated is ignored: %+v", ch)
	}
	other, _ := parseWebhook(c, "repo:push", []byte(`{"repository":{"full_name":"ajeno/a"},"push":{"changes":[{"new":{"type":"branch","name":"main","target":{"hash":"abc"}}}]}}`))
	if other.Push != nil {
		t.Fatal("a repository of another workspace is never built")
	}
}

func TestStatusState(t *testing.T) {
	for _, tc := range []struct {
		c          forja.Check
		state, has string
	}{
		{forja.Check{Status: "queued"}, "INPROGRESS", ""},
		{forja.Check{Status: "in_progress"}, "INPROGRESS", ""},
		{forja.Check{Status: "completed", Conclusion: "success", Output: forja.CheckOutput("Build succeeded", "")}, "SUCCESSFUL", "Build succeeded"},
		{forja.Check{Status: "completed", Conclusion: "failure"}, "FAILED", ""},
		{forja.Check{Status: "completed", Conclusion: "cancelled"}, "STOPPED", ""},
		{forja.Check{Status: "completed", Conclusion: "action_required"}, "INPROGRESS", "a person must merge it"},
		{forja.Check{Status: "completed", Conclusion: "action_required", Output: forja.CheckOutput("Passed, with paths no step tests", "")}, "INPROGRESS", "no step tests"},
	} {
		state, desc := StatusState(tc.c)
		if state != tc.state || !strings.Contains(desc, tc.has) {
			t.Fatalf("%+v: %s %q", tc.c, state, desc)
		}
	}
	if _, desc := StatusState(forja.Check{Status: "completed", Conclusion: "failure", Output: forja.CheckOutput(strings.Repeat("x", 400), "")}); len([]rune(desc)) != 255 {
		t.Fatalf("a long description is cut: %d", len([]rune(desc)))
	}
	if k := statusKey("rendimiento: catalogo"); k != "rendimiento-catalogo" {
		t.Fatalf("key: %q", k)
	}
	if k := statusKey("rendimiento: " + strings.Repeat("a", 60)); len(k) > 40 || k != statusKey("rendimiento: "+strings.Repeat("a", 60)) {
		t.Fatalf("a long name's key: %q", k)
	}
}

// fakeBitbucket is a Bitbucket with tienda/catalogo: pull request 7 from
// «rama» (head «full») merges, 8 conflicts, and each request is recorded.
type fakeBitbucket struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string]string
	full     string
}

func (b *fakeBitbucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	b.mu.Lock()
	b.requests = append(b.requests, r.Method+" "+r.URL.Path)
	b.bodies[r.Method+" "+r.URL.Path] = string(raw)
	full := b.full
	b.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer acceso-1" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	const repo = "/2.0/repositories/tienda/catalogo"
	pr := func(id int, hash string) string {
		return `{"id":` + map[int]string{7: "7", 8: "8"}[id] + `,"state":"OPEN","title":"t","source":{"branch":{"name":"rama"},"commit":{"hash":"` + hash +
			`"},"repository":{"full_name":"tienda/catalogo"}},"destination":{"branch":{"name":"main"}},"links":{"html":{"href":"https://bitbucket.example/tienda/catalogo/pull-requests/7"}}}`
	}
	switch p := r.URL.Path; {
	case p == repo+"/refs/branches" && r.Method == "GET":
		_, _ = io.WriteString(w, `{"values":[{"name":"main","target":{"hash":"base0000"}},{"name":"rama","target":{"hash":"`+full+`"}}]}`)
	case p == repo+"/refs/branches/main":
		_, _ = io.WriteString(w, `{"name":"main","target":{"hash":"base0000"}}`)
	case p == repo+"/refs/branches/nueva" && r.Method == "DELETE":
		w.WriteHeader(http.StatusNotFound)
	case p == repo+"/pullrequests/7":
		_, _ = io.WriteString(w, pr(7, full[:12]))
	case p == repo+"/pullrequests/8":
		_, _ = io.WriteString(w, pr(8, full[:12]))
	case p == repo+"/pullrequests/7/diffstat":
		_, _ = io.WriteString(w, `{"values":[{"status":"modified","new":{"path":"a.go"}}]}`)
	case p == repo+"/pullrequests/8/diffstat":
		_, _ = io.WriteString(w, `{"values":[{"status":"merge conflict","new":{"path":"a.go"}}]}`)
	case p == repo+"/pullrequests/7/merge":
		_, _ = io.WriteString(w, `{"id":7,"state":"MERGED"}`)
	case p == repo+"/pullrequests" && r.Method == "GET":
		_, _ = io.WriteString(w, `{"values":[`+pr(7, full[:12])+`]}`)
	case p == repo+"/pullrequests" && r.Method == "POST":
		_, _ = io.WriteString(w, `{"id":9,"links":{"html":{"href":"https://bitbucket.example/tienda/catalogo/pull-requests/9"}}}`)
	case p == repo+"/src" && r.Method == "POST":
		w.WriteHeader(http.StatusCreated)
	case strings.HasPrefix(p, repo+"/commit/") && strings.HasSuffix(p, "/statuses/build"):
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{}`)
	case p == repo+"/hooks" && r.Method == "GET":
		_, _ = io.WriteString(w, `{"values":[{"uuid":"{otro}","url":"https://elsewhere.example/hook"}]}`)
	case p == repo+"/hooks" && r.Method == "POST":
		w.WriteHeader(http.StatusCreated)
	case p == "/2.0/repositories/tienda":
		_, _ = io.WriteString(w, `{"values":[{"slug":"catalogo","full_name":"tienda/catalogo","is_private":true,"mainbranch":{"name":"trunk"}}]}`)
	case p == repo+"/src/main/rendimiento.yaml":
		_, _ = io.WriteString(w, "services: []\n")
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (b *fakeBitbucket) did(req string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.requests {
		if r == req {
			return true
		}
	}
	return false
}

func TestStatusesAndPulls(t *testing.T) {
	full := "0123456789abcdef0123456789abcdef01234567"
	fb := &fakeBitbucket{bodies: map[string]string{}, full: full}
	srv := httptest.NewServer(fb)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	ctx := context.Background()
	const repo = "bitbucket.org/tienda/catalogo"
	const api = "/2.0/repositories/tienda/catalogo"

	// A check is a build status, written again by its key.
	id, err := f.CreateCheck(ctx, 1, repo, forja.Check{Name: "rendimiento: catalogo", HeadSHA: full, Status: "in_progress", DetailsURL: "https://rendimiento.example.com/apps/catalogo/runs/1"})
	if err != nil || id == 0 {
		t.Fatalf("create: %d %v", id, err)
	}
	if err := f.UpdateCheck(ctx, 1, repo, id, forja.Check{Name: "rendimiento: catalogo", HeadSHA: full, Status: "completed", Conclusion: "action_required"}); err != nil {
		t.Fatal(err)
	}
	var st map[string]string
	_ = json.Unmarshal([]byte(fb.bodies["POST "+api+"/commit/"+full+"/statuses/build"]), &st)
	if st["key"] != "rendimiento-catalogo" || st["state"] != "INPROGRESS" || !strings.Contains(st["description"], "person must merge") || st["url"] == "" {
		t.Fatalf("status: %+v", st)
	}

	// The cut head of a pull request is made whole.
	pr, err := f.Pull(ctx, 1, repo, 7)
	if err != nil || pr.Head.SHA != full || pr.Mergeable == nil || !*pr.Mergeable || pr.MergeableState != "clean" || pr.State != "open" {
		t.Fatalf("pull 7: %+v %v", pr, err)
	}
	conflict, _ := f.Pull(ctx, 1, repo, 8)
	if conflict.Mergeable == nil || *conflict.Mergeable || conflict.MergeableState != "dirty" {
		t.Fatalf("pull 8: %+v", conflict)
	}
	open, err := f.OpenPulls(ctx, 1, repo)
	if err != nil || len(open) != 1 || open[0].Head.SHA != full || open[0].Head.Repo.FullName != repo {
		t.Fatalf("open pulls: %+v %v", open, err)
	}

	// Merging: only while its head is the commit tested.
	if err := f.MergePull(ctx, 1, repo, 7, "otro"); !errors.Is(err, forja.ErrConflict) {
		t.Fatalf("a head that moved: %v", err)
	}
	if fb.did("POST " + api + "/pullrequests/7/merge") {
		t.Fatal("merged a head that moved")
	}
	if err := f.MergePull(ctx, 1, repo, 7, full); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Onboarding: the files in one commit off main, then the pull request.
	opened, err := f.OpenPR(ctx, 1, repo, "main", "nueva", "Add rendimiento.yaml", "body", map[string]string{"rendimiento.yaml": "services: []\n"})
	if err != nil || opened.Number != 7 {
		t.Fatalf("open PR: %+v %v", opened, err)
	}
	src := fb.bodies["POST "+api+"/src"]
	for _, want := range []string{`name="branch"`, "nueva", `name="parents"`, "base0000", `name="rendimiento.yaml"`, "services: []"} {
		if !strings.Contains(src, want) {
			t.Fatalf("the commit's form lacks %q:\n%s", want, src)
		}
	}
	if n, err := f.CreatePull(ctx, 1, repo, "main", "rama", "t", "b"); err != nil || n != 7 {
		t.Fatalf("an open one is found: %d %v", n, err)
	}

	// The wizard's repositories, and the webhook made for an app.
	list, err := f.Repositories(ctx, 1)
	if err != nil || len(list) != 1 || list[0].Repo != repo || list[0].DefaultBranch != "trunk" || !list[0].Private {
		t.Fatalf("repositories: %+v %v", list, err)
	}
	if err := f.EnsureHook(ctx, 1, repo); err != nil {
		t.Fatal(err)
	}
	var hook struct {
		URL    string   `json:"url"`
		Secret string   `json:"secret"`
		Events []string `json:"events"`
	}
	_ = json.Unmarshal([]byte(fb.bodies["POST "+api+"/hooks"]), &hook)
	if hook.URL != "https://rendimiento.example.com"+WebhookPath || hook.Secret != "firma-1" || len(hook.Events) != 5 {
		t.Fatalf("hook: %+v", hook)
	}
	if b, err := f.FileAt(ctx, 1, repo, "rendimiento.yaml", "main"); err != nil || string(b) != "services: []\n" {
		t.Fatalf("file: %q %v", b, err)
	}
	if _, err := f.FileAt(ctx, 1, repo, "no.yaml", "main"); !forja.IsNotFound(err) {
		t.Fatalf("a missing file: %v", err)
	}
	if tok, err := f.CloneToken(ctx, 1); err != nil || tok != "x-token-auth:acceso-1" {
		t.Fatalf("clone token: %q %v", tok, err)
	}
	if u, _ := f.CloneURL(ctx, 1, repo); u != srv.URL+"/tienda/catalogo.git" {
		t.Fatalf("clone URL: %s", u)
	}
}

// An access token about to expire is refreshed, and the new tokens are
// kept each time.
func TestTokenRefresh(t *testing.T) {
	var grants []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/site/oauth2/access_token":
			user, pass, _ := r.BasicAuth()
			_ = r.ParseForm()
			grants = append(grants, r.PostForm)
			if user != "llave-2" || pass != "secreto-2" || r.PostForm.Get("refresh_token") != "viejo" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Invalid refresh_token"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"nuevo","refresh_token":"otro","expires_in":7200}`)
		case "/2.0/repositories/taller/x/refs/branches/main":
			if r.Header.Get("Authorization") != "Bearer nuevo" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"target":{"hash":"abc"}}`)
		}
	}))
	defer srv.Close()
	f, keys := nueva(srv.URL)
	keys.m[2] = Secret{ConsumerSecret: "secreto-2", WebhookSecret: "firma-2", AccessToken: "caduco", RefreshToken: "viejo", Expires: time.Now().Add(time.Minute)}
	sha, err := f.BranchSHA(context.Background(), 2, "bitbucket.org/taller/x", "main")
	if err != nil || sha != "abc" {
		t.Fatalf("branch: %q %v", sha, err)
	}
	if len(grants) != 1 || grants[0].Get("grant_type") != "refresh_token" {
		t.Fatalf("grants: %v", grants)
	}
	if s := keys.m[2]; s.AccessToken != "nuevo" || s.RefreshToken != "otro" || time.Until(s.Expires) < time.Hour {
		t.Fatalf("kept: %+v", s)
	}
	// Once refreshed, it is not asked again.
	if _, err := f.BranchSHA(context.Background(), 2, "bitbucket.org/taller/x", "main"); err != nil || len(grants) != 1 {
		t.Fatalf("refreshed again: %v %d", err, len(grants))
	}
	// A refresh Bitbucket refuses says so.
	keys.m[2] = Secret{ConsumerSecret: "secreto-2", AccessToken: "caduco", RefreshToken: "revocado"}
	f.Forget(2)
	_, err = f.BranchSHA(context.Background(), 2, "bitbucket.org/taller/x", "main")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Says() != "Invalid refresh_token" {
		t.Fatalf("refused: %v", err)
	}
}

func TestSignIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/site/oauth2/access_token":
			_ = r.ParseForm()
			if r.PostForm.Get("code") != "codigo" || r.PostForm.Get("grant_type") != "authorization_code" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"de-ana","refresh_token":"r","expires_in":7200}`)
		case "/2.0/user":
			if r.Header.Get("Authorization") != "Bearer de-ana" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"uuid":"{abc}","account_id":"557058:1","nickname":"Ana López","display_name":"Ana López"}`)
		}
	}))
	defer srv.Close()
	f, _ := nueva(srv.URL)
	to, err := f.AuthorizeURL("estado", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(to)
	if u.Path != "/site/oauth2/authorize" || u.Query().Get("client_id") != "llave-1" || u.Query().Get("state") != "estado" || u.Query().Get("response_type") != "code" {
		t.Fatalf("authorize URL: %s", to)
	}
	login, err := f.SignIn(context.Background(), "codigo")
	if err != nil || login != "Ana-L-pez@bitbucket.org" {
		t.Fatalf("login: %q %v", login, err)
	}
	if _, err := f.SignIn(context.Background(), "otro"); err == nil {
		t.Fatal("a code Bitbucket refuses signs no one in")
	}
	if (&User{UUID: "{x}", AccountID: "557058:1"}).ID() != "557058:1" || (&User{UUID: "{x}"}).ID() != "{x}" {
		t.Fatal("the account's id")
	}
}

func TestNormalizeWorkspace(t *testing.T) {
	for in, want := range map[string]string{
		"tienda": "tienda", " Tienda ": "tienda", "https://bitbucket.org/tienda/": "tienda", "bitbucket.org/tienda/catalogo": "tienda",
		"https://www.bitbucket.org/tienda": "tienda", "": "", "tien da": "", "../x": "",
	} {
		got, ok := NormalizeWorkspace(in)
		if (want == "") == ok || (ok && got != want) {
			t.Fatalf("%q: %q %v, want %q", in, got, ok, want)
		}
	}
	c := &Connection{Workspace: "tienda"}
	if !InWorkspace(c, "bitbucket.org/tienda/catalogo") || InWorkspace(c, "bitbucket.org/otro/catalogo") || InWorkspace(c, "gitlab.com/tienda/catalogo") || InWorkspace(c, "bitbucket.org/tienda/a/b") {
		t.Fatal("in the workspace")
	}
}

func TestSays(t *testing.T) {
	e := &APIError{Status: 400, Body: `{"type":"error","error":{"message":"You can't merge until you resolve all merge conflicts.","detail":"conflicts"}}`}
	if e.Says() != "You can't merge until you resolve all merge conflicts: conflicts" {
		t.Fatalf("says: %q", e.Says())
	}
	if !errors.Is(&APIError{Status: 404}, forja.ErrNotFound) || errors.Is(&APIError{Status: 400}, forja.ErrNotFound) {
		t.Fatal("not found")
	}
	if forja.Says(e) != e.Says() {
		t.Fatal("the forja's sentence")
	}
}
