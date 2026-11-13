package pruebas

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	log := "Running 3 tests using 1 worker\n  ✓ home › opens (1.2s)\n" +
		"@@prueba\texpected\tinicio.spec.ts › home › opens\n" +
		"@@prueba\tunexpected\tpagar.spec.ts › pays a tip\r\n" +
		"@@prueba\tflaky\tpagar.spec.ts › refunds\n" +
		"@@prueba\texpected\tinicio.spec.ts › home › opens\n"
	got, ok := Parse(log)
	if !ok || len(got) != 3 || got[1] != (Result{Name: "pagar.spec.ts › pays a tip", Status: Failed}) {
		t.Fatalf("Parse = %+v %v", got, ok)
	}
	if got, ok := Parse("Error: no tests found\n@@pruebas-sin-informe\n"); ok || got != nil {
		t.Fatalf("no report = %+v %v", got, ok)
	}
}

// A test reports until it passes N releases in a row, then blocks; a
// blocking failure fails the release; flaky runs set it aside.
func TestJudge(t *testing.T) {
	states := map[string]State{}
	step := func(status string) (failures, warnings []string) {
		t.Helper()
		next, f, w := Judge(states, []Result{{Name: "pays", Status: status}}, 3)
		for _, s := range next {
			states[s.Name] = s
		}
		return f, w
	}
	if f, w := step(Failed); len(f) != 0 || len(w) != 1 || states["pays"].State != Advisory {
		t.Fatalf("a new test failing: %v %v %+v", f, w, states["pays"])
	}
	step(Passed)
	step(Passed)
	if _, w := step(Passed); states["pays"].State != Blocking || !strings.Contains(w[0], "now blocks") {
		t.Fatalf("after 3 clean releases: %+v %v", states["pays"], w)
	}
	if f, _ := step(Failed); len(f) != 1 || states["pays"].Streak != 0 {
		t.Fatalf("a blocking failure: %v %+v", f, states["pays"])
	}
	step(Flaky)
	step(Flaky)
	if f, w := step(Flaky); len(f) != 0 || states["pays"].State != Quarantined || !strings.Contains(w[0], "set aside") {
		t.Fatalf("third flake: %v %v %+v", f, w, states["pays"])
	}
	if f, w := step(Failed); len(f) != 0 || len(w) != 0 {
		t.Fatalf("a quarantined test failing counts for nothing: %v %v", f, w)
	}
	step(Passed)
	step(Passed)
	if step(Passed); states["pays"].State != Advisory || states["pays"].Flakes != 0 {
		t.Fatalf("a quarantined test that passes again: %+v", states["pays"])
	}
	if next, _, _ := Judge(states, []Result{{Name: "pays", Status: Skipped}}, 3); len(next) != 0 {
		t.Fatalf("a skipped test changes nothing: %+v", next)
	}
}
