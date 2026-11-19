package platform

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
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
