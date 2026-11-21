package agentes

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

func cand(id, org int64, app string, n int) store.CoderCandidate {
	return store.CoderCandidate{Encargo: store.Encargo{ID: id, OrgID: org, App: app}, Concurrency: n}
}

func ids(l []store.Encargo) []int64 {
	out := []int64{}
	for _, e := range l {
		out = append(out, e.ID)
	}
	return out
}

// The scheduler: one at a time by default, in the queue's order; with more
// allowed, up to that many at once, never two of one app, and an encargo
// whose app is busy waits while one of another app goes first.
func TestPick(t *testing.T) {
	for _, c := range []struct {
		name    string
		cands   []store.CoderCandidate
		running map[int64]Slot
		want    []int64
	}{
		{"one at a time, the first in order", []store.CoderCandidate{cand(7, 1, "tienda", 1), cand(3, 1, "blog", 1)}, nil, []int64{7}},
		{"one at a time, busy", []store.CoderCandidate{cand(3, 1, "blog", 1)}, map[int64]Slot{7: {1, "tienda"}}, []int64{}},
		{"three at once, in order", []store.CoderCandidate{cand(1, 1, "a", 3), cand(2, 1, "b", 3), cand(3, 1, "c", 3), cand(4, 1, "d", 3)}, nil, []int64{1, 2, 3}},
		{"never two of one app", []store.CoderCandidate{cand(1, 1, "a", 3), cand(2, 1, "a", 3), cand(3, 1, "b", 3)}, nil, []int64{1, 3}},
		{"an app it does already waits", []store.CoderCandidate{cand(2, 1, "a", 2), cand(3, 1, "b", 2)}, map[int64]Slot{1: {1, "a"}}, []int64{3}},
		{"the same app in another organization is not busy", []store.CoderCandidate{cand(2, 2, "a", 1)}, map[int64]Slot{1: {1, "a"}}, []int64{2}},
		{"each organization its own count", []store.CoderCandidate{cand(1, 1, "a", 1), cand(2, 1, "b", 1), cand(3, 2, "c", 2), cand(4, 2, "d", 2)}, nil, []int64{1, 3, 4}},
		{"never more than the most", []store.CoderCandidate{cand(1, 1, "a", 9), cand(2, 1, "b", 9), cand(3, 1, "c", 9), cand(4, 1, "d", 9)}, nil, []int64{1, 2, 3}},
		{"zero is one", []store.CoderCandidate{cand(1, 1, "a", 0), cand(2, 1, "b", 0)}, nil, []int64{1}},
		{"not the one it does", []store.CoderCandidate{cand(1, 1, "a", 2)}, map[int64]Slot{1: {1, "a"}}, []int64{}},
		{"not one without an app", []store.CoderCandidate{cand(1, 1, "", 2), cand(2, 1, "b", 2)}, nil, []int64{2}},
	} {
		if got := ids(Pick(c.cands, c.running)); !slices.Equal(got, c.want) {
			t.Errorf("%s: picked %v, want %v", c.name, got, c.want)
		}
	}
}

// Encargos done at once share their key's cargo: taken once, handed over
// when the last one ends, with what became of each.
func TestCoderSharesTheCargo(t *testing.T) {
	st := &coderStore{}
	c := &Coder{Store: st, Lang: "es", Log: slog.Default()}
	ctx := context.Background()
	a, _ := c.takeCargo(ctx, 9, "Encargo 1")
	b, _ := c.takeCargo(ctx, 9, "Encargo 2")
	if a != b || st.cargos != 1 {
		t.Fatalf("cargos %d and %d, taken %d times", a, b, st.cargos)
	}
	_ = c.handOver(ctx, 9, "uno")
	if st.handover != "" {
		t.Fatalf("handed over with one still working: %q", st.handover)
	}
	_ = c.handOver(ctx, 9, "dos")
	if st.handover != "uno\n\ndos" {
		t.Fatalf("handover %q", st.handover)
	}
	if id, _ := c.takeCargo(ctx, 9, "Encargo 3"); id != 3 || st.cargos != 2 {
		t.Fatalf("after the handover: cargo %d, taken %d times", id, st.cargos)
	}
}

// waiter is a model that thinks until it is stopped.
type waiter struct{}

func (waiter) CompleteTools(ctx context.Context, _ int64, _ string, _ ia.ToolRequest) (ia.ToolResponse, error) {
	<-ctx.Done()
	return ia.ToolResponse{}, ctx.Err()
}

// A person stops the encargo it does: it lets go of it at once, gives it
// back to them saying so, and says what it was doing until then.
func TestCoderStops(t *testing.T) {
	st := &coderStore{stop: true}
	r := &repos{st: st, files: map[string][]byte{"saludo.go": []byte("const Saludo = \"Hi\"\n")}}
	c := &Coder{Store: st, Repos: r, Model: waiter{}, Lang: "es", Log: slog.Default(), Poll: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Do(ctx, store.Encargo{ID: 12, OrgID: 1, App: "tienda"}); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("it did not stop")
	}
	if !strings.Contains(st.gaveBack, "Una persona lo detuvo") || !strings.Contains(st.handover, "lo devolvió") {
		t.Fatalf("gave back %q, handover %q", st.gaveBack, st.handover)
	}
	if len(st.doing) == 0 || st.doing[0] != "think:" {
		t.Fatalf("doing %q", st.doing)
	}
}
