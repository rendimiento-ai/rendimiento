package libreta

import (
	"testing"
	"time"
)

func at(h int) time.Time {
	return time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).Add(time.Duration(h) * time.Hour)
}
func ptr(t time.Time) *time.Time { return &t }

// byKey finds a line by where it comes from and its number.
func byKey(t *testing.T, l Libreta, source string, id int64) Item {
	t.Helper()
	for _, it := range l.Items {
		if it.Source == source && it.ID == id {
			return it
		}
	}
	t.Fatalf("no %s %d in %+v", source, id, l.Items)
	return Item{}
}

func TestStates(t *testing.T) {
	in := Input{
		DefaultBranch: "main",
		GitHub:        true,
		Entries: []Entry{
			{ID: 1, Kind: "pendiente", Title: "asked, nothing yet", CreatedAt: at(1)},
			{ID: 2, Kind: "pendiente", Title: "a branch named, no run", Branches: []string{"nueva"}, CreatedAt: at(2)},
			{ID: 3, Kind: "pendiente", Title: "running", Branches: []string{"corre"}, CreatedAt: at(3)},
			{ID: 4, Kind: "pendiente", Title: "passed", Branches: []string{"paso"}, CreatedAt: at(4)},
			{ID: 5, Kind: "pendiente", Title: "merged, release verifying", Branches: []string{"verifica"}, CreatedAt: at(5)},
			{ID: 6, Kind: "pendiente", Title: "published", Branches: []string{"vieja", "publicada"}, CreatedAt: at(6)},
			{ID: 7, Kind: "pendiente", Title: "ticked", Done: true, CreatedAt: at(7), UpdatedAt: at(8)},
			{ID: 8, Kind: "pendiente", Title: "set aside", Discarded: "Maven has no lockfile", Branches: []string{"paso2"}, CreatedAt: at(8), UpdatedAt: at(9)},
			{ID: 9, Kind: "decision", Title: "a decision", CreatedAt: at(9)},
			{ID: 10, Kind: "pendiente", Title: "blocked", Branches: []string{"bloqueada"}, CreatedAt: at(10)},
			{ID: 11, Kind: "pendiente", Title: "closed, still asked", Branches: []string{"cerrada"}, CreatedAt: at(11)},
			{ID: 12, Kind: "pendiente", Title: "merged by rendimiento, no pull request known", Branches: []string{"fusionada"}, CreatedAt: at(12)},
		},
		Encargos: []Encargo{
			{ID: 20, Title: "pending", Status: "pending", CreatedAt: at(1)},
			{ID: 21, Title: "taken, its branch not pushed yet", Status: "taken", Agent: "claude", Branches: []string{"feature/encargo-21-x"}, CreatedAt: at(2)},
			{ID: 22, Title: "delivered", Status: "delivered", CreatedAt: at(3)},
			{ID: 23, Title: "cancelled", Status: "cancelled", CancelNote: "not needed", CreatedAt: at(4), UpdatedAt: at(5)},
			{ID: 24, Title: "failing on its branch", Status: "taken", Agent: "claude", Branches: []string{"fix/encargo-24"}, CreatedAt: at(5)},
			{ID: 25, Title: "reviewed", Status: "reviewed", CreatedAt: at(6), UpdatedAt: at(7)},
			{ID: 26, Title: "merged through its pull request link", Status: "delivered", Pulls: []int{40}, CreatedAt: at(7)},
		},
		Pulls: []Pull{
			{Number: 30, Branch: "paso", Open: true, CreatedAt: at(4)},
			{Number: 31, Branch: "verifica", MergedAt: ptr(at(20)), CreatedAt: at(5)},
			{Number: 32, Branch: "publicada", MergedAt: ptr(at(10)), CreatedAt: at(6)},
			{Number: 33, Branch: "cerrada", ClosedAt: ptr(at(12)), CreatedAt: at(11)},
			{Number: 34, Branch: "renovate/maplibre-gl-6.x", Title: "maplibre-gl 6", Deps: true, MergedAt: ptr(at(10)), CreatedAt: at(2)},
			{Number: 35, Branch: "renovate/react-20.x", Title: "React 20", Deps: true, Open: true, CreatedAt: at(3)},
			{Number: 36, Branch: "renovate/old", Title: "old thing", Deps: true, ClosedAt: ptr(at(9)), CreatedAt: at(1)},
			{Number: 40, Branch: "otra", MergedAt: ptr(at(10)), CreatedAt: at(7)},
		},
		Runs: []Run{
			{ID: 100, Branch: "corre", Status: "running", CreatedAt: at(3)},
			{ID: 101, Branch: "paso", Status: "succeeded", CreatedAt: at(4)},
			{ID: 102, Branch: "fix/encargo-24", Status: "failed", Failed: []string{"web:test"}, CreatedAt: at(6)},
			{ID: 103, Branch: "bloqueada", Status: "succeeded", Merge: "blocked", CreatedAt: at(10)},
			{ID: 104, Branch: "paso2", Status: "succeeded", CreatedAt: at(8)},
			{ID: 105, Branch: "fusionada", Status: "succeeded", Merge: "merged", CreatedAt: at(30)},
			{ID: 106, Branch: "renovate/react-20.x", Status: "succeeded", CreatedAt: at(3)},
			// The default branch: a run before the merges, one after (its
			// release failed), one later (passed), one after the last merge.
			{ID: 200, Branch: "main", Status: "succeeded", CreatedAt: at(9)},
			{ID: 201, Branch: "main", Status: "succeeded", CreatedAt: at(11)},
			{ID: 202, Branch: "main", Status: "succeeded", CreatedAt: at(12)},
			{ID: 203, Branch: "main", Status: "succeeded", CreatedAt: at(21)},
		},
		Releases: []Release{
			{Number: 70, RunID: 200, Verify: "passed"},
			{Number: 71, RunID: 201, Verify: "failed"},
			{Number: 72, RunID: 202, Verify: "passed"},
			{Number: 73, RunID: 203, Verify: "verifying"},
		},
		Tests: &Tests{BlockAfter: 5, Advisory: map[string]int{"vuelve": 1, "paga": 3}},
	}
	l := Build(in)

	for _, tc := range []struct {
		source  string
		id      int64
		status  string
		why     string
		release int64
	}{
		{FromRuta, 1, Pedido, "", 0},
		{FromRuta, 2, EnCurso, WhyBranch, 0},
		{FromRuta, 3, EnCurso, WhyRunning, 0},
		{FromRuta, 4, Listo, WhyPassed, 0},
		{FromRuta, 5, Integrado, WhyVerifying, 73},
		{FromRuta, 6, Publicado, "", 72}, // 71 failed its verification; 72 holds it too
		{FromRuta, 7, Hecho, WhyMarked, 0},
		{FromRuta, 8, Descartado, "", 0},
		{FromRuta, 9, Hecho, WhyDecided, 0},
		{FromRuta, 10, Esperando, WhyBlocked, 0},
		{FromRuta, 11, Pedido, WhyClosed, 0},
		{FromRuta, 12, Integrado, WhyMerged, 0},
		{FromEncargo, 20, Pedido, "", 0},
		{FromEncargo, 21, EnCurso, WhyTaken, 0},
		{FromEncargo, 22, Esperando, WhyReview, 0},
		{FromEncargo, 23, Descartado, "", 0},
		{FromEncargo, 24, EnCurso, WhyFailed, 0},
		{FromEncargo, 25, Hecho, WhyAccepted, 0},
		{FromEncargo, 26, Publicado, "", 72},
		{FromRenovate, 34, Publicado, "", 72},
		{FromRenovate, 35, Listo, WhyPassed, 0},
		{FromRenovate, 36, Descartado, WhyClosed, 0},
	} {
		it := byKey(t, l, tc.source, tc.id)
		if it.Status != tc.status || it.Why != tc.why || it.Release != tc.release {
			t.Errorf("%s %d (%s) = %s/%s #%d, want %s/%s #%d", tc.source, tc.id, it.Title, it.Status, it.Why, it.Release, tc.status, tc.why, tc.release)
		}
	}
	if it := byKey(t, l, FromRuta, 8); it.Reason != "Maven has no lockfile" || it.DoneAt == nil {
		t.Errorf("set aside: %+v", it)
	}
	if it := byKey(t, l, FromEncargo, 23); it.Reason != "not needed" {
		t.Errorf("cancelled encargo's reason: %+v", it)
	}
	if it := byKey(t, l, FromEncargo, 24); it.Agent != "claude" || it.Run != 102 || len(it.Failed) != 1 {
		t.Errorf("failing encargo: %+v", it)
	}
	if it := byKey(t, l, FromRuta, 6); it.Branch != "publicada" || it.Pull != 32 || it.DoneAt == nil || !it.DoneAt.Equal(at(10)) {
		t.Errorf("the published branch speaks for the entry: %+v", it)
	}

	// The tests wait for clean releases: the least advanced one counts.
	var tests *Item
	for i := range l.Items {
		if l.Items[i].Source == FromPruebas {
			tests = &l.Items[i]
		}
	}
	if tests == nil || tests.Status != Esperando || tests.Progress == nil || *tests.Progress != (Progress{Done: 1, Of: 5}) || len(tests.Tests) != 2 {
		t.Errorf("tests: %+v", tests)
	}

	// Open lines first, newest first; then the finished, latest first.
	seenDone := false
	for _, it := range l.Items {
		if !Open(it.Status) {
			seenDone = true
		} else if seenDone {
			t.Fatalf("an open line after a finished one: %+v", l.Items)
		}
	}
	if l.Items[0].Source != FromRuta || l.Items[0].ID != 11 {
		t.Errorf("first line: %+v", l.Items[0])
	}
}

// A Renovate pull request an encargo already speaks for is not repeated.
func TestRenovateClaimed(t *testing.T) {
	l := Build(Input{DefaultBranch: "main", GitHub: true,
		Encargos: []Encargo{{ID: 1, Title: "fix the renovate branch", Status: "taken", Branches: []string{"renovate/x"}}},
		Pulls:    []Pull{{Number: 9, Branch: "renovate/x", Deps: true, Open: true}},
	})
	if len(l.Items) != 1 || l.Items[0].Source != FromEncargo || l.Items[0].Pull != 9 {
		t.Fatalf("items: %+v", l.Items)
	}
}

// An encargo's branch by name counts only once it is seen (a run or a pull
// request); a branch named by hand on a Ruta entry counts at once.
func TestUnseenBranch(t *testing.T) {
	l := Build(Input{DefaultBranch: "main",
		Encargos: []Encargo{{ID: 1, Title: "x", Status: "pending", Branches: []string{"feature/encargo-1-x"}}},
	})
	if it := l.Items[0]; it.Status != Pedido || it.Branch != "" {
		t.Fatalf("unseen branch: %+v", it)
	}
}

// What was kept of a branch (its merge, its release) speaks for it when
// its runs and its pull request are gone, and before what they say now.
func TestKept(t *testing.T) {
	in := Input{
		DefaultBranch: "main",
		Entries: []Entry{
			{ID: 1, Kind: "pendiente", Title: "shipped long ago", Branches: []string{"antigua"},
				Kept: map[string]Kept{"antigua": {MergedAt: ptr(at(1)), Release: 4}}, CreatedAt: at(0)},
			{ID: 2, Kind: "pendiente", Title: "merged long ago, released now", Branches: []string{"fusion"},
				Kept: map[string]Kept{"fusion": {MergedAt: ptr(at(2))}}, CreatedAt: at(0)},
			{ID: 3, Kind: "pendiente", Title: "kept merge wins over a later failed run", Branches: []string{"reusada"},
				Kept: map[string]Kept{"reusada": {MergedAt: ptr(at(3))}}, CreatedAt: at(0)},
			{ID: 4, Kind: "decision", Title: "a decision, kept shipped", Branches: []string{"decidida"},
				Kept: map[string]Kept{"decidida": {MergedAt: ptr(at(1)), Release: 5}}, CreatedAt: at(0)},
		},
		Runs: []Run{
			{ID: 10, Branch: "main", Status: "succeeded", Release: 9, CreatedAt: at(4)},
			{ID: 11, Branch: "reusada", Status: "failed", Failed: []string{"test"}, CreatedAt: at(5)},
		},
		Releases: []Release{{Number: 9, RunID: 10, Verify: "passed"}},
	}
	l := Build(in)
	if it := byKey(t, l, FromRuta, 1); it.Status != Publicado || it.Release != 4 || it.DoneAt == nil || !it.DoneAt.Equal(at(1)) {
		t.Errorf("kept release: %+v", it)
	}
	if it := byKey(t, l, FromRuta, 2); it.Status != Publicado || it.Release != 9 {
		t.Errorf("kept merge, release from the runs: %+v", it)
	}
	if it := byKey(t, l, FromRuta, 3); it.Status != Publicado || it.Run != 11 {
		t.Errorf("kept merge wins: %+v", it)
	}
	if it := byKey(t, l, FromRuta, 4); it.Status != Publicado || it.Release != 5 {
		t.Errorf("a decision with a kept branch: %+v", it)
	}
}
