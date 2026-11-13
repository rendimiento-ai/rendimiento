package store

import (
	"context"
	"time"
)

// AppDay is one app's numbers of a day, for the summary's scoreboard.
type AppDay struct {
	App       string `json:"app"`
	Releases  int    `json:"releases"`
	Healthy   int    `json:"healthy"` // releases whose verification passed
	Rollbacks int    `json:"rollbacks"`
	Night     int    `json:"night"` // releases made from 21:00 to 6:00
	Runs      int    `json:"runs"`  // finished: passed or failed
	Failed    int    `json:"failed"`
	Merges    int    `json:"merges"` // pull requests rendimiento merged
	Problems  int    `json:"problems"`
	// Hours are the runs that passed and failed, by the hour they started
	// (0 to 23, in the day's time zone).
	Hours [24][2]int `json:"hours"`
}

// DayBoard is each app's numbers between from and to, in the time zone of
// from: the scoreboard of the day's summary. Apps with nothing that day are
// left out.
func (s *Store) DayBoard(ctx context.Context, orgID int64, from, to time.Time) ([]AppDay, error) {
	tz := from.Location().String()
	byApp := map[string]*AppDay{}
	get := func(app string) *AppDay {
		if byApp[app] == nil {
			byApp[app] = &AppDay{App: app}
		}
		return byApp[app]
	}
	rows, err := s.pool.Query(ctx, `SELECT a.name, r.rollback_of IS NOT NULL, r.verify_status = $5,
		  extract(hour FROM r.created_at AT TIME ZONE $4)::int
		FROM releases r JOIN apps a ON a.id = r.app_id
		WHERE a.org_id = $1 AND r.created_at >= $2 AND r.created_at < $3`, orgID, from, to, tz, VerifyPassed)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var app string
		var back, healthy bool
		var hour int
		if err := rows.Scan(&app, &back, &healthy, &hour); err != nil {
			rows.Close()
			return nil, err
		}
		d := get(app)
		if back {
			d.Rollbacks++
			continue
		}
		d.Releases++
		if healthy {
			d.Healthy++
		}
		if hour >= 21 || hour < 6 {
			d.Night++
		}
	}
	rows.Close()
	rows, err = s.pool.Query(ctx, `SELECT a.name, extract(hour FROM r.created_at AT TIME ZONE $4)::int, r.status = 'failed',
		  r.merge_state IN ('merged', 'resolved'), count(*)
		FROM runs r JOIN apps a ON a.id = r.app_id
		WHERE a.org_id = $1 AND r.created_at >= $2 AND r.created_at < $3 AND r.status IN ('succeeded', 'failed')
		GROUP BY 1, 2, 3, 4`, orgID, from, to, tz)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var app string
		var hour, n int
		var failed, merged bool
		if err := rows.Scan(&app, &hour, &failed, &merged, &n); err != nil {
			rows.Close()
			return nil, err
		}
		d := get(app)
		d.Runs += n
		if failed {
			d.Failed += n
			d.Hours[hour%24][1] += n
		} else {
			d.Hours[hour%24][0] += n
		}
		if merged {
			d.Merges += n
		}
	}
	rows.Close()
	rows, err = s.pool.Query(ctx, `SELECT p.app, count(*) FROM problems p JOIN apps a ON a.name = p.app
		WHERE a.org_id = $1 AND p.first_at >= $2 AND p.first_at < $3 GROUP BY 1`, orgID, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var app string
		var n int
		if err := rows.Scan(&app, &n); err != nil {
			rows.Close()
			return nil, err
		}
		get(app).Problems += n
	}
	rows.Close()
	out := make([]AppDay, 0, len(byApp))
	for _, d := range byApp {
		out = append(out, *d)
	}
	return out, rows.Err()
}

// EncargosToReview counts an organization's encargos delivered and
// waiting for a person.
func (s *Store) EncargosToReview(ctx context.Context, orgID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM encargos WHERE org_id = $1 AND status = $2`, orgID, EncargoDelivered).Scan(&n)
	return n, err
}
