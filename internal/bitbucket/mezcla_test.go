package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/mezcla"
)

// entrada is a file in a commit of the fake: its content and whether it is
// executable.
type entrada struct {
	data string
	exec bool
}

// objeto is a commit of the fake.
type objeto struct {
	parents []string
	tree    map[string]entrada
	message string
}

// repositorio is a Bitbucket that keeps a repository's commits as git
// would: each with its parents and its whole tree, written through /src
// as Bitbucket documents it.
type repositorio struct {
	mu       sync.Mutex
	commits  map[string]*objeto
	branches map[string]string
	n        int
	// unSoloPadre: a Bitbucket that dropped several parents (its
	// documentation says it will) and writes the first alone.
	unSoloPadre bool
	// empuja: someone pushes on the branch right after each commit.
	empuja bool
}

func nuevoRepositorio() *repositorio {
	return &repositorio{commits: map[string]*objeto{}, branches: map[string]string{}}
}

func (b *repositorio) commit(parents []string, tree map[string]entrada, message string) string {
	b.n++
	sha := fmt.Sprintf("%040x", 0xbeef000+b.n)
	b.commits[sha] = &objeto{parents: parents, tree: tree, message: message}
	return sha
}

func (b *repositorio) resolve(ref string) string {
	if sha, ok := b.branches[ref]; ok {
		return sha
	}
	return ref
}

func (b *repositorio) ancestors(sha string) []string {
	seen, out, queue := map[string]bool{}, []string{}, []string{sha}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if seen[c] || b.commits[c] == nil {
			continue
		}
		seen[c] = true
		out = append(out, c)
		queue = append(queue, b.commits[c].parents...)
	}
	return out
}

func (b *repositorio) mergeBase(x, y string) string {
	in := map[string]bool{}
	for _, c := range b.ancestors(x) {
		in[c] = true
	}
	for _, c := range b.ancestors(y) {
		if in[c] {
			return c
		}
	}
	return ""
}

func (b *repositorio) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const p = "/2.0/repositories/tienda/a"
	path := r.URL.EscapedPath()
	answer := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	two := func(spec string) (string, string) {
		x, y, _ := strings.Cut(spec, "..")
		x, _ = url.PathUnescape(x)
		y, _ = url.PathUnescape(y)
		return b.resolve(x), b.resolve(y)
	}
	switch {
	case path == "/site/oauth2/access_token":
		_, _ = io.WriteString(w, `{"access_token":"acceso-largo","refresh_token":"otro","expires_in":7200}`)
	case r.Method == http.MethodGet && strings.HasPrefix(path, p+"/refs/branches/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(path, p+"/refs/branches/"))
		sha, ok := b.branches[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		answer(map[string]any{"name": name, "target": map[string]string{"hash": sha}})
	case strings.HasPrefix(path, p+"/merge-base/"):
		x, y := two(strings.TrimPrefix(path, p+"/merge-base/"))
		answer(map[string]string{"hash": b.mergeBase(x, y)})
	case strings.HasPrefix(path, p+"/diffstat/"):
		// Bitbucket's x..y: what x changed since its merge base with y.
		x, y := two(strings.TrimPrefix(path, p+"/diffstat/"))
		from, to := b.commits[b.mergeBase(x, y)].tree, b.commits[x].tree
		var names []string
		for n := range from {
			names = append(names, n)
		}
		for n := range to {
			if _, ok := from[n]; !ok {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		values := []map[string]any{}
		for _, n := range names {
			if from[n] != to[n] {
				values = append(values, map[string]any{"old": map[string]string{"path": n}, "new": map[string]string{"path": n}})
			}
		}
		answer(map[string]any{"values": values})
	case r.Method == http.MethodGet && strings.HasPrefix(path, p+"/src/"):
		ref, name, _ := strings.Cut(strings.TrimPrefix(path, p+"/src/"), "/")
		ref, _ = url.PathUnescape(ref)
		name, _ = url.PathUnescape(name)
		c := b.commits[b.resolve(ref)]
		f, ok := entrada{}, false
		if c != nil {
			f, ok = c.tree[name]
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("format") == "meta" {
			attrs := []string{}
			if f.exec {
				attrs = append(attrs, "executable")
			}
			answer(map[string]any{"path": name, "type": "commit_file", "attributes": attrs})
			return
		}
		_, _ = io.WriteString(w, f.data)
	case r.Method == http.MethodPost && path == p+"/src":
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		branch := r.FormValue("branch")
		head, exists := b.branches[branch]
		parents := []string{head}
		if s := r.FormValue("parents"); s != "" {
			parents = strings.Split(s, ",")
			// A branch that exists must be at the (first) parent.
			if exists && parents[0] != head {
				w.WriteHeader(http.StatusConflict)
				return
			}
		}
		if b.unSoloPadre {
			parents = parents[:1]
		}
		tree := map[string]entrada{}
		for k, v := range b.commits[parents[0]].tree {
			tree[k] = v
		}
		for name, fh := range r.MultipartForm.File {
			f, _ := fh[0].Open()
			raw, _ := io.ReadAll(f)
			tree[name] = entrada{string(raw), strings.Contains(fh[0].Header.Get("Content-Disposition"), `x-attributes="executable"`)}
		}
		for _, gone := range r.MultipartForm.Value["files"] {
			if _, ok := r.MultipartForm.File[gone]; !ok {
				delete(tree, gone)
			}
		}
		sha := b.commit(parents, tree, r.FormValue("message"))
		b.branches[branch] = sha
		if b.empuja {
			b.branches[branch] = b.commit([]string{sha}, tree, "someone else's")
		}
		w.Header().Set("Location", "http://"+r.Host+p+"/commit/"+sha)
		w.WriteHeader(http.StatusCreated)
	case strings.HasPrefix(path, p+"/commit/"):
		c := b.commits[strings.TrimPrefix(path, p+"/commit/")]
		if c == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		parents := []map[string]string{}
		for _, x := range c.parents {
			parents = append(parents, map[string]string{"hash": x})
		}
		answer(map[string]any{"hash": strings.TrimPrefix(path, p+"/commit/"), "message": c.message, "parents": parents})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// A branch and the default branch that both changed main.ts in the same
// place (a conflict) and lista.txt in different places (no conflict); the
// default branch also brings an executable script, and both have another.
func mezclaDePrueba(b *repositorio) (main, head string) {
	base := b.commit(nil, map[string]entrada{
		"main.ts":   {"import a\nimport b\n\nfunc main() {}\n", false},
		"lista.txt": {"uno\ndos\ntres\ncuatro\ncinco\n", false},
		"run.sh":    {"#!/bin/sh\n", true},
	}, "base")
	main = b.commit([]string{base}, map[string]entrada{
		"main.ts":   {"import a\nimport b, s\n\nfunc main() {}\n", false},
		"lista.txt": {"uno\ndos\ntres\ncuatro\nCINCO\n", false},
		"run.sh":    {"#!/bin/sh\n", true},
		"nuevo.sh":  {"#!/bin/sh\necho hola\n", true},
	}, "main")
	head = b.commit([]string{base}, map[string]entrada{
		"main.ts":   {"import a\nimport c\n\nfunc main() {}\n", false},
		"lista.txt": {"UNO\ndos\ntres\ncuatro\ncinco\n", false},
		"run.sh":    {"#!/bin/sh\n", true},
		"otro.ts":   {"export {}\n", false},
	}, "rama")
	b.branches["main"], b.branches["rama"] = main, head
	return main, head
}

// juntar does what the coding agent's merge_default_branch does, through
// the forja: the merge base, what each side changed, each file at its
// commit, and git's marks where both changed the same place.
func juntar(t *testing.T, f forja.Repos, repo, branch, main string) (head, other string, files map[string][]byte, conflicts []string) {
	t.Helper()
	ctx := context.Background()
	head, _ = f.BranchSHA(ctx, 1, repo, branch)
	other, _ = f.BranchSHA(ctx, 1, repo, main)
	base, _, err := f.MergeBase(ctx, 1, repo, other, head)
	if err != nil || base == "" {
		t.Fatalf("merge base: %q %v", base, err)
	}
	theirs, ok1, err1 := f.ChangedFiles(ctx, 1, repo, base, other)
	ours, ok2, err2 := f.ChangedFiles(ctx, 1, repo, base, head)
	if err1 != nil || err2 != nil || !ok1 || !ok2 {
		t.Fatalf("changed files: %v %v", err1, err2)
	}
	mine := map[string]bool{}
	for _, p := range ours {
		mine[p] = true
	}
	files = map[string][]byte{}
	for _, p := range theirs {
		th, err := f.FileAt(ctx, 1, repo, p, other)
		if err != nil {
			t.Fatalf("%s at %s: %v", p, other, err)
		}
		if !mine[p] {
			files[p] = th
			continue
		}
		o, _ := f.FileAt(ctx, 1, repo, p, head)
		bs, _ := f.FileAt(ctx, 1, repo, p, base)
		merged, conflict, ok := mezcla.Merge(bs, o, th, branch, main)
		if !ok {
			t.Fatalf("%s is binary", p)
		}
		files[p] = merged
		if conflict {
			conflicts = append(conflicts, p)
		}
	}
	return head, other, files, conflicts
}

const resuelto = "import a\nimport b, s\nimport c\n\nfunc main() {}\n"

// Bitbucket's /src takes two parents: the merge commit has the branch's
// head and the default branch's as parents, and the resolved tree.
func TestCommitMergeOnBitbucket(t *testing.T) {
	b := nuevoRepositorio()
	main, head := mezclaDePrueba(b)
	srv := httptest.NewServer(b)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	ctx := context.Background()
	repo := RepoOf("tienda/a")

	h, other, files, conflicts := juntar(t, f, repo, "rama", "main")
	if h != head || other != main {
		t.Fatalf("heads %s %s", h, other)
	}
	if len(conflicts) != 1 || conflicts[0] != "main.ts" || !strings.Contains(string(files["main.ts"]), "<<<<<<< rama") ||
		string(files["lista.txt"]) != "UNO\ndos\ntres\ncuatro\nCINCO\n" || string(files["nuevo.sh"]) != "#!/bin/sh\necho hola\n" {
		t.Fatalf("conflicts %v, files %q", conflicts, files)
	}
	files["main.ts"] = []byte(resuelto)

	// The branch moved meanwhile: nothing is written.
	if _, err := f.CommitMerge(ctx, 1, repo, "rama", main, other, "Merge main into rama", files, nil); !errors.Is(err, forja.ErrConflict) {
		t.Fatalf("a head that moved: %v", err)
	}
	sha, err := f.CommitMerge(ctx, 1, repo, "rama", head, other, "Merge main into rama", files, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := b.commits[sha]
	if b.branches["rama"] != sha || m == nil || fmt.Sprint(m.parents) != fmt.Sprint([]string{head, main}) || m.message != "Merge main into rama" {
		t.Fatalf("the merge: %s %+v", sha, m)
	}
	want := map[string]entrada{
		"main.ts":   {resuelto, false},
		"lista.txt": {"UNO\ndos\ntres\ncuatro\nCINCO\n", false},
		"run.sh":    {"#!/bin/sh\n", true},
		"nuevo.sh":  {"#!/bin/sh\necho hola\n", true}, // executable as on main
		"otro.ts":   {"export {}\n", false},
	}
	if fmt.Sprint(m.tree) != fmt.Sprint(want) {
		t.Fatalf("the merge's tree:\n%v\nwant\n%v", m.tree, want)
	}
}

// A Bitbucket that writes the commit with one parent: the resolution is
// there, but it is no merge, and CommitMerge says so.
func TestCommitMergeWithOneParent(t *testing.T) {
	b := nuevoRepositorio()
	b.unSoloPadre = true
	main, head := mezclaDePrueba(b)
	srv := httptest.NewServer(b)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	_, err := f.CommitMerge(context.Background(), 1, RepoOf("tienda/a"), "rama", head, main, "Merge", map[string][]byte{"main.ts": []byte(resuelto)}, nil)
	if !errors.Is(err, ErrNoMergeCommit) {
		t.Fatalf("one parent: %v", err)
	}
}

// The commit CommitFiles answers is the one it wrote (Bitbucket's
// Location), not the branch's head after someone else's push; a script it
// changes stays executable.
func TestCommitFilesAnswersItsCommit(t *testing.T) {
	b := nuevoRepositorio()
	_, head := mezclaDePrueba(b)
	b.empuja = true
	srv := httptest.NewServer(b)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	sha, err := f.CommitFiles(context.Background(), 1, RepoOf("tienda/a"), "rama", "Arreglo", map[string][]byte{"run.sh": []byte("#!/bin/sh\nexit 0\n")}, []string{"otro.ts"})
	if err != nil {
		t.Fatal(err)
	}
	c := b.commits[sha]
	if c == nil || c.message != "Arreglo" || len(c.parents) != 1 || c.parents[0] != head || b.branches["rama"] == sha {
		t.Fatalf("answered %s: %+v", sha, c)
	}
	if f := c.tree["run.sh"]; !f.exec || f.data != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("run.sh: %+v", f)
	}
	if _, ok := c.tree["otro.ts"]; ok {
		t.Fatal("otro.ts was not removed")
	}
}
