package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Who wrote a message to the helper (el ayudante, decision 188).
const (
	AyudantePersona   = "persona"  // the person's question
	AyudanteRespuesta = "ayudante" // the helper's answer
)

// AyudanteGuarda is how long the helper keeps a person's conversation.
const AyudanteGuarda = 7 * 24 * time.Hour

// AyudanteEnlace is a link the helper gave: its words, where it goes (a page
// of rendimiento, "/apps/x/runs/1", or the book's page, "libro:guide/ai/"),
// and its kind ("pagina" or "libro").
type AyudanteEnlace struct {
	Texto string `json:"texto"`
	Href  string `json:"href"`
	Tipo  string `json:"tipo"`
}

// AyudanteMensaje is one message of a person's conversation with the helper.
type AyudanteMensaje struct {
	ID         int64            `json:"id"`
	OrgID      int64            `json:"-"`
	Who        string           `json:"-"`
	Kind       string           `json:"kind"`
	Body       string           `json:"body"`
	Pagina     string           `json:"pagina,omitempty"`
	Enlaces    []AyudanteEnlace `json:"enlaces"`
	Leido      []string         `json:"leido"`
	Provider   string           `json:"provider,omitempty"`
	Model      string           `json:"model,omitempty"`
	CostMicros int64            `json:"costMicros"`
	CreatedAt  time.Time        `json:"createdAt"`
}

const ayudanteCols = `id, org_id, who, kind, body, pagina, enlaces, leido, provider, model, cost_micros, created_at`

func scanAyudante(row pgx.Row) (AyudanteMensaje, error) {
	var m AyudanteMensaje
	err := row.Scan(&m.ID, &m.OrgID, &m.Who, &m.Kind, &m.Body, &m.Pagina, &m.Enlaces, &m.Leido, &m.Provider, &m.Model, &m.CostMicros, &m.CreatedAt)
	if m.Enlaces == nil {
		m.Enlaces = []AyudanteEnlace{}
	}
	if m.Leido == nil {
		m.Leido = []string{}
	}
	return m, err
}

// AgregarAyudante keeps one message of a person's conversation with the helper.
func (s *Store) AgregarAyudante(ctx context.Context, m AyudanteMensaje) (AyudanteMensaje, error) {
	if m.Enlaces == nil {
		m.Enlaces = []AyudanteEnlace{}
	}
	if m.Leido == nil {
		m.Leido = []string{}
	}
	return scanAyudante(s.pool.QueryRow(ctx, `INSERT INTO ayudante_mensajes (org_id, who, kind, body, pagina, enlaces, leido, provider, model, cost_micros)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING `+ayudanteCols,
		m.OrgID, m.Who, m.Kind, m.Body, m.Pagina, m.Enlaces, m.Leido, m.Provider, m.Model, m.CostMicros))
}

// AyudanteDe is a person's conversation with the helper since a moment,
// oldest first, at most the last 200 messages. Only theirs: nobody else's.
func (s *Store) AyudanteDe(ctx context.Context, orgID int64, who string, since time.Time) ([]AyudanteMensaje, error) {
	rows, err := s.pool.Query(ctx, `SELECT * FROM (SELECT `+ayudanteCols+` FROM ayudante_mensajes
		WHERE org_id = $1 AND who = $2 AND created_at >= $3 ORDER BY id DESC LIMIT 200) m ORDER BY id`, orgID, who, since)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (AyudanteMensaje, error) { return scanAyudante(r) })
	if out == nil {
		out = []AyudanteMensaje{}
	}
	return out, err
}

// PodarAyudante deletes the helper's messages older than a moment, of
// everyone, and says how many went.
func (s *Store) PodarAyudante(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ayudante_mensajes WHERE created_at < $1`, before)
	return tag.RowsAffected(), err
}
