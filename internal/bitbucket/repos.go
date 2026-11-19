package bitbucket

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"testing/fstest"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// repoAPI is the API path of a repository: /repositories/workspace/slug.
func repoAPI(repo string) string {
	ws, slug, _ := strings.Cut(fullName(repo), "/")
	return "/repositories/" + url.PathEscape(ws) + "/" + url.PathEscape(slug)
}

// in calls the API on a repository, as the connection installation.
func (f *Forja) in(ctx context.Context, installation int64, repo, method, rest string, body, out any) error {
	c, _, err := f.connection(ctx, installation)
	if err != nil {
		return err
	}
	return f.api(ctx, c, method, repoAPI(repo)+rest, body, out)
}

// escapePath escapes a file's path segment by segment.
func escapePath(p string) string {
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// page is one page of a Bitbucket list.
type page[T any] struct {
	Values []T    `json:"values"`
	Next   string `json:"next"`
}

// list reads up to pages pages of a list, following each page's next;
// more is true when Bitbucket had more.
func list[T any](ctx context.Context, f *Forja, c *Connection, path string, pages int) (out []T, more bool, err error) {
	next := f.apiURL() + path
	for i := 0; i < pages && next != ""; i++ {
		var p page[T]
		if err := f.apiRaw(ctx, c, http.MethodGet, next, nil, &p); err != nil {
			return nil, false, err
		}
		out = append(out, p.Values...)
		next = p.Next
		if next != "" && !strings.HasPrefix(next, f.apiURL()+"/") {
			return out, true, nil // never follow a link elsewhere with the token
		}
	}
	return out, next != "", nil
}

// listIn is list on a repository of the connection installation.
func listIn[T any](ctx context.Context, f *Forja, installation int64, repo, rest string, pages int) ([]T, bool, error) {
	c, _, err := f.connection(ctx, installation)
	if err != nil {
		return nil, false, err
	}
	return list[T](ctx, f, c, repoAPI(repo)+rest, pages)
}

// ---- reading the code ----

// FileAt reads one file of repo at ref (a commit or a branch).
func (f *Forja) FileAt(ctx context.Context, installation int64, repo, filePath, ref string) ([]byte, error) {
	var raw []byte
	err := f.in(ctx, installation, repo, http.MethodGet, "/src/"+url.PathEscape(ref)+"/"+escapePath(filePath), nil, &raw)
	return raw, err
}

// The tree rendimiento reads: files over maxFile are left out (detection
// reads manifests, not data), at most maxTree in all, and, when it is read
// file by file, at most maxFiles.
const (
	maxFile  = 1 << 20
	maxTree  = 64 << 20
	maxFiles = 400
)

// RepoFS is repo's tree at ref: its archive in one request, or, when
// Bitbucket does not give it, read file by file through the API.
func (f *Forja) RepoFS(ctx context.Context, installation int64, repo, ref string) (fs.FS, error) {
	c, _, err := f.connection(ctx, installation)
	if err != nil {
		return nil, err
	}
	var raw []byte
	archive := f.siteURL() + "/" + fullName(repo) + "/get/" + url.PathEscape(ref) + ".tar.gz"
	if err := f.apiRaw(ctx, c, http.MethodGet, archive, nil, &raw); err == nil {
		if tree, err := untar(raw); err == nil {
			return tree, nil
		}
	}
	return f.walk(ctx, c, repo, ref)
}

// walk reads a tree through /src: its listing, then each file.
func (f *Forja) walk(ctx context.Context, c *Connection, repo, ref string) (fs.FS, error) {
	type entry struct {
		Type string `json:"type"` // commit_file, commit_directory
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	entries, _, err := list[entry](ctx, f, c, repoAPI(repo)+"/src/"+url.PathEscape(ref)+"/?max_depth=20&pagelen=100", 20)
	if err != nil {
		return nil, err
	}
	out := fstest.MapFS{}
	total, files := 0, 0
	for _, e := range entries {
		name := path.Clean(e.Path)
		if name == "." || strings.Contains(name, "..") {
			continue
		}
		switch e.Type {
		case "commit_directory":
			out[name] = &fstest.MapFile{Mode: fs.ModeDir | 0o755}
		case "commit_file":
			if e.Size > maxFile || total+int(e.Size) > maxTree || files >= maxFiles {
				continue
			}
			var data []byte
			if err := f.api(ctx, c, http.MethodGet, repoAPI(repo)+"/src/"+url.PathEscape(ref)+"/"+escapePath(name), nil, &data); err != nil {
				return nil, err
			}
			total += len(data)
			files++
			out[name] = &fstest.MapFile{Data: data, Mode: 0o644}
		}
	}
	return out, nil
}

// untar reads a repository's archive into a tree, without its first folder
// (workspace-repo-sha/).
func untar(raw []byte) (fs.FS, error) {
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(zr)
	out := fstest.MapFS{}
	total := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		_, name, ok := strings.Cut(h.Name, "/")
		if !ok || name == "" || strings.Contains(name, "..") {
			continue
		}
		name = path.Clean(strings.TrimSuffix(name, "/"))
		switch h.Typeflag {
		case tar.TypeDir:
			out[name] = &fstest.MapFile{Mode: fs.ModeDir | 0o755}
		case tar.TypeReg:
			if h.Size > maxFile || total+int(h.Size) > maxTree {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(tr, maxFile))
			if err != nil {
				return nil, err
			}
			total += len(data)
			out[name] = &fstest.MapFile{Data: data, Mode: fs.FileMode(h.Mode) & 0o777}
		}
	}
	return out, nil
}

// ChangedFiles lists the files that differ between base and head, from
// their merge base (Bitbucket's diffstat of head..base); complete is false
// when the list was cut.
func (f *Forja) ChangedFiles(ctx context.Context, installation int64, repo, base, head string) ([]string, bool, error) {
	type stat struct {
		Old *struct {
			Path string `json:"path"`
		} `json:"old"`
		New *struct {
			Path string `json:"path"`
		} `json:"new"`
	}
	stats, more, err := listIn[stat](ctx, f, installation, repo, "/diffstat/"+url.PathEscape(head)+".."+url.PathEscape(base)+"?pagelen=500", 4)
	if err != nil {
		return nil, false, err
	}
	seen := map[string]bool{}
	var files []string
	for _, s := range stats {
		for _, side := range []*struct {
			Path string `json:"path"`
		}{s.Old, s.New} {
			if side != nil && side.Path != "" && !seen[side.Path] {
				seen[side.Path] = true
				files = append(files, side.Path)
			}
		}
	}
	return files, !more, nil
}

// bbBranch is a branch as Bitbucket writes it.
type bbBranch struct {
	Name   string `json:"name"`
	Target struct {
		Hash string `json:"hash"`
	} `json:"target"`
}

// BranchSHA is the commit a branch points at.
func (f *Forja) BranchSHA(ctx context.Context, installation int64, repo, branch string) (string, error) {
	var b bbBranch
	err := f.in(ctx, installation, repo, http.MethodGet, "/refs/branches/"+url.PathEscape(branch), nil, &b)
	return b.Target.Hash, err
}

// Branches lists the repository's branches (the first 100).
func (f *Forja) Branches(ctx context.Context, installation int64, repo string) ([]forja.Branch, error) {
	list, _, err := listIn[bbBranch](ctx, f, installation, repo, "/refs/branches?pagelen=100", 1)
	if err != nil {
		return nil, err
	}
	out := make([]forja.Branch, len(list))
	for i, b := range list {
		out[i].Name, out[i].Commit.SHA = b.Name, b.Target.Hash
	}
	return out, nil
}

// bbCommit is a commit as Bitbucket writes it.
type bbCommit struct {
	Hash    string    `json:"hash"`
	Message string    `json:"message"`
	Date    time.Time `json:"date"`
	Parents []struct {
		Hash string `json:"hash"`
	} `json:"parents"`
}

func (f *Forja) commit(ctx context.Context, installation int64, repo, sha string) (*bbCommit, error) {
	var c bbCommit
	err := f.in(ctx, installation, repo, http.MethodGet, "/commit/"+url.PathEscape(sha), nil, &c)
	return &c, err
}

// CommitSubject is the first line of a commit's message.
func (f *Forja) CommitSubject(ctx context.Context, installation int64, repo, sha string) (string, error) {
	c, err := f.commit(ctx, installation, repo, sha)
	if err != nil {
		return "", err
	}
	subject, _, _ := strings.Cut(c.Message, "\n")
	return strings.TrimSpace(subject), nil
}

// MergeBase is the best common ancestor of two commits or branches, with
// its commit time.
func (f *Forja) MergeBase(ctx context.Context, installation int64, repo, base, head string) (string, time.Time, error) {
	var c bbCommit
	err := f.in(ctx, installation, repo, http.MethodGet, "/merge-base/"+url.PathEscape(base)+".."+url.PathEscape(head), nil, &c)
	return c.Hash, c.Date, err
}

// FirstParent is a commit's first parent ("" for the first commit).
func (f *Forja) FirstParent(ctx context.Context, installation int64, repo, sha string) (string, error) {
	c, err := f.commit(ctx, installation, repo, sha)
	if err != nil {
		return "", err
	}
	if len(c.Parents) == 0 {
		return "", nil
	}
	return c.Parents[0].Hash, nil
}

// ---- cloning ----

// CloneToken is what build pods clone with: Bitbucket takes an OAuth
// token as the password of the user "x-token-auth" (the clone script uses
// a token with a ":" as user:password).
func (f *Forja) CloneToken(ctx context.Context, installation int64) (string, error) {
	c, _, err := f.connection(ctx, installation)
	if err != nil {
		return "", err
	}
	tok, err := f.token(ctx, c, false)
	if err != nil {
		return "", err
	}
	return "x-token-auth:" + tok, nil
}

// ---- saying whether a commit passed: build statuses ----

// StatusState is the build status a check is shown as on Bitbucket, and
// its description. A person must merge it (action_required) is
// INPROGRESS with a description that says so: Bitbucket has no state of
// its own for it.
func StatusState(c forja.Check) (state, description string) {
	if c.Output != nil {
		description = c.Output.Title
	}
	switch c.Status {
	case "completed":
		switch c.Conclusion {
		case "success", "neutral", "skipped":
			state = "SUCCESSFUL"
		case "cancelled":
			state = "STOPPED"
		case "action_required":
			state = "INPROGRESS"
			if description == "" {
				description = "a person must merge it in rendimiento"
			}
		default: // failure, timed_out
			state = "FAILED"
		}
	default: // queued, in_progress
		state = "INPROGRESS"
	}
	if r := []rune(description); len(r) > 255 {
		description = string(r[:254]) + "…"
	}
	return state, description
}

// statusKey is the key a check's build status is kept under: Bitbucket
// keeps one per commit and key, so writing it again updates it.
func statusKey(name string) string {
	k := strings.Trim(notName.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if k == "" || len(k) > 40 {
		sum := sha1.Sum([]byte(name))
		return "rendimiento-" + hex.EncodeToString(sum[:])[:16]
	}
	return k
}

// postStatus writes a commit's build status.
func (f *Forja) postStatus(ctx context.Context, installation int64, repo string, c forja.Check) error {
	if c.HeadSHA == "" {
		return errors.New("a Bitbucket build status needs its commit")
	}
	state, desc := StatusState(c)
	link := c.DetailsURL
	if link == "" {
		link = strings.TrimRight(f.BaseURL, "/") // Bitbucket asks for one
	}
	body := map[string]any{"key": statusKey(c.Name), "state": state, "name": c.Name, "url": link}
	if desc != "" {
		body["description"] = desc
	}
	return f.in(ctx, installation, repo, http.MethodPost, "/commit/"+url.PathEscape(c.HeadSHA)+"/statuses/build", body, nil)
}

// CreateCheck writes the commit's build status under the check's name. A
// build status has no number: its id is 1, so the platform finishes it
// (by name and commit, see UpdateCheck).
func (f *Forja) CreateCheck(ctx context.Context, installation int64, repo string, c forja.Check) (int64, error) {
	if err := f.postStatus(ctx, installation, repo, c); err != nil {
		return 0, err
	}
	return 1, nil
}

// UpdateCheck writes the status again under the same key: the check must
// say its name and commit (the platform does).
func (f *Forja) UpdateCheck(ctx context.Context, installation int64, repo string, _ int64, c forja.Check) error {
	return f.postStatus(ctx, installation, repo, c)
}

// ---- pull requests ----

// bbPR is a pull request as Bitbucket writes it.
type bbPR struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"` // OPEN, MERGED, DECLINED, SUPERSEDED
	Draft  bool   `json:"draft"`
	Source struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
		Commit struct {
			Hash string `json:"hash"`
		} `json:"commit"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	} `json:"source"`
	Destination struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	} `json:"destination"`
	MergeCommit *struct {
		Hash string `json:"hash"`
	} `json:"merge_commit"`
	CreatedOn time.Time `json:"created_on"`
	UpdatedOn time.Time `json:"updated_on"`
	Author    struct {
		Nickname string `json:"nickname"`
		Type     string `json:"type"` // user, app_user, team
	} `json:"author"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

// change is a pull request in the forja's words. repo is the
// destination's.
func (m bbPR) change(repo string) forja.Change {
	var c forja.Change
	c.Number, c.Title, c.Draft = m.ID, m.Title, m.Draft
	c.State = "closed"
	if m.State == "OPEN" {
		c.State = "open"
	}
	c.Head.Ref, c.Head.SHA, c.Base.Ref = m.Source.Branch.Name, m.Source.Commit.Hash, m.Destination.Branch.Name
	c.Head.Repo.FullName = repo
	if src := m.Source.Repository.FullName; src != "" && !strings.EqualFold(RepoOf(src), repo) {
		c.Head.Repo.FullName = RepoOf(src) // a fork's: never built
	}
	if m.State != "OPEN" {
		at := m.UpdatedOn
		c.ClosedAt = &at
		if m.State == "MERGED" {
			c.MergedAt = &at
			if m.MergeCommit != nil {
				c.MergeCommitSHA = m.MergeCommit.Hash
			}
		}
	}
	c.CreatedAt = m.CreatedOn
	c.User.Login = m.Author.Nickname
	if m.Author.Type == "app_user" || strings.HasSuffix(strings.ToLower(m.Author.Nickname), "[bot]") || strings.EqualFold(m.Author.Nickname, "renovate-bot") {
		c.User.Type = "Bot"
	}
	return c
}

// fullHeads writes the whole commit of each open pull request's head:
// Bitbucket lists them cut to 12 characters, and runs are by the whole
// one. The branches give most at once; the rest are asked one by one.
func (f *Forja) fullHeads(ctx context.Context, installation int64, repo string, changes []forja.Change) {
	var heads map[string]string
	asked := 0
	for i := range changes {
		c := &changes[i]
		if c.State != "open" || len(c.Head.SHA) == 40 || c.Head.SHA == "" {
			continue
		}
		if heads == nil {
			heads = map[string]string{}
			if list, err := f.Branches(ctx, installation, repo); err == nil {
				for _, b := range list {
					heads[b.Name] = b.Commit.SHA
				}
			}
		}
		if full := heads[c.Head.Ref]; strings.HasPrefix(full, c.Head.SHA) {
			c.Head.SHA = full
			continue
		}
		if asked < 20 {
			asked++
			if cm, err := f.commit(ctx, installation, repo, c.Head.SHA); err == nil && strings.HasPrefix(cm.Hash, c.Head.SHA) {
				c.Head.SHA = cm.Hash
			}
		}
	}
}

// prs lists pull requests with the query given, up to pages of 50.
func (f *Forja) prs(ctx context.Context, installation int64, repo, query string, pages int) ([]forja.Change, error) {
	list, _, err := listIn[bbPR](ctx, f, installation, repo, "/pullrequests?"+query, pages)
	if err != nil {
		return nil, err
	}
	out := make([]forja.Change, len(list))
	for i, m := range list {
		out[i] = m.change(repo)
	}
	f.fullHeads(ctx, installation, repo, out)
	return out, nil
}

// RecentPullRequests lists the repository's 100 most recently updated pull
// requests, open and closed.
func (f *Forja) RecentPullRequests(ctx context.Context, installation int64, repo string) ([]forja.Change, error) {
	return f.prs(ctx, installation, repo, "state=OPEN&state=MERGED&state=DECLINED&state=SUPERSEDED&sort=-updated_on&pagelen=50", 2)
}

// OpenPulls lists the open pull requests (up to 100), the most recently
// updated first.
func (f *Forja) OpenPulls(ctx context.Context, installation int64, repo string) ([]forja.Change, error) {
	return f.prs(ctx, installation, repo, "state=OPEN&sort=-updated_on&pagelen=50", 2)
}

// openFrom lists the open pull requests from a branch.
func (f *Forja) openFrom(ctx context.Context, installation int64, repo, branch string) ([]bbPR, error) {
	q := url.Values{"state": {"OPEN"}, "q": {fmt.Sprintf(`source.branch.name = "%s"`, strings.ReplaceAll(branch, `"`, `\"`))}}
	list, _, err := listIn[bbPR](ctx, f, installation, repo, "/pullrequests?"+q.Encode(), 1)
	return list, err
}

// Pull is one pull request, with whether it can be merged: Bitbucket does
// not say it, so its diffstat is read; a file in "merge conflict" is a
// conflict ("dirty"), and a draft waits ("blocked").
func (f *Forja) Pull(ctx context.Context, installation int64, repo string, number int) (*forja.Change, error) {
	var m bbPR
	if err := f.in(ctx, installation, repo, http.MethodGet, fmt.Sprintf("/pullrequests/%d", number), nil, &m); err != nil {
		return nil, err
	}
	changes := []forja.Change{m.change(repo)}
	f.fullHeads(ctx, installation, repo, changes)
	c := changes[0]
	if c.State != "open" {
		return &c, nil
	}
	type stat struct {
		Status string `json:"status"`
	}
	stats, _, err := listIn[stat](ctx, f, installation, repo, fmt.Sprintf("/pullrequests/%d/diffstat?pagelen=500", number), 4)
	if err != nil {
		return &c, nil // unknown: Mergeable stays nil, and the next look decides
	}
	yes, no := true, false
	c.Mergeable, c.MergeableState = &yes, "clean"
	for _, s := range stats {
		if strings.Contains(s.Status, "conflict") || s.Status == "local deleted" || s.Status == "remote deleted" {
			c.Mergeable, c.MergeableState = &no, "dirty"
			break
		}
	}
	if m.Draft && c.MergeableState == "clean" {
		c.MergeableState = "blocked"
	}
	return &c, nil
}

// MergePull merges a pull request while its head is still sha: Bitbucket's
// merge takes no commit, so the head is read first, and a head that moved
// is forja.ErrConflict.
func (f *Forja) MergePull(ctx context.Context, installation int64, repo string, number int, sha string) error {
	pr, err := f.Pull(ctx, installation, repo, number)
	if err != nil {
		return err
	}
	if pr.State != "open" || pr.Head.SHA != sha {
		return fmt.Errorf("pull request #%d moved to %s: %w", number, pr.Head.SHA, forja.ErrConflict)
	}
	var m bbPR
	if err := f.in(ctx, installation, repo, http.MethodPost, fmt.Sprintf("/pullrequests/%d/merge", number), map[string]any{"type": "pullrequest"}, &m); err != nil {
		return err
	}
	if m.State != "" && m.State != "MERGED" {
		return fmt.Errorf("bitbucket left pull request #%d %s", number, strings.ToLower(m.State))
	}
	return nil
}

// newPR opens a pull request from branch into base.
func (f *Forja) newPR(ctx context.Context, installation int64, repo, base, branch, title, body string) (*bbPR, error) {
	var m bbPR
	err := f.in(ctx, installation, repo, http.MethodPost, "/pullrequests", map[string]any{
		"title": title, "description": body,
		"source":      map[string]any{"branch": map[string]string{"name": branch}},
		"destination": map[string]any{"branch": map[string]string{"name": base}},
	}, &m)
	return &m, err
}

// CreatePull opens a pull request from a branch into base and returns its
// number; when the branch already has one open, that one's.
func (f *Forja) CreatePull(ctx context.Context, installation int64, repo, base, branch, title, body string) (int, error) {
	open, err := f.openFrom(ctx, installation, repo, branch)
	if err != nil {
		return 0, err
	}
	if len(open) > 0 {
		return open[0].ID, nil
	}
	m, err := f.newPR(ctx, installation, repo, base, branch, title, body)
	if err != nil {
		return 0, err
	}
	return m.ID, nil
}

// Comment writes a comment on a pull request and returns its id; with id
// set, it edits that comment instead.
func (f *Forja) Comment(ctx context.Context, installation int64, repo string, number int, id int64, body string) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	content := map[string]any{"content": map[string]string{"raw": body}}
	if id != 0 {
		err := f.in(ctx, installation, repo, http.MethodPut, fmt.Sprintf("/pullrequests/%d/comments/%d", number, id), content, &out)
		if !forja.IsNotFound(err) {
			return id, err
		}
	}
	err := f.in(ctx, installation, repo, http.MethodPost, fmt.Sprintf("/pullrequests/%d/comments", number), content, &out)
	return out.ID, err
}

// ClosePull declines a pull request.
func (f *Forja) ClosePull(ctx context.Context, installation int64, repo string, number int) error {
	return f.in(ctx, installation, repo, http.MethodPost, fmt.Sprintf("/pullrequests/%d/decline", number), nil, nil)
}

// DeleteBranch deletes a branch; one already gone is not an error.
func (f *Forja) DeleteBranch(ctx context.Context, installation int64, repo, branch string) error {
	err := f.in(ctx, installation, repo, http.MethodDelete, "/refs/branches/"+url.PathEscape(branch), nil, nil)
	if forja.IsNotFound(err) {
		return nil
	}
	return err
}

// OpenPR commits files on a new branch off base in one commit (Bitbucket's
// /src, as a form) and opens a pull request (or finds the one open).
// Running it again with the same branch makes the branch again.
func (f *Forja) OpenPR(ctx context.Context, installation int64, repo, base, branch, title, body string, files map[string]string) (*forja.Opened, error) {
	if err := f.DeleteBranch(ctx, installation, repo, branch); err != nil {
		return nil, err
	}
	parent, err := f.BranchSHA(ctx, installation, repo, base)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("message", title)
	_ = mw.WriteField("branch", branch)
	_ = mw.WriteField("parents", parent)
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		w, err := mw.CreateFormFile(strings.TrimPrefix(p, "/"), path.Base(p))
		if err != nil {
			return nil, err
		}
		_, _ = io.WriteString(w, files[p])
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	if err := f.in(ctx, installation, repo, http.MethodPost, "/src", form{contentType: mw.FormDataContentType(), data: buf.Bytes()}, nil); err != nil {
		return nil, err
	}
	open, err := f.openFrom(ctx, installation, repo, branch)
	if err != nil {
		return nil, err
	}
	if len(open) > 0 {
		return &forja.Opened{Number: open[0].ID, HTMLURL: open[0].Links.HTML.Href}, nil
	}
	m, err := f.newPR(ctx, installation, repo, base, branch, title, body)
	if err != nil {
		return nil, err
	}
	return &forja.Opened{Number: m.ID, HTMLURL: m.Links.HTML.Href}, nil
}

// ErrNoMergeCommit: Bitbucket's API writes commits with one parent, so
// rendimiento cannot join a conflict there itself yet.
var ErrNoMergeCommit = errors.New("on Bitbucket, rendimiento cannot write a merge commit yet")

// CommitMerge is not done on Bitbucket yet (ErrNoMergeCommit): the
// conflict stays a person's.
func (f *Forja) CommitMerge(context.Context, int64, string, string, string, string, string, map[string][]byte, []string) (string, error) {
	return "", ErrNoMergeCommit
}

// ---- for the wizard ----

// Repository is a repository of the workspace the wizard can make an app
// of.
type Repository struct {
	Slug          string    `json:"slug"`
	FullName      string    `json:"fullName"` // workspace/slug
	Repo          string    `json:"repo"`     // bitbucket.org/workspace/slug
	DefaultBranch string    `json:"defaultBranch"`
	WebURL        string    `json:"webUrl"`
	Private       bool      `json:"private"`
	UpdatedOn     time.Time `json:"updatedOn"`
}

// Repositories lists the workspace's repositories the connection
// administers (where it may add a webhook), the most recently updated
// first, up to 300.
func (f *Forja) Repositories(ctx context.Context, installation int64) ([]Repository, error) {
	c, _, err := f.connection(ctx, installation)
	if err != nil {
		return nil, err
	}
	type repo struct {
		Slug       string    `json:"slug"`
		FullName   string    `json:"full_name"`
		IsPrivate  bool      `json:"is_private"`
		UpdatedOn  time.Time `json:"updated_on"`
		Mainbranch *struct {
			Name string `json:"name"`
		} `json:"mainbranch"`
		Links struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	}
	q := url.Values{"role": {"admin"}, "sort": {"-updated_on"}, "pagelen": {"100"}}
	list, _, err := list[repo](ctx, f, c, "/repositories/"+url.PathEscape(c.Workspace)+"?"+q.Encode(), 3)
	if err != nil {
		return nil, err
	}
	out := []Repository{}
	for _, r := range list {
		branch := "main"
		if r.Mainbranch != nil && r.Mainbranch.Name != "" {
			branch = r.Mainbranch.Name
		}
		out = append(out, Repository{Slug: r.Slug, FullName: r.FullName, Repo: RepoOf(r.FullName), DefaultBranch: branch,
			WebURL: r.Links.HTML.Href, Private: r.IsPrivate, UpdatedOn: r.UpdatedOn})
	}
	return out, nil
}

// HookEvents are what the repository's webhook delivers.
var HookEvents = []string{"repo:push", "pullrequest:created", "pullrequest:updated", "pullrequest:fulfilled", "pullrequest:rejected"}

// EnsureHook adds the repository's webhook to rendimiento (pushes and pull
// requests, signed with the connection's secret), or brings the one there
// up to date: rendimiento does it for the person when an app is created.
func (f *Forja) EnsureHook(ctx context.Context, installation int64, repo string) error {
	c, sec, err := f.connection(ctx, installation)
	if err != nil {
		return err
	}
	hookURL := strings.TrimRight(f.BaseURL, "/") + WebhookPath
	type hook struct {
		UUID string `json:"uuid"`
		URL  string `json:"url"`
	}
	hooks, _, err := list[hook](ctx, f, c, repoAPI(repo)+"/hooks?pagelen=100", 1)
	if err != nil {
		return err
	}
	body := map[string]any{"description": "rendimiento", "url": hookURL, "active": true, "secret": sec.WebhookSecret, "events": HookEvents}
	for _, h := range hooks {
		if h.URL == hookURL {
			return f.api(ctx, c, http.MethodPut, repoAPI(repo)+"/hooks/"+url.PathEscape(h.UUID), body, nil)
		}
	}
	return f.api(ctx, c, http.MethodPost, repoAPI(repo)+"/hooks", body, nil)
}
