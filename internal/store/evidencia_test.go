package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/tachar"
)

// A failed release's evidence is kept redacted and capped, read by app and
// release number, flagged on its release, and deleted after its days.
func TestReleaseEvidence(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	app := &App{Name: "evidencia", Repo: "p0dxD/evidencia", DefaultBranch: "main"}
	if err := s.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	var rels []*Release
	for range 2 {
		r := &Release{AppID: app.ID, SHA: "abc", Images: map[string]string{}}
		if err := s.CreateRelease(ctx, r); err != nil {
			t.Fatal(err)
		}
		rels = append(rels, r)
	}
	if _, err := s.ReleaseEvidence(ctx, app.ID, rels[1].Number); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before any: %v", err)
	}
	code := int32(1)
	ev := &ReleaseEvidence{CapturedAt: time.Now().UTC(), Where: "kubernetes", Services: []ServiceEvidence{{Name: "web", Pods: []PodEvidence{{Name: "web-1", Phase: "Running",
		Containers: []ContainerEvidence{{Name: "web", State: "waiting", Reason: "CrashLoopBackOff", LastExitCode: &code,
			Log: "starting\nDATABASE_URL=postgresql://app:Xk29fjQ02mzP@web-postgres:5432/web\nnul \x00 byte\npanic: boom"}}}}}},
		Events: []EvidenceEvent{{Type: "Warning", Reason: "BackOff", Object: "Pod/web-1", Message: "Back-off"}}}
	if err := s.SaveReleaseEvidence(ctx, rels[1].ID, ev); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReleaseEvidence(ctx, app.ID, rels[1].Number)
	if err != nil {
		t.Fatal(err)
	}
	log := got.Services[0].Pods[0].Containers[0].Log
	if strings.Contains(log, "Xk29fjQ02mzP") || !strings.Contains(log, tachar.Omitida) || !strings.Contains(log, "panic: boom") || strings.Contains(log, "\x00") ||
		len(got.Events) != 1 || *got.Services[0].Pods[0].Containers[0].LastExitCode != 1 {
		t.Fatalf("kept: %+v (log %q)", got, log)
	}
	with, err := s.ReleasesWithEvidence(ctx, []int64{rels[0].ID, rels[1].ID})
	if err != nil || with[rels[0].ID] || !with[rels[1].ID] {
		t.Fatalf("with evidence: %v %v", with, err)
	}
	// Kept again (a retried verification), it replaces the first.
	ev.Where = "local"
	if err := s.SaveReleaseEvidence(ctx, rels[1].ID, ev); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ReleaseEvidence(ctx, app.ID, rels[1].Number); got.Where != "local" {
		t.Fatalf("replaced: %+v", got)
	}
	if n, err := s.PruneReleaseEvidence(ctx, time.Now().Add(-time.Hour)); err != nil || n != 0 {
		t.Fatalf("pruned a recent one: %d %v", n, err)
	}
	if n, err := s.PruneReleaseEvidence(ctx, time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	if _, err := s.ReleaseEvidence(ctx, app.ID, rels[1].Number); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after pruning: %v", err)
	}
}

// Cap shortens the longest logs from their start, keeping their ends,
// until the whole fits; states stay.
func TestReleaseEvidenceCap(t *testing.T) {
	line := strings.Repeat("x", 99) + "\n"
	long := strings.Repeat(line, 2000) + "the error at the end"
	ev := &ReleaseEvidence{Where: "kubernetes", Services: []ServiceEvidence{{Name: "web", Pods: []PodEvidence{
		{Name: "web-1", Containers: []ContainerEvidence{{Name: "web", State: "waiting", Restarts: 3, Log: long, PreviousLog: long}}},
		{Name: "web-2", Containers: []ContainerEvidence{{Name: "web", State: "running", Log: "short"}}},
	}}}}
	for range 200 {
		ev.Events = append(ev.Events, EvidenceEvent{Type: "Warning", Reason: "BackOff", Object: "Pod/web-1", Message: strings.Repeat("m", 100)})
	}
	ev.Cap(MaxEvidence)
	b, _ := json.Marshal(ev)
	if len(b) > MaxEvidence || !ev.Truncated {
		t.Fatalf("%d bytes, truncated %v", len(b), ev.Truncated)
	}
	c := ev.Services[0].Pods[0].Containers[0]
	if !strings.HasSuffix(c.Log, "the error at the end") || !strings.HasPrefix(c.Log, "…\n") || c.Restarts != 3 || ev.Services[0].Pods[1].Containers[0].Log != "short" {
		t.Fatalf("log of %d bytes: %q…", len(c.Log), c.Log[:min(40, len(c.Log))])
	}
	if len(ev.Events) != 200 {
		t.Fatalf("events dropped while logs could be shortened: %d", len(ev.Events))
	}

	// One that fits is left as it is.
	small := &ReleaseEvidence{Services: []ServiceEvidence{{Name: "web", Pods: []PodEvidence{{Name: "web-1", Containers: []ContainerEvidence{{Log: "ok"}}}}}}}
	small.Cap(MaxEvidence)
	if small.Truncated || small.Services[0].Pods[0].Containers[0].Log != "ok" {
		t.Fatalf("a small one: %+v", small)
	}
}

// The summary agents read: states, the warnings and the not-ready
// containers' last lines; of a release whose containers were all ready
// (it failed its checks), every container's.
func TestReleaseEvidenceSummary(t *testing.T) {
	code := int32(137)
	ev := &ReleaseEvidence{Where: "kubernetes", Services: []ServiceEvidence{{Name: "api", ReplicaSet: "api-5f", Message: "ProgressDeadlineExceeded: timed out",
		Pods: []PodEvidence{{Name: "api-5f-1", Phase: "Running", Containers: []ContainerEvidence{
			{Name: "api", State: "terminated", Reason: "OOMKilled", ExitCode: &code, Log: "a\nb\nc\nd"},
			{Name: "sidecar", State: "running", Ready: true, Log: "sidecar is fine"}}}}}},
		Events: []EvidenceEvent{{Type: "Normal", Reason: "Pulled", Object: "Pod/api-5f-1"}, {Type: "Warning", Reason: "OOMKilling", Object: "Pod/api-5f-1", Message: "out of memory"}},
		Notes:  []string{"the events: forbidden"}}
	sum := ev.Summary(2)
	for _, want := range []string{"service api, ReplicaSet api-5f: ProgressDeadlineExceeded: timed out", "api: terminated OOMKilled (exit code 137)", "| c\n", "| d", "OOMKilling: out of memory", "Note: the events: forbidden"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary without %q:\n%s", want, sum)
		}
	}
	if strings.Contains(sum, "| b") || strings.Contains(sum, "Pulled") || strings.Contains(sum, "sidecar is fine") {
		t.Errorf("summary with too much:\n%s", sum)
	}
	ev.Services[0].Pods[0].Containers = ev.Services[0].Pods[0].Containers[1:]
	if sum := ev.Summary(2); !strings.Contains(sum, "| sidecar is fine") {
		t.Errorf("all ready, without their lines:\n%s", sum)
	}
}
