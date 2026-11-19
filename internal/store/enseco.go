package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ---- en seco: a branch's checks, against the published release ----

// dryStage is the stage of a check run en seco (spec.StageDryRun).
const dryStage = "en-seco"

// PublishedRelease is the app's release that is live: the newest one that
// was deployed (a release a pre-deploy task blocked never was), or
// ErrNotFound.
func (s *Store) PublishedRelease(ctx context.Context, appID int64) (*Release, error) {
	return scanRelease(s.pool.QueryRow(ctx, `SELECT `+releaseCols+` FROM releases
		WHERE app_id = $1 AND verify_status <> $2 ORDER BY number DESC LIMIT 1`, appID, VerifyBlocked))
}

// SetRunDry records what the run checks en seco: the release they run
// against (0 with a note when none ran), and whether one failed.
func (s *Store) SetRunDry(ctx context.Context, runID int64, d DryRun) error {
	var release *int64
	if d.Release != 0 {
		release = &d.Release
	}
	_, err := s.pool.Exec(ctx, `UPDATE runs SET dry_release = $2, dry_note = $3, dry_failed = $4 WHERE id = $1`, runID, release, d.Note, d.Failed)
	return err
}

// SaveRunDryTask records a check's state en seco (and the tail of its log).
func (s *Store) SaveRunDryTask(ctx context.Context, runID int64, t ReleaseTask) error {
	log := t.Log
	if len(log) > maxTaskLog {
		log = log[len(log)-maxTaskLog:]
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO run_dry_tasks (run_id, name, status, message, log, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (run_id, name) DO UPDATE SET status = $3, message = $4, log = $5, started_at = $6, finished_at = $7`,
		runID, t.Name, t.Status, t.Message, log, t.StartedAt, t.FinishedAt)
	return err
}

// RunDryTasks are the run's checks en seco, without their logs, in the
// order they started.
func (s *Store) RunDryTasks(ctx context.Context, runID int64) ([]ReleaseTask, error) {
	rows, err := s.pool.Query(ctx, `SELECT name, status, message, started_at, finished_at
		FROM run_dry_tasks WHERE run_id = $1 ORDER BY started_at NULLS LAST, name`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReleaseTask
	for rows.Next() {
		t := ReleaseTask{Stage: dryStage}
		if err := rows.Scan(&t.Name, &t.Status, &t.Message, &t.StartedAt, &t.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RunDryTaskLog is the log of one of the run's checks en seco.
func (s *Store) RunDryTaskLog(ctx context.Context, runID int64, name string) (string, error) {
	var log string
	err := s.pool.QueryRow(ctx, `SELECT log FROM run_dry_tasks WHERE run_id = $1 AND name = $2`, runID, name).Scan(&log)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return log, err
}
