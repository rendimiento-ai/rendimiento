package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// What the libreta of an app reads (Ruta 150): the Ruta's to-dos and
// decisions about the app, the branches each one is worked on, and every
// encargo about the app. Its states are computed elsewhere (package
// libreta) from these and from the app's runs, pull requests and releases.

// maxLibreta is how many entries, and how many encargos, a libreta reads.
const maxLibreta = 300

// RutaOfApp lists an app's to-dos and decisions that are not archived,
// newest first.
func (s *Store) RutaOfApp(ctx context.Context, orgID int64, app string) ([]RutaEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+rutaCols+` FROM ruta_entries
		WHERE org_id = $1 AND app = $2 AND archived_at IS NULL AND kind IN ('pendiente', 'decision')
		ORDER BY created_at DESC, id DESC LIMIT $3`, orgOr(orgID), app, maxLibreta)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RutaEntry, error) { return scanRuta(r) })
}

// RutaBranches are the branches each entry is worked on, oldest link first.
func (s *Store) RutaBranches(ctx context.Context, ids []int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT entry_id, branch FROM ruta_ramas WHERE entry_id = ANY($1) ORDER BY created_at, branch`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var b string
		if err := rows.Scan(&id, &b); err != nil {
			return nil, err
		}
		out[id] = append(out[id], b)
	}
	return out, rows.Err()
}

// LinkRutaBranch notes that an entry is worked on a branch (once).
func (s *Store) LinkRutaBranch(ctx context.Context, entryID int64, branch, who string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO ruta_ramas (entry_id, branch, created_by) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, entryID, branch, who)
	return err
}

// AppEncargo is an encargo about an app, with why it was cancelled.
type AppEncargo struct {
	Encargo
	CancelNote string
}

// EncargosOfApp lists every encargo about an app (the reviewed and the
// cancelled ones too), newest first, with the note it was cancelled with.
func (s *Store) EncargosOfApp(ctx context.Context, orgID int64, app string) ([]AppEncargo, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+encargoCols+`,
		COALESCE((SELECT l.note FROM encargo_log l WHERE l.encargo_id = e.id AND l.what = 'cancelled' ORDER BY l.at DESC, l.id DESC LIMIT 1), '')`+encargoFrom+`
		WHERE e.org_id = $1 AND e.app = $2 ORDER BY e.created_at DESC, e.id DESC LIMIT $3`, orgOr(orgID), app, maxLibreta)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AppEncargo, error) {
		var a AppEncargo
		e := &a.Encargo
		err := r.Scan(&e.ID, &e.Title, &e.Body, &e.App, &e.RunID, &e.Status, &e.CreatedBy, &e.ByAgent, &e.KeyID, &e.KeyName,
			&e.CargoID, &e.WaitingRun, &e.Result, &e.Links, &e.Comment, &e.CreatedAt, &e.UpdatedAt, &e.OrgID,
			&e.Difficulty, &e.EstimateMicros, &e.EstimateReason, &e.Route, &e.DoNow, &e.SpentMicros, &e.RunBranch, &a.CancelNote)
		return a, err
	})
}
