package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// EncargoAsking is an encargo the coding agent stopped on to ask the
// person something (Ruta 178): nobody works on it and nothing is spent
// until a person answers (it goes back to the agent's queue, to resume on
// the same branch), passes it to their own agent, or AskWait goes by.
const EncargoAsking = "asking"

// AskWait is how long a question waits for an answer before the encargo
// goes to the person's own agent with it.
const AskWait = 72 * time.Hour

// EncargoPregunta is a question the coding agent asked about an encargo:
// what it asks, the options it offers (none, or 2 to 4), what it left
// itself to go on from and what it had spent; then the person's answer.
type EncargoPregunta struct {
	ID          int64      `json:"id"`
	EncargoID   int64      `json:"encargoId"`
	OrgID       int64      `json:"orgId,omitempty"`
	Question    string     `json:"question"`
	Options     []string   `json:"options"`
	Notes       string     `json:"notes,omitempty"`
	SpentMicros int64      `json:"spentMicros,omitempty"`
	AskedBy     string     `json:"askedBy,omitempty"`
	AskedAt     time.Time  `json:"askedAt"`
	Answer      string     `json:"answer,omitempty"`
	AnsweredBy  string     `json:"answeredBy,omitempty"`
	AnsweredAt  *time.Time `json:"answeredAt,omitempty"`
	// PedidoID is the request for a permission this question is (Ruta
	// 184): «Para usted» shows it as one, with the grant's options.
	PedidoID *int64 `json:"pedidoId,omitempty"`
}

// openQuestionCol is an encargo's open question, as JSON (NULL when it
// asks nothing): the one column encargoCols adds for it.
const openQuestionCol = `(SELECT json_build_object('id', q.id, 'encargoId', q.encargo_id, 'question', q.question, 'options', q.options, 'askedAt', q.asked_at, 'pedidoId', q.pedido_id)
		FROM encargo_preguntas q WHERE q.encargo_id = e.id AND e.status = 'asking' AND q.answered_at IS NULL AND q.passed_at IS NULL ORDER BY q.id DESC LIMIT 1)`

const preguntaCols = `id, encargo_id, org_id, question, options, notes, spent_micros, asked_by, asked_at, answer, answered_by, answered_at, pedido_id`

func scanPregunta(r pgx.Row) (EncargoPregunta, error) {
	var p EncargoPregunta
	err := r.Scan(&p.ID, &p.EncargoID, &p.OrgID, &p.Question, &p.Options, &p.Notes, &p.SpentMicros, &p.AskedBy, &p.AskedAt, &p.Answer, &p.AnsweredBy, &p.AnsweredAt, &p.PedidoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if p.Options == nil {
		p.Options = []string{}
	}
	return p, err
}

// AskEncargo is the agent that has an encargo stopping to ask the person:
// it is asking, with nobody working on it, until answered.
func (s *Store) AskEncargo(ctx context.Context, id, keyID int64, who string, q EncargoPregunta) (Encargo, error) {
	if q.Options == nil {
		q.Options = []string{}
	}
	return s.change(ctx, id, who, "asked", q.Question, `WITH u AS (
			UPDATE encargos SET status = 'asking', waiting_run = NULL, cargo_id = NULL, doing = '', stop_asked = false, updated_at = now()
			WHERE id = $1 AND key_id = $2 AND status IN ('taken', 'waiting') RETURNING id, org_id)
		INSERT INTO encargo_preguntas (encargo_id, org_id, question, options, notes, spent_micros, asked_by, pedido_id)
		SELECT u.id, u.org_id, $3, $4, $5, $6, $7, $8 FROM u`,
		id, keyID, q.Question, q.Options, q.Notes, q.SpentMicros, who, q.PedidoID)
}

// AnswerEncargo is a person's answer to the question an encargo of their
// organization asks: it goes back to the coding agent's queue, to resume
// with the answer (ResumeAnswer). ErrEncargoState when it asks nothing.
func (s *Store) AnswerEncargo(ctx context.Context, id, orgID int64, who, answer string) (Encargo, error) {
	return s.change(ctx, id, who, "answered", answer, `WITH q AS (
			UPDATE encargo_preguntas SET answer = $3, answered_by = $4, answered_at = now()
			WHERE encargo_id = $1 AND answered_at IS NULL AND passed_at IS NULL
				AND EXISTS (SELECT 1 FROM encargos WHERE id = $1 AND org_id = $2 AND status = 'asking') RETURNING encargo_id)
		UPDATE encargos SET status = 'pending', key_id = NULL, cargo_id = NULL, do_now = true, route = 'platform', updated_at = now()
		WHERE id IN (SELECT encargo_id FROM q)`, id, orgOr(orgID), answer, who)
}

// PassAskedEncargo gives an encargo that asks to the person's own agent,
// with the question in note: pending, for their agent, out of the coding
// agent's queue (as GiveBackEncargo). who is the person, or the coding
// agent when nobody answered in AskWait.
func (s *Store) PassAskedEncargo(ctx context.Context, id, orgID int64, who, note string) (Encargo, error) {
	return s.change(ctx, id, who, "released", note, `WITH q AS (
			UPDATE encargo_preguntas SET passed_by = $3, passed_at = now()
			WHERE encargo_id = $1 AND answered_at IS NULL AND passed_at IS NULL
				AND EXISTS (SELECT 1 FROM encargos WHERE id = $1 AND org_id = $2 AND status = 'asking') RETURNING encargo_id)
		UPDATE encargos SET status = 'pending', key_id = NULL, cargo_id = NULL, waiting_run = NULL, do_now = false, route = 'local', doing = '',
			stop_asked = false, queue_pos = NULL, updated_at = now()
		WHERE id IN (SELECT encargo_id FROM q)`, id, orgOr(orgID), who)
}

// OpenQuestion is the question an encargo asks now (ErrNotFound when none).
func (s *Store) OpenQuestion(ctx context.Context, id int64) (EncargoPregunta, error) {
	return scanPregunta(s.pool.QueryRow(ctx, `SELECT `+preguntaCols+` FROM encargo_preguntas
		WHERE encargo_id = $1 AND answered_at IS NULL AND passed_at IS NULL ORDER BY id DESC LIMIT 1`, id))
}

// ResumeAnswer is the answered question the coding agent resumes an
// encargo with, once: it is marked resumed (ErrNotFound when there is
// none, as for an encargo it never asked about).
func (s *Store) ResumeAnswer(ctx context.Context, id int64) (EncargoPregunta, error) {
	return scanPregunta(s.pool.QueryRow(ctx, `UPDATE encargo_preguntas SET resumed_at = now()
		WHERE id = (SELECT id FROM encargo_preguntas WHERE encargo_id = $1 AND answered_at IS NOT NULL AND resumed_at IS NULL ORDER BY id DESC LIMIT 1)
		RETURNING `+preguntaCols, id))
}

// UnansweredSince lists the open questions asked before `before`, of
// encargos still asking, in every organization: the ones to pass to the
// person's own agent.
func (s *Store) UnansweredSince(ctx context.Context, before time.Time) ([]EncargoPregunta, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+preguntaCols+` FROM encargo_preguntas q
		WHERE q.answered_at IS NULL AND q.passed_at IS NULL AND q.asked_at < $1
			AND EXISTS (SELECT 1 FROM encargos e WHERE e.id = q.encargo_id AND e.status = 'asking')
		ORDER BY q.id LIMIT 100`, before)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (EncargoPregunta, error) { return scanPregunta(r) })
}
