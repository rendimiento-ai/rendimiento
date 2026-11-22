package store

import (
	"context"
	"strings"
	"time"

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
		err := r.Scan(encargoFields(e, &a.CancelNote)...)
		return a, err
	})
}

// ---- what the libreta keeps (Ruta 150) ----

// KeptBranch is what rendimiento wrote down about a branch an entry is
// worked on: when it was merged (its head commit then) and the release
// that carried it, so the libreta knows it after its runs are gone.
type KeptBranch struct {
	MergedSHA  string
	MergedAt   *time.Time
	Release    int64
	ReleasedAt *time.Time
}

// RutaBranchesKept is what was kept of each entry's branches: entry →
// branch → what is known. Branches with nothing kept are left out.
func (s *Store) RutaBranchesKept(ctx context.Context, ids []int64) (map[int64]map[string]KeptBranch, error) {
	out := map[int64]map[string]KeptBranch{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT entry_id, branch, COALESCE(merged_sha, ''), merged_at, COALESCE(release_number, 0), released_at
		FROM ruta_ramas WHERE entry_id = ANY($1) AND (merged_at IS NOT NULL OR release_number IS NOT NULL)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var b string
		var k KeptBranch
		if err := rows.Scan(&id, &b, &k.MergedSHA, &k.MergedAt, &k.Release, &k.ReleasedAt); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = map[string]KeptBranch{}
		}
		out[id][b] = k
	}
	return out, rows.Err()
}

// LinkRutaRefs links a branch to the entries a commit names (a «Ruta: N»
// trailer), when they are of the organization, not archived, and about
// the app or about none. It never changes an entry's app. It returns how
// many links are new.
func (s *Store) LinkRutaRefs(ctx context.Context, orgID int64, app, branch string, ids []int64, who string) (int64, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" || len(ids) == 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO ruta_ramas (entry_id, branch, created_by)
		SELECT id, $3::text, $5::text FROM ruta_entries
		WHERE id = ANY($4) AND org_id = $1 AND (app = $2 OR app IS NULL) AND archived_at IS NULL
		ON CONFLICT DO NOTHING`, orgOr(orgID), app, branch, ids, who)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// cargoTouched are the entries an agent wrote or changed in a cargo: those
// written in it, and those ruta_actualizar changed in it (its arguments
// start with the entry's number, as the tool's input is marshalled).
const cargoTouched = `(e.cargo_id = $1 OR e.id IN (
	SELECT substring(a.args from '^\{"id":([0-9]+)[,}]')::bigint FROM agent_actions a
	WHERE a.cargo_id = $1 AND a.tool = 'ruta_actualizar' AND a.ok AND a.args ~ '^\{"id":[0-9]+[,}]'))`

// libretaLinkable is which entries a cargo's branch may be linked to by
// itself: to-dos and decisions about the app that stand.
const libretaLinkable = `e.archived_at IS NULL AND e.discarded IS NULL AND e.kind IN ('pendiente', 'decision')`

// LinkCargoEntries links a branch an agent ran in its cargo to the to-dos
// and decisions about that app it wrote or changed in the same cargo.
func (s *Store) LinkCargoEntries(ctx context.Context, cargoID, orgID int64, app, branch, who string) (int64, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" || app == "" {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO ruta_ramas (entry_id, branch, created_by)
		SELECT e.id, $4::text, $5::text FROM ruta_entries e
		WHERE e.org_id = $2 AND e.app = $3 AND `+libretaLinkable+` AND `+cargoTouched+`
		ON CONFLICT DO NOTHING`, cargoID, orgOr(orgID), app, branch, who)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// LinkCargoLaunches links an entry an agent wrote or changed in its cargo
// to the branches of the entry's app it ran (ejecucion_lanzar) earlier in
// the same cargo.
func (s *Store) LinkCargoLaunches(ctx context.Context, cargoID, entryID int64, who string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO ruta_ramas (entry_id, branch, created_by)
		SELECT DISTINCT e.id, l.branch, $3::text FROM ruta_entries e
		JOIN (SELECT lower(btrim(j->>'app')) AS app, btrim(j->>'rama') AS branch FROM (
			SELECT CASE WHEN a.args LIKE '{%}' THEN a.args::jsonb END AS j FROM agent_actions a
			WHERE a.cargo_id = $1 AND a.tool = 'ejecucion_lanzar' AND a.ok) x
			WHERE j IS NOT NULL) l ON l.app = lower(e.app)
		WHERE e.id = $2 AND `+libretaLinkable+` AND l.branch <> ''
		ON CONFLICT DO NOTHING`, cargoID, entryID, who)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// MarkRutaMerged writes down that a branch was merged (its head commit,
// and when), on every link to it from the app's entries (or from entries
// about no app) that did not know it yet.
func (s *Store) MarkRutaMerged(ctx context.Context, orgID int64, app, branch, sha string, at time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE ruta_ramas r SET merged_sha = NULLIF($4, ''), merged_at = $5
		FROM ruta_entries e
		WHERE r.entry_id = e.id AND e.org_id = $1 AND (e.app = $2 OR e.app IS NULL) AND r.branch = $3 AND r.merged_at IS NULL`,
		orgOr(orgID), app, branch, sha, at)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// MarkRutaReleased writes down the release that carried the app's merged
// branches: a release passed its verification, and its run (created at
// built) came after their merge, so it holds them. Only links with no
// release yet take it: the first release that passes is the one.
func (s *Store) MarkRutaReleased(ctx context.Context, orgID int64, app string, number int64, built, at time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE ruta_ramas r SET release_number = $3, released_at = $5
		FROM ruta_entries e
		WHERE r.entry_id = e.id AND e.org_id = $1 AND e.app = $2 AND r.merged_at IS NOT NULL AND r.merged_at <= $4 AND r.release_number IS NULL`,
		orgOr(orgID), app, number, built, at)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// KeepRutaBranch writes down what the libreta worked out of an entry's
// branch (its merge, the release that carried it) where nothing was kept
// yet: what was written when it happened wins.
func (s *Store) KeepRutaBranch(ctx context.Context, entryID int64, branch string, mergedAt *time.Time, release int64, releasedAt *time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE ruta_ramas SET
			merged_at = COALESCE(merged_at, $3),
			released_at = CASE WHEN release_number IS NULL AND $4::bigint <> 0 THEN $5 ELSE released_at END,
			release_number = COALESCE(release_number, NULLIF($4::bigint, 0))
		WHERE entry_id = $1 AND branch = $2 AND ((merged_at IS NULL AND $3::timestamptz IS NOT NULL) OR (release_number IS NULL AND $4::bigint <> 0))`,
		entryID, branch, mergedAt, release, releasedAt)
	return err
}
