package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNotFailed is a retry asked of a run that did not fail.
var ErrNotFailed = errors.New("only a run that failed can be retried")

// ErrRetriesSpent is a retry asked of a run already retried as often as
// it may be.
var ErrRetriesSpent = errors.New("this run was already retried as often as it may be")

// RetryRun queues a failed run again to run what did not pass: its steps
// that failed or were skipped go back to pending (a failed one counts one
// more try, and its log keeps the earlier attempt under a line saying so);
// those that passed keep their result and their image. At most max retries.
func (s *Store) RetryRun(ctx context.Context, runID int64, max int) (*Run, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		var retries int
		err := tx.QueryRow(ctx, `SELECT status, retries FROM runs WHERE id = $1 FOR UPDATE`, runID).Scan(&status, &retries)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNotFound
		case err != nil:
			return err
		case status != RunFailed:
			return ErrNotFailed
		case retries >= max:
			return ErrRetriesSpent
		}
		mark := fmt.Sprintf("\n=== retry %d: running again what did not pass ===\n", retries+1)
		if _, err := tx.Exec(ctx, `UPDATE steps SET
				tries = CASE WHEN status = 'failed' THEN tries + 1 ELSE tries END,
				log = CASE WHEN log = '' THEN log ELSE log || $2 END,
				status = 'pending', digest = '', message = '', started_at = NULL, finished_at = NULL
			WHERE run_id = $1 AND status NOT IN ('succeeded', 'reused')`, runID, mark); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE runs SET status = 'queued', retries = retries + 1, message = 'retrying what failed',
			started_at = NULL, finished_at = NULL WHERE id = $1`, runID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetRun(ctx, runID)
}

// StumbleWindow is how far back stumbles count toward calling a step flaky.
const StumbleWindow = 30 * 24 * time.Hour

// RecordStumble notes that a step of an app failed and then passed on the
// same commit, and says how many times that happened in StumbleWindow.
func (s *Store) RecordStumble(ctx context.Context, appID int64, stepID string, runID int64) (int, error) {
	if _, err := s.pool.Exec(ctx, `INSERT INTO step_stumbles (app_id, step_id, run_id) VALUES ($1, $2, $3)`, appID, stepID, runID); err != nil {
		return 0, err
	}
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM step_stumbles WHERE app_id = $1 AND step_id = $2 AND at > $3`,
		appID, stepID, time.Now().Add(-StumbleWindow)).Scan(&n)
	return n, err
}

// Stumbles are an app's steps that stumbled in StumbleWindow, with how often.
func (s *Store) Stumbles(ctx context.Context, appID int64) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT step_id, count(*) FROM step_stumbles WHERE app_id = $1 AND at > $2 GROUP BY step_id`,
		appID, time.Now().Add(-StumbleWindow))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var step string
		var n int
		if err := rows.Scan(&step, &n); err != nil {
			return nil, err
		}
		out[step] = n
	}
	return out, rows.Err()
}
