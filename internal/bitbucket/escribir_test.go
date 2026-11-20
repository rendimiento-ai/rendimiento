package bitbucket

import (
	"context"
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

// escritor is a Bitbucket that keeps branches and the commits written on them.
type escritor struct {
	mu       sync.Mutex
	branches map[string]string
	forms    []*http.Request
	files    []map[string]string
	refresh  int
}

func (b *escritor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const p = "/2.0/repositories/tienda/a"
	path := r.URL.EscapedPath()
	switch {
	case path == "/site/oauth2/access_token":
		b.refresh++
		_, _ = io.WriteString(w, `{"access_token":"acceso-largo","refresh_token":"otro","expires_in":7200}`)
	case r.Method == http.MethodGet && strings.HasPrefix(path, p+"/refs/branches/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(path, p+"/refs/branches/"))
		sha, ok := b.branches[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"name":"`+name+`","target":{"hash":"`+sha+`"}}`)
	case r.Method == http.MethodPost && path == p+"/refs/branches":
		var body struct {
			Name   string `json:"name"`
			Target struct {
				Hash string `json:"hash"`
			} `json:"target"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.branches[body.Name] = body.Target.Hash
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodPost && path == p+"/src":
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		got := map[string]string{"message": r.FormValue("message"), "branch": r.FormValue("branch"), "files": strings.Join(r.MultipartForm.Value["files"], ",")}
		for name, fh := range r.MultipartForm.File {
			f, _ := fh[0].Open()
			raw, _ := io.ReadAll(f)
			got["file:"+name] = string(raw)
		}
		b.files = append(b.files, got)
		b.branches[r.FormValue("branch")] = "nuevo0000000000000000000000000000000000"
		w.WriteHeader(http.StatusCreated)
	case path == p+"/commit/0123456789ab":
		_, _ = io.WriteString(w, `{"hash":"`+completo+`"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// completo is the whole commit Bitbucket's webhooks cut to 0123456789ab.
const completo = "0123456789abcdef0123456789abcdef01234567"

func TestWritingOnABranch(t *testing.T) {
	b := &escritor{branches: map[string]string{"main": "base"}}
	srv := httptest.NewServer(b)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	ctx := context.Background()
	repo := RepoOf("tienda/a")

	if err := f.CreateBranch(ctx, 1, repo, "fix/uno", "base"); err != nil || b.branches["fix/uno"] != "base" {
		t.Fatalf("new branch: %v %v", err, b.branches)
	}
	// Again, at the same commit: kept.
	if err := f.CreateBranch(ctx, 1, repo, "fix/uno", "base"); err != nil {
		t.Fatalf("again: %v", err)
	}
	sha, err := f.CommitFiles(ctx, 1, repo, "fix/uno", "Arreglo", map[string][]byte{"web/package-lock.json": []byte("{}")}, []string{"viejo.txt"})
	if err != nil || sha != "nuevo0000000000000000000000000000000000" {
		t.Fatalf("commit: %q %v", sha, err)
	}
	got := b.files[0]
	if got["message"] != "Arreglo" || got["branch"] != "fix/uno" || got["files"] != "viejo.txt" || got["file:web/package-lock.json"] != "{}" {
		t.Fatalf("form: %v", got)
	}
}

func TestRenovateAccess(t *testing.T) {
	b := &escritor{branches: map[string]string{}}
	srv := httptest.NewServer(b)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	acc, err := f.Renovate(context.Background(), 1, []string{RepoOf("tienda/a")}, 55*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Renovate adds the /2.0 itself; the hour left was not enough.
	if acc.Platform != "bitbucket" || acc.Endpoint != srv.URL+"/" || acc.Token != "acceso-largo" || acc.Repos["tienda/a"] != "bitbucket.org/tienda/a" || b.refresh != 1 {
		t.Fatalf("renovate: %+v, %d refreshes", acc, b.refresh)
	}
}

func TestMergedHeadIsWhole(t *testing.T) {
	srv := httptest.NewServer(&escritor{branches: map[string]string{}})
	defer srv.Close()
	f, _ := nueva(srv.URL)
	body := []byte(`{"repository":{"full_name":"tienda/a"},"pullrequest":{"id":7,"state":"MERGED",
		"source":{"branch":{"name":"rama"},"commit":{"hash":"0123456789ab"},"repository":{"full_name":"tienda/a"}},
		"destination":{"branch":{"name":"main"},"repository":{"full_name":"tienda/a"}}}}`)
	h := http.Header{"X-Hub-Signature": {Sign("firma-1", body)}, "X-Event-Key": {"pullrequest:fulfilled"}}
	ev, err := f.ParseWebhook(h, body)
	if err != nil {
		t.Fatal(err)
	}
	if ch := ev.Change; ch == nil || !ch.Merged || ch.HeadSHA != completo {
		t.Fatalf("merged: %+v", ev.Change)
	}
}

func TestPushOfSeveralBranches(t *testing.T) {
	c := &Connection{ID: 3, Workspace: "tienda"}
	ev, err := parseWebhook(c, "repo:push", []byte(`{"repository":{"full_name":"tienda/a"},"push":{"changes":[
		{"new":{"type":"branch","name":"uno","target":{"hash":"aaa","message":"uno"}}},
		{"new":{"type":"tag","name":"v1","target":{"hash":"ccc"}}},
		{"new":null,"old":{"type":"branch","name":"vieja","target":{"hash":"ddd"}}},
		{"new":{"type":"branch","name":"dos","target":{"hash":"bbb","message":"dos"}}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Push == nil || ev.Push.Branch != "uno" || len(ev.More) != 2 || !ev.More[0].Deleted || ev.More[0].Branch != "vieja" || ev.More[1].Branch != "dos" || ev.More[1].SHA != "bbb" {
		t.Fatalf("pushes: %+v %+v", ev.Push, ev.More)
	}
}
