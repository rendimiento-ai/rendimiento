package gitlab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// escritor is a GitLab that keeps branches and the commits written on them.
type escritor struct {
	mu       sync.Mutex
	branches map[string]string
	commits  []map[string]any
	refresh  int
}

func (g *escritor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	path := r.URL.EscapedPath()
	const p = "/api/v4/projects/a%2Fb"
	switch {
	case path == "/oauth/token":
		g.refresh++
		_, _ = io.WriteString(w, `{"access_token":"acceso-largo","refresh_token":"renueva-2","expires_in":7200}`)
	case r.Method == http.MethodGet && strings.HasPrefix(path, p+"/repository/branches/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(path, p+"/repository/branches/"))
		sha, ok := g.branches[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"name":"`+name+`","commit":{"id":"`+sha+`"}}`)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, p+"/repository/branches/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(path, p+"/repository/branches/"))
		delete(g.branches, name)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && path == p+"/repository/branches":
		g.branches[r.URL.Query().Get("branch")] = r.URL.Query().Get("ref")
		_, _ = io.WriteString(w, `{}`)
	case path == p+"/repository/merge_base":
		_, _ = io.WriteString(w, `{"id":"base"}`)
	case strings.HasPrefix(path, p+"/repository/files/"):
		if strings.HasPrefix(path, p+"/repository/files/viejo.txt/") {
			_, _ = io.WriteString(w, "antes")
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case r.Method == http.MethodPost && path == p+"/repository/commits":
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		g.commits = append(g.commits, body)
		_, _ = io.WriteString(w, `{"id":"nuevo"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestWritingOnABranch(t *testing.T) {
	g := &escritor{branches: map[string]string{"main": "base", "otra": "ajeno"}}
	srv := httptest.NewServer(g)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	ctx := context.Background()
	repo := RepoOf(&Connection{BaseURL: srv.URL}, "a/b")

	if err := f.CreateBranch(ctx, 1, repo, "fix/uno", "base"); err != nil || g.branches["fix/uno"] != "base" {
		t.Fatalf("new branch: %v %v", err, g.branches)
	}
	// One made elsewhere, that does not hold the commit, starts again there.
	g.branches["fix/dos"] = "ajeno"
	if err := f.CreateBranch(ctx, 1, repo, "fix/dos", "otro"); err != nil || g.branches["fix/dos"] != "otro" {
		t.Fatalf("moved branch: %v %v", err, g.branches)
	}
	sha, err := f.CommitFiles(ctx, 1, repo, "fix/uno", "Arreglo", map[string][]byte{"viejo.txt": []byte("después"), "nuevo.bin": {0, 1}}, []string{"quitar.txt"})
	if err != nil || sha != "nuevo" {
		t.Fatalf("commit: %q %v", sha, err)
	}
	c := g.commits[0]
	actions, _ := c["actions"].([]any)
	if c["branch"] != "fix/uno" || c["commit_message"] != "Arreglo" || len(actions) != 3 {
		t.Fatalf("commit body: %v", c)
	}
	want := []string{"create nuevo.bin AAE=", "update viejo.txt " + base64.StdEncoding.EncodeToString([]byte("después")), "delete quitar.txt "}
	for i, a := range actions {
		m := a.(map[string]any)
		got := m["action"].(string) + " " + m["file_path"].(string) + " "
		if s, ok := m["content"].(string); ok {
			got += s
		}
		if got != want[i] {
			t.Errorf("action %d: %q, want %q", i, got, want[i])
		}
	}
}

func TestRenovateAccess(t *testing.T) {
	g := &escritor{branches: map[string]string{}}
	srv := httptest.NewServer(g)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	repo := RepoOf(&Connection{BaseURL: srv.URL}, "grupo/tienda")
	// The kept token has an hour left: not enough for a run of 55 minutes
	// and its margin, so it is refreshed first.
	acc, err := f.Renovate(context.Background(), 1, []string{repo}, 55*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Platform != "gitlab" || acc.Endpoint != srv.URL+"/api/v4/" || acc.Token != "acceso-largo" || acc.Repos["grupo/tienda"] != repo || g.refresh != 1 {
		t.Fatalf("renovate: %+v, %d refreshes", acc, g.refresh)
	}
	// A short run takes the kept one.
	if acc, err := f.Renovate(context.Background(), 1, []string{repo}, time.Minute); err != nil || acc.Token != "acceso-largo" || g.refresh != 1 {
		t.Fatalf("short run: %+v %v", acc, err)
	}
}

// conCandado is a store of connections with the token lock of Postgres,
// shared by two processes.
type conCandado struct {
	conexiones
	mu    *sync.Mutex
	veces *int
}

func (c conCandado) LockToken(context.Context, int64) (func(), error) {
	c.mu.Lock()
	*c.veces++
	return c.mu.Unlock, nil
}

func TestTokenRefreshOneAtATime(t *testing.T) {
	var mu sync.Mutex
	current, refreshes := "renueva-1", 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		// GitLab takes each refresh token once: a second refresh with the
		// same one is refused.
		if r.URL.Path != "/oauth/token" || r.PostForm.Get("refresh_token") != current {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		refreshes++
		current = "renueva-" + string(rune('1'+refreshes))
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(w, `{"access_token":"acceso-`+current+`","refresh_token":"`+current+`","expires_in":7200}`)
	}))
	defer srv.Close()
	keys := &llavero{m: map[int64]Secret{1: {ApplicationSecret: "secreto-1", AccessToken: "viejo", RefreshToken: "renueva-1", Expires: time.Now().Add(-time.Minute)}}}
	var lock sync.Mutex
	veces := 0
	conns := conCandado{conexiones{{ID: 1, BaseURL: srv.URL, ApplicationID: "app-1"}}, &lock, &veces}
	// Two processes: each its own Forja (and cache), the same secrets.
	a := &Forja{Connections: conns, Keys: keys, BaseURL: "https://rendimiento.example.com"}
	b := &Forja{Connections: conns, Keys: keys, BaseURL: "https://rendimiento.example.com"}
	var wg sync.WaitGroup
	toks := make([]string, 2)
	errs := make([]error, 2)
	for i, f := range []*Forja{a, b} {
		wg.Add(1)
		go func(i int, f *Forja) {
			defer wg.Done()
			toks[i], errs[i] = f.CloneToken(context.Background(), 1)
		}(i, f)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || toks[0] != toks[1] || refreshes != 1 || veces != 2 {
		t.Fatalf("tokens %q, errors %v, %d refreshes, %d locks", toks, errs, refreshes, veces)
	}
}
