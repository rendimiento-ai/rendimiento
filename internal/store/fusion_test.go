package store

import (
	"context"
	"testing"
)

// Every state the platform records for a run's pull request is one the
// database accepts (the check once lacked "resolved"), and a merged run
// stays merged.
func TestSetRunMergeAcceptsEveryState(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	app := &App{Name: "fusiones", Repo: "o/fusiones"}
	if err := s.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	run := &Run{AppID: app.ID, SHA: "a", Branch: "rama", Event: "branch"}
	if err := s.CreateRun(ctx, run, nil); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{MergeConflict, MergeResolved, MergeBlocked, MergeMerged} {
		if err := s.SetRunMerge(ctx, run.ID, 7, state, "pull request #7: "+state); err != nil {
			t.Fatalf("%s: %v", state, err)
		}
		got, err := s.GetRun(ctx, run.ID)
		if err != nil || got.Merge != state || got.Pull != 7 {
			t.Fatalf("%s: kept %+v, %v", state, got, err)
		}
	}
	_ = s.SetRunMerge(ctx, run.ID, 7, MergeConflict, "later")
	if got, _ := s.GetRun(ctx, run.ID); got.Merge != MergeMerged {
		t.Fatalf("a merged run changed to %q", got.Merge)
	}
}
