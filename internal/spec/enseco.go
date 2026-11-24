package spec

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// ---- en seco: a branch's new checks, against the published release or a copy ----
//
// Post-deploy tasks and the app's own tests (tests:) run only once a
// release is live, so a mistake in one of them is found when it rolls a
// release back. A branch can have them run "en seco" first, where a
// failure only warns. Each check asks for it (dryRun: true); one with
// service: never does, since it carries the app's environment, database
// included. What they run against (dryRunOn):
//   - published: the release already published, in the app's namespace,
//     on a branch that changes only the checks (their definition in
//     rendimiento.yaml, or the build whose image holds them, such as e2e/);
//   - preview: a copy of the branch (step 2), with previews: its pull
//     request's copy, or a short copy made only for the checks when it has
//     none. Never production's data;
//   - both.
// Without dryRunOn: published when the branch changes only the checks, a
// copy when it changes the app too (the published release would not have
// the change). A branch that touches neither runs nothing.

// What a dry check runs against (dryRunOn).
const (
	DryOnPublished = "published"
	DryOnPreview   = "preview"
	DryOnBoth      = "both"
)

// validateDryOn checks a dryRunOn (p is where it is written).
func (s *Spec) validateDryOn(p string, dry bool, on string) []error {
	switch on {
	case "":
		return nil
	case DryOnPublished, DryOnPreview, DryOnBoth:
	default:
		return []error{fmt.Errorf("%s.dryRunOn %q must be published, preview or both", p, on)}
	}
	if !dry {
		return []error{fmt.Errorf("%s.dryRunOn only applies with dryRun: true", p)}
	}
	if on != DryOnPublished && s.Previews == nil {
		return []error{fmt.Errorf("%s.dryRunOn %s runs against a copy of the branch: the app needs previews:", p, on)}
	}
	return nil
}

// onPublished says whether a dry check runs against the published release
// on a branch that changes only the checks.
func onPublished(t Task) bool { return t.DryRunOn != DryOnPreview }

// onCopy says whether a dry check runs against a copy of the branch: when
// it asks for one, or, without dryRunOn, when the branch changes the app.
func onCopy(t Task, appChanged bool) bool {
	switch t.DryRunOn {
	case DryOnPreview, DryOnBoth:
		return true
	case "":
		return appChanged
	}
	return false
}

// DryChecks are the checks that run en seco: the post-deploy tasks with
// dryRun (never one with service:) and the app's tests with dryRun, in
// order. Their after: names only the other dry checks: the published
// release already ran the rest.
func (s Spec) DryChecks() []Task {
	var out []Task
	for _, t := range s.Tasks {
		if t.PostDeploy() && t.DryRun && t.Service == "" {
			out = append(out, t)
		}
	}
	if s.Tests != nil && s.Tests.DryRun {
		out = append(out, s.Tests.Task(&s))
	}
	names := map[string]bool{}
	for _, t := range out {
		names[t.Name] = true
	}
	for i := range out {
		var after []string
		for _, a := range out[i].After {
			if names[a] {
				after = append(after, a)
			}
		}
		out[i].After = after
	}
	return out
}

// DryPlan is what a branch's run does en seco. It is not kept in an
// object, so it needs no deep copy.
// +kubebuilder:object:generate=false
type DryPlan struct {
	// Checks are the dry checks to run against the published release (all
	// of those that may, so after: holds), when Changed names at least one
	// and the app did not change.
	Checks []Task
	// Changed are the dry checks the branch changed: their definition,
	// or the build whose image holds them.
	Changed []string
	// AppChanged says the branch also changes the app (a service, job or
	// other build, or rendimiento.yaml beyond the checks): nothing runs
	// against the published release then.
	AppChanged bool
	// Copy are the dry checks to run against a copy of the branch (its
	// pull request's, or a short one): the app changed, or they ask for
	// it (dryRunOn: preview or both) and one of the checks changed.
	Copy []Task
}

// Runs says whether the plan runs anything against the published release.
func (d DryPlan) Runs() bool { return len(d.Checks) > 0 && len(d.Changed) > 0 && !d.AppChanged }

// RunsOnCopy says whether the plan runs anything against a copy of the
// branch.
func (d DryPlan) RunsOnCopy() bool { return len(d.Copy) > 0 }

// Touched says whether the branch touches the app or its checks at all:
// one that changes only the book runs nothing en seco.
func (d DryPlan) Touched() bool { return d.AppChanged || len(d.Changed) > 0 }

// DryRunPlan decides what a branch (this spec, with files changed against
// the default branch) runs en seco, given published, the spec of the
// release that is live. Against it only when the branch changes nothing
// but the checks, and at least one dry check is among what changed;
// against a copy of the branch when the app changed too, or when a check
// that changed asks for one (dryRunOn).
func (s Spec) DryRunPlan(published Spec, files []string) DryPlan {
	var plan DryPlan
	checks := s.DryChecks()
	if len(checks) == 0 {
		return plan
	}
	builds := s.checkBuilds()
	for name := range published.checkBuilds() {
		builds[name] = true
	}
	// The app's own parts: what a change to them changes in what runs.
	type part struct {
		dir, dockerfile string
		watch           []string
	}
	var app []part
	for _, svc := range s.Services {
		if svc.Image == "" {
			app = append(app, part{svc.Path, svc.Build.Dockerfile, svc.Watch})
		}
	}
	for _, j := range s.Jobs {
		if j.Path != "" {
			app = append(app, part{j.Path, j.Build.Dockerfile, j.Watch})
		}
	}
	touchedBuild := map[string]bool{}
	ownFile := false // the branch changes rendimiento.yaml itself
	for _, f := range files {
		f = cleanPath(f)
		ownFile = ownFile || f == FileName
		if f == "." || f == FileName || inert(f) {
			continue
		}
		for _, b := range s.Builds {
			if builds[b.Name] && touches(f, b.Path, b.Watch, b.Build.Dockerfile) {
				touchedBuild[b.Name] = true
			}
		}
		for _, b := range s.Builds {
			if !builds[b.Name] && touches(f, b.Path, b.Watch, b.Build.Dockerfile) {
				plan.AppChanged = true
			}
		}
		for _, p := range app {
			if touches(f, p.dir, p.watch, p.dockerfile) {
				plan.AppChanged = true
			}
		}
	}
	// rendimiento.yaml may differ from the published release's only in
	// the checks. A branch that does not touch the file does not change
	// it, even when the default branch moved on since it was made.
	if ownFile && !sameJSON(s.withoutChecks(builds), published.withoutChecks(builds)) {
		plan.AppChanged = true
	}
	before := map[string]Task{}
	for _, t := range published.Tasks {
		if t.PostDeploy() {
			before[t.Name] = t
		}
	}
	beforeBuilds := map[string]ImageBuild{}
	for _, b := range published.Builds {
		beforeBuilds[b.Name] = b
	}
	for _, b := range s.Builds {
		if prev, ok := beforeBuilds[b.Name]; builds[b.Name] && (!ok || !sameJSON(prev, b)) {
			touchedBuild[b.Name] = true
		}
	}
	for _, t := range s.Tasks {
		if !t.PostDeploy() || !t.DryRun || t.Service != "" {
			continue
		}
		prev, ok := before[t.Name]
		if !ok || !sameJSON(prev, t) || touchedBuild[strings.TrimPrefix(t.Image, "build:")] {
			plan.Changed = append(plan.Changed, t.Name)
		}
	}
	if s.Tests != nil && s.Tests.DryRun {
		if published.Tests == nil || !sameJSON(*published.Tests, *s.Tests) || touchedBuild[s.Tests.Build] {
			plan.Changed = append(plan.Changed, TestsTaskName)
		}
	}
	var copyChecks, publishedChecks []Task
	for _, t := range checks {
		if onPublished(t) {
			publishedChecks = append(publishedChecks, t)
		}
		if onCopy(t, plan.AppChanged) {
			copyChecks = append(copyChecks, t)
		}
	}
	if len(plan.Changed) > 0 && !plan.AppChanged {
		plan.Checks = onlyAmong(publishedChecks)
	}
	if plan.AppChanged || len(plan.Changed) > 0 {
		plan.Copy = onlyAmong(copyChecks)
	}
	return plan
}

// onlyAmong keeps in each check's after: only the checks given, which are
// the ones that run.
func onlyAmong(checks []Task) []Task {
	if len(checks) == 0 {
		return nil
	}
	names := map[string]bool{}
	for _, t := range checks {
		names[t.Name] = true
	}
	out := make([]Task, len(checks))
	for i, t := range checks {
		var after []string
		for _, a := range t.After {
			if names[a] {
				after = append(after, a)
			}
		}
		t.After = after
		out[i] = t
	}
	return out
}

// DryChecksAmong are this spec's dry checks named in among (a copy's spec
// gives the checks their copy's addresses), with after: kept to them.
func (s Spec) DryChecksAmong(among []Task) []Task {
	want := map[string]bool{}
	for _, t := range among {
		want[t.Name] = true
	}
	var out []Task
	for _, t := range s.DryChecks() {
		if want[t.Name] {
			out = append(out, t)
		}
	}
	return onlyAmong(out)
}

// checkBuilds are the builds whose image only checks run: named by a
// post-deploy task's image (build:<name>) or by tests.build.
func (s Spec) checkBuilds() map[string]bool {
	out := map[string]bool{}
	for _, t := range s.Tasks {
		if name, ok := strings.CutPrefix(t.Image, "build:"); ok && t.PostDeploy() {
			out[name] = true
		}
	}
	if s.Tests != nil && s.Tests.Build != "" {
		out[s.Tests.Build] = true
	}
	return out
}

// withoutChecks is the spec without its checks: no post-deploy tasks, no
// tests, and none of the given builds.
func (s Spec) withoutChecks(builds map[string]bool) Spec {
	out := s
	out.Tasks, out.Builds, out.Tests = nil, nil, nil
	for _, t := range s.Tasks {
		if !t.PostDeploy() {
			out.Tasks = append(out.Tasks, t)
		}
	}
	for _, b := range s.Builds {
		if !builds[b.Name] {
			out.Builds = append(out.Builds, b)
		}
	}
	return out
}

// touches says whether a changed file is under dir, one of the watch
// paths, or the Dockerfile.
func touches(f, dir string, watch []string, dockerfile string) bool {
	if Touches(f, cleanPath(dir), watch) {
		return true
	}
	return dockerfile != "" && f == cleanPath(path.Join(cleanPath(dir), dockerfile))
}

// sameJSON says whether two values are written the same.
func sameJSON(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}
