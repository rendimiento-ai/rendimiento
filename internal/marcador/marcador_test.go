package marcador

import (
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// November 12: seven healthy releases of rendimiento-ai, a tough
// afternoon of tests, four problems; stockpulse and jobsentry at night.
func TestBuild(t *testing.T) {
	ra := store.AppDay{App: "rendimiento-ai", Releases: 7, Healthy: 7, Runs: 36, Failed: 8, Merges: 12, Problems: 4}
	ra.Hours[17] = [2]int{4, 2}
	ra.Hours[18] = [2]int{6, 2}
	sp := store.AppDay{App: "stockpulse", Releases: 1, Healthy: 1, Night: 1, Runs: 6}
	sp.Hours[21] = [2]int{5, 0}
	js := store.AppDay{App: "jobsentry", Releases: 1, Healthy: 1, Night: 1, Runs: 1}

	b := Build([]store.AppDay{js, sp, ra}, 7)
	if b.Releases != 9 || b.Healthy != 9 || b.Runs != 43 || b.Failed != 8 || b.Merges != 12 || b.Problems != 4 || b.ToReview != 7 {
		t.Fatalf("totals %+v", b)
	}
	if b.Hours[18] != [2]int{6, 2} || b.Hours[21] != [2]int{5, 0} {
		t.Fatalf("hours %v", b.Hours)
	}
	if b.Apps[0].App != "rendimiento-ai" || b.Apps[2].App != "jobsentry" {
		t.Fatalf("apps in order %v", b.Apps)
	}
	if b.Card == nil || b.Card.Kind != CardNube || b.Card.Count != 4 || b.Card.App != "rendimiento-ai" {
		t.Fatalf("card %+v, want the cloud of 4 problems", b.Card)
	}

	// Without the problems: a day of many failures is a stumble.
	ra.Problems = 0
	if c := Build([]store.AppDay{ra, sp}, 0).Card; c == nil || c.Kind != CardTropiezo || c.Count != 8 || c.Of != 42 {
		t.Fatalf("card %+v, want the stumble 8/42", c)
	}
	// Few failures, all healthy: the flower.
	ra.Failed = 1
	if c := Build([]store.AppDay{ra, sp}, 0).Card; c == nil || c.Kind != CardFlor || c.Count != 8 {
		t.Fatalf("card %+v, want the flower of 8", c)
	}
	// Only work to review: the hummingbird. A quiet day: no card.
	if c := Build(nil, 3).Card; c == nil || c.Kind != CardColibri || c.Count != 3 {
		t.Fatalf("card %+v, want the hummingbird", c)
	}
	if c := Build(nil, 0).Card; c != nil {
		t.Fatalf("a quiet day has a card: %+v", c)
	}
}
