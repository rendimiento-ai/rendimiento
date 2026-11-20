package store

import (
	"context"
	"time"
)

// queueLock is the advisory lock key of the run queue's owner (any number
// no other code uses; "rend" in ASCII).
const queueLock = 0x72656e64

// LockQueue waits until this process owns the run queue: a Postgres
// advisory lock, held on a connection of its own until release is called
// or the process dies (Postgres then lets it go). Only the owner recovers
// interrupted runs and works the queue, so a second copy of the platform
// (an overlapping pod, a binary started by hand) cannot put back in the
// queue runs the owner is still doing. It returns ctx's error if ctx ends
// first.
func (s *Store) LockQueue(ctx context.Context, every time.Duration) (release func(), err error) {
	return s.lock(ctx, queueLock, every)
}

// agentsLock is the advisory lock key of the agents' owner ("agen").
const agentsLock = 0x6167656e

// LockAgents waits until this process is the one running the platform's
// agents (see LockQueue): during an update, the new pod's agents start once
// the old one's stop, so nothing is diagnosed twice.
func (s *Store) LockAgents(ctx context.Context, every time.Duration) (release func(), err error) {
	return s.lock(ctx, agentsLock, every)
}

// lock holds an advisory lock on a connection of its own (see LockQueue).
func (s *Store) lock(ctx context.Context, key int64, every time.Duration) (release func(), err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	for {
		var ok bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&ok); err != nil {
			conn.Release()
			return nil, err
		}
		if ok {
			return func() {
				_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, key)
				conn.Release()
			}, nil
		}
		select {
		case <-ctx.Done():
			conn.Release()
			return nil, ctx.Err()
		case <-time.After(every):
		}
	}
}

// LockToken waits until this process may refresh a forja connection's
// OAuth token (forja "gitlab" or "bitbucket", connection id): a Postgres
// advisory lock on a connection of its own, so two processes never refresh
// the same token at once (the forja replaces the refresh token on each
// use, and the second refresh would fail with the old one). Whoever gets
// it next reads the kept token again before refreshing. release lets it
// go; it returns ctx's error if ctx ends first.
func (s *Store) LockToken(ctx context.Context, forja string, id int64) (release func(), err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext('forja-token:' || $1), $2::int)`, forja, id); err != nil {
		conn.Release()
		return nil, err
	}
	return func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('forja-token:' || $1), $2::int)`, forja, id)
		conn.Release()
	}, nil
}
