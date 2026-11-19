package platform

import (
	"context"
	"fmt"
	"strings"

	gh "github.com/rendimiento-ai/rendimiento/internal/github"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ---- what a branch's run did not test ----
//
// A run that passed vouches only for the files its steps read. When a
// branch also changes a folder no step builds or tests, its run still
// reports that it passed, but:
//   - rendimiento does not merge it by itself (only on a person's yes);
//   - its GitHub check concludes action_required, not success, and says
//     which paths no step tests, so a tool that waits for green checks
//     (Renovate's automerge) waits too;
//   - a person's yes in rendimiento turns the check green, and rendimiento
//     merges it.

// branchFiles are the files a branch's commit changes against the default
// branch; complete is false when GitHub listed only part of them (300
// files). Nothing (ok false) for runs of the default branch, or when
// GitHub does not say what changed.
func (p *Platform) branchFiles(ctx context.Context, app *store.App, run *store.Run) (files []string, complete, ok bool) {
	if run.Deploy || run.Branch == app.DefaultBranch || !fullSHA.MatchString(run.SHA) {
		return nil, false, false
	}
	files, complete, err := p.GitHub.ChangedFiles(ctx, app.InstallationID, app.Repo, app.DefaultBranch, run.SHA)
	if err != nil {
		p.Log.Warn("list the branch's changed files", "app", app.Name, "run", run.ID, "err", err)
		return nil, false, false
	}
	return files, complete, true
}

// untested lists what of the branch's change (files, as branchFiles says),
// no step of its run builds or tests. Nothing when the change is unknown.
func (p *Platform) untested(app *store.App, run *store.Run, sp spec.Spec, files []string, complete, ok bool) []string {
	if !ok {
		return nil
	}
	if !complete {
		// More than GitHub lists (300 files): judge what it listed.
		p.Log.Info("untested paths: the list of changed files is cut short", "app", app.Name, "run", run.ID)
	}
	return sp.Untested(files)
}

// untestedShown is how many untested paths a message names before "and N more".
const untestedShown = 5

// untestedPaths names the untested paths for a sentence: `mobile/`,
// `scripts/` and 3 more.
func untestedPaths(paths []string) string {
	shown := paths
	if len(shown) > untestedShown {
		shown = shown[:untestedShown]
	}
	quoted := make([]string, len(shown))
	for i, s := range shown {
		quoted[i] = "`" + s + "`"
	}
	out := strings.Join(quoted, ", ")
	if more := len(paths) - len(shown); more > 0 {
		out += fmt.Sprintf(" and %d more", more)
	}
	return out
}

// untestedTitle is the GitHub check's title for a run that passed with
// untested paths.
func untestedTitle(paths []string) string {
	return fmt.Sprintf("Build succeeded, but no step builds or tests %s: a person must merge this", untestedPaths(paths))
}

// approvedUntestedTitle is the check's title once a person approved it.
func approvedUntestedTitle(who string, paths []string) string {
	return fmt.Sprintf("Build succeeded; %s approved merging it, though no step builds or tests %s", who, untestedPaths(paths))
}

// untestedSummary is the check's text: why it is not green, and how a
// person merges it.
func untestedSummary(p *Platform, app *store.App, run *store.Run) string {
	return fmt.Sprintf("The run passed, but no step of %s builds or tests %s, so nothing has checked that part of the change. "+
		"rendimiento does not merge it by itself, and this check stays short of success so tools that wait for green checks (Renovate's automerge) wait too.\n\n"+
		"To merge it anyway, use **Merge this branch** on the [run's page](%s/apps/%s/runs/%d) (the check then turns green), or merge it in GitHub. "+
		"To have it tested, add a step for it to rendimiento.yaml.",
		app.Name, untestedPaths(run.Untested), p.Config.BaseURL, app.Name, run.ID)
}

// approveUntested turns the check of a run with untested paths green once
// a person said yes to merging it: the yes stands for the test no step did.
// The same for a run whose checks failed en seco.
func (p *Platform) approveUntested(ctx context.Context, app *store.App, run *store.Run, who string) {
	if (len(run.Untested) == 0 && !run.DryFailed()) || run.CheckRun == nil || *run.CheckRun == 0 || run.Status != store.RunSucceeded {
		return
	}
	title := approvedDryTitle(who, run)
	if len(run.Untested) > 0 {
		title = approvedUntestedTitle(who, run.Untested)
	}
	if err := p.GitHub.UpdateCheck(ctx, app.InstallationID, app.Repo, *run.CheckRun, gh.CheckRun{
		Status: "completed", Conclusion: "success",
		Output: gh.CheckOutput(title, fmt.Sprintf("[View run](%s/apps/%s/runs/%d)", p.Config.BaseURL, app.Name, run.ID)),
	}); err != nil {
		p.Log.Warn("update check run", "err", err)
	}
}
