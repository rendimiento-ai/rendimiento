package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/platform"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// fakePulls is a forja that keeps pull requests: what OpenPull asks of it.
// The rest of forja.Repos is left nil: a call to it fails the test loudly.
type fakePulls struct {
	forja.Repos
	mu     sync.Mutex
	open   []forja.Change
	opened []string // "branch: title | body" of each pull request opened
	merged int      // nothing merges here: it must stay 0
}

func (f *fakePulls) OpenPulls(context.Context, int64, string) ([]forja.Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]forja.Change(nil), f.open...), nil
}

func (f *fakePulls) CreatePull(_ context.Context, _ int64, _, base, branch, title, body string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pr := forja.Change{Number: 40 + len(f.opened), State: "open"}
	pr.Head.Ref, pr.Base.Ref = branch, base
	f.open = append(f.open, pr)
	f.opened = append(f.opened, branch+": "+title+" | "+body)
	return pr.Number, nil
}

func (f *fakePulls) CommitSubject(context.Context, int64, string, string) (string, error) {
	return "Lo que dice el commit", nil
}

func (f *fakePulls) FileAt(_ context.Context, _ int64, _, path, _ string) ([]byte, error) {
	if strings.HasPrefix(path, "cambios/") {
		return []byte("- **Runs:** lo que cambia."), nil
	}
	return nil, errors.New("not found")
}

func (f *fakePulls) MergePull(context.Context, int64, string, int, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merged++
	return nil
}

// seen is what was opened and merged, read under the lock (the server's
// goroutines wrote it).
func (f *fakePulls) seen() ([]string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.opened...), f.merged
}

// "Just open the pull request": it opens the branch's pull request with the
// merge action's title and body, and merges nothing; one already open is
// answered with its link; the default branch has none to open. On GitLab it
// is the merge request. A guest may not, nor an agent's key.
func TestOpenPullOnly(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	fake := &fakePulls{}
	e.s.Platform.Forja = fake
	e.s.Platform.Forjas = map[string]forja.Repos{forja.GitLab: fake}
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	gh := &store.App{Name: "soloabre", Repo: "p0dxD/soloabre", DefaultBranch: "main", Spec: sp}
	gl := &store.App{Name: "soloabre-gl", Repo: "git.example.com/grupo/soloabre", DefaultBranch: "main", Spec: sp, Forja: forja.GitLab}
	for _, a := range []*store.App{gh, gl} {
		if err := e.store.CreateApp(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.store.SetMember(ctx, store.DefaultOrg, "invitada-sa", store.MemberGuest, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetMember(ctx, store.DefaultOrg, "colabora-sa", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	admin, guest, collab := e.session(t, "p0dxD"), e.session(t, "invitada-sa"), e.session(t, "colabora-sa")
	run := func(a *store.App, branch string, deploy bool) *store.Run {
		t.Helper()
		r := &store.Run{AppID: a.ID, SHA: strings.Repeat("ab", 20), Branch: branch, Event: "push", Deploy: deploy}
		if err := e.store.CreateRun(ctx, r, nil); err != nil {
			t.Fatal(err)
		}
		if err := e.store.FinishRun(ctx, r.ID, store.RunSucceeded, ""); err != nil {
			t.Fatal(err)
		}
		return r
	}
	open := func(r *store.Run, cookie *http.Cookie, hdr map[string]string) (int, string, platform.PullOpened) {
		t.Helper()
		var got platform.PullOpened
		res, body := e.do(t, "POST", fmt.Sprintf("/api/runs/%d/pull", r.ID), "", cookie, hdr)
		_ = json.Unmarshal([]byte(body), &got)
		return res.StatusCode, body, got
	}

	// Opened: the merge action's title and body, nobody's yes, nothing merged.
	probar := run(gh, "probar", false)
	code, body, got := open(probar, collab, nil)
	if code != 200 || got.Number != 40 || got.AlreadyOpen || got.URL != "https://github.com/p0dxD/soloabre/pull/40" {
		t.Fatalf("opening: %d %s", code, body)
	}
	opened, merged := fake.seen()
	if len(opened) != 1 || opened[0] != "probar: Lo que dice el commit | - **Runs:** lo que cambia." {
		t.Fatalf("opened %q", opened)
	}
	if r, _ := e.store.GetRun(ctx, probar.ID); r.ApprovedBy != "" || r.Merge != "" || merged != 0 {
		t.Fatalf("just opening approved or merged it: approved by %q, merge %q, merged %d", r.ApprovedBy, r.Merge, merged)
	}

	// Already open: it says so, with its link, and opens nothing more.
	code, body, got = open(probar, admin, nil)
	if opened, _ = fake.seen(); code != 200 || got.Number != 40 || !got.AlreadyOpen || got.URL != "https://github.com/p0dxD/soloabre/pull/40" || len(opened) != 1 {
		t.Fatalf("already open: %d %s (opened %q)", code, body, opened)
	}
	// A fork's pull request of a branch with the same name is not this one.
	fork := forja.Change{Number: 7, State: "open"}
	fork.Head.Ref, fork.Base.Ref, fork.Head.Repo.FullName = "de-otro", "main", "otra/soloabre"
	fake.mu.Lock()
	fake.open = append(fake.open, fork)
	fake.mu.Unlock()
	if code, body, got = open(run(gh, "de-otro", false), admin, nil); code != 200 || got.AlreadyOpen || got.Number != 41 {
		t.Fatalf("beside a fork's: %d %s", code, body)
	}

	// The default branch has nothing to open, in the forja's words and in Spanish.
	principal := run(gh, "main", true)
	if code, body, _ = open(principal, admin, nil); code != 409 || !strings.Contains(body, "no pull request to open") {
		t.Fatalf("the default branch: %d %s", code, body)
	}
	if code, body, _ = open(principal, admin, map[string]string{"Accept-Language": "es"}); code != 409 || !strings.Contains(body, "no tiene solicitud de incorporación que abrir") {
		t.Fatalf("the default branch, in Spanish: %d %s", code, body)
	}

	// GitLab: the merge request, at its address.
	glRun := run(gl, "probar", false)
	if code, body, got = open(glRun, admin, nil); code != 200 || got.URL != "https://git.example.com/grupo/soloabre/-/merge_requests/42" {
		t.Fatalf("on GitLab: %d %s", code, body)
	}
	if code, body, _ = open(run(gl, "main", true), admin, nil); code != 409 || !strings.Contains(body, "no merge request to open") {
		t.Fatalf("GitLab's default branch: %d %s", code, body)
	}

	// Where any open pull request is the yes, opening it would merge it:
	// refused, unless the run waits for a person anyway.
	if err := e.store.SetAppAutoMerge(ctx, gh.ID, true, true); err != nil {
		t.Fatal(err)
	}
	cualquiera := run(gh, "cualquiera", false)
	if code, body, _ = open(cualquiera, admin, nil); code != 409 || !strings.Contains(body, "is the yes to merge it") {
		t.Fatalf("in an app that merges any open pull request: %d %s", code, body)
	}
	if err := e.store.SetRunUntested(ctx, cualquiera.ID, []string{"mobile/"}); err != nil {
		t.Fatal(err)
	}
	if code, body, got = open(cualquiera, admin, nil); code != 200 || got.Number != 43 {
		t.Fatalf("a run that waits for a person anyway: %d %s", code, body)
	}
	if err := e.store.SetAppAutoMerge(ctx, gh.ID, true, false); err != nil {
		t.Fatal(err)
	}

	// The same people as merging: a guest may not.
	otra := run(gh, "otra", false)
	if code, body, _ = open(otra, guest, nil); code != 403 {
		t.Fatalf("a guest opened it: %d %s", code, body)
	}

	// Nor an agent: its key is not a person's session.
	res, kb := e.do(t, "POST", "/api/agent-keys", `{"name":"agente solo abrir","canWrite":true,"days":30}`, admin, nil)
	var key CreatedAgentKey
	if res.StatusCode != 201 || json.Unmarshal([]byte(kb), &key) != nil || key.Token == "" {
		t.Fatalf("an agent key: %d %s", res.StatusCode, kb)
	}
	if code, body, _ = open(otra, nil, map[string]string{"Authorization": "Bearer " + key.Token}); code != 401 {
		t.Fatalf("an agent key opened it: %d %s", code, body)
	}
	if opened, merged = fake.seen(); len(opened) != 4 || merged != 0 {
		t.Fatalf("opened %q (merged %d), want four and none merged", opened, merged)
	}
}
