package spec

import (
	"fmt"
	"strings"
)

// Tests are the app's own browser tests, run against each release once it
// is live (decision 136, step 13, phase 2). Their image is one of the app's
// builds: a Dockerfile from the Playwright image that copies the tests in,
// so the code and its browsers ship with the release. Each test starts by
// only reporting; after BlockingAfter clean releases in a row it may roll
// a release back.
type Tests struct {
	// Build names the build (builds:) whose image holds the tests.
	Build string `json:"build"`
	// Command runs them (default: npx playwright test). It must leave
	// Playwright's JSON report where PLAYWRIGHT_JSON_OUTPUT_NAME says: the
	// default command does.
	Command string `json:"command,omitempty"`
	// URL is where they point (BASE_URL); default: the first service with a
	// domain, over https.
	URL string `json:"url,omitempty"`
	// Env and SecretEnv are as for a task (a test user's password, say).
	Env       map[string]string `json:"env,omitempty"`
	SecretEnv map[string]string `json:"secretEnv,omitempty"`
	// BlockingAfter is how many releases in a row a test passes before a
	// failure of it rolls a release back (default 5, 1 to 50).
	BlockingAfter int `json:"blockingAfter,omitempty"`
	// Timeout bounds the run, in seconds (default the deploy tasks' 10 min).
	Timeout int `json:"timeout,omitempty"`
}

// TestsTaskName is the post-deploy task that runs the app's tests.
const TestsTaskName = "pruebas"

// DefaultBlockingAfter is how many clean releases make a test blocking.
const DefaultBlockingAfter = 5

// TestsReportLine starts each line the tests task prints about one test,
// read by the platform: "@@prueba<TAB>status<TAB>title", status as
// Playwright says it (expected, unexpected, flaky or skipped).
const TestsReportLine = "@@prueba"

// TestsNoReport is printed when the tests left no report (they did not run).
const TestsNoReport = "@@pruebas-sin-informe"

// testsReport prints one line per test from Playwright's JSON report.
const testsReport = `const fs = require("fs"); let r;
try { r = JSON.parse(fs.readFileSync(process.env.PLAYWRIGHT_JSON_OUTPUT_NAME, "utf8")); } catch (e) { console.log("` + TestsNoReport + `"); process.exit(0); }
const walk = (s, path) => {
  for (const sp of s.specs || []) for (const t of sp.tests || [])
    console.log("` + TestsReportLine + `\t" + t.status + "\t" + path.concat(t.projectName ? ["[" + t.projectName + "]"] : [], sp.title).filter(Boolean).join(" › "));
  for (const c of s.suites || []) walk(c, path.concat(c.title));
};
for (const s of r.suites || []) walk(s, [s.title]);`

// BlockAfter is how many clean releases make a test blocking.
func (t *Tests) BlockAfter() int {
	if t == nil || t.BlockingAfter == 0 {
		return DefaultBlockingAfter
	}
	return t.BlockingAfter
}

// Task is the post-deploy task that runs the tests: in the build's image,
// with one retry (a test that passes on it is flaky), Playwright's list and
// JSON reporters, and a line per test at the end. It never fails by itself:
// the platform judges each test.
func (t *Tests) Task(s *Spec) Task {
	cmd := t.Command
	if cmd == "" {
		cmd = "npx playwright test"
	}
	env := map[string]string{"PLAYWRIGHT_JSON_OUTPUT_NAME": "/tmp/pruebas.json", "CI": "true"}
	if u := t.url(s); u != "" {
		env["BASE_URL"] = u
	}
	for k, v := range t.Env {
		env[k] = v
	}
	script := fmt.Sprintf("%s --retries=1 --reporter=list,json; node -e %s; exit 0", cmd, shellQuote(testsReport))
	return Task{Name: TestsTaskName, Stage: StagePostDeploy, Image: "build:" + t.Build, Command: script,
		Optional: true, Env: env, SecretEnv: t.SecretEnv, Timeout: t.Timeout}
}

// url is BASE_URL: the one given, or the first service's domain.
func (t *Tests) url(s *Spec) string {
	if t.URL != "" {
		return t.URL
	}
	for _, svc := range s.Services {
		if svc.Domain != "" {
			return "https://" + svc.Domain
		}
	}
	return ""
}

func (t *Tests) validate(s *Spec) []error {
	if t == nil {
		return nil
	}
	var errs []error
	found := false
	for _, b := range s.Builds {
		found = found || b.Name == t.Build
	}
	if t.Build == "" || !found {
		errs = append(errs, fmt.Errorf("tests.build %q must name one of the app's builds: an image with the tests and their browsers", t.Build))
	}
	if t.BlockingAfter != 0 && (t.BlockingAfter < 1 || t.BlockingAfter > 50) {
		errs = append(errs, fmt.Errorf("tests.blockingAfter must be 1 to 50 releases"))
	}
	if t.Timeout < 0 {
		errs = append(errs, fmt.Errorf("tests.timeout must be a number of seconds"))
	}
	if t.URL != "" && !strings.HasPrefix(t.URL, "http://") && !strings.HasPrefix(t.URL, "https://") {
		errs = append(errs, fmt.Errorf("tests.url %q must start with http:// or https://", t.URL))
	}
	if t.url(s) == "" {
		errs = append(errs, fmt.Errorf("tests need a url, or a service with a domain"))
	}
	for k := range t.Env {
		if !envKey.MatchString(k) {
			errs = append(errs, fmt.Errorf("tests.env %q is not a valid variable name", k))
		}
	}
	for k, ref := range t.SecretEnv {
		name, key, ok := strings.Cut(ref, "/")
		if !envKey.MatchString(k) || !ok || name == "" || key == "" {
			errs = append(errs, fmt.Errorf("tests.secretEnv %s must be secret/key", k))
		}
	}
	for _, task := range s.Tasks {
		if task.Name == TestsTaskName {
			errs = append(errs, fmt.Errorf("tasks: %q is the name of the tests' own task", TestsTaskName))
		}
	}
	return errs
}

// shellQuote quotes s for sh -c.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
