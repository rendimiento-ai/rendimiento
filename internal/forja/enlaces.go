package forja

import (
	"fmt"
	"net/url"
	"strings"
)

// The forjas' short names, as apps.forja says them.
const (
	GitHub    = "github"
	GitLab    = "gitlab"
	Bitbucket = "bitbucket"
)

// Of is the forja a repository lives on, read from how rendimiento writes
// it: owner/name on GitHub (whose accounts have no dots), bitbucket.org/
// workspace/repo on Bitbucket, and a GitLab's address first on GitLab
// (git.example.com/group/project).
func Of(repo string) string {
	first, _, _ := strings.Cut(repo, "/")
	switch {
	case strings.EqualFold(first, "bitbucket.org"):
		return Bitbucket
	case strings.ContainsAny(first, ".:"):
		return GitLab
	}
	return GitHub
}

// RepoURL is the repository's page (web/src/forja.ts says the same).
func RepoURL(name, repo string) string {
	if name == "" || name == GitHub {
		return "https://github.com/" + repo
	}
	return "https://" + repo
}

// PullURL is a change's page: the pull request on GitHub and Bitbucket,
// the merge request on GitLab.
func PullURL(name, repo string, n int) string {
	switch name {
	case GitLab:
		return fmt.Sprintf("%s/-/merge_requests/%d", RepoURL(name, repo), n)
	case Bitbucket:
		return fmt.Sprintf("%s/pull-requests/%d", RepoURL(name, repo), n)
	}
	return fmt.Sprintf("%s/pull/%d", RepoURL(name, repo), n)
}

// NewPullURL is where a person opens the change of a branch (into base;
// "" is the default branch).
func NewPullURL(name, repo, base, branch string) string {
	switch name {
	case GitLab:
		q := url.Values{"merge_request[source_branch]": {branch}}
		if base != "" {
			q.Set("merge_request[target_branch]", base)
		}
		return RepoURL(name, repo) + "/-/merge_requests/new?" + q.Encode()
	case Bitbucket:
		q := url.Values{"source": {branch}}
		if base != "" {
			q.Set("dest", base)
		}
		return RepoURL(name, repo) + "/pull-requests/new?" + q.Encode()
	}
	if base != "" {
		return fmt.Sprintf("%s/compare/%s...%s?expand=1", RepoURL(name, repo), base, branch)
	}
	return RepoURL(name, repo) + "/pull/new/" + branch
}

// BranchOfLink reads the repository and branch out of a link to a branch:
// GitHub's …/pull/new/<branch> or …/tree/<branch>, GitLab's
// …/-/merge_requests/new?merge_request[source_branch]=<branch> or
// …/-/tree/<branch>, and Bitbucket's …/pull-requests/new?source=<branch> or
// …/src/<branch>. A link to a change into another branch than the default
// one (GitHub's compare, a GitLab target_branch, a Bitbucket dest) is not
// read: what reads these opens changes into the default branch.
func BranchOfLink(link string) (repo, branch string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", "", false
	}
	p := strings.Trim(u.Path, "/")
	if strings.EqualFold(u.Host, "github.com") {
		parts := strings.SplitN(p, "/", 3)
		if len(parts) < 3 {
			return "", "", false
		}
		repo = parts[0] + "/" + parts[1]
		for _, prefix := range []string{"pull/new/", "tree/"} {
			if b, found := strings.CutPrefix(parts[2], prefix); found && b != "" {
				return repo, strings.TrimSuffix(b, "/"), true
			}
		}
		return "", "", false
	}
	if strings.EqualFold(u.Host, "bitbucket.org") {
		if r, found := strings.CutSuffix(p, "/pull-requests/new"); found && u.Query().Get("source") != "" && u.Query().Get("dest") == "" {
			return "bitbucket.org/" + r, u.Query().Get("source"), true
		}
		if r, b, found := strings.Cut(p, "/src/"); found && b != "" && strings.Count(r, "/") == 1 {
			return "bitbucket.org/" + r, b, true
		}
		return "", "", false
	}
	host := u.Host
	if r, found := strings.CutSuffix(p, "/-/merge_requests/new"); found && u.Query().Get("merge_request[source_branch]") != "" && u.Query().Get("merge_request[target_branch]") == "" {
		return host + "/" + r, u.Query().Get("merge_request[source_branch]"), true
	}
	if r, b, found := strings.Cut(p, "/-/tree/"); found && b != "" {
		return host + "/" + r, b, true
	}
	return "", "", false
}
