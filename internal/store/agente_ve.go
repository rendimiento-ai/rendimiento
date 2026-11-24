package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// What the platform's coding agent reads about the app of the encargo it
// does (agentes.Coder's read-only tools), never another app's.

// SlowRequest is one slow request the platform's log (the Registro) noted:
// its route (a pattern, never a path), how long it took and its answer.
type SlowRequest struct {
	At     time.Time `json:"at"`
	Route  string    `json:"route"`
	Ms     int64     `json:"ms"`
	Status int       `json:"status"`
}

// SlowRequests are an app's slow requests in the Registro since a time,
// the newest first: its own lines, and with platform the platform's own
// (they carry no app: the app is rendimiento itself, SELF_APP).
func (s *Store) SlowRequests(ctx context.Context, app string, platform bool, since time.Time, limit int) ([]SlowRequest, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return collect(ctx, s, `SELECT at, attrs->>'route', COALESCE((attrs->>'ms')::bigint, 0), COALESCE((attrs->>'status')::int, 0)
		FROM registro WHERE at >= $3 AND message = 'slow request' AND attrs ? 'route' AND (app = $1 OR ($2 AND app = ''))
		ORDER BY at DESC, id DESC LIMIT $4`,
		[]any{app, platform, since, limit}, func(r pgx.CollectableRow) (SlowRequest, error) {
			var q SlowRequest
			err := r.Scan(&q.At, &q.Route, &q.Ms, &q.Status)
			return q, err
		})
}

// HallazgosDeApp are the lookout's findings about one app, the newest
// first.
func (s *Store) HallazgosDeApp(ctx context.Context, app string, limit int) ([]VigiaHallazgo, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return collect(ctx, s, `SELECT `+hallazgoCols+` FROM vigia_hallazgos WHERE app = $1 ORDER BY id DESC LIMIT $2`,
		[]any{app, limit}, func(row pgx.CollectableRow) (VigiaHallazgo, error) { return leerHallazgo(row) })
}
