package spec

import (
	"path"
	"sort"
	"strings"
)

// ---- what a branch's run tests ----
//
// A branch's run passing says something only about the files its steps
// read. A change to a folder no step builds or tests (a mobile app beside
// the services, say) passes without anything having looked at it, so it
// must not be merged by itself on the strength of that run.

// TestedPaths are the repository paths a run of a branch or pull request
// builds or tests: each built service's, build's and job's folder (and its
// watch paths and Dockerfile), and each build-stage task that runs on
// branches (when: always) with its watch paths. "." is the whole
// repository: a step that builds or runs from the root reads everything.
// Tasks that run only for the default branch (when: deploy) and pre- and
// post-deploy tasks do not test a branch, so they do not count.
func (s Spec) TestedPaths() []string {
	var out []string
	add := func(dir string, watch []string, dockerfile string) {
		for _, p := range append([]string{dir}, watch...) {
			out = append(out, cleanPath(p))
		}
		if dockerfile != "" {
			if f := cleanPath(path.Join(cleanPath(dir), dockerfile)); f != "." && !strings.HasPrefix(f, "..") {
				out = append(out, f)
			}
		}
	}
	for _, svc := range s.Services {
		if svc.Image == "" {
			add(svc.Path, svc.Watch, svc.Build.Dockerfile)
		}
	}
	for _, b := range s.Builds {
		add(b.Path, b.Watch, b.Build.Dockerfile)
	}
	for _, j := range s.Jobs {
		if j.Path != "" {
			add(j.Path, j.Watch, j.Build.Dockerfile)
		}
	}
	for _, t := range s.Tasks {
		if !t.Deploys() && t.When == TaskOnAlways {
			add(t.Path, t.Watch, "")
		}
	}
	return out
}

// cleanPath is a repository path without leading or trailing slashes, "."
// for the root.
func cleanPath(p string) string {
	p = strings.Trim(path.Clean("/"+strings.TrimSpace(p)), "/")
	if p == "" {
		return "."
	}
	return p
}

// inert says whether a changed file is one no build reads: documentation,
// licences and the settings of git, editors and Renovate. Those need no
// step to be merged.
func inert(f string) bool {
	base := path.Base(f)
	switch strings.ToLower(base) {
	case ".gitignore", ".gitattributes", ".editorconfig", ".mailmap",
		"renovate.json", "renovate.json5", ".renovaterc", ".renovaterc.json",
		"codeowners", "license", "licence", "notice", "copying":
		return true
	}
	lower := strings.ToLower(base)
	return strings.HasSuffix(lower, ".md") || strings.HasPrefix(lower, "license.") || strings.HasPrefix(lower, "licence.")
}

// Untested lists what of the changed files no step of a branch's run
// builds or tests, as the shortest folders that hold no tested path
// ("mobile/"), or the file itself when every folder above it holds one.
// rendimiento.yaml is always tested: the run reads it to plan its steps.
// The list is sorted and empty when the whole change is covered.
func (s Spec) Untested(files []string) []string {
	tested := s.TestedPaths()
	for _, t := range tested {
		if t == "." {
			return nil // a step reads the whole repository
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		f = cleanPath(f)
		if f == "." || f == FileName || inert(f) || coveredBy(f, tested) {
			continue
		}
		g := group(f, tested)
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out
}

// coveredBy says whether f is one of the tested paths or under one.
func coveredBy(f string, tested []string) bool {
	for _, t := range tested {
		if f == t || strings.HasPrefix(f, t+"/") {
			return true
		}
	}
	return false
}

// group is how an untested file is named: its shortest folder that holds
// no tested path, with a trailing slash, or the file itself.
func group(f string, tested []string) string {
	parts := strings.Split(f, "/")
	for i := 1; i < len(parts); i++ {
		dir := strings.Join(parts[:i], "/")
		holds := false
		for _, t := range tested {
			if t == dir || strings.HasPrefix(t, dir+"/") {
				holds = true
				break
			}
		}
		if !holds {
			return dir + "/"
		}
	}
	return f
}
