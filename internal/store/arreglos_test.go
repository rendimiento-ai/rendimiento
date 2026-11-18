package store

import (
	"context"
	"errors"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// Fix runs are kept with who asked, their files and log; Renovate's branch
// gets a fix once a commit; and a commit a fix made is known as one.
func TestFixRuns(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	sp := spec.Spec{Services: []spec.Service{{Name: "web"}}}
	sp.Default()
	app := &App{Name: "candados", Repo: "o/candados", DefaultBranch: "main", Spec: sp}
	if err := s.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	r := &Run{AppID: app.ID, SHA: "aaa1111", Branch: "renovate/react", Event: "push"}
	if err := s.CreateRun(ctx, r, nil); err != nil {
		t.Fatal(err)
	}
	f, err := s.StartFixRun(ctx, FixRun{AppID: app.ID, Branch: "renovate/react", SHA: "aaa1111", Fix: "candado", AskedKind: FixByRenovate,
		AskedBy: "renovate", RunID: &r.ID, Image: "node:22-bookworm-slim", ImageFrom: "default"})
	if err != nil || f.ID == 0 || f.Status != FixRunning {
		t.Fatalf("start: %+v %v", f, err)
	}
	if _, err := s.StartFixRun(ctx, FixRun{AppID: app.ID, Branch: "renovate/react", SHA: "aaa1111", Fix: "candado", AskedKind: FixByRenovate}); !errors.Is(err, ErrFixDone) {
		t.Fatalf("a second Renovate fix of the same commit: %v", err)
	}
	// A person may ask again on the same commit.
	if _, err := s.StartFixRun(ctx, FixRun{AppID: app.ID, Branch: "renovate/react", SHA: "aaa1111", Fix: "candado", AskedKind: FixByPerson, AskedBy: "ana"}); err != nil {
		t.Fatalf("a person's: %v", err)
	}
	f.Status, f.Files, f.CommitSHA = FixSucceeded, []string{"web/package-lock.json"}, "bbb2222"
	if err := s.FinishFixRun(ctx, f, "added 3 packages\n"); err != nil {
		t.Fatal(err)
	}
	got, err := s.FixRunByID(ctx, app.ID, f.ID)
	if err != nil || got.Status != FixSucceeded || len(got.Files) != 1 || got.FinishedAt == nil || *got.RunID != r.ID || got.Image != "node:22-bookworm-slim" {
		t.Fatalf("read: %+v %v", got, err)
	}
	if log, err := s.FixRunLog(ctx, app.ID, f.ID); err != nil || log != "added 3 packages\n" {
		t.Fatalf("log %q %v", log, err)
	}
	if list, err := s.FixRuns(ctx, app.ID, r.ID, 10); err != nil || len(list) != 1 || list[0].ID != f.ID {
		t.Fatalf("of the run: %+v %v", list, err)
	}
	if list, _ := s.FixRuns(ctx, app.ID, 0, 10); len(list) != 2 {
		t.Fatalf("of the app: %d", len(list))
	}
	if yes, err := s.MadeByFix(ctx, app.ID, "bbb2222"); err != nil || !yes {
		t.Fatalf("made by a fix: %v %v", yes, err)
	}
	if yes, _ := s.MadeByFix(ctx, app.ID, "aaa1111"); yes {
		t.Fatal("the fixed commit is not one a fix made")
	}
}
