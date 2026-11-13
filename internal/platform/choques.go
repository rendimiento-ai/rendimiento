package platform

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	gh "github.com/rendimiento-ai/rendimiento/internal/github"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// resolveAddOnly merges the default branch into a pull request's branch
// when every place where both changed a file is one where both only added
// lines (see mergeAddOnly): it commits the merge on the branch, as a person
// with git would, and returns the commit and the files it joined. When a
// file cannot be joined that way, it changes nothing and returns those
// files, for whoever resolves it.
func (p *Platform) resolveAddOnly(ctx context.Context, app *store.App, run *store.Run, branch string) (sha string, joined, conflicts []string, err error) {
	inst, repo := app.InstallationID, app.Repo
	main, err := p.GitHub.BranchSHA(ctx, inst, repo, app.DefaultBranch)
	if err != nil {
		return "", nil, nil, err
	}
	base, _, err := p.GitHub.MergeBase(ctx, inst, repo, main, run.SHA)
	if err != nil || base == "" {
		return "", nil, nil, fmt.Errorf("no merge base of %s and %s: %v", app.DefaultBranch, branch, err)
	}
	ours, ok1, err := p.GitHub.ChangedFiles(ctx, inst, repo, base, run.SHA)
	if err != nil {
		return "", nil, nil, err
	}
	theirs, ok2, err := p.GitHub.ChangedFiles(ctx, inst, repo, base, main)
	if err != nil {
		return "", nil, nil, err
	}
	if !ok1 || !ok2 {
		return "", nil, nil, fmt.Errorf("too many files changed to merge without git")
	}
	mine := map[string]bool{}
	for _, f := range ours {
		mine[f] = true
	}
	read := func(path, ref string) ([]byte, bool, error) {
		b, err := p.GitHub.FileAt(ctx, inst, repo, path, ref)
		if gh.IsNotFound(err) {
			return nil, false, nil
		}
		return b, err == nil, err
	}
	files, remove := map[string][]byte{}, []string{}
	for _, f := range theirs {
		t, tok, err := read(f, main)
		if err != nil {
			return "", nil, nil, err
		}
		if !mine[f] {
			// Only the default branch changed it: the merge takes its version.
			if tok {
				files[f] = t
			} else {
				remove = append(remove, f)
			}
			continue
		}
		o, ook, err := read(f, run.SHA)
		if err != nil {
			return "", nil, nil, err
		}
		switch {
		case !ook && !tok:
			continue // both deleted it
		case !ook || !tok:
			conflicts = append(conflicts, f) // one deleted what the other changed
			continue
		case bytes.Equal(o, t):
			continue // the same change on both sides
		}
		b, _, err := read(f, base)
		if err != nil {
			return "", nil, nil, err
		}
		merged, ok := mergeAddOnly(b, o, t)
		if !ok {
			conflicts = append(conflicts, f)
			continue
		}
		files[f] = merged
		joined = append(joined, f)
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return "", nil, conflicts, nil
	}
	sort.Strings(joined)
	msg := fmt.Sprintf("Merge %s into %s: both sides only added lines\n\nrendimiento joined both sides, %s's lines first, in:\n- %s\n",
		app.DefaultBranch, branch, app.DefaultBranch, strings.Join(joined, "\n- "))
	sha, err = p.GitHub.CommitMerge(ctx, inst, repo, branch, run.SHA, main, msg, files, remove)
	return sha, joined, nil, err
}

// queueResolved tests the merge commit rendimiento made on a branch, and
// carries over the yes given to the commit before it: the new one holds
// nothing but that commit and what is already on the default branch.
func (p *Platform) queueResolved(ctx context.Context, app *store.App, run *store.Run, branch, sha string) {
	next, err := p.QueueRun(ctx, app, branch, sha, "push")
	if err != nil || next == nil {
		p.Log.Warn("merge: could not test the resolved commit", "app", app.Name, "branch", branch, "err", err)
		return
	}
	if run.ApprovedBy != "" {
		if err := p.Store.ApproveRun(ctx, next.ID, run.ApprovedBy); err != nil {
			p.Log.Warn("merge: could not carry over the yes", "app", app.Name, "run", next.ID, "err", err)
		}
	}
}
