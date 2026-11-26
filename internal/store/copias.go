package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- the copies on the app's Caminos tab (Ruta 180) ----

// Why a copy went (Copia.GoneWhy).
const (
	CopiaMerged = "merged" // its pull request was merged
	CopiaClosed = "closed" // its pull request was closed
	CopiaIdle   = "idle"   // no push for longer than previews.keep
	CopiaMax    = "max"    // to make room under previews.max
	CopiaPerson = "person" // a person deleted it from rendimiento
	CopiaApp    = "app"    // its app is gone, or no longer asks for previews
	CopiaSeco   = "seco"   // a short copy whose checks en seco ended
	CopiaDeps   = "deps"   // its pull request is a dependency update, which gets no copy
)

// Copia is one copy's life, as the store keeps it beside the copy itself:
// the run that made it, its own tests' result, the day a person moved its
// deletion to, and why and when it went.
type Copia struct {
	ID          int64      `json:"-"`
	AppID       int64      `json:"-"`
	Pull        int        `json:"pull,omitempty"`
	SecoRun     int64      `json:"secoRun,omitempty"`
	RunID       int64      `json:"run,omitempty"`
	Branch      string     `json:"branch,omitempty"`
	MadeAt      time.Time  `json:"madeAt"`
	KeepUntil   *time.Time `json:"keepUntil,omitempty"`
	TestsPassed *int       `json:"-"`
	TestsTotal  *int       `json:"-"`
	TestsFailed []string   `json:"-"`
	GoneAt      *time.Time `json:"goneAt,omitempty"`
	GoneWhy     string     `json:"why,omitempty"`
	GoneNote    string     `json:"note,omitempty"`
	GoneBy      string     `json:"by,omitempty"`
}

const copiaCols = `id, app_id, pull, seco_run, run_id, branch, made_at, keep_until, tests_passed, tests_total, tests_failed, gone_at, gone_why, gone_note, gone_by`

func scanCopia(row pgx.Row) (Copia, error) {
	var c Copia
	err := row.Scan(&c.ID, &c.AppID, &c.Pull, &c.SecoRun, &c.RunID, &c.Branch, &c.MadeAt, &c.KeepUntil,
		&c.TestsPassed, &c.TestsTotal, &c.TestsFailed, &c.GoneAt, &c.GoneWhy, &c.GoneNote, &c.GoneBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// CopiaMade records that a copy was made or written again from run (a new
// push): its live row, made the first time. A new commit's tests have not
// run yet, so the last ones are forgotten; a day a person moved its
// deletion to stays.
func (s *Store) CopiaMade(ctx context.Context, appID int64, pull int, secoRun, runID int64, branch string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO copias (app_id, pull, seco_run, run_id, branch) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (app_id, pull, seco_run) WHERE gone_at IS NULL DO UPDATE SET
			tests_passed = CASE WHEN copias.run_id = $4 THEN copias.tests_passed END,
			tests_total = CASE WHEN copias.run_id = $4 THEN copias.tests_total END,
			tests_failed = CASE WHEN copias.run_id = $4 THEN copias.tests_failed ELSE '{}' END,
			run_id = $4, branch = $5`, appID, pull, secoRun, runID, branch)
	return err
}

// LiveCopia is the live row of app's copy (ErrNotFound when the store has
// none: a copy made before it kept them).
func (s *Store) LiveCopia(ctx context.Context, appID int64, pull int, secoRun int64) (Copia, error) {
	return scanCopia(s.pool.QueryRow(ctx, `SELECT `+copiaCols+` FROM copias
		WHERE app_id = $1 AND pull = $2 AND seco_run = $3 AND gone_at IS NULL`, appID, pull, secoRun))
}

// SetCopiaTests records the copy's own tests' result: how many passed of
// how many, and the names of those that failed.
func (s *Store) SetCopiaTests(ctx context.Context, appID int64, pull int, passed, total int, failed []string) error {
	if failed == nil {
		failed = []string{}
	}
	_, err := s.pool.Exec(ctx, `UPDATE copias SET tests_passed = $3, tests_total = $4, tests_failed = $5
		WHERE app_id = $1 AND pull = $2 AND seco_run = 0 AND gone_at IS NULL`, appID, pull, passed, total, failed)
	return err
}

// KeepCopiaUntil moves the day the copy is deleted to until (a person's
// «keep it 3 more days»), making its row when the store had none.
func (s *Store) KeepCopiaUntil(ctx context.Context, appID int64, pull int, runID int64, branch string, until time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO copias (app_id, pull, run_id, branch, keep_until) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (app_id, pull, seco_run) WHERE gone_at IS NULL DO UPDATE SET keep_until = $5`, appID, pull, runID, branch, until)
	return err
}

// CopiaGone records that a copy went: why (CopiaMerged...), in words
// (note), and who deleted it when a person did. A copy the store had no
// row of (made before it kept them) gets one, with the run it ran (runID),
// so it is still listed among those deleted lately.
func (s *Store) CopiaGone(ctx context.Context, appID int64, pull int, secoRun, runID int64, why, note, by string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE copias SET gone_at = now(), gone_why = $4, gone_note = $5, gone_by = $6
		WHERE app_id = $1 AND pull = $2 AND seco_run = $3 AND gone_at IS NULL`, appID, pull, secoRun, why, note, by)
	if err != nil || tag.RowsAffected() > 0 {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO copias (app_id, pull, seco_run, run_id, gone_at, gone_why, gone_note, gone_by)
		VALUES ($1, $2, $3, $7, now(), $4, $5, $6)`, appID, pull, secoRun, why, note, by, runID)
	return err
}

// Copias are app's live copy rows and those that went since a time, the
// newest first.
func (s *Store) Copias(ctx context.Context, appID int64, goneSince time.Time) (live, gone []Copia, err error) {
	rows, err := s.pool.Query(ctx, `SELECT `+copiaCols+` FROM copias
		WHERE app_id = $1 AND (gone_at IS NULL OR gone_at >= $2) ORDER BY COALESCE(gone_at, made_at) DESC LIMIT 200`, appID, goneSince)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanCopia(rows)
		if err != nil {
			return nil, nil, err
		}
		if c.GoneAt == nil {
			live = append(live, c)
		} else {
			gone = append(gone, c)
		}
	}
	return live, gone, rows.Err()
}

// LastCopia is the newest row of pull request pull's copy, live or gone
// (ErrNotFound: none).
func (s *Store) LastCopia(ctx context.Context, appID int64, pull int) (Copia, error) {
	return scanCopia(s.pool.QueryRow(ctx, `SELECT `+copiaCols+` FROM copias
		WHERE app_id = $1 AND pull = $2 AND seco_run = 0 ORDER BY id DESC LIMIT 1`, appID, pull))
}

// LastPassedRunOf is the app's latest run of a branch that passed (not a
// deploy), without its steps, or ErrNotFound.
func (s *Store) LastPassedRunOf(ctx context.Context, appID int64, branch string) (*Run, error) {
	return scanRun(s.pool.QueryRow(ctx, `SELECT `+runCols+` FROM runs
		WHERE app_id = $1 AND branch = $2 AND status = $3 AND NOT deploy ORDER BY id DESC LIMIT 1`, appID, branch, RunSucceeded))
}
