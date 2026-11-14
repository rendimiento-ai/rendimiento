package platform

import (
	"context"
	"fmt"
	"strings"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
)

// PullsForDelivery opens the pull request of each branch an encargo
// delivers (a link github.com/<repo>/pull/new/<branch> or …/tree/<branch>
// to a repository of the organization's apps), so the person sees it and
// its preview copy before deciding, and "Merge this branch" merges that
// same pull request. It returns the links with each pull request's
// added before its branch's, which stays: accepting the encargo still
// finds the branch there. A link it cannot open a pull request for is
// left as it was.
func (p *Platform) PullsForDelivery(ctx context.Context, orgID, encargo int64, title, result string, links []string) []string {
	if p.GitHub == nil {
		return links
	}
	out := make([]string, 0, len(links)+1)
	for _, link := range links {
		repo, branch, ok := branchLink(link)
		if !ok {
			out = append(out, link)
			continue
		}
		apps, err := p.Store.AppsForRepo(ctx, repo)
		if err != nil {
			out = append(out, link)
			continue
		}
		opened := false
		for _, a := range apps {
			if a.OrgID != orgID || branch == a.DefaultBranch || opened {
				continue
			}
			body := M("Encargo %d, delivered in rendimiento: %s\n\n%s\n\nOpened by rendimiento when the encargo was delivered, so its checks and its preview copy can be seen before merging.",
				encargo, title, result)
			number, err := p.GitHub.CreatePull(ctx, a.InstallationID, a.Repo, a.DefaultBranch, branch, title, body)
			if err != nil {
				p.Log.Warn("encargo delivered: opening its pull request", "encargo", encargo, "repo", repo, "branch", branch, "err", githubSays(err))
				continue
			}
			out = append(out, fmt.Sprintf("https://github.com/%s/pull/%d", a.Repo, number))
			opened = true
			p.Log.Info("encargo delivered: pull request opened", "encargo", encargo, "repo", repo, "branch", branch, "pull", number)
		}
		out = append(out, link)
	}
	if len(out) > 10 { // what an encargo keeps; the branch links stay first in line
		out = out[:10]
	}
	return dedupe(out)
}

func dedupe(links []string) []string {
	seen := map[string]bool{}
	out := links[:0]
	for _, l := range links {
		if k := strings.TrimRight(l, "/"); !seen[k] {
			seen[k] = true
			out = append(out, l)
		}
	}
	return out
}
