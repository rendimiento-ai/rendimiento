package platform

import (
	"context"
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rendimiento-ai/rendimiento/internal/arreglos"
	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

type fixRepos struct {
	*fakeGitHub
	commits []string // the branches committed to
}

func (r *fixRepos) CommitFiles(_ context.Context, _ int64, _, branch, _ string, files map[string][]byte, _ []string) (string, error) {
	r.commits = append(r.commits, branch)
	return strings.Repeat("f", 39) + string(rune('0'+len(r.commits))), nil
}

type fixExec struct{ n int }

func (e *fixExec) Fix(_ context.Context, _ pipeline.Source, job pipeline.FixJob, w io.Writer) (pipeline.FixOutput, error) {
	e.n++
	return pipeline.FixOutput{Changes: []pipeline.FixChange{{Path: job.Path + "/package-lock.json", Data: []byte(`{"v":3}`)}}}, nil
}

// A Renovate branch's run that failed on a stale lockfile gets the recipe
// that covers its folder, by itself, committed to the branch; once a
// commit, never on a commit a fix made, never a fix of the app's own, and
// never on a person's branch.
func TestRenovateFixOncePerCommit(t *testing.T) {
	p, fake, _, _ := setup(t)
	ctx := context.Background()
	yaml := "services: [{name: web, path: web}]\nfixes:\n  - {name: candado, recipe: npm, path: web}\n  - {name: propio, path: api, image: node:22, command: 'npm i', writes: [package-lock.json]}\n"
	fake.files["main"] = fstest.MapFS{"rendimiento.yaml": {Data: []byte(yaml)}}
	sp, err := spec.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	app := &store.App{Name: "candados-renovate", Repo: "p0dxD/candados-renovate", DefaultBranch: "main", Spec: *sp}
	if err := p.Store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	repos := &fixRepos{fakeGitHub: fake}
	exec := &fixExec{}
	p.Fixes = &arreglos.Service{Exec: exec, Repos: repos, Store: p.Store}
	bot := forja.Change{Number: 5}
	bot.Head.Ref, bot.Head.Repo.FullName, bot.User.Type = "renovate/react", app.Repo, "Bot"
	fake.mu.Lock()
	fake.pulls = append(fake.pulls, bot)
	fake.mu.Unlock()

	steps := []pipeline.Step{{ID: "web:build", Service: "web", Kind: pipeline.KindBuild, Path: "web"}, {ID: "api:build", Service: "api", Kind: pipeline.KindBuild, Path: "api"}}
	failed := map[string]pipeline.StepResult{"web:build": {Status: pipeline.StatusFailed}, "api:build": {Status: pipeline.StatusFailed}}
	mkRun := func(branch, sha string) *store.Run {
		r := &store.Run{AppID: app.ID, SHA: sha, Branch: branch, Event: "push"}
		if err := p.Store.CreateRun(ctx, r, steps); err != nil {
			t.Fatal(err)
		}
		for _, st := range steps {
			if err := p.Store.AppendLog(ctx, r.ID, st.ID, "#9 4.1 npm error `npm ci` can only install packages when your package.json and package-lock.json or npm-shrinkwrap.json are in sync.\n"); err != nil {
				t.Fatal(err)
			}
		}
		return r
	}
	run := mkRun("renovate/react", strings.Repeat("a", 40))
	p.fixRenovate(ctx, app, run, steps, failed)
	if exec.n != 1 || len(repos.commits) != 1 || repos.commits[0] != "renovate/react" {
		t.Fatalf("ran %d, committed %v (only the recipe, in web)", exec.n, repos.commits)
	}
	// The same commit again (a retried run): not a second time.
	p.fixRenovate(ctx, app, run, steps, failed)
	if exec.n != 1 {
		t.Fatalf("the same commit was fixed %d times", exec.n)
	}
	// The commit the fix made fails again: no fix of it, no loop.
	again := mkRun("renovate/react", strings.Repeat("f", 39)+"1")
	p.fixRenovate(ctx, app, again, steps, failed)
	if exec.n != 1 {
		t.Fatalf("a fix's own commit was fixed")
	}
	// A person's branch is never fixed by itself.
	mine := mkRun("feature/react", strings.Repeat("b", 40))
	p.fixRenovate(ctx, app, mine, steps, failed)
	if exec.n != 1 {
		t.Fatal("a person's branch was fixed by itself")
	}
	list, err := p.Store.FixRuns(ctx, app.ID, run.ID, 10)
	if err != nil || len(list) != 1 || list[0].AskedKind != store.FixByRenovate || list[0].Status != store.FixSucceeded || list[0].CommitSHA == "" {
		t.Fatalf("history %+v %v", list, err)
	}
}
