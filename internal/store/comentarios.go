package store

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Comentario is what a person says about an agent's work (a handed-over
// cargo, an encargo) without sending it back, or an agent's answer to it.
// The comments on one cargo or encargo are a thread, oldest first.
type Comentario struct {
	ID        int64  `json:"id"`
	OrgID     int64  `json:"orgId"`
	CargoID   *int64 `json:"cargoId,omitempty"`
	EncargoID *int64 `json:"encargoId,omitempty"`

	// AnswersTo is the person's comment an agent's answer is about.
	AnswersTo  *int64     `json:"answersTo,omitempty"`
	Author     string     `json:"author"`
	ByAgent    bool       `json:"byAgent"`
	Body       string     `json:"body"`
	CreatedAt  time.Time  `json:"createdAt"`
	AnsweredAt *time.Time `json:"answeredAt,omitempty"`
	SeenAt     *time.Time `json:"seenAt,omitempty"`

	// What it is about: the encargo's title (and app) or the cargo's
	// purpose, and the agent key whose work it is.
	About  string `json:"about"`
	App    string `json:"app,omitempty"`
	WorkOf *int64 `json:"-"`

	// New: an agent's answer to a comment of whoever reads it, not seen yet.
	New bool `json:"new,omitempty"`

	answeredAuthor string // the author of the comment it answers
}

const comentarioCols = `c.id, c.org_id, c.cargo_id, c.encargo_id, c.responde_a, c.author, c.by_agent, c.body, c.created_at, c.answered_at, c.seen_at,
	COALESCE(e.title, cg.purpose, ''), COALESCE(e.app, ''), COALESCE(e.key_id, cg.key_id), COALESCE(p.author, '')`

const comentarioFrom = ` FROM comentarios c LEFT JOIN cargos cg ON cg.id = c.cargo_id LEFT JOIN encargos e ON e.id = c.encargo_id
	LEFT JOIN comentarios p ON p.id = c.responde_a`

// scanComentario reads a comment; New is for login, its reader.
func scanComentario(row pgx.Row, login string) (Comentario, error) {
	var c Comentario
	err := row.Scan(&c.ID, &c.OrgID, &c.CargoID, &c.EncargoID, &c.AnswersTo, &c.Author, &c.ByAgent, &c.Body, &c.CreatedAt, &c.AnsweredAt, &c.SeenAt,
		&c.About, &c.App, &c.WorkOf, &c.answeredAuthor)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	c.New = c.ByAgent && c.SeenAt == nil && login != "" && c.answeredAuthor == login
	return c, err
}

func (s *Store) comentarios(ctx context.Context, login, where string, args ...any) ([]Comentario, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+comentarioCols+comentarioFrom+` WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Comentario, error) { return scanComentario(r, login) })
}

// Comentario returns one comment.
func (s *Store) Comentario(ctx context.Context, id int64) (Comentario, error) {
	return scanComentario(s.pool.QueryRow(ctx, `SELECT `+comentarioCols+comentarioFrom+` WHERE c.id = $1`, id), "")
}

// ComentarCargo is a person's comment on an agent's cargo of the
// organization (ErrNotFound when it has no such cargo).
func (s *Store) ComentarCargo(ctx context.Context, orgID, cargoID int64, author, body string) (Comentario, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO comentarios (org_id, cargo_id, author, body)
		SELECT k.org_id, cg.id, $3::text, $4::text FROM cargos cg JOIN agent_keys k ON k.id = cg.key_id WHERE cg.id = $2 AND k.org_id = $1
		RETURNING id`, orgOr(orgID), cargoID, author, body).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comentario{}, ErrNotFound
	}
	if err != nil {
		return Comentario{}, err
	}
	return s.Comentario(ctx, id)
}

// ComentarEncargo is a person's comment on an encargo of the organization,
// in any state: it does not send it back (ErrNotFound when there is none).
func (s *Store) ComentarEncargo(ctx context.Context, orgID, encargoID int64, author, body string) (Comentario, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO comentarios (org_id, encargo_id, author, body)
		SELECT e.org_id, e.id, $3::text, $4::text FROM encargos e WHERE e.id = $2 AND e.org_id = $1
		RETURNING id`, orgOr(orgID), encargoID, author, body).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comentario{}, ErrNotFound
	}
	if err != nil {
		return Comentario{}, err
	}
	return s.Comentario(ctx, id)
}

// ResponderComentario is an agent's answer to a person's comment of the
// organization: it goes in the same thread, and the comment is answered.
// ErrNotFound when there is no such comment, or it is an agent's.
func (s *Store) ResponderComentario(ctx context.Context, orgID, id, keyID int64, author, body string) (Comentario, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Comentario{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var answer int64
	err = tx.QueryRow(ctx, `INSERT INTO comentarios (org_id, cargo_id, encargo_id, responde_a, author, by_agent, key_id, body)
		SELECT p.org_id, p.cargo_id, p.encargo_id, p.id, $4::text, true, $3::bigint, $5::text FROM comentarios p WHERE p.id = $2 AND p.org_id = $1 AND NOT p.by_agent
		RETURNING id`, orgOr(orgID), id, keyID, author, body).Scan(&answer)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comentario{}, ErrNotFound
	}
	if err != nil {
		return Comentario{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE comentarios SET answered_at = now() WHERE id = $1 AND answered_at IS NULL`, id); err != nil {
		return Comentario{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Comentario{}, err
	}
	return s.Comentario(ctx, answer)
}

// ListComentarios is the thread of a cargo (cargoID) or an encargo
// (encargoID), oldest first; with neither, the organization's latest
// comments (up to limit), still oldest first. New is for login.
func (s *Store) ListComentarios(ctx context.Context, orgID, cargoID, encargoID int64, login string, limit int) ([]Comentario, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	list, err := s.comentarios(ctx, login, `c.org_id = $1 AND ($2 = 0 OR c.cargo_id = $2) AND ($3 = 0 OR c.encargo_id = $3)
		ORDER BY c.id DESC LIMIT $4`, orgOr(orgID), cargoID, encargoID, limit)
	slices.Reverse(list)
	return list, err
}

// ComentariosSinResponder are people's comments no agent has answered,
// oldest first.
func (s *Store) ComentariosSinResponder(ctx context.Context, orgID int64, limit int) ([]Comentario, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.comentarios(ctx, "", `c.org_id = $1 AND NOT c.by_agent AND c.answered_at IS NULL ORDER BY c.id LIMIT $2`, orgOr(orgID), limit)
}

// RespuestasSinVer are agents' answers to login's comments that login has
// not seen yet, oldest first: the bell shows them.
func (s *Store) RespuestasSinVer(ctx context.Context, orgID int64, login string) ([]Comentario, error) {
	return s.comentarios(ctx, login, `c.org_id = $1 AND c.by_agent AND c.seen_at IS NULL AND p.author = $2 ORDER BY c.id LIMIT 50`, orgOr(orgID), login)
}

// VerRespuestas notes that login saw these answers to their comments.
func (s *Store) VerRespuestas(ctx context.Context, orgID int64, login string, ids []int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE comentarios c SET seen_at = now() FROM comentarios p
		WHERE p.id = c.responde_a AND p.author = $2 AND c.org_id = $1 AND c.id = ANY($3) AND c.by_agent AND c.seen_at IS NULL`, orgOr(orgID), login, ids)
	return err
}
