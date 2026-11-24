package catalog

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/fresco"
)

// kubeLento is a cluster with nothing in it that can be made slow to
// answer: while it is, every list waits for the gate to open. It counts
// the looks at the cluster (one list of Apps each, its first).
type kubeLento struct {
	client.Client
	mu     sync.Mutex
	gate   chan struct{} // nil: answers at once
	builds atomic.Int32
}

func (k *kubeLento) slow() {
	k.mu.Lock()
	k.gate = make(chan struct{})
	k.mu.Unlock()
}

func (k *kubeLento) answer() {
	k.mu.Lock()
	if k.gate != nil {
		close(k.gate)
		k.gate = nil
	}
	k.mu.Unlock()
}

func (k *kubeLento) List(ctx context.Context, l client.ObjectList, _ ...client.ListOption) error {
	if _, ok := l.(*v1alpha1.AppList); ok {
		k.builds.Add(1)
	}
	k.mu.Lock()
	gate := k.gate
	k.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// eventually waits for cond, up to a few seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
	}
}

// Looks at the catalog while a new one is slow to build do not wait for
// it, nor queue up behind it: they all get the one there is, and only one
// new one is built. The look after it is built has the new one.
func TestBuilderServesTheOldCatalogWhileItBuildsANewOne(t *testing.T) {
	k := &kubeLento{}
	b := &Builder{Kube: k, TTL: 50 * time.Millisecond, Wait: time.Minute}
	ctx := context.Background()
	old, err := b.Get(ctx, false)
	if err != nil || old == nil || k.builds.Load() != 1 {
		t.Fatalf("first catalog: %v, %d builds", err, k.builds.Load())
	}

	k.slow()
	time.Sleep(60 * time.Millisecond) // older than TTL
	const looks = 20
	got := make(chan *Catalog, looks)
	for range looks {
		go func() {
			c, err := b.Get(ctx, false)
			if err != nil {
				t.Error(err)
			}
			got <- c
		}()
	}
	for range looks {
		select {
		case c := <-got:
			if c != old {
				t.Fatal("a look did not get the catalog there was")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a look waited for the new catalog")
		}
	}
	eventually(t, "no new catalog was started", func() bool { return k.builds.Load() >= 2 })
	time.Sleep(100 * time.Millisecond)
	if n := k.builds.Load(); n != 2 {
		t.Fatalf("%d looks started %d builds, want 1", looks, n-1)
	}

	k.answer()
	eventually(t, "the new catalog never came", func() bool {
		c, err := b.Get(ctx, false)
		return err == nil && c != old
	})
}

// A first catalog is waited for only Wait: then the look is told it is
// still being read, and the next look, once it is built, has it. Looks in
// the meantime join the one build.
func TestBuilderWaitsOnlySoLongForTheFirstCatalog(t *testing.T) {
	k := &kubeLento{}
	k.slow()
	b := &Builder{Kube: k, Wait: 100 * time.Millisecond}
	ctx := context.Background()
	for range 3 {
		start := time.Now()
		if _, err := b.Get(ctx, false); !errors.Is(err, fresco.ErrSlow) || time.Since(start) > 3*time.Second {
			t.Fatalf("slow first catalog: %v after %s", err, time.Since(start))
		}
	}
	if n := k.builds.Load(); n != 1 {
		t.Fatalf("%d builds, want 1", n)
	}
	k.answer()
	eventually(t, "the first catalog never came", func() bool {
		c, err := b.Get(ctx, false)
		return err == nil && c != nil
	})
}

// A build that never ends gives up after Build, and the next look tries again.
func TestBuilderGivesUpOnABuild(t *testing.T) {
	k := &kubeLento{}
	k.slow()
	b := &Builder{Kube: k, Wait: time.Minute, Build: 100 * time.Millisecond}
	start := time.Now()
	if _, err := b.Get(context.Background(), false); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("a build that never ends: %v after %s", err, time.Since(start))
	}
	k.answer()
	if c, err := b.Get(context.Background(), false); err != nil || c == nil || k.builds.Load() != 2 {
		t.Fatalf("after it gave up: %v, %d builds", err, k.builds.Load())
	}
}
