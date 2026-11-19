package store

import (
	"context"
	"testing"
	"time"
)

// A run keeps what it checked en seco: the release, each check and its
// log, and whether one failed; the published release skips one a
// pre-deploy task blocked.
func TestRunDry(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	app := &App{Name: "en-seco", Repo: "o/en-seco"}
	if err := s.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishedRelease(ctx, app.ID); err != ErrNotFound {
		t.Fatalf("published release of an app without one: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := s.CreateRelease(ctx, &Release{AppID: app.ID, SHA: "a", Images: map[string]string{}}); err != nil {
			t.Fatal(err)
		}
	}
	second, _ := s.GetRelease(ctx, app.ID, 2)
	if err := s.SetVerification(ctx, second.ID, VerifyBlocked, "not deployed"); err != nil {
		t.Fatal(err)
	}
	if rel, err := s.PublishedRelease(ctx, app.ID); err != nil || rel.Number != 1 {
		t.Fatalf("published release %+v, %v; want #1", rel, err)
	}

	run := &Run{AppID: app.ID, SHA: "b", Branch: "humo-con-clima", Event: "branch"}
	if err := s.CreateRun(ctx, run, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetRun(ctx, run.ID); got.Dry != nil || got.DryFailed() {
		t.Fatalf("a run with nothing en seco: %+v", got.Dry)
	}
	now := time.Now()
	if err := s.SaveRunDryTask(ctx, run.ID, ReleaseTask{Name: "humo", Status: TaskFailed, Message: "exit 1", Log: "falta el clima\n", StartedAt: &now, FinishedAt: &now}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRunDry(ctx, run.ID, DryRun{Release: 1, Failed: true}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(ctx, run.ID)
	if err != nil || got.Dry == nil || got.Dry.Release != 1 || !got.DryFailed() || len(got.Dry.Tasks) != 1 || got.Dry.Tasks[0].Status != TaskFailed {
		t.Fatalf("run %+v, %v", got.Dry, err)
	}
	if log, err := s.RunDryTaskLog(ctx, run.ID, "humo"); err != nil || log != "falta el clima\n" {
		t.Fatalf("log %q, %v", log, err)
	}
	if last, _ := s.LastRunOf(ctx, app.ID, "humo-con-clima"); !last.DryFailed() {
		t.Fatal("a run read without its steps forgets that its dry run failed")
	}

	none := &Run{AppID: app.ID, SHA: "c", Branch: "sin-version", Event: "branch"}
	if err := s.CreateRun(ctx, none, nil); err != nil {
		t.Fatal(err)
	}
	_ = s.SetRunDry(ctx, none.ID, DryRun{Note: "no release is published yet"})
	if got, _ := s.GetRun(ctx, none.ID); got.Dry == nil || got.Dry.Note == "" || got.Dry.Release != 0 || got.DryFailed() {
		t.Fatalf("a run with a note: %+v", got.Dry)
	}
}
