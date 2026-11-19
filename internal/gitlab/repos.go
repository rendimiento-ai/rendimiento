package gitlab

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"testing/fstest"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// projectAPI is the API path of a project of the connection: its path,
// escaped as one segment (group%2Fproject).
func projectAPI(c *Connection, repo string) string {
	return "/projects/" + url.PathEscape(project(c, repo))
}

// in calls the API on a project, as the connection installation.
func (f *Forja) in(ctx context.Context, installation int64, repo, method, rest string, body, out any) error {
	c, _, err := f.connection(ctx, installation)
	if err != nil {
		return err
	}
	return f.api(ctx, c, method, projectAPI(c, repo)+rest, body, out)
}

// ---- reading the code ----

// FileAt reads one file of repo at ref.
func (f *Forja) FileAt(ctx context.Context, installation int64, repo, filePath, ref string) ([]byte, error) {
	var raw []byte
	err := f.in(ctx, installation, repo, http.MethodGet, "/repository/files/"+url.PathEscape(strings.TrimPrefix(filePath, "/"))+"/raw?ref="+url.QueryEscape(ref), nil, &raw)
	return raw, err
}

// The archive rendimiento reads a tree from: files over maxFile are left
// out (detection reads manifests, not data), and at most maxTree in all.
const (
	maxFile = 1 << 20
	maxTree = 64 << 20
)

// RepoFS is repo's tree at ref, read from its archive in one request.
func (f *Forja) RepoFS(ctx context.Context, installation int64, repo, ref string) (fs.FS, error) {
	var raw []byte
	if err := f.in(ctx, installation, repo, http.MethodGet, "/repository/archive.tar.gz?sha="+url.QueryEscape(ref), nil, &raw); err != nil {
		return nil, err
	}
	return untar(raw)
}

// untar reads a project's archive into a tree, without its first folder
// (project-ref-sha/).
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

// ChangedFiles lists the files that differ between base and head (from
// their merge base, as GitHub's compare does); complete is false when
// GitLab gave up or cut the list.
func (f *Forja) ChangedFiles(ctx context.Context, installation int64, repo, base, head string) ([]string, bool, error) {
	var out struct {
		Diffs []struct {
			OldPath string `json:"old_path"`
			NewPath string `json:"new_path"`
		} `json:"diffs"`
		CompareTimeout bool `json:"compare_timeout"`
	}
	q := url.Values{"from": {base}, "to": {head}, "straight": {"false"}}
	if err := f.in(ctx, installation, repo, http.MethodGet, "/repository/compare?"+q.Encode(), nil, &out); err != nil {
		return nil, false, err
	}
	seen := map[string]bool{}
	var files []string
	for _, d := range out.Diffs {
		for _, p := range []string{d.OldPath, d.NewPath} {
			if p != "" && !seen[p] {
				seen[p] = true
				files = append(files, p)
			}
		}
	}
	return files, !out.CompareTimeout && len(out.Diffs) < 1000, nil
}

// glBranch is a branch as GitLab writes it.
type glBranch struct {
	Name   string `json:"name"`
	Commit struct {
		ID string `json:"id"`
	} `json:"commit"`
}

// BranchSHA is the commit a branch points at.
func (f *Forja) BranchSHA(ctx context.Context, installation int64, repo, branch string) (string, error) {
	var b glBranch
	err := f.in(ctx, installation, repo, http.MethodGet, "/repository/branches/"+url.PathEscape(branch), nil, &b)
	return b.Commit.ID, err
}

// Branches lists the project's branches (the first 100).
func (f *Forja) Branches(ctx context.Context, installation int64, repo string) ([]forja.Branch, error) {
	var list []glBranch
	if err := f.in(ctx, installation, repo, http.MethodGet, "/repository/branches?per_page=100", nil, &list); err != nil {
		return nil, err
	}
	out := make([]forja.Branch, len(list))
	for i, b := range list {
		out[i].Name, out[i].Commit.SHA = b.Name, b.Commit.ID
	}
	return out, nil
}

// glCommit is a commit as GitLab writes it.
type glCommit struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	ParentIDs     []string  `json:"parent_ids"`
	CommittedDate time.Time `json:"committed_date"`
}

// CommitSubject is the first line of a commit's message.
func (f *Forja) CommitSubject(ctx context.Context, installation int64, repo, sha string) (string, error) {
	var c glCommit
	err := f.in(ctx, installation, repo, http.MethodGet, "/repository/commits/"+url.PathEscape(sha), nil, &c)
	return strings.TrimSpace(c.Title), err
}

// MergeBase is the best common ancestor of two commits or branches, with
// its commit time.
func (f *Forja) MergeBase(ctx context.Context, installation int64, repo, base, head string) (string, time.Time, error) {
	var c glCommit
	q := url.Values{"refs[]": {base, head}}
	err := f.in(ctx, installation, repo, http.MethodGet, "/repository/merge_base?"+q.Encode(), nil, &c)
	return c.ID, c.CommittedDate, err
}

// FirstParent is a commit's first parent ("" for the first commit).
func (f *Forja) FirstParent(ctx context.Context, installation int64, repo, sha string) (string, error) {
	var c glCommit
	if err := f.in(ctx, installation, repo, http.MethodGet, "/repository/commits/"+url.PathEscape(sha), nil, &c); err != nil {
		return "", err
	}
	if len(c.ParentIDs) == 0 {
		return "", nil
	}
	return c.ParentIDs[0], nil
}

// ---- cloning ----

// CloneToken is what build pods clone with: GitLab takes an OAuth token
// as the password of the user "oauth2" (the clone script uses a token
// with a ":" as user:password).
func (f *Forja) CloneToken(ctx context.Context, installation int64) (string, error) {
	c, _, err := f.connection(ctx, installation)
	if err != nil {
		return "", err
	}
	tok, err := f.token(ctx, c, false)
	if err != nil {
		return "", err
	}
	return "oauth2:" + tok, nil
}

// ---- saying whether a commit passed: commit statuses ----

// StatusState is the commit status a check is shown as on GitLab, and
// its description. A person must merge it (action_required) is pending
// with a description that says so: GitLab has no state of its own for it.
func StatusState(c forja.Check) (state, description string) {
	if c.Output != nil {
		description = c.Output.Title
	}
	switch c.Status {
	case "queued":
		state = "pending"
	case "completed":
		switch c.Conclusion {
		case "success", "neutral", "skipped":
			state = "success"
		case "cancelled":
			state = "canceled"
		case "action_required":
			state = "pending"
			if description == "" {
				description = "a person must merge it in rendimiento"
			}
		default: // failure, timed_out
			state = "failed"
		}
	default: // in_progress
		state = "running"
	}
	if r := []rune(description); len(r) > 255 {
		description = string(r[:254]) + "…"
	}
	return state, description
}

// postStatus writes a commit status and returns its id.
func (f *Forja) postStatus(ctx context.Context, installation int64, repo string, c forja.Check) (int64, error) {
	if c.HeadSHA == "" {
		return 0, errors.New("a GitLab commit status needs its commit")
	}
	state, desc := StatusState(c)
	body := map[string]any{"state": state, "name": c.Name}
	if desc != "" {
		body["description"] = desc
	}
	if c.DetailsURL != "" {
		body["target_url"] = c.DetailsURL
	}
	var out struct {
		ID int64 `json:"id"`
	}
	err := f.in(ctx, installation, repo, http.MethodPost, "/statuses/"+url.PathEscape(c.HeadSHA), body, &out)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusBadRequest && state == "pending" && strings.Contains(ae.Body, "transition") {
		// A running status cannot go back to pending: finishing it lets a
		// new pending one take its place.
		if err := f.in(ctx, installation, repo, http.MethodPost, "/statuses/"+url.PathEscape(c.HeadSHA), map[string]any{"state": "canceled", "name": c.Name}, nil); err != nil {
			return 0, err
		}
		err = f.in(ctx, installation, repo, http.MethodPost, "/statuses/"+url.PathEscape(c.HeadSHA), body, &out)
	}
	return out.ID, err
}

// CreateCheck writes the commit's status under the check's name.
func (f *Forja) CreateCheck(ctx context.Context, installation int64, repo string, c forja.Check) (int64, error) {
	return f.postStatus(ctx, installation, repo, c)
}

// UpdateCheck writes the status again: GitLab keeps one per commit and
// name, so the check must say both (the platform does).
func (f *Forja) UpdateCheck(ctx context.Context, installation int64, repo string, _ int64, c forja.Check) error {
	_, err := f.postStatus(ctx, installation, repo, c)
	return err
}

// ---- merge requests ----

// glMR is a merge request as GitLab writes it.
type glMR struct {
	IID                 int        `json:"iid"`
	Title               string     `json:"title"`
	Labels              []string   `json:"labels"`
	State               string     `json:"state"` // opened, closed, merged, locked
	Draft               bool       `json:"draft"`
	WorkInProgress      bool       `json:"work_in_progress"`
	SourceBranch        string     `json:"source_branch"`
	TargetBranch        string     `json:"target_branch"`
	SHA                 string     `json:"sha"`
	SourceProjectID     int64      `json:"source_project_id"`
	TargetProjectID     int64      `json:"target_project_id"`
	MergedAt            *time.Time `json:"merged_at"`
	MergeCommitSHA      string     `json:"merge_commit_sha"`
	SquashCommitSHA     string     `json:"squash_commit_sha"`
	CreatedAt           time.Time  `json:"created_at"`
	ClosedAt            *time.Time `json:"closed_at"`
	WebURL              string     `json:"web_url"`
	MergeStatus         string     `json:"merge_status"`
	DetailedMergeStatus string     `json:"detailed_merge_status"`
	Author              struct {
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"author"`
}

// change is a merge request in the forja's words. repo is the target's.
func (m glMR) change(repo string) forja.Change {
	var c forja.Change
	c.Number, c.Title, c.Draft = m.IID, m.Title, m.Draft || m.WorkInProgress
	for _, l := range m.Labels {
		c.Labels = append(c.Labels, struct {
			Name string `json:"name"`
		}{l})
	}
	switch m.State {
	case "opened", "locked":
		c.State = "open"
	default:
		c.State = "closed"
	}
	c.Head.Ref, c.Head.SHA, c.Base.Ref = m.SourceBranch, m.SHA, m.TargetBranch
	c.Head.Repo.FullName = repo
	if m.SourceProjectID != 0 && m.SourceProjectID != m.TargetProjectID {
		c.Head.Repo.FullName = fmt.Sprintf("fork/%d", m.SourceProjectID) // never this repository
	}
	if m.State == "merged" {
		c.MergedAt = m.MergedAt
		if c.MergedAt == nil {
			c.MergedAt = m.ClosedAt
		}
		if c.MergedAt == nil {
			now := time.Now()
			c.MergedAt = &now
		}
		c.MergeCommitSHA = m.MergeCommitSHA
		if c.MergeCommitSHA == "" {
			c.MergeCommitSHA = m.SquashCommitSHA
		}
	}
	c.CreatedAt, c.ClosedAt = m.CreatedAt, m.ClosedAt
	c.User.Login = m.Author.Username
	if m.Author.Bot {
		c.User.Type = "Bot"
	}
	c.Mergeable, c.MergeableState = mergeable(m)
	return c
}

// mergeable reads whether GitLab would merge it, the way GitHub says it:
// nil while it is still checking, false and "dirty" for a conflict,
// "blocked" for what GitLab asks first (a review, a draft). A pipeline
// still running is not blocking: merging then waits for it (see
// MergePull).
func mergeable(m glMR) (*bool, string) {
	yes, no := true, false
	switch m.DetailedMergeStatus {
	case "mergeable", "ci_must_pass", "ci_still_running":
		return &yes, "clean"
	case "conflict", "broken_status", "need_rebase":
		return &no, "dirty"
	case "checking", "unchecked", "preparing", "approvals_syncing":
		return nil, ""
	case "":
		switch m.MergeStatus { // GitLab before 15.6
		case "can_be_merged":
			return &yes, "clean"
		case "cannot_be_merged":
			return &no, "dirty"
		default:
			return nil, ""
		}
	}
	return &yes, "blocked"
}

// mrs lists merge requests with the query given.
func (f *Forja) mrs(ctx context.Context, installation int64, repo, query string) ([]forja.Change, error) {
	var list []glMR
	if err := f.in(ctx, installation, repo, http.MethodGet, "/merge_requests?"+query, nil, &list); err != nil {
		return nil, err
	}
	out := make([]forja.Change, len(list))
	for i, m := range list {
		out[i] = m.change(repo)
	}
	return out, nil
}

// RecentPullRequests lists the project's 100 most recently updated merge
// requests, open and closed.
func (f *Forja) RecentPullRequests(ctx context.Context, installation int64, repo string) ([]forja.Change, error) {
	return f.mrs(ctx, installation, repo, "state=all&order_by=updated_at&sort=desc&per_page=100")
}

// OpenPulls lists the open merge requests (up to 100), the most recently
// updated first.
func (f *Forja) OpenPulls(ctx context.Context, installation int64, repo string) ([]forja.Change, error) {
	return f.mrs(ctx, installation, repo, "state=opened&order_by=updated_at&sort=desc&per_page=100")
}

// Pull is one merge request, with whether it can be merged.
func (f *Forja) Pull(ctx context.Context, installation int64, repo string, number int) (*forja.Change, error) {
	var m glMR
	if err := f.in(ctx, installation, repo, http.MethodGet, fmt.Sprintf("/merge_requests/%d", number), nil, &m); err != nil {
		return nil, err
	}
	c := m.change(repo)
	return &c, nil
}

// waits is a merge GitLab will do itself, once the pipeline passes.
type waits struct{}

func (waits) Error() string { return "GitLab merges it when its pipeline succeeds" }
func (waits) Says() string  { return "GitLab merges it when its pipeline succeeds" }

// MergePull merges a merge request while its head is still sha. When
// GitLab will not merge it yet (its pipeline still runs), rendimiento sets
// it to merge when the pipeline succeeds, and says so.
func (f *Forja) MergePull(ctx context.Context, installation int64, repo string, number int, sha string) error {
	var m glMR
	where := fmt.Sprintf("/merge_requests/%d/merge", number)
	err := f.in(ctx, installation, repo, http.MethodPut, where, map[string]any{"sha": sha}, &m)
	var ae *APIError
	if errors.As(err, &ae) && (ae.Status == http.StatusMethodNotAllowed || ae.Status == http.StatusNotAcceptable || ae.Status == http.StatusUnprocessableEntity) {
		m = glMR{}
		err = f.in(ctx, installation, repo, http.MethodPut, where, map[string]any{"sha": sha, "merge_when_pipeline_succeeds": true, "auto_merge": true}, &m)
	}
	if err != nil {
		return err
	}
	if m.State != "" && m.State != "merged" {
		return waits{}
	}
	return nil
}

// CreatePull opens a merge request from a branch into base and returns its
// number; when the branch already has one open, that one's.
func (f *Forja) CreatePull(ctx context.Context, installation int64, repo, base, branch, title, body string) (int, error) {
	var m glMR
	err := f.in(ctx, installation, repo, http.MethodPost, "/merge_requests", map[string]any{"source_branch": branch, "target_branch": base, "title": title, "description": body}, &m)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusConflict {
		open, err := f.mrs(ctx, installation, repo, "state=opened&source_branch="+url.QueryEscape(branch))
		if err != nil {
			return 0, err
		}
		if len(open) > 0 {
			return open[0].Number, nil
		}
	}
	return m.IID, err
}

// Comment writes a note on a merge request and returns its id; with id
// set, it edits that note instead.
func (f *Forja) Comment(ctx context.Context, installation int64, repo string, number int, id int64, body string) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	if id != 0 {
		err := f.in(ctx, installation, repo, http.MethodPut, fmt.Sprintf("/merge_requests/%d/notes/%d", number, id), map[string]any{"body": body}, &out)
		if !forja.IsNotFound(err) {
			return id, err
		}
	}
	err := f.in(ctx, installation, repo, http.MethodPost, fmt.Sprintf("/merge_requests/%d/notes", number), map[string]any{"body": body}, &out)
	return out.ID, err
}

// ClosePull closes a merge request without merging it.
func (f *Forja) ClosePull(ctx context.Context, installation int64, repo string, number int) error {
	return f.in(ctx, installation, repo, http.MethodPut, fmt.Sprintf("/merge_requests/%d", number), map[string]any{"state_event": "close"}, nil)
}

// DeleteBranch deletes a branch; one already gone is not an error.
func (f *Forja) DeleteBranch(ctx context.Context, installation int64, repo, branch string) error {
	err := f.in(ctx, installation, repo, http.MethodDelete, "/repository/branches/"+url.PathEscape(branch), nil, nil)
	if forja.IsNotFound(err) {
		return nil
	}
	return err
}

// OpenPR commits files on a new branch off base in one commit and opens a
// merge request (or finds the one open). Running it again with the same
// branch makes the branch again.
func (f *Forja) OpenPR(ctx context.Context, installation int64, repo, base, branch, title, body string, files map[string]string) (*forja.Opened, error) {
	if err := f.DeleteBranch(ctx, installation, repo, branch); err != nil {
		return nil, err
	}
	q := url.Values{"branch": {branch}, "ref": {base}}
	if err := f.in(ctx, installation, repo, http.MethodPost, "/repository/branches?"+q.Encode(), nil, nil); err != nil {
		return nil, err
	}
	var actions []map[string]any
	for p, content := range files {
		action := "create"
		if _, err := f.FileAt(ctx, installation, repo, p, branch); err == nil {
			action = "update"
		}
		actions = append(actions, map[string]any{"action": action, "file_path": p, "content": content})
	}
	if err := f.in(ctx, installation, repo, http.MethodPost, "/repository/commits", map[string]any{"branch": branch, "commit_message": title, "actions": actions}, nil); err != nil {
		return nil, err
	}
	var list []glMR
	if err := f.in(ctx, installation, repo, http.MethodGet, "/merge_requests?state=opened&source_branch="+url.QueryEscape(branch), nil, &list); err != nil {
		return nil, err
	}
	if len(list) > 0 {
		return &forja.Opened{Number: list[0].IID, HTMLURL: list[0].WebURL}, nil
	}
	var m glMR
	if err := f.in(ctx, installation, repo, http.MethodPost, "/merge_requests", map[string]any{"source_branch": branch, "target_branch": base, "title": title, "description": body}, &m); err != nil {
		return nil, err
	}
	return &forja.Opened{Number: m.IID, HTMLURL: m.WebURL}, nil
}

// ErrNoMergeCommit: GitLab's API writes commits with one parent, so
// rendimiento cannot join a conflict there itself yet.
var ErrNoMergeCommit = errors.New("on GitLab, rendimiento cannot write a merge commit yet")

// CommitMerge is not done on GitLab yet (ErrNoMergeCommit): the conflict
// stays a person's.
func (f *Forja) CommitMerge(context.Context, int64, string, string, string, string, string, map[string][]byte, []string) (string, error) {
	return "", ErrNoMergeCommit
}

// ---- for the wizard ----

// Project is a project the connection can make apps of.
type Project struct {
	ID             int64     `json:"id"`
	Path           string    `json:"path"` // group/project
	Repo           string    `json:"repo"` // git.example.com/group/project
	DefaultBranch  string    `json:"defaultBranch"`
	WebURL         string    `json:"webUrl"`
	Private        bool      `json:"private"`
	LastActivityAt time.Time `json:"lastActivityAt"`
}

// Projects lists the projects the connection maintains (where it may add
// a webhook), the most recently active first, up to 300.
func (f *Forja) Projects(ctx context.Context, installation int64) ([]Project, error) {
	c, _, err := f.connection(ctx, installation)
	if err != nil {
		return nil, err
	}
	out := []Project{}
	for page := 1; page <= 3; page++ {
		var list []struct {
			ID                int64     `json:"id"`
			PathWithNamespace string    `json:"path_with_namespace"`
			DefaultBranch     string    `json:"default_branch"`
			WebURL            string    `json:"web_url"`
			Visibility        string    `json:"visibility"`
			LastActivityAt    time.Time `json:"last_activity_at"`
		}
		q := url.Values{"membership": {"true"}, "min_access_level": {"40"}, "archived": {"false"}, "order_by": {"last_activity_at"}, "per_page": {"100"}, "page": {fmt.Sprint(page)}}
		if err := f.api(ctx, c, http.MethodGet, "/projects?"+q.Encode(), nil, &list); err != nil {
			return nil, err
		}
		for _, p := range list {
			out = append(out, Project{ID: p.ID, Path: p.PathWithNamespace, Repo: RepoOf(c, p.PathWithNamespace), DefaultBranch: p.DefaultBranch,
				WebURL: p.WebURL, Private: p.Visibility != "public", LastActivityAt: p.LastActivityAt})
		}
		if len(list) < 100 {
			break
		}
	}
	return out, nil
}

// EnsureHook adds the project's webhook to rendimiento (pushes and merge
// requests, with the connection's token), or brings the one there up to
// date: rendimiento does it for the person when an app is created.
func (f *Forja) EnsureHook(ctx context.Context, installation int64, repo string) error {
	c, sec, err := f.connection(ctx, installation)
	if err != nil {
		return err
	}
	hookURL := strings.TrimRight(f.BaseURL, "/") + WebhookPath
	var hooks []struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	if err := f.api(ctx, c, http.MethodGet, projectAPI(c, repo)+"/hooks?per_page=100", nil, &hooks); err != nil {
		return err
	}
	body := map[string]any{"url": hookURL, "token": sec.WebhookToken, "push_events": true, "merge_requests_events": true,
		"enable_ssl_verification": strings.HasPrefix(hookURL, "https://")}
	for _, h := range hooks {
		if h.URL == hookURL {
			return f.api(ctx, c, http.MethodPut, fmt.Sprintf("%s/hooks/%d", projectAPI(c, repo), h.ID), body, nil)
		}
	}
	return f.api(ctx, c, http.MethodPost, projectAPI(c, repo)+"/hooks", body, nil)
}
