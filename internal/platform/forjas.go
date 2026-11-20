package platform

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// notConnected is a forja an app says it lives on that is not connected
// here (GitLab, before it is): every call says so.
type notConnected struct{ name string }

func (n notConnected) err() error {
	return fmt.Errorf("%s is not connected to rendimiento: connect it on the Environment page", n.name)
}

func (n notConnected) FileAt(context.Context, int64, string, string, string) ([]byte, error) {
	return nil, n.err()
}
func (n notConnected) RepoFS(context.Context, int64, string, string) (fs.FS, error) {
	return nil, n.err()
}
func (n notConnected) ChangedFiles(context.Context, int64, string, string, string) ([]string, bool, error) {
	return nil, false, n.err()
}
func (n notConnected) BranchSHA(context.Context, int64, string, string) (string, error) {
	return "", n.err()
}
func (n notConnected) Branches(context.Context, int64, string) ([]forja.Branch, error) {
	return nil, n.err()
}
func (n notConnected) CommitSubject(context.Context, int64, string, string) (string, error) {
	return "", n.err()
}
func (n notConnected) MergeBase(context.Context, int64, string, string, string) (string, time.Time, error) {
	return "", time.Time{}, n.err()
}
func (n notConnected) FirstParent(context.Context, int64, string, string) (string, error) {
	return "", n.err()
}
func (n notConnected) CloneToken(context.Context, int64) (string, error) { return "", n.err() }
func (n notConnected) CreateCheck(context.Context, int64, string, forja.Check) (int64, error) {
	return 0, n.err()
}
func (n notConnected) UpdateCheck(context.Context, int64, string, int64, forja.Check) error {
	return n.err()
}
func (n notConnected) OpenPR(context.Context, int64, string, string, string, string, string, map[string]string) (*forja.Opened, error) {
	return nil, n.err()
}
func (n notConnected) CreatePull(context.Context, int64, string, string, string, string, string) (int, error) {
	return 0, n.err()
}
func (n notConnected) RecentPullRequests(context.Context, int64, string) ([]forja.Change, error) {
	return nil, n.err()
}
func (n notConnected) OpenPulls(context.Context, int64, string) ([]forja.Change, error) {
	return nil, n.err()
}
func (n notConnected) Pull(context.Context, int64, string, int) (*forja.Change, error) {
	return nil, n.err()
}
func (n notConnected) MergePull(context.Context, int64, string, int, string) error { return n.err() }
func (n notConnected) ClosePull(context.Context, int64, string, int) error         { return n.err() }
func (n notConnected) Comment(context.Context, int64, string, int, int64, string) (int64, error) {
	return 0, n.err()
}
func (n notConnected) DeleteBranch(context.Context, int64, string, string) error { return n.err() }
func (n notConnected) CommitMerge(context.Context, int64, string, string, string, string, string, map[string][]byte, []string) (string, error) {
	return "", n.err()
}
func (n notConnected) Files(context.Context, int64, string, string, int) (map[string][]byte, error) {
	return nil, n.err()
}
func (n notConnected) CreateBranch(context.Context, int64, string, string, string) error {
	return n.err()
}
func (n notConnected) CommitFiles(context.Context, int64, string, string, string, map[string][]byte, []string) (string, error) {
	return "", n.err()
}

// Repos is every forja connected here, as one: each call goes to the forja
// its repository lives on (forja.Of), so what works on an app's repository
// (the declared fixes, the coding agent, the diagnostician) works on GitHub,
// GitLab and Bitbucket alike.
type Repos struct{ p *Platform }

// Repos is the platform's forjas as one (see Repos).
func (p *Platform) Repos() Repos { return Repos{p} }

// of is the forja of a repository.
func (r Repos) of(repo string) forja.Repos {
	return r.p.repos(&store.App{Forja: forja.Of(repo)})
}

var _ forja.Repos = Repos{}

func (r Repos) FileAt(ctx context.Context, in int64, repo, path, ref string) ([]byte, error) {
	return r.of(repo).FileAt(ctx, in, repo, path, ref)
}
func (r Repos) RepoFS(ctx context.Context, in int64, repo, ref string) (fs.FS, error) {
	return r.of(repo).RepoFS(ctx, in, repo, ref)
}
func (r Repos) ChangedFiles(ctx context.Context, in int64, repo, base, head string) ([]string, bool, error) {
	return r.of(repo).ChangedFiles(ctx, in, repo, base, head)
}
func (r Repos) BranchSHA(ctx context.Context, in int64, repo, branch string) (string, error) {
	return r.of(repo).BranchSHA(ctx, in, repo, branch)
}
func (r Repos) Branches(ctx context.Context, in int64, repo string) ([]forja.Branch, error) {
	return r.of(repo).Branches(ctx, in, repo)
}
func (r Repos) CommitSubject(ctx context.Context, in int64, repo, sha string) (string, error) {
	return r.of(repo).CommitSubject(ctx, in, repo, sha)
}
func (r Repos) Files(ctx context.Context, in int64, repo, ref string, maxFile int) (map[string][]byte, error) {
	return r.of(repo).Files(ctx, in, repo, ref, maxFile)
}
func (r Repos) MergeBase(ctx context.Context, in int64, repo, base, head string) (string, time.Time, error) {
	return r.of(repo).MergeBase(ctx, in, repo, base, head)
}
func (r Repos) FirstParent(ctx context.Context, in int64, repo, sha string) (string, error) {
	return r.of(repo).FirstParent(ctx, in, repo, sha)
}

// CloneToken has no repository to go by: it is GitHub's (a run clones
// with Platform.CloneToken, which knows its app's forja).
func (r Repos) CloneToken(ctx context.Context, in int64) (string, error) {
	return r.p.repos(&store.App{}).CloneToken(ctx, in)
}
func (r Repos) CreateCheck(ctx context.Context, in int64, repo string, c forja.Check) (int64, error) {
	return r.of(repo).CreateCheck(ctx, in, repo, c)
}
func (r Repos) UpdateCheck(ctx context.Context, in int64, repo string, id int64, c forja.Check) error {
	return r.of(repo).UpdateCheck(ctx, in, repo, id, c)
}
func (r Repos) CreateBranch(ctx context.Context, in int64, repo, branch, sha string) error {
	return r.of(repo).CreateBranch(ctx, in, repo, branch, sha)
}
func (r Repos) CommitFiles(ctx context.Context, in int64, repo, branch, message string, files map[string][]byte, remove []string) (string, error) {
	return r.of(repo).CommitFiles(ctx, in, repo, branch, message, files, remove)
}
func (r Repos) OpenPR(ctx context.Context, in int64, repo, base, branch, title, body string, files map[string]string) (*forja.Opened, error) {
	return r.of(repo).OpenPR(ctx, in, repo, base, branch, title, body, files)
}
func (r Repos) CreatePull(ctx context.Context, in int64, repo, base, branch, title, body string) (int, error) {
	return r.of(repo).CreatePull(ctx, in, repo, base, branch, title, body)
}
func (r Repos) RecentPullRequests(ctx context.Context, in int64, repo string) ([]forja.Change, error) {
	return r.of(repo).RecentPullRequests(ctx, in, repo)
}
func (r Repos) OpenPulls(ctx context.Context, in int64, repo string) ([]forja.Change, error) {
	return r.of(repo).OpenPulls(ctx, in, repo)
}
func (r Repos) Pull(ctx context.Context, in int64, repo string, number int) (*forja.Change, error) {
	return r.of(repo).Pull(ctx, in, repo, number)
}
func (r Repos) MergePull(ctx context.Context, in int64, repo string, number int, sha string) error {
	return r.of(repo).MergePull(ctx, in, repo, number, sha)
}
func (r Repos) ClosePull(ctx context.Context, in int64, repo string, number int) error {
	return r.of(repo).ClosePull(ctx, in, repo, number)
}
func (r Repos) Comment(ctx context.Context, in int64, repo string, number int, id int64, body string) (int64, error) {
	return r.of(repo).Comment(ctx, in, repo, number, id, body)
}
func (r Repos) DeleteBranch(ctx context.Context, in int64, repo, branch string) error {
	return r.of(repo).DeleteBranch(ctx, in, repo, branch)
}
func (r Repos) CommitMerge(ctx context.Context, in int64, repo, branch, head, other, message string, files map[string][]byte, remove []string) (string, error) {
	return r.of(repo).CommitMerge(ctx, in, repo, branch, head, other, message, files, remove)
}
