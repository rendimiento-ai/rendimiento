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

// A run keeps what it checked en seco against a copy of the branch: which
// copy, its address until it is deleted, each check and its log; a check
// that failed there keeps the run from being merged by itself, also when
// nothing ran against the published release.
func TestRunDryCopy(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	app := &App{Name: "en-seco-copia", Repo: "o/en-seco-copia"}
	if err := s.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	run := &Run{AppID: app.ID, SHA: "e", Branch: "renovate/react-19.x", Event: "branch"}
	if err := s.CreateRun(ctx, run, nil); err != nil {
		t.Fatal(err)
	}
	dc := DryCopy{Kind: DryCopyPull, Pull: 31, URL: "https://reservas-pr-31.example.com"}
	if err := s.SetRunDryCopy(ctx, run.ID, dc, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetRun(ctx, run.ID); got.Dry == nil || got.Dry.Copy == nil || got.Dry.Copy.Pull != 31 || got.Dry.Release != 0 || got.DryFailed() {
		t.Fatalf("a run checked against its pull request's copy: %+v", got.Dry)
	}
	now := time.Now()
	if err := s.SaveRunDryCopyTask(ctx, run.ID, ReleaseTask{Name: "pruebas", Status: TaskFailed, Message: "1 of 3 tests failed", Log: "reserva una cita\n", StartedAt: &now, FinishedAt: &now}); err != nil {
		t.Fatal(err)
	}
	dc.Failed = true
	if err := s.SetRunDryCopy(ctx, run.ID, dc, true); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(ctx, run.ID)
	if err != nil || !got.DryFailed() || !got.Dry.Copy.Failed || len(got.Dry.Copy.Tasks) != 1 || got.Dry.Copy.Tasks[0].Stage != dryStage || len(got.Dry.Tasks) != 0 {
		t.Fatalf("run %+v %+v, %v", got.Dry, got.Dry.Copy, err)
	}
	if log, err := s.RunDryCopyTaskLog(ctx, run.ID, "pruebas"); err != nil || log != "reserva una cita\n" {
		t.Fatalf("log %q, %v", log, err)
	}
	if _, err := s.RunDryCopyTaskLog(ctx, run.ID, "humo"); err != ErrNotFound {
		t.Fatalf("the log of a check that did not run: %v", err)
	}
	if last, _ := s.LastRunOf(ctx, app.ID, "renovate/react-19.x"); !last.DryFailed() {
		t.Fatal("a run read without its steps forgets that a check failed against the copy")
	}
	// The pull request closed: its copy is gone, and the page stops linking to it.
	if err := s.DryCopyGone(ctx, app.ID, 31); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetRun(ctx, run.ID); !got.Dry.Copy.Gone {
		t.Fatalf("the copy after its pull request closed: %+v", got.Dry.Copy)
	}
}
