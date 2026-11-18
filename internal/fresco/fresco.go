// Package fresco keeps slow answers (another service's numbers, a look at
// the cluster) so a page does not wait for them each time: a new answer is
// given as is; an old one is given at once while a new one is made behind
// it; and an answer never made yet is waited for, but only so long. A
// making that fails keeps the old answer, and the next look tries again.
package fresco

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrSlow is what a first look gets when its answer takes longer than
// Wait: it is still being made, and the next look will likely have it.
var ErrSlow = errors.New("still being read; try again in a moment")

// Cache keeps one answer per key. The zero value is not ready: set TTL.
type Cache[K comparable, V any] struct {
	// TTL is how long an answer is new; an older one is still given, and
	// a new one is made behind it.
	TTL time.Duration
	// Stale is how old an answer may be and still be given (0: any age).
	// Past it, a look waits for a new one as if there were none.
	Stale time.Duration
	// Wait is how long a look waits for an answer it does not have
	// (0: until it is made or the look gives up).
	Wait time.Duration
	// Build bounds one making (0: one minute). A making goes on after the
	// look that started it leaves, so the next look finds it done.
	Build time.Duration
	// Parallel is how many makings run at once (0: 4).
	Parallel int
	// Keep drops answers nobody asked for in this long (0: 30 minutes).
	Keep time.Duration

	mu      sync.Mutex
	entries map[K]*entry[V]
	slots   chan struct{}
	pruned  time.Time
	now     func() time.Time // tests
}

type entry[V any] struct {
	val    V
	has    bool
	at     time.Time // when val was made
	used   time.Time // last looked for
	making *call
}

// call is one making in flight; done closes when it ends.
type call struct {
	done chan struct{}
	err  error
}

func (c *Cache[K, V]) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// Get is key's answer: made with fetch when it has none, or when its
// answer is old (then the old one is the answer, and the new one is for
// the next look). Without an answer it waits up to Wait, else ErrSlow.
func (c *Cache[K, V]) Get(ctx context.Context, key K, fetch func(context.Context) (V, error)) (V, error) {
	return c.get(ctx, key, fetch, false)
}

// Refresh waits for a new answer (joining a making in flight), for a
// person who asked to look again. It waits as long as ctx lets it.
func (c *Cache[K, V]) Refresh(ctx context.Context, key K, fetch func(context.Context) (V, error)) (V, error) {
	return c.get(ctx, key, fetch, true)
}

func (c *Cache[K, V]) get(ctx context.Context, key K, fetch func(context.Context) (V, error), refresh bool) (V, error) {
	now := c.clock()
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[K]*entry[V]{}
		n := c.Parallel
		if n <= 0 {
			n = 4
		}
		c.slots = make(chan struct{}, n)
	}
	c.prune(now)
	e := c.entries[key]
	if e == nil {
		e = &entry[V]{}
		c.entries[key] = e
	}
	e.used = now
	usable := e.has && (c.Stale <= 0 || now.Sub(e.at) < c.Stale)
	fresh := usable && now.Sub(e.at) < c.TTL
	if fresh && !refresh {
		v := e.val
		c.mu.Unlock()
		return v, nil
	}
	cl := e.making
	if cl == nil {
		cl = &call{done: make(chan struct{})}
		e.making = cl
		go c.make(context.WithoutCancel(ctx), e, cl, fetch)
	}
	if usable && !refresh {
		v := e.val
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()

	var timeout <-chan time.Time
	if c.Wait > 0 && !refresh {
		t := time.NewTimer(c.Wait)
		defer t.Stop()
		timeout = t.C
	}
	var zero V
	select {
	case <-cl.done:
	case <-timeout:
		return zero, ErrSlow
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cl.err != nil && (refresh || !e.has || (c.Stale > 0 && c.clock().Sub(e.at) >= c.Stale)) {
		return zero, cl.err
	}
	return e.val, nil
}

// make runs one making in a slot and keeps what it made; a failure keeps
// the old answer.
func (c *Cache[K, V]) make(ctx context.Context, e *entry[V], cl *call, fetch func(context.Context) (V, error)) {
	var v V
	var err error
	defer func() {
		c.mu.Lock()
		if err == nil {
			e.val, e.has, e.at = v, true, c.clock()
		}
		cl.err = err
		e.making = nil
		c.mu.Unlock()
		close(cl.done)
	}()
	c.slots <- struct{}{}
	defer func() { <-c.slots }()
	build := c.Build
	if build <= 0 {
		build = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, build)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	v, err = fetch(ctx)
}

// Forget drops key's answer (every key's, with no key): the next look
// waits for a new one. A making in flight is left to end on its own; what
// it makes is not kept for later looks.
func (c *Cache[K, V]) Forget(keys ...K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(keys) == 0 {
		clear(c.entries)
		return
	}
	for _, k := range keys {
		delete(c.entries, k)
	}
}

// prune drops answers nobody looked for in Keep, at most once a minute.
func (c *Cache[K, V]) prune(now time.Time) {
	if now.Sub(c.pruned) < time.Minute {
		return
	}
	c.pruned = now
	keep := c.Keep
	if keep <= 0 {
		keep = 30 * time.Minute
	}
	for k, e := range c.entries {
		if e.making == nil && now.Sub(e.used) >= keep {
			delete(c.entries, k)
		}
	}
}
