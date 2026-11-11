package platform

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ---- kinds of branch ----
//
// The Runs tab sorts an app's runs by the kind of their branch. A branch's
// kind comes from the labels of its pull request (the open one, or else the
// latest), and when it has none that say, from the start of its name.

// The kinds of branch.
const (
	KindMain    = "main"    // the default branch: the runs that deploy
	KindFix     = "fix"     // a fix of something that was wrong
	KindFeature = "feature" // something new
	KindRelease = "release" // a version being prepared
	KindDeps    = "deps"    // dependency updates (Renovate, Dependabot)
	KindOther   = "other"
)

// kindLabels are the pull request labels that say a kind, written as
// kindOf reads them (lowercase, without "type:" or "kind/" before).
var kindLabels = map[string]string{
	"bug": KindFix, "bugfix": KindFix, "fix": KindFix, "defect": KindFix, "regression": KindFix,
	"arreglo": KindFix, "error": KindFix, "hotfix": KindFix,
	"enhancement": KindFeature, "feature": KindFeature, "new feature": KindFeature,
	"función": KindFeature, "funcion": KindFeature, "mejora": KindFeature,
	"release": KindRelease, "version": KindRelease, "versión": KindRelease,
	"dependencies": KindDeps, "dependency": KindDeps, "deps": KindDeps,
	"renovate": KindDeps, "dependabot": KindDeps, "dependencias": KindDeps,
}

// kindPrefixes are the starts of a branch name that say a kind, when its
// pull request does not.
var kindPrefixes = []struct{ prefix, kind string }{
	{"fix/", KindFix}, {"fix-", KindFix}, {"bug/", KindFix}, {"bugfix/", KindFix}, {"hotfix/", KindFix},
	{"arreglo/", KindFix}, {"arreglo-", KindFix},
	{"feat/", KindFeature}, {"feature/", KindFeature}, {"feat-", KindFeature},
	{"funcion/", KindFeature}, {"funcion-", KindFeature},
	{"release/", KindRelease}, {"release-", KindRelease}, {"version-", KindRelease}, {"changelog-v", KindRelease},
	{"renovate/", KindDeps}, {"dependabot/", KindDeps},
}

// kindOf is a branch's kind, from its pull request's labels and its name.
func kindOf(branch, defaultBranch string, labels []string) string {
	if branch == defaultBranch {
		return KindMain
	}
	for _, l := range labels {
		l = strings.ToLower(strings.TrimSpace(l))
		for _, cut := range []string{"type:", "type/", "kind:", "kind/", "tipo:", "tipo/"} {
			l = strings.TrimSpace(strings.TrimPrefix(l, cut))
		}
		if k, ok := kindLabels[l]; ok {
			return k
		}
	}
	name := strings.ToLower(branch)
	for _, p := range kindPrefixes {
		if strings.HasPrefix(name, p.prefix) {
			return p.kind
		}
	}
	return KindOther
}

// BranchKind is what the Runs tab knows of a branch: its kind, and its
// pull request when it has one.
type BranchKind struct {
	Kind string `json:"kind"`
	Pull int    `json:"pull,omitempty"`
	Open bool   `json:"open,omitempty"` // its pull request is open
}

// BranchKinds is the answer for the Runs tab. GitHub is false when it could
// not be asked: the kinds then come from the branch names only.
type BranchKinds struct {
	Branches map[string]BranchKind `json:"branches"`
	GitHub   bool                  `json:"github"`
}

// branchPullsTTL is how long an app's pull requests are kept before GitHub
// is asked again.
const branchPullsTTL = 2 * time.Minute

type branchPull struct {
	number int
	open   bool
	labels []string
}

type branchPullsEntry struct {
	at    time.Time
	pulls map[string]branchPull // by head branch
}

// branchPullsCache keeps each app's last answer from GitHub.
type branchPullsCache struct {
	mu    sync.Mutex
	byApp map[int64]branchPullsEntry
}

// BranchKinds is the kind of each of the branches.
func (p *Platform) BranchKinds(ctx context.Context, a *store.App, branches []string) BranchKinds {
	pulls, ok := p.branchPulls(ctx, a)
	out := BranchKinds{Branches: map[string]BranchKind{}, GitHub: ok}
	for _, b := range branches {
		if _, done := out.Branches[b]; done || b == "" {
			continue
		}
		pr := pulls[b]
		k := BranchKind{Kind: kindOf(b, a.DefaultBranch, pr.labels), Pull: pr.number, Open: pr.open}
		if b == a.DefaultBranch {
			k.Pull, k.Open = 0, false
		}
		out.Branches[b] = k
	}
	return out
}

// branchPulls is the pull request of each branch of the app (the open one,
// or else the latest), from GitHub, kept for branchPullsTTL.
func (p *Platform) branchPulls(ctx context.Context, a *store.App) (map[string]branchPull, bool) {
	c := &p.branchPullsCache
	now := time.Now()
	c.mu.Lock()
	if e, ok := c.byApp[a.ID]; ok && now.Sub(e.at) < branchPullsTTL {
		c.mu.Unlock()
		return e.pulls, true
	}
	c.mu.Unlock()
	if p.GitHub == nil {
		return nil, false
	}
	prs, err := p.GitHub.RecentPullRequests(ctx, a.InstallationID, a.Repo)
	if err != nil {
		p.Log.Warn("branch kinds: pull requests", "app", a.Name, "err", err)
		return nil, false
	}
	pulls := map[string]branchPull{}
	for _, pr := range prs { // the most recently updated first
		if pr.Base.Ref == pr.Head.Ref {
			continue
		}
		open := pr.State == "open"
		if had, ok := pulls[pr.Head.Ref]; ok && (had.open || !open) {
			continue // keep the open one, or the latest
		}
		labels := make([]string, len(pr.Labels))
		for i, l := range pr.Labels {
			labels[i] = l.Name
		}
		pulls[pr.Head.Ref] = branchPull{number: pr.Number, open: open, labels: labels}
	}
	c.mu.Lock()
	if c.byApp == nil {
		c.byApp = map[int64]branchPullsEntry{}
	}
	c.byApp[a.ID] = branchPullsEntry{at: now, pulls: pulls}
	c.mu.Unlock()
	return pulls, true
}
