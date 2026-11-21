package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The coding agent's queue (la Ruta nueva): what it does next and in which
// order, what it does now, and how many it may do at once.

// MaxCodeConcurrency is the most encargos an organization lets the coding
// agent do at once; 1 is the default (one at a time, in order).
const MaxCodeConcurrency = 3

// CoderCandidate is an encargo the coding agent could take now, with how
// many its organization lets it do at once.
type CoderCandidate struct {
	Encargo
	Concurrency int
}

// CoderCandidates are the coding agent's queues of every organization, each
// in its order (the scheduler, agentes.Pick, chooses from them), up to
// limit.
func (s *Store) CoderCandidates(ctx context.Context, limit int) ([]CoderCandidate, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `SELECT `+encargoCols+`, a.code_concurrency`+encargoFrom+`
		JOIN ai_settings a ON a.org_id = e.org_id
		WHERE `+coderWhere+` ORDER BY e.org_id, `+coderOrder+` LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CoderCandidate, error) {
		var c CoderCandidate
		e := &c.Encargo
		err := r.Scan(encargoFields(e, &c.Concurrency)...)
		return c, err
	})
}

// CoderQueue is an organization's queue: the pending encargos the coding
// agent will do, in the order it takes them.
func (s *Store) CoderQueue(ctx context.Context, orgID int64) ([]Encargo, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+encargoCols+encargoFrom+`
		JOIN ai_settings a ON a.org_id = e.org_id
		WHERE e.org_id = $1 AND `+coderWhere+` ORDER BY `+coderOrder+` LIMIT 200`, orgOr(orgID))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Encargo, error) { return scanEncargo(r) })
}

// ReorderQueue puts an organization's pending encargos in the order of ids
// (the first is done first); the ones it leaves out go after them. It
// answers how many it placed.
func (s *Store) ReorderQueue(ctx context.Context, orgID int64, ids []int64) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE encargos SET queue_pos = NULL WHERE org_id = $1 AND status = 'pending' AND queue_pos IS NOT NULL AND NOT (id = ANY($2))`,
		orgOr(orgID), ids); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `UPDATE encargos e SET queue_pos = o.pos FROM unnest($2::bigint[]) WITH ORDINALITY AS o(id, pos)
		WHERE e.id = o.id AND e.org_id = $1 AND e.status = 'pending'`, orgOr(orgID), ids)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), tx.Commit(ctx)
}

// SetEncargoApp says which app a pending encargo nobody has is about (it
// was asked without one): the estimator rates it again, knowing the app,
// unless a person chose who does it.
func (s *Store) SetEncargoApp(ctx context.Context, id int64, who, app string) (Encargo, error) {
	return s.change(ctx, id, who, "app", app, `UPDATE encargos SET app = $2, difficulty = NULL, estimate_micros = NULL, estimate_reason = '', estimated_at = NULL,
		route = CASE WHEN EXISTS (SELECT 1 FROM encargo_log WHERE encargo_id = $1 AND what = 'routed') THEN route ELSE '' END, updated_at = now()
		WHERE id = $1 AND status = 'pending' AND key_id IS NULL`, id, app)
}

// SetEncargoDoing keeps what the coding agent does now on an encargo it
// has ("tool:what"), for its queue's page.
func (s *Store) SetEncargoDoing(ctx context.Context, id int64, doing string) error {
	if r := []rune(doing); len(r) > 300 {
		doing = string(r[:300])
	}
	_, err := s.pool.Exec(ctx, `UPDATE encargos SET doing = $2 WHERE id = $1 AND status IN ('taken', 'waiting')`, id, doing)
	return err
}

// StopEncargo is a person asking the platform's coding agent to stop the
// encargo it does: it gives it back to them, with what it did. Only one a
// platform agent has (ErrEncargoState otherwise).
func (s *Store) StopEncargo(ctx context.Context, id int64, who string) (Encargo, error) {
	return s.change(ctx, id, who, "stop", "", `UPDATE encargos SET stop_asked = true, updated_at = now()
		WHERE id = $1 AND status IN ('taken', 'waiting') AND NOT stop_asked
			AND key_id IN (SELECT id FROM agent_keys WHERE created_by = 'rendimiento')`, id)
}

// ShouldStop says whether the coding agent should stop an encargo: a
// person asked it to, or it is no longer the agent's to do (dropped).
func (s *Store) ShouldStop(ctx context.Context, id int64) (bool, error) {
	var stop bool
	err := s.pool.QueryRow(ctx, `SELECT stop_asked OR status NOT IN ('taken', 'waiting') FROM encargos WHERE id = $1`, id).Scan(&stop)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return stop, err
}

// CoderOf lists an organization's encargos a platform agent (by its name)
// has now (taken or waiting), the first taken first.
func (s *Store) CoderOf(ctx context.Context, orgID int64, name string) ([]Encargo, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+encargoCols+encargoFrom+`
		WHERE e.org_id = $1 AND k.name = $2 AND k.created_by = 'rendimiento' AND e.status IN ('taken', 'waiting') ORDER BY e.updated_at`, orgOr(orgID), name)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Encargo, error) { return scanEncargo(r) })
}

// CoderFinished lists an organization's encargos a platform agent (by its
// name) finished and are not closed yet: delivered (waiting for the
// person's review), and given back to the person (pending, for their own
// agent), the most recent first.
func (s *Store) CoderFinished(ctx context.Context, orgID int64, name string, limit int) (delivered, returned []Encargo, err error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	// Delivered: its last delivery is the agent's. Given back: pending,
	// and the last thing that changed who has it is the agent letting it go.
	rows, err := s.pool.Query(ctx, `SELECT `+encargoCols+encargoFrom+`
		WHERE e.org_id = $1 AND (
			(e.status = 'delivered' AND (SELECT l.who FROM encargo_log l WHERE l.encargo_id = e.id AND l.what = 'delivered' ORDER BY l.id DESC LIMIT 1) = $2::text)
			OR (e.status = 'pending' AND e.key_id IS NULL AND (SELECT l.what || '|' || l.who FROM encargo_log l WHERE l.encargo_id = e.id
				AND l.what IN ('taken', 'released', 'returned', 'sent', 'routed', 'app') ORDER BY l.id DESC LIMIT 1) = 'released|' || $2::text))
		ORDER BY e.updated_at DESC LIMIT $3`, orgOr(orgID), name, limit)
	if err != nil {
		return nil, nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Encargo, error) { return scanEncargo(r) })
	for _, e := range list {
		if e.Status == EncargoDelivered {
			delivered = append(delivered, e)
		} else {
			returned = append(returned, e)
		}
	}
	return delivered, returned, err
}

// SetCodeConcurrency is how many encargos the coding agent may do at once
// for an organization (1 to MaxCodeConcurrency; never two of one app).
func (s *Store) SetCodeConcurrency(ctx context.Context, orgID int64, n int, by string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO ai_settings (org_id, code_concurrency, updated_by, updated_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (org_id) DO UPDATE SET code_concurrency = $2, updated_by = $3, updated_at = now()`, orgID, n, by)
	return err
}

// AISpentSince is what an organization's calls from a source cost since a
// moment (the coding agent's today, on its queue's page).
func (s *Store) AISpentSince(ctx context.Context, orgID int64, source string, since time.Time) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(sum(cost_micros), 0) FROM ai_usage WHERE org_id = $1 AND source = $2 AND at >= $3`,
		orgOr(orgID), source, since).Scan(&n)
	return n, err
}

// UnconfirmedManual is a known failure the diagnostician wrote with a
// stronger model that nobody rated yet, with the run to rate it on.
type UnconfirmedManual struct {
	ID    int64     `json:"id"`
	Title string    `json:"title"`
	RunID int64     `json:"runId"`
	App   string    `json:"app"`
	At    time.Time `json:"at"`
}

// UnconfirmedManuals lists an organization's known failures tagged tag
// (agentes.Unconfirmed) whose diagnosis nobody rated, the newest first.
func (s *Store) UnconfirmedManuals(ctx context.Context, orgID int64, tag string, limit int) ([]UnconfirmedManual, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.title, d.run_id, a.name, r.created_at
		FROM ruta_entries r JOIN run_diagnoses d ON d.ruta_entry = r.id JOIN runs ru ON ru.id = d.run_id JOIN apps a ON a.id = ru.app_id
		WHERE r.org_id = $1 AND r.archived_at IS NULL AND r.kind = 'manual' AND $2 = ANY(r.tags) AND d.feedback = ''
		ORDER BY r.created_at DESC LIMIT $3`, orgOr(orgID), tag, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (UnconfirmedManual, error) {
		var m UnconfirmedManual
		err := r.Scan(&m.ID, &m.Title, &m.RunID, &m.App, &m.At)
		return m, err
	})
}

// RutaCounts is what the Ruta's front page counts: encargos agents have
// now, open to-dos, what was done today (encargos delivered and to-dos
// ticked), and stories people told in the last week.
type RutaCounts struct {
	WithAgents int `json:"withAgents"`
	Pendientes int `json:"pendientes"`
	DoneToday  int `json:"doneToday"`
	VividoNew  int `json:"vividoNew"`
}

// CountRuta counts an organization's RutaCounts; today starts at since.
func (s *Store) CountRuta(ctx context.Context, orgID int64, since time.Time) (RutaCounts, error) {
	var c RutaCounts
	err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM encargos WHERE org_id = $1 AND status IN ('taken', 'waiting')),
		(SELECT count(*) FROM ruta_entries WHERE org_id = $1 AND archived_at IS NULL AND kind = 'pendiente' AND NOT done),
		(SELECT count(DISTINCT l.encargo_id) FROM encargo_log l JOIN encargos e ON e.id = l.encargo_id WHERE e.org_id = $1 AND l.what = 'delivered' AND l.at >= $2)
			+ (SELECT count(*) FROM ruta_entries WHERE org_id = $1 AND archived_at IS NULL AND kind = 'pendiente' AND done AND updated_at >= $2),
		(SELECT count(*) FROM ruta_entries WHERE org_id = $1 AND archived_at IS NULL AND kind = 'vivido' AND created_at >= now() - interval '7 days')`,
		orgOr(orgID), since).Scan(&c.WithAgents, &c.Pendientes, &c.DoneToday, &c.VividoNew)
	return c, err
}
