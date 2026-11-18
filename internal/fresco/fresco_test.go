package fresco

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// clock is a test's time, moved by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (k *clock) now() time.Time { k.mu.Lock(); defer k.mu.Unlock(); return k.t }
func (k *clock) add(d time.Duration) {
	k.mu.Lock()
	k.t = k.t.Add(d)
	k.mu.Unlock()
}

// maker answers what a test says, counts its makings and can hold one.
type maker struct {
	mu    sync.Mutex
	val   int
	err   error
	hold  chan struct{}
	calls atomic.Int32
}

func (m *maker) set(val int, err error, hold chan struct{}) {
	m.mu.Lock()
	m.val, m.err, m.hold = val, err, hold
	m.mu.Unlock()
}

func (m *maker) fetch(ctx context.Context) (int, error) {
	m.calls.Add(1)
	m.mu.Lock()
	v, err, hold := m.val, m.err, m.hold
	m.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return v, err
}

// waitIdle waits until no making of key is in flight.
func waitIdle[K comparable, V any](t *testing.T, c *Cache[K, V], key K) {
	t.Helper()
	for range 200 {
		c.mu.Lock()
		e := c.entries[key]
		busy := e != nil && e.making != nil
		c.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the making never ended")
}

func TestFreshIsReused(t *testing.T) {
	k := &clock{t: time.Unix(1_000_000, 0)}
	c := &Cache[string, int]{TTL: time.Minute, now: k.now}
	m := &maker{val: 1}
	ctx := context.Background()
	for i := range 3 {
		v, err := c.Get(ctx, "a", m.fetch)
		if err != nil || v != 1 {
			t.Fatalf("look %d: %d, %v", i, v, err)
		}
		k.add(10 * time.Second)
	}
	if n := m.calls.Load(); n != 1 {
		t.Fatalf("made %d times, want 1", n)
	}
	// Another key is its own answer.
	m.set(2, nil, nil)
	if v, _ := c.Get(ctx, "b", m.fetch); v != 2 {
		t.Fatalf("b = %d", v)
	}
}

// An old answer is given at once while the new one is made, which then
// replaces it.
func TestStaleIsAnsweredThenReplaced(t *testing.T) {
	k := &clock{t: time.Unix(1_000_000, 0)}
	c := &Cache[string, int]{TTL: time.Minute, now: k.now}
	m := &maker{val: 1}
	ctx := context.Background()
	if v, _ := c.Get(ctx, "a", m.fetch); v != 1 {
		t.Fatal("first")
	}
	k.add(2 * time.Minute)
	hold := make(chan struct{})
	m.set(2, nil, hold)
	start := time.Now()
	v, err := c.Get(ctx, "a", m.fetch)
	if err != nil || v != 1 || time.Since(start) > time.Second {
		t.Fatalf("old look: %d, %v after %s", v, err, time.Since(start))
	}
	// A second look meanwhile does not start another making.
	if v, _ := c.Get(ctx, "a", m.fetch); v != 1 {
		t.Fatalf("second old look: %d", v)
	}
	close(hold)
	waitIdle(t, c, "a")
	if n := m.calls.Load(); n != 2 {
		t.Fatalf("made %d times, want 2", n)
	}
	if v, _ := c.Get(ctx, "a", m.fetch); v != 2 {
		t.Fatalf("after the making: %d, want 2", v)
	}
}

// A failed making keeps the old answer; with none, the look gets the error.
func TestFailureKeepsOld(t *testing.T) {
	k := &clock{t: time.Unix(1_000_000, 0)}
	c := &Cache[string, int]{TTL: time.Minute, now: k.now}
	m := &maker{val: 1}
	ctx := context.Background()
	_, _ = c.Get(ctx, "a", m.fetch)
	k.add(2 * time.Minute)
	m.set(0, errors.New("Umami did not answer"), nil)
	if v, err := c.Get(ctx, "a", m.fetch); err != nil || v != 1 {
		t.Fatalf("old look: %d, %v", v, err)
	}
	waitIdle(t, c, "a")
	if v, err := c.Get(ctx, "a", m.fetch); err != nil || v != 1 {
		t.Fatalf("after a failure: %d, %v; want the old 1", v, err)
	}
	waitIdle(t, c, "a")
	if n := m.calls.Load(); n != 3 {
		t.Fatalf("made %d times, want 3 (each old look tries again)", n)
	}
	if _, err := c.Get(ctx, "new", m.fetch); err == nil || err.Error() != "Umami did not answer" {
		t.Fatalf("first look of a failing key: %v", err)
	}
	// Past Stale, the old answer is no longer given: the error is.
	c.Stale = 5 * time.Minute
	k.add(10 * time.Minute)
	if _, err := c.Get(ctx, "a", m.fetch); err == nil {
		t.Fatal("an answer past Stale was given")
	}
	// A making that panics is a failure, not a crash.
	if _, err := c.Get(ctx, "p", func(context.Context) (int, error) { panic("boom") }); err == nil {
		t.Fatal("a panic was an answer")
	}
}

// A first look waits only Wait; the making goes on and the next look has it.
func TestFirstWaitIsBounded(t *testing.T) {
	c := &Cache[string, int]{TTL: time.Minute, Wait: 50 * time.Millisecond}
	hold := make(chan struct{})
	m := &maker{val: 7, hold: hold}
	start := time.Now()
	_, err := c.Get(context.Background(), "a", m.fetch)
	if !errors.Is(err, ErrSlow) {
		t.Fatalf("err = %v, want ErrSlow", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("waited %s", d)
	}
	close(hold)
	waitIdle(t, c, "a")
	if v, err := c.Get(context.Background(), "a", m.fetch); err != nil || v != 7 {
		t.Fatalf("next look: %d, %v", v, err)
	}
	if n := m.calls.Load(); n != 1 {
		t.Fatalf("made %d times, want 1", n)
	}
}

// Many first looks of one key share one making; Parallel bounds makings.
func TestOneMakingAndParallel(t *testing.T) {
	c := &Cache[int, int]{TTL: time.Minute, Parallel: 2}
	var running, most atomic.Int32
	release := make(chan struct{})
	fetch := func(context.Context) (int, error) {
		n := running.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		<-release
		running.Add(-1)
		return 1, nil
	}
	var wg sync.WaitGroup
	var calls atomic.Int32
	for i := range 6 {
		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = c.Get(context.Background(), i, func(ctx context.Context) (int, error) {
					calls.Add(1)
					return fetch(ctx)
				})
			}()
		}
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := calls.Load(); n != 6 {
		t.Fatalf("made %d times, want one per key (6)", n)
	}
	if m := most.Load(); m > 2 {
		t.Fatalf("%d makings ran at once, want at most 2", m)
	}
}

// Refresh waits for a new answer; Forget makes the next look wait for one.
func TestRefreshAndForget(t *testing.T) {
	c := &Cache[string, int]{TTL: time.Hour}
	m := &maker{val: 1}
	ctx := context.Background()
	_, _ = c.Get(ctx, "a", m.fetch)
	m.set(2, nil, nil)
	if v, _ := c.Get(ctx, "a", m.fetch); v != 1 {
		t.Fatal("fresh answer not kept")
	}
	if v, err := c.Refresh(ctx, "a", m.fetch); err != nil || v != 2 {
		t.Fatalf("refresh: %d, %v", v, err)
	}
	m.set(3, nil, nil)
	c.Forget("a")
	if v, _ := c.Get(ctx, "a", m.fetch); v != 3 {
		t.Fatalf("after Forget: %d, want 3", v)
	}
	m.set(0, errors.New("down"), nil)
	if _, err := c.Refresh(ctx, "a", m.fetch); err == nil {
		t.Fatal("a failed refresh answered")
	}
	if v, _ := c.Get(ctx, "a", m.fetch); v != 3 {
		t.Fatalf("a failed refresh lost the answer: %d", v)
	}
}
