package gitlab

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
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

func (c conexiones) GitLabConnection(_ context.Context, id int64) (*Connection, error) {
	for _, x := range c {
		if x.ID == id {
			return x, nil
		}
	}
	return nil, ErrNotConnected
}

func (c conexiones) GitLabConnections(context.Context) ([]*Connection, error) { return c, nil }

// llavero keeps secrets in memory.
type llavero struct {
	mu sync.Mutex
	m  map[int64]Secret
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
	return nil
}

// A forja with two connections (1 at base, 2 at another address), each
// with its webhook token.
func nueva(base string) (*Forja, *llavero) {
	keys := &llavero{m: map[int64]Secret{
		1: {ApplicationSecret: "secreto-1", WebhookToken: "ficha-1", AccessToken: "acceso-1", RefreshToken: "renueva-1", Expires: time.Now().Add(time.Hour)},
		2: {ApplicationSecret: "secreto-2", WebhookToken: "ficha-2"},
	}}
	return &Forja{
		Connections: conexiones{
			{ID: 1, BaseURL: base, ApplicationID: "app-1"},
			{ID: 2, BaseURL: "https://git.example.com", ApplicationID: "app-2"},
		},
		Keys: keys, BaseURL: "https://rendimiento.example.com",
	}, keys
}

func TestWebhookToken(t *testing.T) {
	f, _ := nueva("https://gitlab.com")
	push := []byte(`{"object_kind":"push","ref":"refs/heads/main","after":"aaa","project":{"path_with_namespace":"tienda/catalogo"},
		"commits":[{"id":"bbb","message":"uno"},{"id":"aaa","message":"dos [skip ci]"}]}`)
	for _, tc := range []struct {
		token string
		want  error
		inst  int64
		repo  string
	}{
		{"ficha-1", nil, 1, "gitlab.com/tienda/catalogo"},
		{"ficha-2", nil, 2, "git.example.com/tienda/catalogo"},
		{"otra", forja.ErrBadSignature, 0, ""},
		{"", forja.ErrBadSignature, 0, ""},
		{"ficha-", forja.ErrBadSignature, 0, ""},
	} {
		h := http.Header{"X-Gitlab-Token": {tc.token}, "X-Gitlab-Event": {"Push Hook"}}
		ev, err := f.ParseWebhook(h, push)
		if !errors.Is(err, tc.want) {
			t.Fatalf("token %q: err %v, want %v", tc.token, err, tc.want)
		}
		if tc.want != nil {
			continue
		}
		p := ev.Push
		if p == nil || p.Installation != tc.inst || p.Repo != tc.repo || p.Branch != "main" || p.SHA != "aaa" || !p.SkipCI || len(p.Messages) != 2 || p.Deleted {
			t.Fatalf("token %q: push %+v", tc.token, p)
		}
	}
	if _, err := f.ParseWebhook(http.Header{"X-Gitlab-Token": {"ficha-1"}}, []byte("{")); !errors.Is(err, forja.ErrBadPayload) {
		t.Fatalf("bad JSON: %v", err)
	}
	if _, err := (&Forja{Connections: conexiones{}}).ParseWebhook(http.Header{"X-Gitlab-Token": {"x"}}, push); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("no connection: %v", err)
	}
}

func TestWebhookEvents(t *testing.T) {
	c := &Connection{ID: 3, BaseURL: "https://git.example.com:8443/gitlab"}
	del, _ := parseWebhook(c, "Push Hook", []byte(`{"ref":"refs/heads/vieja","after":"0000000000000000000000000000000000000000","project":{"path_with_namespace":"a/b"}}`))
	if del.Push == nil || !del.Push.Deleted || del.Push.Repo != "git.example.com:8443/gitlab/a/b" {
		t.Fatalf("deleted branch: %+v", del.Push)
	}
	tag, _ := parseWebhook(c, "Tag Push Hook", []byte(`{"ref":"refs/tags/v1","after":"abc","project":{"path_with_namespace":"a/b"}}`))
	if tag.Push != nil || tag.Change != nil || tag.Kind != "Tag Push Hook" {
		t.Fatalf("tag: %+v", tag)
	}
	mr := func(action string) *forja.ChangeEvent {
		ev, err := parseWebhook(c, "Merge Request Hook", []byte(`{"object_kind":"merge_request","project":{"path_with_namespace":"a/b"},
			"object_attributes":{"iid":7,"action":"`+action+`","source_branch":"rama","updated_at":"2026-11-19 10:00:00 UTC","last_commit":{"id":"ccc"},
			"source":{"path_with_namespace":"otra/b"},"target":{"path_with_namespace":"a/b"}}}`))
		if err != nil {
			t.Fatal(err)
		}
		return ev.Change
	}
	if ch := mr("open"); ch.Action != "opened" || ch.Number != 7 || ch.HeadRef != "rama" || ch.HeadSHA != "ccc" || ch.HeadRepo != "git.example.com:8443/gitlab/otra/b" || ch.Repo != "git.example.com:8443/gitlab/a/b" {
		t.Fatalf("open: %+v", ch)
	}
	if ch := mr("reopen"); ch.Action != "reopened" {
		t.Fatalf("reopen: %+v", ch)
	}
	if ch := mr("close"); ch.Action != "closed" || ch.Merged {
		t.Fatalf("close: %+v", ch)
	}
	if ch := mr("merge"); ch.Action != "closed" || !ch.Merged || ch.MergedAt == nil || ch.MergedAt.Hour() != 10 {
		t.Fatalf("merge: %+v", ch)
	}
	if ch := mr("update"); ch.Action == "opened" || ch.Action == "closed" {
		t.Fatalf("update: %+v", ch)
	}
}

func TestStatusState(t *testing.T) {
	out := forja.CheckOutput
	for _, tc := range []struct {
		c           forja.Check
		state, desc string
	}{
		{forja.Check{Status: "queued"}, "pending", ""},
		{forja.Check{Status: "in_progress"}, "running", ""},
		{forja.Check{Status: "completed", Conclusion: "success", Output: out("passed", "")}, "success", "passed"},
		{forja.Check{Status: "completed", Conclusion: "failure", Output: out("build failed", "")}, "failed", "build failed"},
		{forja.Check{Status: "completed", Conclusion: "cancelled"}, "canceled", ""},
		{forja.Check{Status: "completed", Conclusion: "action_required", Output: out("a person must merge it", "")}, "pending", "a person must merge it"},
		{forja.Check{Status: "completed", Conclusion: "action_required"}, "pending", "a person must merge it in rendimiento"},
	} {
		state, desc := StatusState(tc.c)
		if state != tc.state || desc != tc.desc {
			t.Errorf("%+v: %s %q, want %s %q", tc.c, state, desc, tc.state, tc.desc)
		}
	}
	if _, desc := StatusState(forja.Check{Status: "completed", Conclusion: "failure", Output: out(strings.Repeat("é", 300), "")}); len([]rune(desc)) != 255 {
		t.Errorf("a long description is cut to 255 characters: %d", len([]rune(desc)))
	}
}

// gitlabFalso answers like GitLab for one project, a/b, and writes down
// every request.
type gitlabFalso struct {
	mu       sync.Mutex
	requests []string
	bodies   []map[string]any
	statuses []map[string]any
	refresh  int
	running  bool // the status is running: going back to pending is refused
}

func (g *gitlabFalso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var body map[string]any
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	g.requests = append(g.requests, r.Method+" "+r.URL.EscapedPath())
	g.bodies = append(g.bodies, body)
	path := r.URL.EscapedPath()
	switch {
	case path == "/oauth/token":
		r.Form, _ = url.ParseQuery(string(raw))
		switch {
		case r.Form.Get("grant_type") == "refresh_token" && r.Form.Get("refresh_token") == "renueva-1" && r.Form.Get("client_secret") == "secreto-1":
			g.refresh++
			_, _ = io.WriteString(w, `{"access_token":"acceso-nuevo","refresh_token":"renueva-2","expires_in":7200}`)
		case r.Form.Get("grant_type") == "authorization_code" && r.Form.Get("code") == "codigo" && r.Form.Get("redirect_uri") != "":
			_, _ = io.WriteString(w, `{"access_token":"acceso-de-ana","refresh_token":"r","expires_in":7200}`)
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
		}
	case r.Header.Get("Authorization") == "Bearer acceso-viejo":
		w.WriteHeader(http.StatusUnauthorized)
	case path == "/api/v4/user":
		_, _ = io.WriteString(w, `{"id":42,"username":"ana"}`)
	case strings.HasPrefix(path, "/api/v4/projects/a%2Fb/statuses/"):
		if g.running && body["state"] == "pending" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"Cannot transition status via :enqueue from :running"}`)
			return
		}
		g.running = body["state"] == "running"
		g.statuses = append(g.statuses, body)
		_, _ = io.WriteString(w, `{"id":99}`)
	case path == "/api/v4/projects/a%2Fb/merge_requests/5/merge":
		if body["sha"] != "abc" {
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"message":"SHA does not match HEAD of source branch"}`)
			return
		}
		if body["merge_when_pipeline_succeeds"] == true {
			_, _ = io.WriteString(w, `{"iid":5,"state":"opened"}`)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, `{"message":"405 Method Not Allowed"}`)
	case path == "/api/v4/projects/a%2Fb/merge_requests/6/merge":
		_, _ = io.WriteString(w, `{"iid":6,"state":"merged"}`)
	case path == "/api/v4/projects/a%2Fb/merge_requests/5":
		_, _ = io.WriteString(w, `{"iid":5,"state":"opened","source_branch":"rama","target_branch":"main","sha":"abc","source_project_id":1,"target_project_id":1,
			"detailed_merge_status":"conflict","labels":["renovate"],"author":{"username":"renovate-bot","bot":true}}`)
	case path == "/api/v4/projects/a%2Fb/repository/files/rendimiento.yaml/raw":
		if r.URL.Query().Get("ref") != "main" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"404 File Not Found"}`)
			return
		}
		_, _ = io.WriteString(w, "services: []\n")
	case path == "/api/v4/projects/a%2Fb/repository/archive.tar.gz":
		_, _ = w.Write(archivo(map[string]string{"b-main-abc/package.json": "{}", "b-main-abc/web/Dockerfile": "FROM x"}))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"404 Not Found"}`)
	}
}

// archivo is a tar.gz with the files given.
func archivo(files map[string]string) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, content := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(content))
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

func TestStatusesAndMerges(t *testing.T) {
	g := &gitlabFalso{}
	srv := httptest.NewServer(g)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	ctx := context.Background()
	repo := RepoOf(&Connection{BaseURL: srv.URL}, "a/b")

	id, err := f.CreateCheck(ctx, 1, repo, forja.Check{Name: "rendimiento: b", HeadSHA: "abc", Status: "in_progress", DetailsURL: "https://rendimiento.example.com/apps/b/runs/1"})
	if err != nil || id != 99 {
		t.Fatalf("create: %d %v", id, err)
	}
	if s := g.statuses[0]; s["state"] != "running" || s["name"] != "rendimiento: b" || s["target_url"] != "https://rendimiento.example.com/apps/b/runs/1" {
		t.Fatalf("status: %v", s)
	}
	// A person must merge it: pending, after finishing the running one.
	if err := f.UpdateCheck(ctx, 1, repo, id, forja.Check{Name: "rendimiento: b", HeadSHA: "abc", Status: "completed", Conclusion: "action_required", Output: forja.CheckOutput("a person must merge it", "")}); err != nil {
		t.Fatal(err)
	}
	last := g.statuses[len(g.statuses)-1]
	if last["state"] != "pending" || last["description"] != "a person must merge it" || g.statuses[len(g.statuses)-2]["state"] != "canceled" {
		t.Fatalf("action_required: %v", g.statuses)
	}
	if err := f.UpdateCheck(ctx, 1, repo, id, forja.Check{Status: "completed", Conclusion: "success"}); err == nil {
		t.Fatal("an update without its commit cannot be written on GitLab")
	}

	// A merge request still waiting for its pipeline merges when it passes.
	err = f.MergePull(ctx, 1, repo, 5, "abc")
	if err == nil || forja.Says(err) != "GitLab merges it when its pipeline succeeds" {
		t.Fatalf("merge when the pipeline succeeds: %v", err)
	}
	if err := f.MergePull(ctx, 1, repo, 6, "abc"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := f.MergePull(ctx, 1, repo, 5, "otro"); !errors.Is(err, forja.ErrConflict) {
		t.Fatalf("a head that moved: %v", err)
	}
	pr, err := f.Pull(ctx, 1, repo, 5)
	if err != nil {
		t.Fatal(err)
	}
	if pr.State != "open" || pr.Mergeable == nil || *pr.Mergeable || pr.MergeableState != "dirty" || pr.Head.Repo.FullName != repo || pr.User.Type != "Bot" || pr.Labels[0].Name != "renovate" {
		t.Fatalf("pull: %+v", pr)
	}

	// Reading the code.
	raw, err := f.FileAt(ctx, 1, repo, "rendimiento.yaml", "main")
	if err != nil || string(raw) != "services: []\n" {
		t.Fatalf("file: %q %v", raw, err)
	}
	if _, err := f.FileAt(ctx, 1, repo, "rendimiento.yaml", "otra"); !forja.IsNotFound(err) {
		t.Fatalf("missing file: %v", err)
	}
	fsys, err := f.RepoFS(ctx, 1, repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := fs.ReadFile(fsys, "web/Dockerfile"); err != nil || string(b) != "FROM x" {
		t.Fatalf("tree: %q %v", b, err)
	}
	if tok, err := f.CloneToken(ctx, 1); err != nil || tok != "oauth2:acceso-1" {
		t.Fatalf("clone token: %q %v", tok, err)
	}
	if u, err := f.CloneURL(ctx, 1, repo); err != nil || u != srv.URL+"/a/b.git" {
		t.Fatalf("clone URL: %q %v", u, err)
	}
}

func TestTokenRefresh(t *testing.T) {
	g := &gitlabFalso{}
	srv := httptest.NewServer(g)
	defer srv.Close()
	f, keys := nueva(srv.URL)
	ctx := context.Background()
	// An expired token is refreshed, and the new ones are kept.
	keys.m[1] = Secret{ApplicationSecret: "secreto-1", WebhookToken: "ficha-1", AccessToken: "acceso-viejo", RefreshToken: "renueva-1", Expires: time.Now().Add(-time.Minute)}
	if tok, err := f.CloneToken(ctx, 1); err != nil || tok != "oauth2:acceso-nuevo" {
		t.Fatalf("refreshed: %q %v", tok, err)
	}
	if k := keys.m[1]; k.AccessToken != "acceso-nuevo" || k.RefreshToken != "renueva-2" || time.Until(k.Expires) < time.Hour {
		t.Fatalf("kept: %+v", k)
	}
	if tok, _ := f.CloneToken(ctx, 1); tok != "oauth2:acceso-nuevo" || g.refresh != 1 {
		t.Fatalf("a valid token is not refreshed again: %q, %d refreshes", tok, g.refresh)
	}
	// A connection never authorized says so.
	if _, err := f.CloneToken(ctx, 2); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("not authorized: %v", err)
	}
}

func TestSignIn(t *testing.T) {
	g := &gitlabFalso{}
	srv := httptest.NewServer(g)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	c := &Connection{ID: 1, BaseURL: srv.URL, ApplicationID: "app-1"}
	to, err := url.Parse(AuthorizeURLFor(c, "estado", "https://rendimiento.example.com"+CallbackPath, ScopeSignIn))
	if err != nil {
		t.Fatal(err)
	}
	q := to.Query()
	if to.Path != "/oauth/authorize" || q.Get("client_id") != "app-1" || q.Get("state") != "estado" || q.Get("response_type") != "code" ||
		q.Get("scope") != "read_user" || q.Get("redirect_uri") != "https://rendimiento.example.com/api/forjas/gitlab/callback" {
		t.Fatalf("authorize URL: %s", to)
	}
	if u, _ := f.AuthorizeURL("e", "https://rendimiento.example.com/x"); !strings.HasPrefix(u, srv.URL+"/oauth/authorize?") {
		t.Fatalf("the forja's sign-in is the oldest connection's: %s", u)
	}
	login, err := f.SignIn(context.Background(), "codigo")
	if err != nil || login != "ana@"+strings.TrimPrefix(srv.URL, "http://") {
		t.Fatalf("sign in: %q %v", login, err)
	}
	if _, err := f.SignIn(context.Background(), "otro"); err == nil {
		t.Fatal("a wrong code signs nobody in")
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                             "https://gitlab.com",
		"gitlab.com":                   "https://gitlab.com",
		"https://Git.Example.com/":     "https://git.example.com",
		"https://example.com/gitlab/":  "https://example.com/gitlab",
		"http://git.example.lan:8080":  "http://git.example.lan:8080",
		"http://192.168.1.10":          "http://192.168.1.10",
		"http://git.example.com":       "",
		"ftp://git.example.com":        "",
		"https://user:pw@example.com":  "",
		"https://git.example.com/?a=b": "",
	} {
		got, err := NormalizeURL(in)
		if want == "" {
			if err == nil {
				t.Errorf("%q: accepted as %q", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: %q %v, want %q", in, got, err, want)
		}
	}
	if Host("https://git.example.com:8443/gitlab") != "git.example.com:8443/gitlab" {
		t.Error(Host("https://git.example.com:8443/gitlab"))
	}
}

func TestSays(t *testing.T) {
	for body, want := range map[string]string{
		`{"message":"Branch cannot be merged."}`:            "Branch cannot be merged",
		`{"message":["Another open merge request"]}`:        "Another open merge request",
		`{"message":{"title":["can't be blank"]}}`:          "title can't be blank",
		`{"error":"invalid_grant","error_description":"x"}`: "x",
		`not json`: "",
	} {
		if got := (&APIError{Status: 400, Body: body}).Says(); got != want {
			t.Errorf("%s: %q, want %q", body, got, want)
		}
	}
}
