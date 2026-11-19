// Package forja is what rendimiento does where an app's code lives: GitHub
// today, GitLab and Bitbucket next. A forja (a forge) is connected once;
// then it tells rendimiento of each push, lends it a token to clone, shows
// whether a commit passed, opens and merges changes (pull requests on
// GitHub, merge requests on GitLab), and signs people in.
//
// The platform, the API and the agents speak to a Forja; each forge's
// package (internal/github) implements it. Connecting one (GitHub's App
// manifest, GitLab's OAuth application) stays in that package, since every
// forge does it its own way.
//
// The words follow GitHub's, the first forja: a "pull" is any change
// request, a check's Status and Conclusion are GitHub's values, and the
// neutral types keep GitHub's JSON names. Another forja maps its own into
// them.
package forja

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// Forja is one forge rendimiento works with.
type Forja interface {
	// Name is the forge's short name: "github".
	Name() string
	// Ready is nil once the forja is connected (ErrNotConfigured, in the
	// forge's own words, before).
	Ready() error
	Repos
	Webhooks
	SignIn
}

// Repos is what rendimiento does in a repository. installation is the
// connection the repository is reached through (a GitHub App installation);
// repo is "owner/name".
type Repos interface {
	// Reading the code.
	FileAt(ctx context.Context, installation int64, repo, path, ref string) ([]byte, error)
	RepoFS(ctx context.Context, installation int64, repo, ref string) (fs.FS, error)
	ChangedFiles(ctx context.Context, installation int64, repo, base, head string) ([]string, bool, error)
	BranchSHA(ctx context.Context, installation int64, repo, branch string) (string, error)
	Branches(ctx context.Context, installation int64, repo string) ([]Branch, error)
	CommitSubject(ctx context.Context, installation int64, repo, sha string) (string, error)
	MergeBase(ctx context.Context, installation int64, repo, base, head string) (string, time.Time, error)
	FirstParent(ctx context.Context, installation int64, repo, sha string) (string, error)

	// Cloning: a token build pods clone with (see the pipeline's clone).
	CloneToken(ctx context.Context, installation int64) (string, error)

	// Saying whether a commit passed.
	CreateCheck(ctx context.Context, installation int64, repo string, c Check) (int64, error)
	UpdateCheck(ctx context.Context, installation int64, repo string, id int64, c Check) error

	// Opening and merging changes.
	OpenPR(ctx context.Context, installation int64, repo, base, branch, title, body string, files map[string]string) (*Opened, error)
	CreatePull(ctx context.Context, installation int64, repo, base, branch, title, body string) (int, error)
	RecentPullRequests(ctx context.Context, installation int64, repo string) ([]Change, error)
	OpenPulls(ctx context.Context, installation int64, repo string) ([]Change, error)
	Pull(ctx context.Context, installation int64, repo string, number int) (*Change, error)
	MergePull(ctx context.Context, installation int64, repo string, number int, sha string) error
	ClosePull(ctx context.Context, installation int64, repo string, number int) error
	Comment(ctx context.Context, installation int64, repo string, number int, id int64, body string) (int64, error)
	DeleteBranch(ctx context.Context, installation int64, repo, branch string) error
	CommitMerge(ctx context.Context, installation int64, repo, branch, head, other, message string, files map[string][]byte, remove []string) (string, error)
}

// Webhooks is how a forja tells rendimiento of each push.
type Webhooks interface {
	// ParseWebhook checks a delivery's signature and reads it: ErrBadSignature
	// when it is not the forja's, ErrBadPayload when it cannot be read, and
	// Ready's error before the forja is connected.
	ParseWebhook(header http.Header, body []byte) (*Event, error)
	// Allows says whether rendimiento serves this account (GITHUB_ACCOUNTS):
	// events from any other are acknowledged and ignored.
	Allows(account string) bool
}

// SignIn is signing people in with their account on the forja.
type SignIn interface {
	// AuthorizeURL is where the login button sends people; the forja sends
	// them back to redirect with state and a code.
	AuthorizeURL(state, redirect string) (string, error)
	// SignIn trades the code for the person's login.
	SignIn(ctx context.Context, code string) (string, error)
}

var (
	// ErrBadSignature is a webhook delivery not signed by the forja.
	ErrBadSignature = errors.New("bad signature")
	// ErrBadPayload is a webhook delivery that could not be read.
	ErrBadPayload = errors.New("invalid JSON")
	// ErrNotFound is what a forja answers for something that is not there
	// (a file, a branch): errors.Is finds it in the forja's own errors.
	ErrNotFound = errors.New("not found")
	// ErrConflict is a forja refusing because something moved meanwhile
	// (merging a pull whose head changed).
	ErrConflict = errors.New("conflict")
)

// IsNotFound reports whether err says that something is not there.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// Says is the sentence the forja gave for refusing, without the rest of its
// answer; err's own text when it gave none.
func Says(err error) string {
	var s interface{ Says() string }
	if errors.As(err, &s) {
		if says := s.Says(); says != "" {
			return says
		}
	}
	return err.Error()
}

// SaysSkipCI reports whether a commit message asks for no run, the way
// GitHub Actions reads it: [skip ci], [ci skip] or [no ci], in any case.
func SaysSkipCI(message string) bool {
	m := strings.ToLower(message)
	return strings.Contains(m, "[skip ci]") || strings.Contains(m, "[ci skip]") || strings.Contains(m, "[no ci]")
}
