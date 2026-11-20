package gitlab

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"sort"
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

// Files is repo's text files at ref, each up to maxFile bytes (read from
// its archive, as RepoFS).
func (f *Forja) Files(ctx context.Context, installation int64, repo, ref string, maxFile int) (map[string][]byte, error) {
	tree, err := f.RepoFS(ctx, installation, repo, ref)
	if err != nil {
		return nil, err
	}
	return forja.FilesOf(tree, maxFile)
}

// CreateBranch makes branch start at sha. One that already exists and
// holds sha is kept (a second try goes on from the first); one that does
// not is made again at sha (GitLab moves a branch no other way).
func (f *Forja) CreateBranch(ctx context.Context, installation int64, repo, branch, sha string) error {
	create := func() error {
		q := url.Values{"branch": {branch}, "ref": {sha}}
		return f.in(ctx, installation, repo, http.MethodPost, "/repository/branches?"+q.Encode(), nil, nil)
	}
	head, err := f.BranchSHA(ctx, installation, repo, branch)
	if forja.IsNotFound(err) {
		return create()
	}
	if err != nil {
		return err
	}
	if head == sha {
		return nil
	}
	if base, _, err := f.MergeBase(ctx, installation, repo, sha, head); err == nil && base == sha {
		return nil
	}
	if err := f.DeleteBranch(ctx, installation, repo, branch); err != nil {
		return err
	}
	return create()
}

// CommitFiles commits files (any content, as base64) and removals on
// branch in one commit, and returns its SHA.
func (f *Forja) CommitFiles(ctx context.Context, installation int64, repo, branch, message string, files map[string][]byte, remove []string) (string, error) {
	actions, err := f.fileActions(ctx, installation, repo, branch, "", files, remove)
	if err != nil {
		return "", err
	}
	var c glCommit
	err = f.in(ctx, installation, repo, http.MethodPost, "/repository/commits", map[string]any{"branch": branch, "commit_message": message, "actions": actions}, &c)
	return c.ID, err
}

// ErrNoMergeCommit is a project where rendimiento cannot write a merge
// commit: one that merges only by fast-forward or rebase, or squashes
// always.
var ErrNoMergeCommit = errors.New("on GitLab, rendimiento cannot write a merge commit in this project")

// mergeWait is how long CommitMerge waits between looks at its merge
// request while GitLab works out whether it can be merged.
var mergeWait = 500 * time.Millisecond

// CommitMerge commits files and removals on branch (whose head must still
// be head) as a merge of other into it: the result has other among its
// ancestors and the tree head has with files and removals.
//
// GitLab's commits API writes commits with one parent (start_sha is only
// where a new branch starts), and nothing else in its API writes a commit
// object; only a merge request's merge makes one with two. So rendimiento
// makes the merge GitLab's way, in a way that cannot conflict:
//
//  1. ours: the resolved tree committed on branch, on head ([skip ci]: the
//     merge that follows is what gets tested);
//  2. theirs: the same tree committed on a passing branch made at other
//     (rendimiento/mezcla-...), from the difference between other and ours;
//  3. a merge request of the passing branch into branch, merged with the
//     message: both sides have the same tree, so GitLab's merge is clean
//     and its tree is the resolved one. The passing branch is deleted.
//
// The merge commit's parents are ours (on head) and theirs (on other): the
// default branch is merged, and a merge request of branch into it no
// longer conflicts. Two commits more than git would write, but the same
// history. When GitLab refuses the merge (approvals, a pipeline it waits
// for), the passing branch and its merge request are removed and the
// error says the resolution stays committed on branch without the merge.
func (f *Forja) CommitMerge(ctx context.Context, installation int64, repo, branch, head, other, message string, files map[string][]byte, remove []string) (string, error) {
	now, err := f.BranchSHA(ctx, installation, repo, branch)
	if err != nil {
		return "", err
	}
	if now != head {
		return "", fmt.Errorf("%w: %s moved to %s while it was being merged", forja.ErrConflict, branch, now)
	}
	var proj struct {
		MergeMethod  string `json:"merge_method"`
		SquashOption string `json:"squash_option"`
	}
	if err := f.in(ctx, installation, repo, http.MethodGet, "", nil, &proj); err != nil {
		return "", err
	}
	if (proj.MergeMethod != "" && proj.MergeMethod != "merge") || proj.SquashOption == "always" {
		return "", fmt.Errorf("%w (it merges by %s, squash %s)", ErrNoMergeCommit, proj.MergeMethod, proj.SquashOption)
	}
	short := func(s string) string { return s[:min(len(s), 7)] }

	// 1. Ours.
	ours := head
	actions, err := f.fileActions(ctx, installation, repo, branch, other, files, remove)
	if err != nil {
		return "", err
	}
	if len(actions) > 0 {
		var c glCommit
		msg := "Resolve the merge of " + short(other) + " [skip ci]"
		if err := f.in(ctx, installation, repo, http.MethodPost, "/repository/commits", map[string]any{"branch": branch, "commit_message": msg, "actions": actions}, &c); err != nil {
			return "", err
		}
		ours = c.ID
	}
	committed := func(err error) error {
		if ours == head {
			return err
		}
		return fmt.Errorf("the resolution is committed on %s as %s, but not as a merge of %s: %w", branch, short(ours), short(other), err)
	}

	// 2. Theirs, on a passing branch.
	side := "rendimiento/mezcla-" + short(other) + "-" + short(ours)
	_ = f.DeleteBranch(ctx, installation, repo, side)
	q := url.Values{"branch": {side}, "ref": {other}}
	if err := f.in(ctx, installation, repo, http.MethodPost, "/repository/branches?"+q.Encode(), nil, nil); err != nil {
		return "", committed(err)
	}
	number := 0
	defer func() {
		cleanup := context.WithoutCancel(ctx)
		if number != 0 {
			_ = f.ClosePull(cleanup, installation, repo, number)
		}
		_ = f.DeleteBranch(cleanup, installation, repo, side)
	}()
	theirs := other
	actions, err = f.treeActions(ctx, installation, repo, other, ours)
	if err != nil {
		return "", committed(err)
	}
	if len(actions) > 0 {
		var c glCommit
		msg := "Resolve the merge of " + short(other) + " into " + branch + " [skip ci]"
		if err := f.in(ctx, installation, repo, http.MethodPost, "/repository/commits", map[string]any{"branch": side, "commit_message": msg, "actions": actions}, &c); err != nil {
			return "", committed(err)
		}
		theirs = c.ID
	}

	// 3. The merge request, merged.
	var m glMR
	if err := f.in(ctx, installation, repo, http.MethodPost, "/merge_requests", map[string]any{"source_branch": side, "target_branch": branch,
		"title": "Merge " + short(other) + " into " + branch, "description": "rendimiento's coding agent merges the default branch here; this merge request closes by itself.",
		"remove_source_branch": true, "squash": false}, &m); err != nil {
		return "", committed(err)
	}
	number = m.IID
	for i := 0; i < 20 && stillChecking(m); i++ {
		time.Sleep(mergeWait)
		if err := f.in(ctx, installation, repo, http.MethodGet, fmt.Sprintf("/merge_requests/%d", number), nil, &m); err != nil {
			return "", committed(err)
		}
	}
	var merged glMR
	if err := f.in(ctx, installation, repo, http.MethodPut, fmt.Sprintf("/merge_requests/%d/merge", number), map[string]any{
		"sha": theirs, "merge_commit_message": message, "should_remove_source_branch": true, "squash": false}, &merged); err != nil {
		return "", committed(err)
	}
	if merged.State != "merged" {
		return "", committed(fmt.Errorf("GitLab left merge request !%d %s", number, merged.State))
	}
	number = 0 // merged: nothing to close
	if merged.MergeCommitSHA != "" {
		return merged.MergeCommitSHA, nil
	}
	return f.BranchSHA(ctx, installation, repo, branch)
}

// stillChecking reports whether GitLab is still working out whether a merge
// request can be merged (merging it then would be refused).
func stillChecking(m glMR) bool {
	switch m.DetailedMergeStatus {
	case "unchecked", "checking", "preparing", "approvals_syncing":
		return true
	case "":
		return m.MergeStatus == "" || m.MergeStatus == "unchecked" || m.MergeStatus == "checking" || m.MergeStatus == "cannot_be_merged_recheck"
	}
	return false
}

// fileActions are the commits API's actions that write files and removals
// on ref: a file ref lacks is created, executable when it is executable at
// modes (another commit, "" for none: a file the default branch brings
// keeps its mode there).
func (f *Forja) fileActions(ctx context.Context, installation int64, repo, ref, modes string, files map[string][]byte, remove []string) ([]map[string]any, error) {
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	actions := []map[string]any{}
	for _, p := range names {
		a := map[string]any{"action": "create", "file_path": strings.TrimPrefix(p, "/"),
			"encoding": "base64", "content": base64.StdEncoding.EncodeToString(files[p])}
		if _, err := f.FileAt(ctx, installation, repo, p, ref); err == nil {
			a["action"] = "update"
		} else if !forja.IsNotFound(err) {
			return nil, err
		} else if modes != "" && f.executable(ctx, installation, repo, p, modes) {
			a["execute_filemode"] = true
		}
		actions = append(actions, a)
	}
	for _, p := range remove {
		if _, ok := files[p]; ok {
			continue
		}
		actions = append(actions, map[string]any{"action": "delete", "file_path": strings.TrimPrefix(p, "/")})
	}
	return actions, nil
}

// executable reports whether a file is executable at ref (false when
// GitLab does not say).
func (f *Forja) executable(ctx context.Context, installation int64, repo, filePath, ref string) bool {
	var meta struct {
		ExecuteFilemode bool `json:"execute_filemode"`
	}
	err := f.in(ctx, installation, repo, http.MethodGet, "/repository/files/"+url.PathEscape(strings.TrimPrefix(filePath, "/"))+"?ref="+url.QueryEscape(ref), nil, &meta)
	return err == nil && meta.ExecuteFilemode
}

// treeActions are the commits API's actions that make from's tree into
// to's: GitLab's straight compare of the two, each file as it is at to,
// with its mode. A difference GitLab cut, a link or a submodule is refused:
// only git would write those right.
func (f *Forja) treeActions(ctx context.Context, installation int64, repo, from, to string) ([]map[string]any, error) {
	var out struct {
		Diffs []struct {
			OldPath     string `json:"old_path"`
			NewPath     string `json:"new_path"`
			AMode       string `json:"a_mode"`
			BMode       string `json:"b_mode"`
			NewFile     bool   `json:"new_file"`
			RenamedFile bool   `json:"renamed_file"`
			DeletedFile bool   `json:"deleted_file"`
		} `json:"diffs"`
		CompareTimeout bool `json:"compare_timeout"`
	}
	q := url.Values{"from": {from}, "to": {to}, "straight": {"true"}}
	if err := f.in(ctx, installation, repo, http.MethodGet, "/repository/compare?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	if out.CompareTimeout || len(out.Diffs) >= 1000 {
		return nil, errors.New("GitLab cut the comparison: too many files to merge without git")
	}
	actions := []map[string]any{}
	for _, d := range out.Diffs {
		for _, mode := range []string{d.AMode, d.BMode} {
			if mode == "120000" || mode == "160000" {
				return nil, fmt.Errorf("%s is a link or a submodule: only git merges it", d.NewPath)
			}
		}
		if d.DeletedFile || d.RenamedFile {
			actions = append(actions, map[string]any{"action": "delete", "file_path": d.OldPath})
			if d.DeletedFile {
				continue
			}
		}
		b, err := f.FileAt(ctx, installation, repo, d.NewPath, to)
		if err != nil {
			return nil, err
		}
		a := map[string]any{"action": "update", "file_path": d.NewPath, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(b)}
		if d.NewFile || d.RenamedFile {
			a["action"] = "create"
			if d.BMode == "100755" {
				a["execute_filemode"] = true
			}
		}
		actions = append(actions, a)
		if !d.NewFile && !d.RenamedFile && d.AMode != "" && d.BMode != "" && d.AMode != d.BMode {
			actions = append(actions, map[string]any{"action": "chmod", "file_path": d.NewPath, "execute_filemode": d.BMode == "100755"})
		}
	}
	return actions, nil
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
