package spec

import (
	"strings"
	"testing"
)

func TestTestsValidate(t *testing.T) {
	base := "services:\n  - name: web\n    domain: tienda.example.com\nbuilds:\n  - name: e2e\n    path: e2e\n"
	for in, want := range map[string]string{
		base + "tests: {build: otra}":                                                          "must name one of the app's builds",
		base + "tests: {build: e2e, blockingAfter: 51}":                                        "1 to 50 releases",
		base + "tests: {build: e2e, url: tienda.example.com}":                                  "must start with http",
		base + "tests: {build: e2e, secretEnv: {PASS: nada}}":                                  "must be secret/key",
		"services:\n  - name: web\nbuilds:\n  - name: e2e\n    path: e2e\ntests: {build: e2e}": "need a url",
		base + "tests: {build: e2e}\ntasks:\n  - name: pruebas\n    image: x\n    command: y":  "the name of the tests' own task",
		base + "tests: {build: e2e, size: huge}":                                               "tests.size \"huge\" must be small",
		base + "tests: {build: e2e, resources: {memory: mucha}}":                               "tests.resources.memory",
		base + "tests: {build: e2e, size: small, resources: {memory: 1Gi}}":                    "more than its limit",
	} {
		if _, err := Parse([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %v, want %q", in, err, want)
		}
	}
	s, err := Parse([]byte(base + "tests: {build: e2e, env: {TEST_USER: ana}}"))
	if err != nil {
		t.Fatal(err)
	}
	task := s.Tests.Task(s)
	if task.Name != TestsTaskName || task.Stage != StagePostDeploy || task.Image != "build:e2e" || !task.Optional {
		t.Fatalf("task %+v", task)
	}
	if task.Env["BASE_URL"] != "https://tienda.example.com" || task.Env["TEST_USER"] != "ana" || task.Env["PLAYWRIGHT_JSON_OUTPUT_NAME"] == "" {
		t.Fatalf("env %v", task.Env)
	}
	if !strings.Contains(task.Command, "npx playwright test --retries=1 --reporter=list,json") || !strings.HasSuffix(task.Command, "exit 0") {
		t.Fatalf("command %q", task.Command)
	}
	if s.Tests.BlockAfter() != DefaultBlockingAfter {
		t.Fatalf("blocking after %d", s.Tests.BlockAfter())
	}
}

// Without a size the tests get a browser's resources, like a build's
// test: section; a size and resources change them as for a task.
func TestTestsResources(t *testing.T) {
	base := "services:\n  - name: web\n    domain: tienda.example.com\nbuilds:\n  - name: e2e\n    path: e2e\n"
	for in, want := range map[string]Resources{
		"tests: {build: e2e}":                                                  {"250m", "256Mi", "", "2Gi"},
		"tests: {build: e2e, size: large}":                                     {"250m", "512Mi", "2", "1Gi"},
		"tests: {build: e2e, size: medium, resources: {memoryLimit: 3Gi}}":     {"100m", "256Mi", "1", "3Gi"},
		"tests: {build: e2e, resources: {cpuLimit: \"2\", memoryLimit: none}}": {"250m", "256Mi", "2", ""},
	} {
		s, err := Parse([]byte(base + in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		task := s.Tests.Task(s)
		if got := ResourcesFor(task.Size, task.Resources); got != want {
			t.Errorf("%s: task resources %+v, want %+v", in, got, want)
		}
	}
}
