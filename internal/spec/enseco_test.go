package spec

import (
	"reflect"
	"strings"
	"testing"
)

// enSeco is an app like goconcalma: a web service, a smoke test that may
// run en seco, a migration that never may, and its own tests in e2e/.
func enSeco() Spec {
	s := Spec{
		Services: []Service{{Name: "web", Path: "web", Domain: "tienda.example.com"}},
		Builds:   []ImageBuild{{Name: "e2e", Path: "e2e"}, {Name: "herramientas", Path: "tools"}},
		Tasks: []Task{
			{Name: "migrate", Stage: StagePostDeploy, Service: "web", Command: "migrate"},
			{Name: "humo", Stage: StagePostDeploy, Image: "curlimages/curl:8.17.0", Command: "curl -fsS http://web/", DryRun: true, After: []string{"migrate"}},
			{Name: "lint", Image: "node:22", Command: "npm run lint", Path: "web", When: TaskOnAlways},
		},
		Tests: &Tests{Build: "e2e", DryRun: true},
	}
	s.Default()
	return s
}

func TestDryChecks(t *testing.T) {
	checks := enSeco().DryChecks()
	var names []string
	for _, c := range checks {
		names = append(names, c.Name)
		if c.Service != "" {
			t.Errorf("%s runs en seco with service:", c.Name)
		}
	}
	if !reflect.DeepEqual(names, []string{"humo", TestsTaskName}) {
		t.Fatalf("dry checks %v", names)
	}
	if len(checks[0].After) != 0 {
		t.Errorf("humo waits for %v, which does not run en seco", checks[0].After)
	}
	if !checks[1].DryRun || checks[1].Image != "build:e2e" {
		t.Errorf("the tests' task: %+v", checks[1])
	}
}

func TestDryRunPlan(t *testing.T) {
	published := enSeco()
	smoke := enSeco()
	smoke.Tasks[1].Command = "curl -fsS http://web/ && curl -fsS http://web/clima"
	newApp := enSeco()
	newApp.Services[0].Replicas = 3
	notDry := enSeco()
	notDry.Tasks[1].DryRun, notDry.Tests.DryRun = false, false
	notDry.Tasks[1].Command = "curl http://web/otra"
	newTests := enSeco()
	newTests.Tests.Env = map[string]string{"TEST_USER": "prueba@example.com"}

	for _, c := range []struct {
		name       string
		branch     Spec
		files      []string
		changed    []string
		appChanged bool
	}{
		{"the smoke test's command", smoke, []string{"rendimiento.yaml"}, []string{"humo"}, false},
		{"the tests' folder", published, []string{"e2e/tests/compra.spec.ts", "README.md"}, []string{TestsTaskName}, false},
		{"the tests' settings", newTests, []string{"rendimiento.yaml"}, []string{TestsTaskName}, false},
		{"the app's code too", published, []string{"e2e/tests/compra.spec.ts", "web/src/App.tsx"}, []string{TestsTaskName}, true},
		{"another build", published, []string{"tools/x.sh"}, nil, true},
		{"the app in rendimiento.yaml", newApp, []string{"rendimiento.yaml"}, nil, true},
		{"only documentation", published, []string{"docs/guia.md"}, nil, false},
		{"checks without dryRun", notDry, []string{"rendimiento.yaml"}, nil, false},
		{"a build task's folder", published, []string{"web/.eslintrc"}, nil, true},
	} {
		plan := c.branch.DryRunPlan(published, c.files)
		if !reflect.DeepEqual(plan.Changed, c.changed) || plan.AppChanged != c.appChanged {
			t.Errorf("%s: changed %v, app changed %v; want %v, %v", c.name, plan.Changed, plan.AppChanged, c.changed, c.appChanged)
		}
		if want := len(c.changed) > 0 && !c.appChanged; plan.Runs() != want {
			t.Errorf("%s: runs %v, want %v", c.name, plan.Runs(), want)
		}
		if plan.Runs() && len(plan.Checks) != 2 {
			t.Errorf("%s: runs %d checks, want both dry ones", c.name, len(plan.Checks))
		}
	}
}

func TestDryRunRules(t *testing.T) {
	for _, c := range []struct {
		name string
		task Task
		want string
	}{
		{"with service", Task{Name: "migrate", Stage: StagePostDeploy, Service: "web", Command: "migrate", DryRun: true}, "dryRun cannot go with service"},
		{"pre-deploy", Task{Name: "pre", Stage: StagePreDeploy, Image: "alpine:3.22", Command: "true", DryRun: true}, "dryRun only applies to post-deploy tasks"},
		{"a build task", Task{Name: "lint", Image: "alpine:3.22", Command: "true", DryRun: true}, "dryRun only applies to post-deploy tasks"},
	} {
		s := Spec{Services: []Service{{Name: "web"}}, Tasks: []Task{c.task}}
		s.Default()
		err := s.Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// What each check runs against: the published release when only the
// checks changed, a copy of the branch when the app changed, as dryRunOn
// asks otherwise; a branch that touches neither runs nothing.
func TestDryRunTargets(t *testing.T) {
	names := func(ts []Task) []string {
		var out []string
		for _, x := range ts {
			out = append(out, x.Name)
		}
		return out
	}
	withOn := func(smoke, tests string) Spec {
		s := enSeco()
		s.Previews = &Previews{}
		s.Tasks[1].DryRunOn, s.Tests.DryRunOn = smoke, tests
		return s
	}
	for _, c := range []struct {
		name             string
		branch           Spec
		files            []string
		published, other []string // against the published release, against a copy
	}{
		{"only the checks, no dryRunOn", withOn("", ""), []string{"e2e/compra.spec.ts"}, []string{"humo", TestsTaskName}, nil},
		{"the app changed, no dryRunOn", withOn("", ""), []string{"web/package.json"}, nil, []string{"humo", TestsTaskName}},
		{"the app and its tests changed", withOn("", ""), []string{"web/package.json", "e2e/compra.spec.ts"}, nil, []string{"humo", TestsTaskName}},
		{"only documentation", withOn("", ""), []string{"docs/guia.md", "README.md"}, nil, nil},
		{"only the checks, tests on a copy", withOn("", DryOnPreview), []string{"e2e/compra.spec.ts"}, []string{"humo"}, []string{TestsTaskName}},
		{"only the checks, both", withOn(DryOnBoth, DryOnBoth), []string{"e2e/compra.spec.ts"}, []string{"humo", TestsTaskName}, []string{"humo", TestsTaskName}},
		{"the app changed, smoke only published", withOn(DryOnPublished, ""), []string{"web/src/App.tsx"}, nil, []string{TestsTaskName}},
		{"the app changed, both", withOn(DryOnBoth, DryOnBoth), []string{"web/src/App.tsx"}, nil, []string{"humo", TestsTaskName}},
	} {
		published := c.branch
		plan := c.branch.DryRunPlan(published, c.files)
		var got []string
		if plan.Runs() {
			got = names(plan.Checks)
		}
		if !reflect.DeepEqual(got, c.published) || !reflect.DeepEqual(names(plan.Copy), c.other) {
			t.Errorf("%s: published %v, copy %v; want %v, %v", c.name, got, names(plan.Copy), c.published, c.other)
		}
		if plan.RunsOnCopy() != (len(c.other) > 0) {
			t.Errorf("%s: runs on a copy %v", c.name, plan.RunsOnCopy())
		}
		if c.name == "only documentation" && plan.Touched() {
			t.Errorf("a branch of documentation touches the app: %+v", plan)
		}
	}

	// A branch made before the default branch changed the app is not
	// changing the app when it does not touch rendimiento.yaml.
	moved := enSeco()
	moved.Services[0].Replicas = 2
	if plan := enSeco().DryRunPlan(moved, []string{"docs/guia.md"}); plan.Touched() {
		t.Errorf("the default branch moved on, the branch touches only docs: %+v", plan)
	}

	// The copy's checks have the copy's addresses.
	s := withOn("", "")
	s.Tests.URL = "https://tienda.example.com/es"
	short := ShortCopyOf(s, 1802)
	checks := short.DryChecksAmong(s.DryRunPlan(s, []string{"web/x.ts"}).Copy)
	if len(checks) != 2 || checks[1].Env["BASE_URL"] != "https://tienda-seco-1802.example.com/es" || short.Services[0].Domain != "tienda-seco-1802.example.com" {
		t.Fatalf("the short copy's checks: %+v, domain %s", checks, short.Services[0].Domain)
	}
	s.Tests.URL = "https://tienda.real.example.org/"
	if u := PreviewOf(s, 31).Tests.URL; u != "" {
		t.Fatalf("the tests of a copy point at %q, not one of the app's hosts", u)
	}
	if s.Tests.URL != "https://tienda.real.example.org/" || s.Tests.DryRunOn != "" {
		t.Fatalf("copyOf changed the app's own tests: %+v", s.Tests)
	}
}

func TestDryRunOnRules(t *testing.T) {
	base := "services:\n  - name: web\n    domain: tienda.example.com\nbuilds:\n  - name: e2e\n    path: e2e\n"
	for in, want := range map[string]string{
		base + "tests: {build: e2e, dryRun: true, dryRunOn: always}":  "must be published, preview or both",
		base + "tests: {build: e2e, dryRunOn: published}":             "dryRunOn only applies with dryRun: true",
		base + "tests: {build: e2e, dryRun: true, dryRunOn: preview}": "the app needs previews:",
		base + "tasks:\n  - name: humo\n    stage: post-deploy\n    image: alpine:3.22\n    command: \"true\"\n    dryRun: true\n    dryRunOn: both\n": "the app needs previews:",
		base + "tasks:\n  - name: lint\n    image: alpine:3.22\n    command: \"true\"\n    dryRunOn: published\n":                                      "dryRun only applies to post-deploy tasks",
	} {
		if _, err := Parse([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %v, want %q", in, err, want)
		}
	}
	ok := base + "previews: {}\ntests: {build: e2e, dryRun: true, dryRunOn: both}\n"
	if _, err := Parse([]byte(ok)); err != nil {
		t.Fatalf("dryRunOn both with previews: %v", err)
	}
}
