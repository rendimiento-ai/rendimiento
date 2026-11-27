package platform

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
)

// The run's page learns from the step event when a step started and
// finished, under the names the API's steps use, so a finished step's bar
// stops without a reload; a time not known yet is left out.
func TestStepEventCarriesTheStoredTimes(t *testing.T) {
	start := time.Date(2026, 11, 16, 10, 0, 0, 0, time.FixedZone("x", -6*3600))
	end := start.Add(84 * time.Second)
	decode := func(m map[string]any) map[string]any {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	done := decode(stepEvent("web:build", pipeline.StepResult{Status: pipeline.StatusSucceeded, Digest: "sha256:1"}, &start, &end))
	if done["id"] != "web:build" || done["status"] != "succeeded" || done["digest"] != "sha256:1" {
		t.Fatalf("event = %v", done)
	}
	if done["startedAt"] != "2026-11-16T16:00:00Z" || done["finishedAt"] != "2026-11-16T16:01:24Z" {
		t.Fatalf("times = %v, %v", done["startedAt"], done["finishedAt"])
	}

	running := decode(stepEvent("web:test", pipeline.StepResult{Status: pipeline.StatusRunning}, &start, nil))
	if running["startedAt"] != "2026-11-16T16:00:00Z" {
		t.Fatalf("running startedAt = %v", running["startedAt"])
	}
	if _, ok := running["finishedAt"]; ok {
		t.Fatalf("a running step has no finishedAt: %v", running)
	}

	// Where a step ran reaches the page with it.
	lugar := decode(stepEvent("web:test", pipeline.StepResult{Status: pipeline.StatusSucceeded, Lugares: []pipeline.Lugar{{Nodo: "node-a", Arch: "arm64"}}}, &start, &end))
	if l, ok := lugar["lugares"].([]any); !ok || len(l) != 1 || l[0].(map[string]any)["nodo"] != "node-a" {
		t.Fatalf("lugares = %v", lugar["lugares"])
	}
	if _, ok := running["lugares"]; ok {
		t.Fatalf("a step not placed yet says no place: %v", running)
	}

	skipped := decode(stepEvent("deploy", pipeline.StepResult{Status: pipeline.StatusSkipped}, nil, nil))
	if _, ok := skipped["startedAt"]; ok {
		t.Fatalf("unknown times are left out: %v", skipped)
	}
}
