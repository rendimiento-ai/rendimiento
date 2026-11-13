package store

import (
	"context"
	"errors"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// encargoBranch reads the encargo a branch was made for: the coding agent
// names its branches encargo-<n> or encargo-<n>-<words of the title>.
var encargoBranch = regexp.MustCompile(`^encargo-(\d+)(?:-|$)`)

// EncargoOfBranch is the number of the encargo a branch was made for, or 0
// when it is not an encargo's branch (a person's).
func EncargoOfBranch(branch string) int64 {
	m := encargoBranch.FindStringSubmatch(branch)
	if m == nil {
		return 0
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	return n
}

// OpenEncargoOnBranch is the open encargo about a branch of an app, so a
// second one is not made for it: the encargo the branch was made for while
// it is open, else the newest about one of the branch's runs. ErrNotFound
// when there is none.
func (s *Store) OpenEncargoOnBranch(ctx context.Context, app *App, branch string) (Encargo, error) {
	return scanEncargo(s.pool.QueryRow(ctx, `SELECT `+encargoCols+encargoFrom+`
		WHERE e.org_id = $1 AND e.app = $2 AND e.status NOT IN ('reviewed', 'cancelled')
		  AND (e.id = $4 OR e.run_id IN (SELECT id FROM runs WHERE app_id = $3 AND branch = $5))
		ORDER BY (e.id = $4) DESC, e.id DESC LIMIT 1`, app.OrgID, app.Name, app.ID, EncargoOfBranch(branch), branch))
}

// NoteEncargo adds a note to an open encargo's detail (a new failure on
// its branch, say), so whoever has it reads it there. A delivered one goes
// back to pending with the note as its comment, as when a person sends it
// back: what was delivered no longer passes. It keeps its route, so it is
// offered again to the kind of agent that did it.
func (s *Store) NoteEncargo(ctx context.Context, id int64, who, note string) (Encargo, error) {
	return s.change(ctx, id, who, "noted", note, `UPDATE encargos SET
		body = CASE WHEN body = '' THEN $2 ELSE body || E'\n\n' || $2 END,
		comment = CASE WHEN status = 'delivered' THEN $2 ELSE comment END,
		key_id = CASE WHEN status = 'delivered' THEN NULL ELSE key_id END,
		cargo_id = CASE WHEN status = 'delivered' THEN NULL ELSE cargo_id END,
		status = CASE WHEN status = 'delivered' THEN 'pending' ELSE status END,
		updated_at = now() WHERE id = $1 AND status NOT IN ('reviewed', 'cancelled')`, id, note)
}

// RunBranch is the branch of a run and its app's default branch.
func (s *Store) RunBranch(ctx context.Context, runID int64) (branch, defaultBranch string, err error) {
	err = s.pool.QueryRow(ctx, `SELECT r.branch, a.default_branch FROM runs r JOIN apps a ON a.id = r.app_id WHERE r.id = $1`, runID).
		Scan(&branch, &defaultBranch)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	return branch, defaultBranch, nil
}
