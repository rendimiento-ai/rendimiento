package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// encargoBranch reads the encargo a branch was made for: the coding agent
// names its branches <kind>/encargo-<n>-<words of the title> (before,
// encargo-<n> and encargo-<n>-<words>).
var encargoBranch = regexp.MustCompile(`^(?:[a-z][a-z0-9-]*/)?encargo-(\d+)(?:-|$)`)

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

// RamaDeEncargo names an encargo's branch by its kind, its number and the
// first words of its title, so a list of branches says what each one is
// and what it is about: fix/ for an encargo about a failed run (or a
// conflict), feature/ for anything else. «Cambiar la configuración desde
// Entorno» (25) is feature/encargo-25-cambiar-la-configuracion-desde-entorno.
func RamaDeEncargo(e Encargo) string {
	kind := "feature/"
	if e.RunID != nil {
		kind = "fix/"
	}
	id, title := e.ID, e.Title
	name := kind + fmt.Sprintf("encargo-%d", id)
	var words []string
	n := 0
	for _, w := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return !isSlugRune(r) }) {
		w = strings.Map(func(r rune) rune {
			if f, ok := folded[r]; ok {
				return f
			}
			return r
		}, w)
		if n+len(w)+1 > maxBranchWords {
			break
		}
		words, n = append(words, w), n+len(w)+1
	}
	if len(words) > 0 {
		name += "-" + strings.Join(words, "-")
	}
	return name
}

// maxBranchWords is how long the title part of a branch name may get.
const maxBranchWords = 40

// folded writes Spanish letters the way a branch name can hold them.
var folded = map[rune]rune{'á': 'a', 'é': 'e', 'í': 'i', 'ó': 'o', 'ú': 'u', 'ü': 'u', 'ñ': 'n'}

func isSlugRune(r rune) bool {
	_, ok := folded[r]
	return ok || r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
}
