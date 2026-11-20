package renovate

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	gh "github.com/rendimiento-ai/rendimiento/internal/github"
)

func TestParse(t *testing.T) {
	log := strings.Join([]string{
		`{"name":"renovate","level":30,"msg":"Renovate started","time":"2026-09-27T05:00:01.000Z"}`,
		`{"level":30,"msg":"PR created","repository":"p0dxD/stockpulse","pr":42,"prTitle":"Update dependency lxml to v6.1.4","time":"2026-09-27T05:01:00.000Z"}`,
		`{"level":30,"msg":"PR automerged","repository":"p0dxD/stockpulse","pr":{"number":40},"prTitle":"Update fastapi","time":"2026-09-27T05:01:30.000Z"}`,
		`{"level":50,"msg":"Repository has unknown error","repository":"p0dxD/main_configs","err":{"message":"boom"},"time":"2026-09-27T05:02:00.000Z"}`,
		`{"level":30,"msg":"Repository finished","repository":"p0dxD/stockpulse","result":"done","time":"2026-09-27T05:03:00.000Z"}`,
		`plain text line`,
	}, "\n")
	text, res := Parse(strings.NewReader(log), []string{"p0dxD/stockpulse", "p0dxD/main_configs", "p0dxD/untouched"})
	sp := res["p0dxD/stockpulse"]
	if sp.Result != "done" || len(sp.PRs) != 1 || sp.PRs[0] != "Update dependency lxml to v6.1.4" || len(sp.Automerged) != 1 {
		t.Errorf("stockpulse = %+v", sp)
	}
	if mc := res["p0dxD/main_configs"]; len(mc.Errors) != 1 || !strings.Contains(mc.Errors[0], "boom") {
		t.Errorf("main_configs = %+v", mc)
	}
	if _, ok := res["p0dxD/untouched"]; !ok {
		t.Error("every requested repo gets a result")
	}
	for _, want := range []string{"05:01:00 INFO  [p0dxD/stockpulse] PR created: Update dependency lxml", "ERROR [p0dxD/main_configs] Repository has unknown error (boom)", "plain text line"} {
		if !strings.Contains(text, want) {
			t.Errorf("log missing %q:\n%s", want, text)
		}
	}
}

func TestSettings(t *testing.T) {
	s := DefaultSettings()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 27, 5, 0, 0, 0, time.UTC)
	if next := s.Next(from); !next.Equal(from.Add(24 * time.Hour)) {
		t.Errorf("next = %s", next)
	}
	for _, bad := range []Settings{
		{Schedule: "every day", Image: "x", Config: "x"},
		{Schedule: "@daily", Image: "", Config: "x"},
		{Schedule: "@daily", Image: "x", Config: "x", ExtraRepos: []string{"nope"}},
		{Schedule: "@daily", Image: "x", Config: " "},
	} {
		if bad.Validate() == nil {
			t.Errorf("%+v should be invalid", bad)
		}
	}
}

func TestPod(t *testing.T) {
	r := &Runner{Namespace: "rendimiento-builds", ExcludeNodes: []string{"gpu-node"}}
	p := r.pod("renovate-1-2", nil, DefaultSettings(), `["o/a"]`, gitHubEnv(&gh.BotIdentity{Login: "app[bot]", Email: "1+app[bot]@users.noreply.github.com"}), 55*time.Minute)
	env := map[string]string{}
	for _, e := range p.Spec.Containers[0].Env {
		env[e.Name] = e.Value
		if e.Name == "RENOVATE_TOKEN" && (e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil) {
			t.Error("the token must come from the secret")
		}
	}
	if env["RENOVATE_REPOSITORIES"] != `["o/a"]` || env["RENOVATE_GIT_AUTHOR"] != "app[bot] <1+app[bot]@users.noreply.github.com>" || env["LOG_FORMAT"] != "json" {
		t.Errorf("env = %v", env)
	}
	if *p.Spec.AutomountServiceAccountToken || *p.Spec.ActiveDeadlineSeconds != 3300 || p.Spec.Affinity == nil {
		t.Error("pod must have no API credentials, a deadline and the node exclusion")
	}
}

func TestParseKeepsErrorDetails(t *testing.T) {
	line := `{"level":20,"msg":"Unexpected GraphQL errors","repository":"o/a","errors":[{"type":"FORBIDDEN","message":"Resource not accessible by integration"}],"time":"2026-09-27T21:30:00.000Z"}`
	text, _ := Parse(strings.NewReader(line), []string{"o/a"})
	if !strings.Contains(text, "Resource not accessible by integration") {
		t.Errorf("details dropped: %s", text)
	}
}

func TestMissingPermissions(t *testing.T) {
	got := MissingPermissions(map[string]string{"contents": "write", "pull_requests": "write", "checks": "write", "metadata": "read"})
	if strings.Join(got, "|") != "Issues: Read and write|Commit statuses: Read-only" {
		t.Errorf("missing = %v", got)
	}
	if got := MissingPermissions(map[string]string{"contents": "write", "pull_requests": "write", "issues": "write", "statuses": "read", "checks": "write"}); len(got) != 0 {
		t.Errorf("all granted, got %v", got)
	}
}

// Renovate opened a pull request whose lockfile it could not regenerate
// (npm's peer-dependency conflict), and in another repository did not open
// the branch at all; both are kept per repository, briefly.
func TestParseArtifactErrors(t *testing.T) {
	stderr := "npm warn config production Use `--omit=dev` instead.\nnpm error code ERESOLVE\nnpm error ERESOLVE unable to resolve dependency tree\nnpm error\nnpm error While resolving: web@0.1.0\nnpm error Found: react@19.3.1\nnpm error Could not resolve dependency:\nnpm error peer react@\"^19.2.0\" from react-dom@19.2.8\n"
	warn, _ := json.Marshal(map[string]any{
		"name": "renovate", "level": 40, "msg": "artifactErrors", "time": "2026-11-16T05:19:11.000Z",
		"repository": "owner/web", "branch": "renovate/react-monorepo",
		"artifactErrors": []map[string]string{{"fileName": "package-lock.json", "stderr": stderr}},
	})
	notCreated, _ := json.Marshal(map[string]any{
		"name": "renovate", "level": 40, "msg": "Branch not created because artifact update failed", "time": "2026-11-16T05:20:00.000Z",
		"repository": "owner/api", "branch": "renovate/lock-file-maintenance",
		"errors": []map[string]string{{"fileName": "poetry.lock", "stderr": "Because api depends on numpy (^3.0) which doesn't match any versions, version solving failed.\n"}},
	})
	log := strings.Join([]string{
		`{"name":"renovate","level":30,"msg":"PR created","repository":"owner/web","branch":"renovate/react-monorepo","pr":7,"prTitle":"Update react monorepo to v19.3.1","time":"2026-11-16T05:19:10.000Z"}`,
		string(warn), string(warn), // a repeat is kept once
		string(notCreated),
		`{"name":"renovate","level":30,"msg":"Repository finished","repository":"owner/web","result":"done","time":"2026-11-16T05:19:21.000Z"}`,
	}, "\n")
	text, res := Parse(strings.NewReader(log), []string{"owner/web", "owner/api"})
	web := res["owner/web"].ArtifactErrors
	if len(web) != 1 || web[0].Branch != "renovate/react-monorepo" || web[0].File != "package-lock.json" {
		t.Fatalf("web = %+v", web)
	}
	if !strings.Contains(web[0].Error, "ERESOLVE unable to resolve dependency tree") || strings.Contains(web[0].Error, "npm warn") {
		t.Errorf("the error lines, without the warnings: %q", web[0].Error)
	}
	if api := res["owner/api"].ArtifactErrors; len(api) != 1 || api[0].File != "poetry.lock" || !strings.Contains(api[0].Error, "version solving failed") {
		t.Errorf("api = %+v", api)
	}
	if !strings.Contains(text, "WARN  [owner/web] artifactErrors") {
		t.Errorf("the warning is in the log:\n%s", text)
	}
}

func TestErrorLinesBounded(t *testing.T) {
	long := strings.Repeat("npm error something went wrong here\n", 100)
	got := errorLines(long)
	if n := strings.Count(got, "\n"); n != maxErrorLines || !strings.HasSuffix(got, "…") {
		t.Errorf("%d lines: %q", n, got)
	}
	if got := errorLines("just one line"); got != "just one line" {
		t.Errorf("no error words: %q", got)
	}
}

// An installation that may only read commit statuses must not have Renovate
// set them: that call fails and Renovate abandons the repository.
func TestStatusChecksEnv(t *testing.T) {
	env := statusChecksEnv(map[string]string{"statuses": "read"})
	if len(env) != 1 || env[0].Name != "RENOVATE_STATUS_CHECK_NAMES" {
		t.Fatalf("env = %v", env)
	}
	var names map[string]any
	if err := json.Unmarshal([]byte(env[0].Value), &names); err != nil || len(names) != 4 {
		t.Fatalf("%q: %v", env[0].Value, err)
	}
	for k, v := range names {
		if v != nil {
			t.Errorf("%s = %v, want null", k, v)
		}
	}
	if env := statusChecksEnv(map[string]string{"statuses": "write"}); len(env) != 0 {
		t.Errorf("write access keeps Renovate's statuses: %v", env)
	}
}

func TestWarnArtifactErrors(t *testing.T) {
	var buf bytes.Buffer
	r := &Runner{Log: slog.New(slog.NewTextHandler(&buf, nil))}
	b, _ := json.Marshal(RepoResult{ArtifactErrors: []ArtifactError{{Branch: "renovate/react-monorepo", File: "package-lock.json", Error: "npm error ERESOLVE"}}})
	r.warnArtifactErrors(map[string]json.RawMessage{"owner/web": b, "owner/quiet": json.RawMessage(`{"result":"done"}`)})
	got := buf.String()
	if strings.Count(got, "level=WARN") != 1 || !strings.Contains(got, `msg="`+MsgArtifactError+`"`) ||
		!strings.Contains(got, "repo=owner/web") || !strings.Contains(got, "file=package-lock.json") || !strings.Contains(got, "ERESOLVE") {
		t.Errorf("warning = %s", got)
	}
}
