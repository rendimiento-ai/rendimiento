package platform

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The home page's counts: made once and kept, all apps at once; an old
// count is still the answer while a new one is made behind it, so a look
// never waits for GitHub again; one that is slow the first time is left
// out (it comes on the next look), and a failure keeps the old count.
func TestMesaWaiting(t *testing.T) {
	p := &Platform{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	var mu sync.Mutex
	counts := map[int64]int{1: 2, 2: 0, 3: 5}
	slow := map[int64]chan struct{}{}
	fail := map[int64]bool{}
	var made atomic.Int32
	p.mesaCounts.build = func(_ context.Context, a *store.App) (int, error) {
		made.Add(1)
		mu.Lock()
		hold, f, n := slow[a.ID], fail[a.ID], counts[a.ID]
		mu.Unlock()
		if hold != nil {
			<-hold
		}
		if f {
			return 0, errors.New("GitHub did not answer")
		}
		return n, nil
	}
	apps := []*store.App{{ID: 1, Name: "una"}, {ID: 2, Name: "dos"}, {ID: 3, Name: "tres"}}
	ctx := context.Background()

	got := p.MesaWaiting(ctx, apps)
	if got[1] != 2 || got[2] != 0 || got[3] != 5 || len(got) != 3 {
		t.Fatalf("first look: %v", got)
	}
	if made.Load() != 3 {
		t.Fatalf("made %d boards, want 3", made.Load())
	}
	// Fresh: kept, nothing made again.
	if got := p.MesaWaiting(ctx, apps); got[3] != 5 || made.Load() != 3 {
		t.Fatalf("second look: %v, made %d", got, made.Load())
	}

	// Old: the kept count answers at once, even while the new one is slow.
	p.mesaCounts.mu.Lock()
	for id, e := range p.mesaCounts.byApp {
		e.at = time.Now().Add(-2 * mesaCountTTL)
		p.mesaCounts.byApp[id] = e
	}
	p.mesaCounts.mu.Unlock()
	hold := make(chan struct{})
	mu.Lock()
	counts[3], slow[3], fail[1] = 7, hold, true
	mu.Unlock()
	start := time.Now()
	got = p.MesaWaiting(ctx, apps)
	if time.Since(start) > time.Second {
		t.Fatalf("an old count waited %s for the new one", time.Since(start))
	}
	if got[1] != 2 || got[3] != 5 {
		t.Fatalf("old counts: %v, want the kept ones", got)
	}
	close(hold)
	deadline := time.Now().Add(10 * time.Second)
	for {
		p.mesaCounts.mu.Lock()
		n, busy := p.mesaCounts.byApp[3].n, len(p.mesaCounts.making)
		p.mesaCounts.mu.Unlock()
		if n == 7 && busy == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the new count never came: %d, %d making", n, busy)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := p.MesaWaiting(ctx, apps); got[3] != 7 || got[1] != 2 {
		t.Fatalf("after the new count: %v (a failure keeps the old one)", got)
	}

	// Never made and slow: left out after mesaCountWait, not waited for.
	hold4 := make(chan struct{})
	defer close(hold4)
	mu.Lock()
	slow[4] = hold4
	mu.Unlock()
	start = time.Now()
	got = p.MesaWaiting(ctx, append(apps, &store.App{ID: 4, Name: "cuatro"}))
	if took := time.Since(start); took > mesaCountWait+2*time.Second {
		t.Fatalf("waited %s for a slow board", took)
	}
	if _, ok := got[4]; ok || got[3] != 7 {
		t.Fatalf("a slow first count: %v, want it left out", got)
	}
}
