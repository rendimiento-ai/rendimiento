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
		if len(plan.Changed) > 0 && len(plan.Checks) != 2 {
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
