package mesa

import (
	"testing"
	"time"
)

func TestBuild(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	mergedAgo := func(d time.Duration) *time.Time { t := ago(d); return &t }

	pulls := []Pull{ // the most recently updated first
		{Number: 212, Branch: "resumen-completo", SHA: "a1", MergedAt: mergedAgo(2 * time.Hour)},
		{Number: 209, Branch: "choques-solos", SHA: "b1", Open: true},
		{Number: 204, Branch: "encargo-28-copia", SHA: "c1", Open: true},
		{Number: 202, Branch: "encargo-25", SHA: "c1", Open: true},
		{Number: 190, Branch: "viejo", SHA: "d1", MergedAt: mergedAgo(72 * time.Hour)},
		{Number: 188, Branch: "cerrada", SHA: "e1"},
		{Number: 187, Branch: "sin-correr", SHA: "f1", Open: true},
	}
	runs := []Run{
		{ID: 1291, Branch: "choques-solos", Status: "succeeded", At: ago(time.Hour)},
		{ID: 1290, Branch: "encargo-32-comentarios", Status: "running", At: ago(10 * time.Minute)},
		{ID: 1289, Branch: "frontera-espera", Status: "succeeded", Merge: "conflict", At: ago(3 * time.Hour)},
		{ID: 1280, Branch: "rota", Status: "failed", Failed: []string{"api:test"}, At: ago(5 * time.Hour)},
		{ID: 1100, Branch: "olvidada", Status: "failed", At: ago(10 * 24 * time.Hour)},
		{ID: 1200, Branch: "main", Status: "succeeded", At: ago(time.Hour)},
		{ID: 1270, Branch: "choques-solos", Status: "failed", At: ago(4 * time.Hour)},
		{ID: 1288, Branch: "arreglada", Status: "failed", At: ago(2 * time.Hour)},
	}
	encargos := []Encargo{
		{ID: 30, Title: "Choques", Status: "delivered", Links: []string{"https://github.com/o/r/pull/209"}, At: ago(30 * time.Minute)},
		{ID: 32, Title: "Comentarios", Status: "taken", Agent: "codigo", Links: []string{"https://github.com/o/r/pull/new/encargo-32-comentarios"}},
		{ID: 33, Title: "Arreglar 1288", Status: "taken", Agent: "local", RunID: 1288},
		{ID: 36, Title: "Mesa", Status: "delivered", At: ago(time.Minute)},
		{ID: 37, Title: "Sin tomar", Status: "pending"},
	}
	b := Build("main", pulls, true, runs, encargos, now)

	want := map[string][2]string{
		"choques-solos":          {GroupYou, WhyPassed},
		"frontera-espera":        {GroupYou, WhyConflict},
		"rota":                   {GroupYou, WhyFailed},
		"sin-correr":             {GroupYou, WhyUntested},
		"encargo-32-comentarios": {GroupWorking, WhyRunning},
		"arreglada":              {GroupWorking, WhyTaken},
		"encargo-28-copia":       {GroupSpare, WhyCopy},
		"olvidada":               {GroupSpare, WhyStale},
		"resumen-completo":       {GroupToday, WhyMerged},
		"encargo-25":             {GroupYou, WhyUntested},
	}
	got := map[string]Line{}
	loose := 0
	for _, l := range b.Lines {
		if l.Branch == "" {
			loose++
			if l.Group != GroupYou || l.Why != WhyReview || l.Encargos[0].ID != 36 {
				t.Errorf("loose encargo line %+v", l)
			}
			continue
		}
		got[l.Branch] = l
	}
	if loose != 1 {
		t.Errorf("%d loose encargo lines, want 1 (pending ones stay off)", loose)
	}
	for br, w := range want {
		l, ok := got[br]
		if !ok {
			t.Errorf("%s is missing", br)
			continue
		}
		if l.Group != w[0] || l.Why != w[1] {
			t.Errorf("%s: %s/%s, want %s/%s", br, l.Group, l.Why, w[0], w[1])
		}
	}
	for _, gone := range []string{"main", "viejo", "cerrada"} {
		if _, ok := got[gone]; ok {
			t.Errorf("%s should not be on the board", gone)
		}
	}
	if l := got["choques-solos"]; l.Run != 1291 || l.Pull != 209 || len(l.Encargos) != 1 || l.Encargos[0].ID != 30 {
		t.Errorf("choques-solos: %+v", l)
	}
	if l := got["encargo-28-copia"]; l.CopyOf != 202 {
		t.Errorf("the copy points at %d, want 202", l.CopyOf)
	}
	if l := got["arreglada"]; l.Agent != "local" {
		t.Errorf("arreglada's agent is %q", l.Agent)
	}
	if n := b.You(); n != 6 {
		t.Errorf("You() = %d, want 6", n)
	}
	// The groups come in order: you, working, spare, today.
	rank := map[string]int{GroupYou: 0, GroupWorking: 1, GroupSpare: 2, GroupToday: 3}
	for i := 1; i < len(b.Lines); i++ {
		if rank[b.Lines[i-1].Group] > rank[b.Lines[i].Group] {
			t.Fatalf("out of order at %d: %s after %s", i, b.Lines[i].Group, b.Lines[i-1].Group)
		}
	}
}

func TestBuildEmpty(t *testing.T) {
	b := Build("main", nil, false, nil, nil, time.Now())
	if b.Lines == nil || len(b.Lines) != 0 || b.GitHub {
		t.Fatalf("%+v", b)
	}
}
