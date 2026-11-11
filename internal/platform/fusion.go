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
// The yes is a person's: they approve the run in rendimiento (its page, or
// by accepting the encargo that delivered the branch), which opens the pull
// request when there is none. An app may instead take any open pull request
// as the yes (MergeAny), except a dependency update's: Renovate merges what
// its rules allow and leaves the rest for a person. A fork's pull request is
// never merged, and a branch
// nobody approved is only tested. What is merged is the commit that was
// tested and approved, never a newer one.
// The merged branch is deleted, as GitHub's own button offers.

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
		if fork := pr.Head.Repo.FullName; fork != "" && !strings.EqualFold(fork, app.Repo) {
			continue // a fork's branch may share a name and a commit with ours
		}
		run, err := p.Store.RunAt(ctx, app.ID, pr.Head.Ref, pr.Head.SHA)
		if err != nil || run.Status != store.RunSucceeded || run.Deploy {
			continue // not tested yet
		}
		if run.ApprovedBy == "" && (!app.MergeAny || depsPull(pr, app)) {
			continue // nobody said yes in rendimiento
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

// depsPull says whether a pull request is a dependency update (Renovate's,
// Dependabot's). Those tools merge the updates their rules allow by
// themselves; one they left open waits for a person, so an open one is
// never taken as the yes.
func depsPull(pr gh.PullInfo, app *store.App) bool {
	labels := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		labels = append(labels, l.Name)
	}
	return kindOf(pr.Head.Ref, app.DefaultBranch, labels) == KindDeps
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
		} else {
			// The branch did its work.
			if err := p.GitHub.DeleteBranch(ctx, app.InstallationID, app.Repo, pr.Head.Ref); err != nil {
				p.Log.Warn("merge: delete the branch", "app", app.Name, "branch", pr.Head.Ref, "err", err)
			}
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

// NotApprovable is ApproveMerge's answer for a run whose commit rendimiento
// cannot merge: it says why, for the person.
type NotApprovable struct{ Why string }

func (e *NotApprovable) Error() string { return e.Why }

// ApproveMerge is a person's yes to merging a run's commit: it is written
// on the run, the branch gets a pull request when it has none, and it is
// merged now when the run already passed, or when it does.
func (p *Platform) ApproveMerge(ctx context.Context, runID int64, who string) (*store.Run, error) {
	run, err := p.Store.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	app, err := p.Store.GetAppByID(ctx, run.AppID)
	if err != nil {
		return nil, err
	}
	switch {
	case !app.AutoMerge:
		return nil, &NotApprovable{Why: M("this app does not merge by itself (its Settings say who merges)")}
	case run.Deploy || run.Branch == app.DefaultBranch:
		return nil, &NotApprovable{Why: M("a run of the default branch has nothing to merge")}
	case run.Status == store.RunFailed || run.Status == store.RunCancelled:
		return nil, &NotApprovable{Why: M("only a run that passed is merged")}
	case run.Merge == store.MergeMerged:
		return run, nil
	}
	if last, err := p.Store.LastRunOf(ctx, app.ID, run.Branch); err == nil && last.SHA != run.SHA {
		return nil, &NotApprovable{Why: M("the branch has a newer commit: approve its run")}
	}
	title, err := p.GitHub.CommitSubject(ctx, app.InstallationID, app.Repo, run.SHA)
	if err != nil || title == "" {
		title = run.Branch
	}
	// What the change says of itself, when it follows the custom.
	body, _ := p.GitHub.FileAt(ctx, app.InstallationID, app.Repo, "cambios/"+run.Branch+".md", run.SHA)
	number, err := p.GitHub.CreatePull(ctx, app.InstallationID, app.Repo, app.DefaultBranch, run.Branch, title, string(body))
	if err != nil {
		return nil, &NotApprovable{Why: M("GitHub did not open the pull request: %s", githubSays(err))}
	}
	if err := p.Store.ApproveRun(ctx, run.ID, who); err != nil {
		return nil, err
	}
	p.Log.Info("merge approved", "app", app.Name, "branch", run.Branch, "run", run.ID, "pull", number, "by", who)
	p.publishRun(app, run.ID)
	if run.Status == store.RunSucceeded {
		// GitHub takes a few seconds to say whether a new pull request can
		// be merged: not on the person's request.
		go p.MergeReady(context.WithoutCancel(ctx), app)
	}
	return p.Store.GetRun(ctx, run.ID)
}

// ApproveLinks approves what an accepted encargo delivered: each link to a
// branch of an app that merges by itself (github.com/<repo>/pull/new/<branch>
// or /tree/<branch>) approves that branch's last run. What cannot be merged
// is left as it is.
func (p *Platform) ApproveLinks(ctx context.Context, links []string, who string) {
	for _, link := range links {
		repo, branch, ok := branchLink(link)
		if !ok {
			continue
		}
		apps, err := p.Store.AppsForRepo(ctx, repo)
		if err != nil {
			continue
		}
		for _, app := range apps {
			if !app.AutoMerge {
				continue
			}
			run, err := p.Store.LastRunOf(ctx, app.ID, branch)
			if err != nil {
				continue
			}
			if _, err := p.ApproveMerge(ctx, run.ID, who); err != nil {
				p.Log.Info("merge: encargo's branch not approved", "app", app.Name, "branch", branch, "err", err)
			}
			break // one pull request, all of the repository's apps
		}
	}
}

// branchLink reads the repository and branch out of a GitHub link to a
// branch: .../pull/new/<branch> or .../tree/<branch>.
func branchLink(link string) (repo, branch string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(link), "https://github.com/")
	if !found {
		return "", "", false
	}
	rest, _, _ = strings.Cut(rest, "?")
	rest, _, _ = strings.Cut(rest, "#")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 3 {
		return "", "", false
	}
	for _, prefix := range []string{"pull/new/", "tree/"} {
		if b, found := strings.CutPrefix(parts[2], prefix); found && b != "" {
			return parts[0] + "/" + parts[1], strings.TrimSuffix(b, "/"), true
		}
	}
	return "", "", false
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
