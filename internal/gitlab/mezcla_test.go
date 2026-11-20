package gitlab

import (
	"context"
	"encoding/base64"
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

// proyecto is a GitLab that keeps a project's commits as git would: each
// with its parents and its whole tree. Merging a merge request writes a
// commit with two parents, and only when both sides have the same tree
// (what CommitMerge promises; anything else would need a real merge).
type proyecto struct {
	mu       sync.Mutex
	commits  map[string]*objeto
	branches map[string]string
	n        int
	method   string
	mrs      map[int][2]string // source, target
	closed   []int
	merged   []string // merge_commit_message
}

func nuevoProyecto() *proyecto {
	return &proyecto{commits: map[string]*objeto{}, branches: map[string]string{}, method: "merge", mrs: map[int][2]string{}}
}

func (g *proyecto) commit(parents []string, tree map[string]entrada, message string) string {
	g.n++
	sha := fmt.Sprintf("%040x", 0xc0ffee000+g.n)
	g.commits[sha] = &objeto{parents: parents, tree: tree, message: message}
	return sha
}

func (g *proyecto) resolve(ref string) string {
	if sha, ok := g.branches[ref]; ok {
		return sha
	}
	return ref
}

// ancestors are a commit and all it comes from, by distance.
func (g *proyecto) ancestors(sha string) []string {
	seen, out, queue := map[string]bool{}, []string{}, []string{sha}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if seen[c] || g.commits[c] == nil {
			continue
		}
		seen[c] = true
		out = append(out, c)
		queue = append(queue, g.commits[c].parents...)
	}
	return out
}

func (g *proyecto) mergeBase(a, b string) string {
	in := map[string]bool{}
	for _, c := range g.ancestors(a) {
		in[c] = true
	}
	for _, c := range g.ancestors(b) {
		if in[c] {
			return c
		}
	}
	return ""
}

func mode(f entrada) string {
	if f.exec {
		return "100755"
	}
	return "100644"
}

func (g *proyecto) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	path := r.URL.EscapedPath()
	q := r.URL.Query()
	const p = "/api/v4/projects/a%2Fb"
	answer := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case path == "/oauth/token":
		_, _ = io.WriteString(w, `{"access_token":"acceso-largo","refresh_token":"otro","expires_in":7200}`)
	case path == p && r.Method == http.MethodGet:
		answer(map[string]string{"merge_method": g.method, "squash_option": "default_off"})
	case r.Method == http.MethodGet && strings.HasPrefix(path, p+"/repository/branches/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(path, p+"/repository/branches/"))
		sha, ok := g.branches[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		answer(map[string]any{"name": name, "commit": map[string]string{"id": sha}})
	case r.Method == http.MethodDelete && strings.HasPrefix(path, p+"/repository/branches/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(path, p+"/repository/branches/"))
		if _, ok := g.branches[name]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(g.branches, name)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && path == p+"/repository/branches":
		g.branches[q.Get("branch")] = g.resolve(q.Get("ref"))
		answer(map[string]any{})
	case path == p+"/repository/merge_base":
		refs := q["refs[]"]
		answer(map[string]string{"id": g.mergeBase(g.resolve(refs[0]), g.resolve(refs[1]))})
	case path == p+"/repository/compare":
		from, to := g.resolve(q.Get("from")), g.resolve(q.Get("to"))
		if q.Get("straight") != "true" {
			from = g.mergeBase(from, to)
		}
		a, b := g.commits[from].tree, g.commits[to].tree
		var names []string
		for n := range a {
			names = append(names, n)
		}
		for n := range b {
			if _, ok := a[n]; !ok {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		diffs := []map[string]any{}
		for _, n := range names {
			fa, ina := a[n]
			fb, inb := b[n]
			if ina && inb && fa == fb {
				continue
			}
			d := map[string]any{"old_path": n, "new_path": n, "new_file": !ina, "deleted_file": !inb, "renamed_file": false, "a_mode": "0", "b_mode": "0"}
			if ina {
				d["a_mode"] = mode(fa)
			}
			if inb {
				d["b_mode"] = mode(fb)
			}
			diffs = append(diffs, d)
		}
		answer(map[string]any{"diffs": diffs})
	case strings.HasPrefix(path, p+"/repository/files/"):
		rest := strings.TrimPrefix(path, p+"/repository/files/")
		rawFile := strings.HasSuffix(rest, "/raw")
		name, _ := url.PathUnescape(strings.TrimSuffix(rest, "/raw"))
		c := g.commits[g.resolve(q.Get("ref"))]
		f, ok := entrada{}, false
		if c != nil {
			f, ok = c.tree[name]
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if rawFile {
			_, _ = io.WriteString(w, f.data)
			return
		}
		answer(map[string]any{"file_path": name, "execute_filemode": f.exec})
	case r.Method == http.MethodPost && path == p+"/repository/commits":
		var body struct {
			Branch  string `json:"branch"`
			Message string `json:"commit_message"`
			Actions []struct {
				Action   string `json:"action"`
				Path     string `json:"file_path"`
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
				Exec     *bool  `json:"execute_filemode"`
			} `json:"actions"`
		}
		_ = json.Unmarshal(raw, &body)
		head, ok := g.branches[body.Branch]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		tree := map[string]entrada{}
		for k, v := range g.commits[head].tree {
			tree[k] = v
		}
		for _, a := range body.Actions {
			content := a.Content
			if a.Encoding == "base64" {
				b, _ := base64.StdEncoding.DecodeString(a.Content)
				content = string(b)
			}
			f, exists := tree[a.Path]
			switch {
			case a.Action == "create" && !exists:
				tree[a.Path] = entrada{content, a.Exec != nil && *a.Exec}
			case a.Action == "update" && exists:
				tree[a.Path] = entrada{content, f.exec}
			case a.Action == "delete" && exists:
				delete(tree, a.Path)
			case a.Action == "chmod" && exists:
				tree[a.Path] = entrada{f.data, a.Exec != nil && *a.Exec}
			default:
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"message":"A file with this name doesn't exist"}`)
				return
			}
		}
		sha := g.commit([]string{head}, tree, body.Message)
		g.branches[body.Branch] = sha
		answer(map[string]string{"id": sha})
	case r.Method == http.MethodPost && path == p+"/merge_requests":
		var body struct {
			Source string `json:"source_branch"`
			Target string `json:"target_branch"`
		}
		_ = json.Unmarshal(raw, &body)
		iid := len(g.mrs) + 1
		g.mrs[iid] = [2]string{body.Source, body.Target}
		answer(map[string]any{"iid": iid, "state": "opened", "merge_status": "checking", "detailed_merge_status": "checking"})
	case r.Method == http.MethodGet && strings.HasPrefix(path, p+"/merge_requests/"):
		answer(map[string]any{"iid": 1, "state": "opened", "merge_status": "can_be_merged", "detailed_merge_status": "mergeable"})
	case r.Method == http.MethodPut && strings.HasSuffix(path, "/merge"):
		var iid int
		_, _ = fmt.Sscanf(strings.TrimPrefix(path, p+"/merge_requests/"), "%d/merge", &iid)
		var body struct {
			SHA     string `json:"sha"`
			Message string `json:"merge_commit_message"`
			Remove  bool   `json:"should_remove_source_branch"`
		}
		_ = json.Unmarshal(raw, &body)
		mr := g.mrs[iid]
		source, target := g.branches[mr[0]], g.branches[mr[1]]
		if body.SHA != source {
			w.WriteHeader(http.StatusConflict)
			return
		}
		st, tt := g.commits[source].tree, g.commits[target].tree
		if fmt.Sprint(st) != fmt.Sprint(tt) {
			w.WriteHeader(http.StatusNotAcceptable)
			_, _ = io.WriteString(w, `{"message":"Branch cannot be merged"}`)
			return
		}
		sha := g.commit([]string{target, source}, tt, body.Message)
		g.branches[mr[1]] = sha
		g.merged = append(g.merged, body.Message)
		if body.Remove {
			delete(g.branches, mr[0])
		}
		answer(map[string]any{"iid": iid, "state": "merged", "merge_commit_sha": sha})
	case r.Method == http.MethodPut && strings.HasPrefix(path, p+"/merge_requests/"):
		var iid int
		_, _ = fmt.Sscanf(strings.TrimPrefix(path, p+"/merge_requests/"), "%d", &iid)
		g.closed = append(g.closed, iid)
		answer(map[string]any{"iid": iid, "state": "closed"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// A branch and the default branch that both changed main.ts in the same
// place (a conflict) and lista.txt in different places (no conflict); the
// default branch also brings an executable script, and the branch has one
// of its own.
func mezclaDePrueba(g *proyecto) (base, main, head string) {
	base = g.commit(nil, map[string]entrada{
		"main.ts":   {"import a\nimport b\n\nfunc main() {}\n", false},
		"lista.txt": {"uno\ndos\ntres\ncuatro\ncinco\n", false},
		"run.sh":    {"#!/bin/sh\n", true},
	}, "base")
	main = g.commit([]string{base}, map[string]entrada{
		"main.ts":   {"import a\nimport b, s\n\nfunc main() {}\n", false},
		"lista.txt": {"uno\ndos\ntres\ncuatro\nCINCO\n", false},
		"run.sh":    {"#!/bin/sh\n", true},
		"nuevo.sh":  {"#!/bin/sh\necho hola\n", true},
	}, "main")
	head = g.commit([]string{base}, map[string]entrada{
		"main.ts":   {"import a\nimport c\n\nfunc main() {}\n", false},
		"lista.txt": {"UNO\ndos\ntres\ncuatro\ncinco\n", false},
		"run.sh":    {"#!/bin/sh\n", true},
		"otro.ts":   {"export {}\n", false},
	}, "rama")
	g.branches["main"], g.branches["rama"] = main, head
	return base, main, head
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
		b, _ := f.FileAt(ctx, 1, repo, p, base)
		merged, conflict, ok := mezcla.Merge(b, o, th, branch, main)
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

// GitLab writes no commit with two parents: CommitMerge makes the merge
// through a merge request whose sides have the same tree, so it merges
// clean, and the default branch is merged into the branch.
func TestCommitMergeOnGitLab(t *testing.T) {
	mergeWait = 0
	g := nuevoProyecto()
	_, main, head := mezclaDePrueba(g)
	srv := httptest.NewServer(g)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	ctx := context.Background()
	repo := RepoOf(&Connection{BaseURL: srv.URL}, "a/b")

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
	m := g.commits[sha]
	if g.branches["rama"] != sha || m == nil || len(m.parents) != 2 || m.message != "Merge main into rama" {
		t.Fatalf("the merge: %s %+v", sha, m)
	}
	ours, theirs := g.commits[m.parents[0]], g.commits[m.parents[1]]
	if len(ours.parents) != 1 || ours.parents[0] != head || len(theirs.parents) != 1 || theirs.parents[0] != main ||
		!strings.Contains(ours.message, "[skip ci]") || !strings.Contains(theirs.message, "[skip ci]") {
		t.Fatalf("the sides: %+v %+v", ours, theirs)
	}
	want := map[string]entrada{
		"main.ts":   {resuelto, false},
		"lista.txt": {"UNO\ndos\ntres\ncuatro\nCINCO\n", false},
		"run.sh":    {"#!/bin/sh\n", true},
		"nuevo.sh":  {"#!/bin/sh\necho hola\n", true},
		"otro.ts":   {"export {}\n", false},
	}
	if fmt.Sprint(m.tree) != fmt.Sprint(want) {
		t.Fatalf("the merge's tree:\n%v\nwant\n%v", m.tree, want)
	}
	// main is merged: a merge request of rama into it no longer conflicts.
	if g.mergeBase(sha, main) != main {
		t.Fatal("main is not an ancestor of the merge")
	}
	for b := range g.branches {
		if strings.HasPrefix(b, "rendimiento/") {
			t.Fatalf("the passing branch %s was left", b)
		}
	}
	if len(g.closed) != 0 {
		t.Fatalf("closed %v", g.closed)
	}
}

// A project that merges only by fast-forward or rebase cannot take the
// merge request: CommitMerge says so before writing anything.
func TestCommitMergeOnAFastForwardProject(t *testing.T) {
	mergeWait = 0
	g := nuevoProyecto()
	g.method = "ff"
	_, main, head := mezclaDePrueba(g)
	srv := httptest.NewServer(g)
	defer srv.Close()
	f, _ := nueva(srv.URL)
	repo := RepoOf(&Connection{BaseURL: srv.URL}, "a/b")
	_, err := f.CommitMerge(context.Background(), 1, repo, "rama", head, main, "Merge", map[string][]byte{"main.ts": []byte(resuelto)}, nil)
	if !errors.Is(err, ErrNoMergeCommit) || g.branches["rama"] != head || len(g.branches) != 2 {
		t.Fatalf("%v; branches %v", err, g.branches)
	}
}
