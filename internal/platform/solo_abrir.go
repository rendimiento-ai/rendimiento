package platform

import (
	"context"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
)

// ---- just opening the pull request (Ruta 177) ----
//
// Beside "Merge this branch", a person may only open the branch's pull
// request: the same one ApproveMerge would open (its title and body), but
// nobody's yes is written and nothing is merged. Where the app asks for
// previews, the pull request's "opened" event makes its copy as for any
// other (OpenedPreview), and catchUpPreviews makes it when that event is
// lost: nothing more is needed here. In an app where any open pull request
// is the yes (MergeAny), opening it would merge it: it is refused there,
// except for a run that waits for a person anyway (what no step tests, a
// check that failed en seco).

// PullOpened is what OpenPull did: the branch's pull request, and whether it
// was open already.
type PullOpened struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	AlreadyOpen bool   `json:"alreadyOpen,omitempty"` // it was open before: nothing was opened
}

// OpenPull opens the pull request of a run's branch into the default
// branch, without merging it. A branch that has one already gets it back,
// with AlreadyOpen set. What cannot be opened is a NotApprovable, saying why.
func (p *Platform) OpenPull(ctx context.Context, runID int64, who string) (*PullOpened, error) {
	run, err := p.Store.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	app, err := p.Store.GetAppByID(ctx, run.AppID)
	if err != nil {
		return nil, err
	}
	if run.Deploy || run.Branch == app.DefaultBranch {
		return nil, &NotApprovable{Why: inForjaWords(app, M("a run of the default branch has no pull request to open"))}
	}
	if app.AutoMerge && app.MergeAny && len(run.Untested) == 0 && !run.DryFailed() {
		// There an open pull request is the yes (MergeReady): opening it
		// would merge it.
		return nil, &NotApprovable{Why: inForjaWords(app, M("in this app an open pull request is the yes to merge it: Merge this branch opens it and merges it"))}
	}
	repos := p.repos(app)
	if open, err := repos.OpenPulls(ctx, app.InstallationID, app.Repo); err == nil {
		for _, pr := range open {
			if pr.Head.Ref != run.Branch || pr.Base.Ref != app.DefaultBranch {
				continue
			}
			if fork := pr.Head.Repo.FullName; fork != "" && !strings.EqualFold(fork, app.Repo) {
				continue // a fork's branch may share the name
			}
			return &PullOpened{Number: pr.Number, URL: forja.PullURL(app.ForjaOf(), app.Repo, pr.Number), AlreadyOpen: true}, nil
		}
	}
	title, body := p.pullText(ctx, app, run)
	number, err := repos.CreatePull(ctx, app.InstallationID, app.Repo, app.DefaultBranch, run.Branch, title, body)
	if err != nil {
		says := forja.Says(err)
		if strings.Contains(says, "No commits between") {
			return nil, &NotApprovable{Why: inForjaWords(app, M("the branch is already in %s: there is no pull request to open", app.DefaultBranch))}
		}
		return nil, &NotApprovable{Why: inForjaWords(app, M("GitHub did not open the pull request: %s", says))}
	}
	p.Log.Info("pull request opened, not merged", "app", app.Name, "branch", run.Branch, "run", run.ID, "pull", number, "by", who)
	return &PullOpened{Number: number, URL: forja.PullURL(app.ForjaOf(), app.Repo, number)}, nil
}
