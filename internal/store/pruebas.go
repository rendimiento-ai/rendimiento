package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/rendimiento-ai/rendimiento/internal/pruebas"
)

// AppTests is what is kept of each of an app's tests, by name.
func (s *Store) AppTests(ctx context.Context, appID int64) (map[string]pruebas.State, error) {
	rows, err := s.pool.Query(ctx, `SELECT name, state, streak, flakes, last FROM app_tests WHERE app_id = $1`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]pruebas.State{}
	for rows.Next() {
		var t pruebas.State
		if err := rows.Scan(&t.Name, &t.State, &t.Streak, &t.Flakes, &t.Last); err != nil {
			return nil, err
		}
		out[t.Name] = t
	}
	return out, rows.Err()
}

// SaveTests keeps a release's test results and each test's new state, in
// one transaction.
func (s *Store) SaveTests(ctx context.Context, appID, releaseID int64, results []pruebas.Result, states []pruebas.State) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		stateOf := map[string]string{}
		for _, t := range states {
			stateOf[t.Name] = t.State
			if _, err := tx.Exec(ctx, `INSERT INTO app_tests (app_id, name, state, streak, flakes, last) VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (app_id, name) DO UPDATE SET state = $3, streak = $4, flakes = $5, last = $6, updated_at = now()`,
				appID, t.Name, t.State, t.Streak, t.Flakes, t.Last); err != nil {
				return err
			}
		}
		for _, r := range results {
			state := stateOf[r.Name]
			if state == "" {
				state = pruebas.Advisory
			}
			if _, err := tx.Exec(ctx, `INSERT INTO release_tests (release_id, name, status, state) VALUES ($1, $2, $3, $4)
				ON CONFLICT (release_id, name) DO UPDATE SET status = $3, state = $4`, releaseID, r.Name, r.Status, state); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReleaseTest is one test's result in a release, with its state then.
type ReleaseTest struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	State  string `json:"state"`
}

// ReleaseTests are each release's test results, failures first.
func (s *Store) ReleaseTests(ctx context.Context, releaseIDs []int64) (map[int64][]ReleaseTest, error) {
	rows, err := s.pool.Query(ctx, `SELECT release_id, name, status, state FROM release_tests WHERE release_id = ANY($1)
		ORDER BY release_id, status = 'expected', status = 'skipped', name`, releaseIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]ReleaseTest{}
	for rows.Next() {
		var id int64
		var t ReleaseTest
		if err := rows.Scan(&id, &t.Name, &t.Status, &t.State); err != nil {
			return nil, err
		}
		out[id] = append(out[id], t)
	}
	return out, rows.Err()
}
