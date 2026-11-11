package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	gh "github.com/rendimiento-ai/rendimiento/internal/github"
	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ---- merging by itself ----
//
// An app may ask for it (Settings): a branch with an open pull request into
// the default branch is merged by rendimiento once the branch's run passes.
// Opening the pull request is the person's yes; a branch without one is only
// tested. What is merged is the commit that was tested, never a newer one.

// mergeEvery is how often open pull requests are looked at again: one
// opened after its branch passed is merged within this time.
const mergeEvery = time.Minute

// mergeLoop looks at the open pull requests of the apps that merge by
// themselves, every minute until ctx ends.
func (p *Platform) mergeLoop(ctx context.Context) {
	tick := time.NewTicker(mergeEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		apps, err := p.Store.ListApps(ctx)
		if err != nil {
			continue
		}
		for _, app := range apps {
			if app.AutoMerge && ctx.Err() == nil {
				p.MergeReady(ctx, app)
			}
		}
	}
}

// MergeReady merges each open pull request of the app whose head passed its
// run, and records on that run what became of it: merged, in conflict with
// the default branch, or refused by GitHub (a review or a check it asks for).
func (p *Platform) MergeReady(ctx context.Context, app *store.App) {
	if !app.AutoMerge {
		return
	}
	pulls, err := p.GitHub.OpenPulls(ctx, app.InstallationID, app.Repo)
	if err != nil {
		p.Log.Warn("merge: list pull requests", "app", app.Name, "err", err)
		return
	}
	for _, pr := range pulls {
		if pr.Base.Ref != app.DefaultBranch || pr.Head.Ref == app.DefaultBranch || pr.Draft {
			continue
		}
		run, err := p.Store.RunAt(ctx, app.ID, pr.Head.Ref, pr.Head.SHA)
		if err != nil || run.Status != store.RunSucceeded || run.Deploy {
			continue // not tested yet (or a fork's branch of the same name: never built)
		}
		if run.Merge == store.MergeMerged || run.Merge == store.MergeConflict {
			continue // a conflict only goes away with a new commit, which runs again
		}
		if !p.othersPassed(ctx, app, pr.Head.Ref, pr.Head.SHA) {
			continue
		}
		p.mergePull(ctx, app, run, pr.Number)
	}
}

// othersPassed says whether the commit passed in every other app of the
// same repository that ran it: one pull request, all of its apps.
func (p *Platform) othersPassed(ctx context.Context, app *store.App, branch, sha string) bool {
	apps, err := p.Store.AppsForRepo(ctx, app.Repo)
	if err != nil {
		return false
	}
	for _, other := range apps {
		if other.ID == app.ID {
			continue
		}
		if r, err := p.Store.RunAt(ctx, other.ID, branch, sha); err == nil && r.Status != store.RunSucceeded {
			return false
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return false
		}
	}
	return true
}

// mergePull asks GitHub whether the pull request can be merged, merges it
// when it can, and writes the outcome on the run.
func (p *Platform) mergePull(ctx context.Context, app *store.App, run *store.Run, number int) {
	pr, err := p.GitHub.Pull(ctx, app.InstallationID, app.Repo, number)
	if err != nil {
		p.Log.Warn("merge: read pull request", "app", app.Name, "pull", number, "err", err)
		return
	}
	if pr.State != "open" || pr.Head.SHA != run.SHA {
		return // merged or closed by someone, or a newer commit: its own run decides
	}
	state, msg := store.MergeMerged, M("merged pull request #%d", number)
	switch {
	case pr.Mergeable != nil && !*pr.Mergeable:
		state, msg = store.MergeConflict, M("pull request #%d conflicts with %s: not merged", number, app.DefaultBranch)
	case pr.Mergeable == nil:
		return // GitHub is still working it out: the next look decides
	case pr.MergeableState == "blocked":
		state, msg = store.MergeBlocked, M("pull request #%d waits for what GitHub asks of it (a review, a check): not merged", number)
	default:
		if err := p.GitHub.MergePull(ctx, app.InstallationID, app.Repo, number, run.SHA); err != nil {
			var api *gh.APIError
			if errors.As(err, &api) && api.Status == 409 {
				return // the branch moved while merging
			}
			state, msg = store.MergeBlocked, M("pull request #%d not merged: %s", number, githubSays(err))
		}
	}
	if state == run.Merge && state == store.MergeBlocked {
		return // still waiting: nothing new to tell
	}
	if err := p.Store.SetRunMerge(ctx, run.ID, number, state, msg); err != nil {
		p.Log.Error("merge: record", "run", run.ID, "err", err)
		return
	}
	p.publishRun(app, run.ID)
	p.Log.Info("merge", "app", app.Name, "branch", run.Branch, "pull", number, "state", state)
}

// githubSays is the sentence GitHub gave for refusing, without the rest of
// its answer.
func githubSays(err error) string {
	var api *gh.APIError
	if !errors.As(err, &api) {
		return err.Error()
	}
	var body struct {
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(api.Body), &body) == nil && body.Message != "" {
		return strings.TrimSuffix(body.Message, ".")
	}
	return err.Error()
}
