package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Who can ask for a declared fix.
const (
	FixByPerson   = "person"
	FixByAgent    = "agent"
	FixByRenovate = "renovate"
)

// How a fix run ends.
const (
	FixRunning   = "running"
	FixSucceeded = "succeeded" // it wrote files
	FixUnchanged = "unchanged" // it ran, and nothing it may write changed
	FixFailed    = "failed"
)

// FixRun is one run of an app's declared fix.
type FixRun struct {
	ID         int64      `json:"id"`
	AppID      int64      `json:"appId"`
	Branch     string     `json:"branch"`
	SHA        string     `json:"sha"`
	Fix        string     `json:"fix"`
	AskedKind  string     `json:"askedKind"`
	AskedBy    string     `json:"askedBy"`
	EncargoID  *int64     `json:"encargoId,omitempty"`
	RunID      *int64     `json:"runId,omitempty"`
	Status     string     `json:"status"`
	Message    string     `json:"message,omitempty"`
	Image      string     `json:"image,omitempty"`
	ImageFrom  string     `json:"imageFrom,omitempty"`
	Files      []string   `json:"files"`
	Ignored    []string   `json:"ignored"`
	CommitSHA  string     `json:"commitSha,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// ErrFixDone is StartFixRun's answer when Renovate's branch already had
// this fix on this commit: it is tried once a commit.
var ErrFixDone = errors.New("this fix already ran on this commit")

// StartFixRun records a fix run as it starts.
func (s *Store) StartFixRun(ctx context.Context, f FixRun) (FixRun, error) {
	err := s.pool.QueryRow(ctx, `INSERT INTO fix_runs (app_id, branch, sha, fix, asked_kind, asked_by, encargo_id, run_id, image, image_from)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ON CONFLICT DO NOTHING RETURNING id, status, started_at`,
		f.AppID, f.Branch, f.SHA, f.Fix, f.AskedKind, f.AskedBy, f.EncargoID, f.RunID, f.Image, f.ImageFrom).Scan(&f.ID, &f.Status, &f.StartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrFixDone
	}
	f.Files, f.Ignored = []string{}, []string{}
	return f, err
}

// FinishFixRun records how a fix run ended, with its log.
func (s *Store) FinishFixRun(ctx context.Context, f FixRun, log string) error {
	if f.Files == nil {
		f.Files = []string{}
	}
	if f.Ignored == nil {
		f.Ignored = []string{}
	}
	_, err := s.pool.Exec(ctx, `UPDATE fix_runs SET status = $2, message = $3, image = $4, image_from = $5, files = $6, ignored = $7,
		commit_sha = $8, log = $9, finished_at = now() WHERE id = $1`,
		f.ID, f.Status, f.Message, f.Image, f.ImageFrom, f.Files, f.Ignored, f.CommitSHA, log)
	return err
}

// SetFixRunCommit records the commit a fix run's files became.
func (s *Store) SetFixRunCommit(ctx context.Context, id int64, sha string) error {
	_, err := s.pool.Exec(ctx, `UPDATE fix_runs SET commit_sha = $2 WHERE id = $1`, id, sha)
	return err
}

const fixRunCols = `id, app_id, branch, sha, fix, asked_kind, asked_by, encargo_id, run_id, status, message, image, image_from, files, ignored, commit_sha, started_at, finished_at`

func scanFixRun(r pgx.Row) (FixRun, error) {
	var f FixRun
	err := r.Scan(&f.ID, &f.AppID, &f.Branch, &f.SHA, &f.Fix, &f.AskedKind, &f.AskedBy, &f.EncargoID, &f.RunID, &f.Status, &f.Message,
		&f.Image, &f.ImageFrom, &f.Files, &f.Ignored, &f.CommitSHA, &f.StartedAt, &f.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

// FixRunByID is one fix run of an app.
func (s *Store) FixRunByID(ctx context.Context, appID, id int64) (FixRun, error) {
	return scanFixRun(s.pool.QueryRow(ctx, `SELECT `+fixRunCols+` FROM fix_runs WHERE app_id = $1 AND id = $2`, appID, id))
}

// FixRunLog is a fix run's whole log.
func (s *Store) FixRunLog(ctx context.Context, appID, id int64) (string, error) {
	var log string
	err := s.pool.QueryRow(ctx, `SELECT log FROM fix_runs WHERE app_id = $1 AND id = $2`, appID, id).Scan(&log)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return log, err
}

// FixRuns are an app's latest fix runs, newest first; with runID, only those
// that answered that run.
func (s *Store) FixRuns(ctx context.Context, appID, runID int64, limit int) ([]FixRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+fixRunCols+` FROM fix_runs WHERE app_id = $1 AND ($2 = 0 OR run_id = $2) ORDER BY id DESC LIMIT $3`,
		appID, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FixRun{}
	for rows.Next() {
		f, err := scanFixRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// MadeByFix reports whether a commit of the app is one a fix run made: a
// fix is never run by itself on its own commit, so it cannot loop.
func (s *Store) MadeByFix(ctx context.Context, appID int64, sha string) (bool, error) {
	var yes bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM fix_runs WHERE app_id = $1 AND commit_sha = $2 AND commit_sha <> '')`, appID, sha).Scan(&yes)
	return yes, err
}

// NoteEncargoFix adds a fix the coding agent ran to its encargo's history.
func (s *Store) NoteEncargoFix(ctx context.Context, id int64, who, note string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO encargo_log (encargo_id, who, what, note) VALUES ($1, $2, 'fixed', $3)`, id, who, note)
	return err
}
