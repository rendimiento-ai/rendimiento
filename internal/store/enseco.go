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

// ---- en seco against a copy of the branch (step 2) ----

// SetRunDryCopy records what the run checks en seco against a copy of the
// branch: which copy, where it answers, why none ran, whether a check
// failed there. runs.dry_failed says a check failed against either, so
// the run is not merged by itself (it is all MergeReady reads).
func (s *Store) SetRunDryCopy(ctx context.Context, runID int64, c DryCopy, failed bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // after Commit it does nothing
	if _, err := tx.Exec(ctx, `INSERT INTO run_dry_copies (run_id, kind, pull, url, note, failed, gone)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (run_id) DO UPDATE SET kind = $2, pull = $3, url = $4, note = $5, failed = $6, gone = $7`,
		runID, c.Kind, c.Pull, c.URL, c.Note, c.Failed, c.Gone); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE runs SET dry_failed = $2 WHERE id = $1`, runID, failed); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RunDryCopy is what the run checked en seco against a copy of the
// branch, with its checks (without their logs), or ErrNotFound.
func (s *Store) RunDryCopy(ctx context.Context, runID int64) (*DryCopy, error) {
	var c DryCopy
	err := s.pool.QueryRow(ctx, `SELECT kind, pull, url, note, failed, gone FROM run_dry_copies WHERE run_id = $1`, runID).
		Scan(&c.Kind, &c.Pull, &c.URL, &c.Note, &c.Failed, &c.Gone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT name, status, message, started_at, finished_at
		FROM run_dry_copy_tasks WHERE run_id = $1 ORDER BY started_at NULLS LAST, name`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		t := ReleaseTask{Stage: dryStage}
		if err := rows.Scan(&t.Name, &t.Status, &t.Message, &t.StartedAt, &t.FinishedAt); err != nil {
			return nil, err
		}
		c.Tasks = append(c.Tasks, t)
	}
	return &c, rows.Err()
}

// SaveRunDryCopyTask records a check's state en seco against the copy
// (and the tail of its log).
func (s *Store) SaveRunDryCopyTask(ctx context.Context, runID int64, t ReleaseTask) error {
	log := t.Log
	if len(log) > maxTaskLog {
		log = log[len(log)-maxTaskLog:]
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO run_dry_copy_tasks (run_id, name, status, message, log, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (run_id, name) DO UPDATE SET status = $3, message = $4, log = $5, started_at = $6, finished_at = $7`,
		runID, t.Name, t.Status, t.Message, log, t.StartedAt, t.FinishedAt)
	return err
}

// RunDryCopyTaskLog is the log of one of the run's checks en seco against
// the copy.
func (s *Store) RunDryCopyTaskLog(ctx context.Context, runID int64, name string) (string, error) {
	var log string
	err := s.pool.QueryRow(ctx, `SELECT log FROM run_dry_copy_tasks WHERE run_id = $1 AND name = $2`, runID, name).Scan(&log)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return log, err
}

// DryCopyGone marks the copies of app's pull request number gone, on the
// runs whose checks ran against it en seco: their page stops linking to it.
func (s *Store) DryCopyGone(ctx context.Context, appID int64, number int) error {
	_, err := s.pool.Exec(ctx, `UPDATE run_dry_copies SET gone = true
		WHERE kind = $3 AND pull = $2 AND NOT gone AND run_id IN (SELECT id FROM runs WHERE app_id = $1)`, appID, number, DryCopyPull)
	return err
}
