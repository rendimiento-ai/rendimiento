// Package store persists apps, CI runs, step logs, releases and sessions in
// Postgres. Queued runs double as the job queue (FOR UPDATE SKIP LOCKED), so
// no separate broker is needed.
package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

//go:embed migrations/*.sql
var migrations embed.FS

var ErrNotFound = errors.New("not found")

// Store is the platform's Postgres database.
type Store struct{ pool *pgxpool.Pool }

// Open connects to Postgres; call Migrate before use.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Close releases the connection pool.
func (s *Store) Close() { s.pool.Close() }

// Migrate applies embedded migrations in filename order, each once.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			// Serialize concurrent replicas starting at once.
			if _, err := tx.Exec(ctx, `LOCK TABLE schema_migrations IN EXCLUSIVE MODE`); err != nil {
				return err
			}
			var done bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&done); err != nil || done {
				return err
			}
			sql, err := migrations.ReadFile(name)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ---- apps ----

// App is an onboarded application: its repo, installation and the rendimiento.yaml of its default
// branch.
type App struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Repo           string    `json:"repo"`
	InstallationID int64     `json:"installationId"`
	DefaultBranch  string    `json:"defaultBranch"`
	OrgID          int64     `json:"orgId"`
	Spec           spec.Spec `json:"spec"`
	CreatedAt      time.Time `json:"createdAt"`
	// AutoEncargo is the app's own choice of when an encargo opens by
	// itself for a failed run ("" follows the organization's).
	AutoEncargo string `json:"autoEncargo"`
	// AutoMerge has rendimiento merge a branch's open pull request once
	// the branch's run passes: the runs a person approved in rendimiento,
	// or with MergeAny every open pull request.
	AutoMerge bool `json:"autoMerge"`
	MergeAny  bool `json:"mergeAny"`
}

const appCols = `id, name, repo, installation_id, default_branch, spec, created_at, org_id, auto_encargo, auto_merge, merge_any`

// scanApp reads an app row (with its spec, stored as JSON).
func scanApp(row pgx.Row) (*App, error) {
	var a App
	var raw []byte
	if err := row.Scan(&a.ID, &a.Name, &a.Repo, &a.InstallationID, &a.DefaultBranch, &raw, &a.CreatedAt, &a.OrgID, &a.AutoEncargo, &a.AutoMerge, &a.MergeAny); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, json.Unmarshal(raw, &a.Spec)
}

// CreateApp inserts an app, in its organization (the default one when not
// set); names are unique across organizations (an app is a namespace).
func (s *Store) CreateApp(ctx context.Context, a *App) error {
	raw, err := json.Marshal(a.Spec)
	if err != nil {
		return err
	}
	if a.OrgID == 0 {
		a.OrgID = DefaultOrg
	}
	return s.pool.QueryRow(ctx,
		`INSERT INTO apps (name, repo, installation_id, default_branch, spec, org_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		a.Name, a.Repo, a.InstallationID, a.DefaultBranch, raw, a.OrgID).Scan(&a.ID, &a.CreatedAt)
}

// UpdateAppSpec stores the latest rendimiento.yaml of the default branch.
func (s *Store) UpdateAppSpec(ctx context.Context, id int64, sp spec.Spec) error {
	raw, err := json.Marshal(sp)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE apps SET spec = $2 WHERE id = $1`, id, raw)
	return err
}

// GetApp returns an app by name, or ErrNotFound.
func (s *Store) GetApp(ctx context.Context, name string) (*App, error) {
	return scanApp(s.pool.QueryRow(ctx, `SELECT `+appCols+` FROM apps WHERE name = $1`, name))
}

// GetAppByID returns an app by ID, or ErrNotFound.
func (s *Store) GetAppByID(ctx context.Context, id int64) (*App, error) {
	return scanApp(s.pool.QueryRow(ctx, `SELECT `+appCols+` FROM apps WHERE id = $1`, id))
}

// AppsForRepo returns every app deployed from a repo (webhooks fan out to them).
func (s *Store) AppsForRepo(ctx context.Context, repo string) ([]*App, error) {
	return s.listApps(ctx, `SELECT `+appCols+` FROM apps WHERE lower(repo) = lower($1) ORDER BY name`, repo)
}

// ListAppsInOrg returns an organization's apps, by name.
func (s *Store) ListAppsInOrg(ctx context.Context, orgID int64) ([]*App, error) {
	return s.listApps(ctx, `SELECT `+appCols+` FROM apps WHERE org_id = $1 ORDER BY name`, orgID)
}

// ListApps returns every app, by name.
func (s *Store) ListApps(ctx context.Context) ([]*App, error) {
	return s.listApps(ctx, `SELECT `+appCols+` FROM apps ORDER BY name`)
}

// ListReleasedApps lists the apps with at least one release: the ones that
// have been live. An app still waiting for its first build is not down.
func (s *Store) ListReleasedApps(ctx context.Context) ([]*App, error) {
	return s.listApps(ctx, `SELECT `+appCols+` FROM apps WHERE EXISTS (SELECT 1 FROM releases r WHERE r.app_id = apps.id) ORDER BY name`)
}

// listApps runs a query that returns app rows.
func (s *Store) listApps(ctx context.Context, q string, args ...any) ([]*App, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*App
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteApp removes an app and, through foreign keys, its runs and releases.
func (s *Store) DeleteApp(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM apps WHERE id = $1`, id)
	return err
}

// ---- runs & steps ----

// Run is one CI run: a commit of an app built (and on the default branch, released).
type Run struct {
	ID         int64      `json:"id"`
	AppID      int64      `json:"appId"`
	SHA        string     `json:"sha"`
	Branch     string     `json:"branch"`
	Event      string     `json:"event"`
	Deploy     bool       `json:"deploy"`
	Status     string     `json:"status"`
	Message    string     `json:"message"`
	CheckRun   *int64     `json:"checkRun,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Steps      []Step     `json:"steps,omitempty"`
	// Pull is the pull request rendimiento tried to merge when this run
	// passed (0 when none), and Merge what became of it.
	Pull  int    `json:"pull,omitempty"`
	Merge string `json:"merge,omitempty"`
	// ApprovedBy is who said in rendimiento that this commit is to be
	// merged ("" when nobody did).
	ApprovedBy string `json:"approvedBy,omitempty"`
	// Retries is how often a person or an agent had what failed run again.
	Retries int `json:"retries,omitempty"`
}

// What became of the pull request of a run that passed.
const (
	MergeMerged   = "merged"
	MergeConflict = "conflict" // it conflicts with the default branch
	// MergeResolved: it conflicted only where both sides added lines, and
	// rendimiento merged the default branch into it (a new commit, tested).
	MergeResolved = "resolved"
	MergeBlocked  = "blocked" // GitHub refuses it: a review, a required check
)

// Step is one test or build step of a run, with its result.
type Step struct {
	ID         string     `json:"id"`
	Service    string     `json:"service"`
	Kind       string     `json:"kind"`
	DependsOn  []string   `json:"dependsOn"`
	Status     string     `json:"status"`
	Digest     string     `json:"digest,omitempty"`
	Message    string     `json:"message,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	// Tries is how often the step ran: more than 1 when it failed and was
	// retried.
	Tries int `json:"tries,omitempty"`
}

const (
	RunQueued    = "queued"
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
)

const runCols = `id, app_id, sha, branch, event, deploy, status, message, check_run, created_at, started_at, finished_at, pull_number, merge_state, approved_by, retries`

// scanRun reads a run row, without its steps.
func scanRun(row pgx.Row) (*Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.AppID, &r.SHA, &r.Branch, &r.Event, &r.Deploy, &r.Status, &r.Message, &r.CheckRun, &r.CreatedAt, &r.StartedAt, &r.FinishedAt, &r.Pull, &r.Merge, &r.ApprovedBy, &r.Retries)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

// CreateRun queues a run with its planned steps.
func (s *Store) CreateRun(ctx context.Context, r *Run, steps []pipeline.Step) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`INSERT INTO runs (app_id, sha, branch, event, deploy) VALUES ($1, $2, $3, $4, $5) RETURNING id, status, created_at`,
			r.AppID, r.SHA, r.Branch, r.Event, r.Deploy).Scan(&r.ID, &r.Status, &r.CreatedAt)
		if err != nil {
			return err
		}
		r.Steps = nil
		for _, st := range steps {
			deps := st.DependsOn
			if deps == nil {
				deps = []string{}
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO steps (run_id, step_id, service, kind, depends_on) VALUES ($1, $2, $3, $4, $5)`,
				r.ID, st.ID, st.Service, string(st.Kind), deps); err != nil {
				return err
			}
			r.Steps = append(r.Steps, Step{ID: st.ID, Service: st.Service, Kind: string(st.Kind), DependsOn: deps, Status: string(pipeline.StatusPending)})
		}
		return nil
	})
}

// --8<-- [start:claim]

// ClaimRun takes the oldest queued run, of an app not in skip (those with a
// run going), and marks it running. It returns ErrNotFound when there is
// none. Safe across replicas.
func (s *Store) ClaimRun(ctx context.Context, skip []int64) (*Run, error) {
	return s.ClaimRunFor(ctx, Busy{Full: skip})
}

// Busy is what the runs going rule out for the next one: apps that have as
// many runs going as they may (Full), apps with a deploying run going
// (Deploying: their next deploy waits, so releases keep their order), and
// branches with a run going ("<app id>:<branch>", so a branch's runs keep
// theirs).
type Busy struct {
	Full      []int64
	Deploying []int64
	Branches  []string
}

// ClaimRunFor takes the oldest queued run that b does not rule out, and marks
// it running (ErrNotFound when there is none).
func (s *Store) ClaimRunFor(ctx context.Context, b Busy) (*Run, error) {
	for _, l := range []*[]int64{&b.Full, &b.Deploying} {
		if *l == nil {
			*l = []int64{}
		}
	}
	if b.Branches == nil {
		b.Branches = []string{}
	}
	return scanRun(s.pool.QueryRow(ctx, `
		UPDATE runs SET status = 'running', started_at = now()
		WHERE id = (SELECT id FROM runs WHERE status = 'queued' AND NOT (app_id = ANY($1))
			AND NOT (deploy AND app_id = ANY($2)) AND NOT ((app_id::text || ':' || branch) = ANY($3))
			ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING `+runCols, b.Full, b.Deploying, b.Branches))
}

// --8<-- [end:claim]

// maxAttempts bounds how often a run interrupted by a restart is retried.
const maxAttempts = 2

// RequeueOrphans handles runs left "running" by a process that died:
// they are queued again from scratch (builds are repeatable), up to
// maxAttempts, after which they are marked failed.
func (s *Store) RequeueOrphans(ctx context.Context) (requeued, failed int64, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE steps SET status = 'pending', digest = '', message = '', started_at = NULL, finished_at = NULL, log = ''
			WHERE run_id IN (SELECT id FROM runs WHERE status = 'running' AND attempts < $1)`, maxAttempts); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE runs SET status = 'queued', attempts = attempts + 1, started_at = NULL,
			message = 'retrying after a platform restart' WHERE status = 'running' AND attempts < $1`, maxAttempts)
		if err != nil {
			return err
		}
		requeued = tag.RowsAffected()
		if _, err := tx.Exec(ctx, closeSteps+`IN (SELECT id FROM runs WHERE status = 'running')`); err != nil {
			return err
		}
		tag, err = tx.Exec(ctx, `UPDATE runs SET status = 'failed', message = 'interrupted by a platform restart (gave up after retries)', finished_at = now()
			WHERE status = 'running'`)
		failed = tag.RowsAffected()
		return err
	})
	return requeued, failed, err
}

// SetCheckRun remembers the GitHub check run that mirrors a run.
func (s *Store) SetCheckRun(ctx context.Context, runID, checkRun int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE runs SET check_run = $2 WHERE id = $1`, runID, checkRun)
	return err
}

// closeSteps ends every unfinished step of the given runs: a finished run
// never shows a step as pending or running.
const closeSteps = `UPDATE steps SET
		status = CASE WHEN status = 'running' THEN 'failed' ELSE 'skipped' END,
		message = CASE WHEN status = 'running' THEN 'interrupted' ELSE message END,
		finished_at = COALESCE(finished_at, now())
	WHERE status IN ('pending', 'running') AND run_id `

// CreateRejectedRun records a run that failed before it could start (its
// rendimiento.yaml was invalid): it is created finished, with no steps, so
// no worker ever claims it.
func (s *Store) CreateRejectedRun(ctx context.Context, r *Run, message string) error {
	r.Status, r.Message = RunFailed, message
	return s.pool.QueryRow(ctx, `
		INSERT INTO runs (app_id, sha, branch, event, deploy, status, message, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now()) RETURNING id, created_at, started_at, finished_at`,
		r.AppID, r.SHA, r.Branch, r.Event, r.Deploy, RunFailed, message).Scan(&r.ID, &r.CreatedAt, &r.StartedAt, &r.FinishedAt)
}

// FinishRun records a run's outcome and closes any step still pending or running.
func (s *Store) FinishRun(ctx context.Context, runID int64, status, message string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE runs SET status = $2, message = $3, finished_at = now() WHERE id = $1`, runID, status, message); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, closeSteps+`= $1`, runID)
		return err
	})
}

// CancelQueued cancels queued runs of an app on a branch (superseded by a newer push).
func (s *Store) CancelQueued(ctx context.Context, appID int64, branch string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, closeSteps+`IN (SELECT id FROM runs WHERE app_id = $1 AND branch = $2 AND status = 'queued')`, appID, branch); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE runs SET status = 'cancelled', message = 'superseded by a newer commit', finished_at = now()
			WHERE app_id = $1 AND branch = $2 AND status = 'queued'`, appID, branch)
		return err
	})
}

// RunAt is the app's latest run of a commit on a branch (without its steps).
func (s *Store) RunAt(ctx context.Context, appID int64, branch, sha string) (*Run, error) {
	return scanRun(s.pool.QueryRow(ctx, `SELECT `+runCols+` FROM runs WHERE app_id = $1 AND branch = $2 AND sha = $3 ORDER BY id DESC LIMIT 1`, appID, branch, sha))
}

// TriedOn reports whether the app has a run of the commit on the branch
// that is queued, running or passed: a failed or cancelled one does not
// count, so trying again still runs.
func (s *Store) TriedOn(ctx context.Context, appID int64, branch, sha string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM runs WHERE app_id = $1 AND branch = $2 AND sha = $3 AND status IN ($4, $5, $6))`,
		appID, branch, sha, RunQueued, RunRunning, RunSucceeded).Scan(&ok)
	return ok, err
}

// SetRunMerge records what became of the pull request of a run that
// passed, in its message too. A run whose pull request was merged stays so.
func (s *Store) SetRunMerge(ctx context.Context, runID int64, pull int, state, message string) error {
	_, err := s.pool.Exec(ctx, `UPDATE runs SET pull_number = $2, merge_state = $3, message = $4 WHERE id = $1 AND merge_state <> 'merged'`, runID, pull, state, message)
	return err
}

// LastRunOf is the app's latest run of a branch (without its steps).
func (s *Store) LastRunOf(ctx context.Context, appID int64, branch string) (*Run, error) {
	return scanRun(s.pool.QueryRow(ctx, `SELECT `+runCols+` FROM runs WHERE app_id = $1 AND branch = $2 ORDER BY id DESC LIMIT 1`, appID, branch))
}

// ApproveRun records who said the run's commit is to be merged. The first
// person stays.
func (s *Store) ApproveRun(ctx context.Context, runID int64, who string) error {
	_, err := s.pool.Exec(ctx, `UPDATE runs SET approved_by = $2 WHERE id = $1 AND approved_by = ''`, runID, who)
	return err
}

// GetRun returns a run with its steps.
func (s *Store) GetRun(ctx context.Context, id int64) (*Run, error) {
	r, err := scanRun(s.pool.QueryRow(ctx, `SELECT `+runCols+` FROM runs WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT step_id, service, kind, depends_on, status, digest, message, started_at, finished_at, tries
		FROM steps WHERE run_id = $1 ORDER BY step_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var st Step
		if err := rows.Scan(&st.ID, &st.Service, &st.Kind, &st.DependsOn, &st.Status, &st.Digest, &st.Message, &st.StartedAt, &st.FinishedAt, &st.Tries); err != nil {
			return nil, err
		}
		r.Steps = append(r.Steps, st)
	}
	return r, rows.Err()
}

// ListRuns returns an app's most recent runs, newest first.
func (s *Store) ListRuns(ctx context.Context, appID int64, limit int) ([]*Run, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+runCols+` FROM runs WHERE app_id = $1 ORDER BY id DESC LIMIT $2`, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateStep records a step's status, digest, message and times.
func (s *Store) UpdateStep(ctx context.Context, runID int64, stepID string, r pipeline.StepResult) error {
	_, err := s.pool.Exec(ctx, `UPDATE steps SET status = $3,
			digest = CASE WHEN $4 = '' THEN digest ELSE $4 END,
			message = $5,
			started_at = COALESCE(started_at, $6),
			finished_at = $7
		WHERE run_id = $1 AND step_id = $2`,
		runID, stepID, string(r.Status), r.Digest, r.Message, nullTime(r.Started), nullTime(r.Finished))
	return err
}

// maxLog caps stored log size per step; the head is dropped, the tail (where errors are) kept.
const maxLog = 1 << 20

// AppendLog adds output to a step's log, keeping at most maxLog bytes (the tail, where errors are).
func (s *Store) AppendLog(ctx context.Context, runID int64, stepID, chunk string) error {
	_, err := s.pool.Exec(ctx, `UPDATE steps SET log = right(log || $3, $4) WHERE run_id = $1 AND step_id = $2`, runID, stepID, chunk, maxLog)
	return err
}

// StepLog returns a step's stored log, or, once archived, the key of its
// object in the log archive (and an empty log).
func (s *Store) StepLog(ctx context.Context, runID int64, stepID string) (log, ref string, err error) {
	err = s.pool.QueryRow(ctx, `SELECT log, log_ref FROM steps WHERE run_id = $1 AND step_id = $2`, runID, stepID).Scan(&log, &ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return log, ref, err
}

// ArchivableStep is a finished run's step whose log is still in Postgres.
type ArchivableStep struct {
	RunID  int64
	StepID string
	App    string
	Log    string
}

// UnarchivedSteps lists up to limit steps with a log in Postgres, of runs
// that finished before `before` (their logs are complete), oldest first.
func (s *Store) UnarchivedSteps(ctx context.Context, before time.Time, limit int) ([]ArchivableStep, error) {
	rows, err := s.pool.Query(ctx, `SELECT st.run_id, st.step_id, a.name, st.log FROM steps st
		JOIN runs r ON r.id = st.run_id JOIN apps a ON a.id = r.app_id
		WHERE st.log_ref = '' AND st.log <> '' AND r.finished_at IS NOT NULL AND r.finished_at < $1
		ORDER BY st.run_id LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchivableStep
	for rows.Next() {
		var a ArchivableStep
		if err := rows.Scan(&a.RunID, &a.StepID, &a.App, &a.Log); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkArchived records that a step's log (bytes long, in bytes) is stored
// under ref and empties the column. It reports false, changing nothing, if
// the log changed since it was read.
func (s *Store) MarkArchived(ctx context.Context, runID int64, stepID, ref string, bytes int) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE steps SET log = '', log_ref = $3, log_bytes = $4, archived_at = now()
		WHERE run_id = $1 AND step_id = $2 AND log_ref = '' AND octet_length(log) = $4`, runID, stepID, ref, bytes)
	return tag.RowsAffected() == 1, err
}

// ArchiveStats counts archived logs and their size, and logs waiting to be archived.
func (s *Store) ArchiveStats(ctx context.Context) (archived int, bytes int64, pending int, err error) {
	err = s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE log_ref <> ''), COALESCE(sum(log_bytes) FILTER (WHERE log_ref <> ''), 0),
		count(*) FILTER (WHERE log_ref = '' AND log <> '') FROM steps`).Scan(&archived, &bytes, &pending)
	return
}

// ---- releases ----

// Release is what was deployed: the images (by digest) and spec of one successful run, or of a
// rollback.
type Release struct {
	ID         int64             `json:"id"`
	AppID      int64             `json:"appId"`
	Number     int64             `json:"number"`
	RunID      *int64            `json:"runId,omitempty"`
	SHA        string            `json:"sha"`
	Images     map[string]string `json:"images"`
	Spec       spec.Spec         `json:"spec"`
	RollbackOf *int64            `json:"rollbackOf,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	// Verification of the release once live (see Verify* constants).
	VerifyStatus  string     `json:"verifyStatus,omitempty"`
	VerifyMessage string     `json:"verifyMessage,omitempty"`
	VerifiedAt    *time.Time `json:"verifiedAt,omitempty"`
	// Tasks are its post-deploy tasks (filled in by the API, not scanned).
	Tasks []ReleaseTask `json:"tasks,omitempty"`
	// Tests are the app's own tests' results in this release (filled in by
	// the API, not scanned).
	Tests []ReleaseTest `json:"tests,omitempty"`
}

// Release verification states.
const (
	VerifyRunning    = "verifying"
	VerifyPassed     = "passed"
	VerifyFailed     = "failed"      // rolled back
	VerifyFailedKept = "failed-kept" // failed, but rollback is off
	VerifySkipped    = "skipped"
	VerifyBlocked    = "blocked" // a pre-deploy task failed: never deployed
	VerifySuperseded = "superseded"
)

// SetVerification records a release's verification state and message.
func (s *Store) SetVerification(ctx context.Context, releaseID int64, status, message string) error {
	_, err := s.pool.Exec(ctx, `UPDATE releases SET verify_status = $2, verify_message = $3,
		verified_at = CASE WHEN $2 IN ('verifying', '') THEN NULL ELSE now() END WHERE id = $1`, releaseID, status, message)
	return err
}

// VerifyingReleases lists releases whose verification was interrupted
// (the process stopped while watching them).
func (s *Store) VerifyingReleases(ctx context.Context) ([]*Release, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+releaseCols+` FROM releases WHERE verify_status = 'verifying' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Release
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateRelease assigns the next release number for the app.
func (s *Store) CreateRelease(ctx context.Context, r *Release) error {
	images, err := json.Marshal(r.Images)
	if err != nil {
		return err
	}
	sp, err := json.Marshal(r.Spec)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Lock the app row so concurrent releases get distinct numbers.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM apps WHERE id = $1 FOR UPDATE`, r.AppID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO releases (app_id, number, run_id, sha, images, spec, rollback_of)
			VALUES ($1, (SELECT COALESCE(MAX(number), 0) + 1 FROM releases WHERE app_id = $1), $2, $3, $4, $5, $6)
			RETURNING id, number, created_at`,
			r.AppID, r.RunID, r.SHA, images, sp, r.RollbackOf).Scan(&r.ID, &r.Number, &r.CreatedAt)
	})
}

const releaseCols = `id, app_id, number, run_id, sha, images, spec, rollback_of, created_at, verify_status, verify_message, verified_at`

// scanRelease reads a release row (images and spec are JSON).
func scanRelease(row pgx.Row) (*Release, error) {
	var r Release
	var images, sp []byte
	err := row.Scan(&r.ID, &r.AppID, &r.Number, &r.RunID, &r.SHA, &images, &sp, &r.RollbackOf, &r.CreatedAt,
		&r.VerifyStatus, &r.VerifyMessage, &r.VerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(images, &r.Images); err != nil {
		return nil, err
	}
	return &r, json.Unmarshal(sp, &r.Spec)
}

// GetRelease returns an app's release by number.
func (s *Store) GetRelease(ctx context.Context, appID, number int64) (*Release, error) {
	return scanRelease(s.pool.QueryRow(ctx, `SELECT `+releaseCols+` FROM releases WHERE app_id = $1 AND number = $2`, appID, number))
}

// LatestRelease returns the app's newest release, or ErrNotFound.
func (s *Store) LatestRelease(ctx context.Context, appID int64) (*Release, error) {
	return scanRelease(s.pool.QueryRow(ctx, `SELECT `+releaseCols+` FROM releases WHERE app_id = $1 ORDER BY number DESC LIMIT 1`, appID))
}

// ListReleases returns an app's most recent releases, newest first.
func (s *Store) ListReleases(ctx context.Context, appID int64, limit int) ([]*Release, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+releaseCols+` FROM releases WHERE app_id = $1 ORDER BY number DESC LIMIT $2`, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Release
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- sessions ----

// CreateSession stores a login session by the hash of its token.
func (s *Store) CreateSession(ctx context.Context, tokenHash, login string, ttl time.Duration) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (token_hash, login, expires_at) VALUES ($1, $2, now() + $3::interval)`,
		tokenHash, login, fmt.Sprintf("%d seconds", int(ttl.Seconds())))
	return err
}

// SessionInfo returns a valid session's GitHub login and the organization
// it works in, or ErrNotFound.
func (s *Store) SessionInfo(ctx context.Context, tokenHash string) (login string, orgID int64, err error) {
	err = s.pool.QueryRow(ctx, `SELECT login, org_id FROM sessions WHERE token_hash = $1 AND expires_at > now()`, tokenHash).Scan(&login, &orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	return login, orgID, err
}

// SetSessionOrg moves a session to another organization.
func (s *Store) SetSessionOrg(ctx context.Context, tokenHash string, orgID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET org_id = $2 WHERE token_hash = $1`, tokenHash, orgID)
	return err
}

// SessionLogin returns the GitHub login of a valid session, or ErrNotFound.
func (s *Store) SessionLogin(ctx context.Context, tokenHash string) (string, error) {
	var login string
	err := s.pool.QueryRow(ctx, `SELECT login FROM sessions WHERE token_hash = $1 AND expires_at > now()`, tokenHash).Scan(&login)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return login, err
}

// DeleteSession ends a session and removes expired ones.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1 OR expires_at < now()`, tokenHash)
	return err
}

// nullTime is nil for the zero time, so it is stored as NULL.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// IsUniqueViolation reports a duplicate key (e.g. app name taken).
func IsUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 23505")
}

// ResetForTests drops every table. Only for test databases.
func (s *Store) ResetForTests(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`)
	return err
}

// Ping checks the database connection.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// SetAppAutoMerge is whether rendimiento merges the app's pull requests
// once their branch passes, and whether any open one (anyPull) or only
// the runs a person approved.
func (s *Store) SetAppAutoMerge(ctx context.Context, appID int64, on, anyPull bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE apps SET auto_merge = $2, merge_any = $3 WHERE id = $1`, appID, on, on && anyPull)
	return err
}

// SetAppAutoEncargo is an app's own choice of when an encargo opens by
// itself for a failed run ("" to follow the organization).
func (s *Store) SetAppAutoEncargo(ctx context.Context, appID int64, mode string) error {
	_, err := s.pool.Exec(ctx, `UPDATE apps SET auto_encargo = $2 WHERE id = $1`, appID, mode)
	return err
}
