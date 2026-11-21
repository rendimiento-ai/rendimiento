package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Who wrote a message of a conversation about an entry (a plática).
const (
	PlaticaPersona = "persona" // a person, by their login
	PlaticaIA      = "ia"      // the AI the conversation chose, by its provider and model
	PlaticaAgente  = "agente"  // a person's own agent, over MCP, by its key's name
	PlaticaAviso   = "aviso"   // rendimiento itself: why the AI did not answer
)

// PlaticaMiAgente is the conversation's choice of the person's own agent
// (Claude Code, Codex) to answer, over MCP, instead of one of the AI
// gateway's providers (AILocal, AIOwn, AIPlatform).
const PlaticaMiAgente = "agente"

// DefaultPlaticaCap is what one conversation may spend on paid models
// before it asks a person whether to go on: 50 cents.
const DefaultPlaticaCap = 500_000

// platicaThinking is how long a turn of the AI is held: after it, a turn
// that never ended (the process restarted) is free again.
const platicaThinking = 5 * time.Minute

// Platica is the conversation about one Ruta entry: who answers it, «la
// idea hasta ahora» (the summary the AI keeps and people may correct),
// what its AI cost, and what came out of it (a decision, encargos).
type Platica struct {
	ID      int64 `json:"id"`
	OrgID   int64 `json:"orgId"`
	EntryID int64 `json:"entryId"`
	// IA is who answers: local, own, platform or agente ("" until someone
	// chooses: the organization's provider).
	IA     string     `json:"ia"`
	Idea   string     `json:"idea"`
	IdeaBy string     `json:"ideaBy,omitempty"`
	IdeaAt *time.Time `json:"ideaAt,omitempty"`
	// SpentMicros is what its AI cost; AllowedMicros what people allowed
	// past the cap per conversation.
	SpentMicros   int64 `json:"spentMicros"`
	AllowedMicros int64 `json:"allowedMicros"`
	// Thinking: the AI is writing an answer now.
	Thinking   bool      `json:"thinking"`
	DecisionID *int64    `json:"decisionId,omitempty"`
	Encargos   []int64   `json:"encargos"`
	Messages   int       `json:"messages"`
	CreatedBy  string    `json:"createdBy"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// PlaticaMensaje is one message, signed: Author is the person's login, the
// AI's model or the agent key's name, by Kind.
type PlaticaMensaje struct {
	ID        int64  `json:"id"`
	PlaticaID int64  `json:"platicaId"`
	Author    string `json:"author"`
	Kind      string `json:"tipo"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	KeyID     *int64 `json:"keyId,omitempty"`
	Body      string `json:"body"`
	// Read is what the AI read to answer: "ruta:92", "libro:guide/ai/|AI".
	Read       []string  `json:"read"`
	CostMicros int64     `json:"costMicros"`
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

const platicaCols = `p.id, p.org_id, p.entry_id, p.ia, p.idea, p.idea_by, p.idea_at, p.spent_micros, p.allowed_micros,
	COALESCE(p.thinking_since > now() - interval '5 minutes', false), p.decision_id, p.encargos,
	(SELECT count(*) FROM platica_mensajes m WHERE m.platica_id = p.id), p.created_by, p.created_at, p.updated_at`

// platicaSelect reads conversations; Thinking holds for platicaThinking.
func platicaSelect() string { return `SELECT ` + platicaCols + ` FROM platicas p` }

func scanPlatica(row pgx.Row) (Platica, error) {
	var p Platica
	err := row.Scan(&p.ID, &p.OrgID, &p.EntryID, &p.IA, &p.Idea, &p.IdeaBy, &p.IdeaAt, &p.SpentMicros, &p.AllowedMicros,
		&p.Thinking, &p.DecisionID, &p.Encargos, &p.Messages, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if p.Encargos == nil {
		p.Encargos = []int64{}
	}
	return p, err
}

// PlaticaDe is the conversation about an entry (ErrNotFound: nobody
// started one yet).
func (s *Store) PlaticaDe(ctx context.Context, entryID int64) (Platica, error) {
	return scanPlatica(s.pool.QueryRow(ctx, platicaSelect()+` WHERE p.entry_id = $1`, entryID))
}

// PlaticaPorID is one conversation.
func (s *Store) PlaticaPorID(ctx context.Context, id int64) (Platica, error) {
	return scanPlatica(s.pool.QueryRow(ctx, platicaSelect()+` WHERE p.id = $1`, id))
}

// AbrirPlatica is the conversation about an entry, started now by who
// when there was none.
func (s *Store) AbrirPlatica(ctx context.Context, orgID, entryID int64, who string) (Platica, error) {
	if _, err := s.pool.Exec(ctx, `INSERT INTO platicas (org_id, entry_id, created_by) VALUES ($1, $2, $3) ON CONFLICT (entry_id) DO NOTHING`,
		orgOr(orgID), entryID, who); err != nil {
		return Platica{}, err
	}
	return s.PlaticaDe(ctx, entryID)
}

const mensajeCols = `id, platica_id, author, kind, provider, model, key_id, body, read, cost_micros, note, created_at`

func scanMensaje(row pgx.Row) (PlaticaMensaje, error) {
	var m PlaticaMensaje
	err := row.Scan(&m.ID, &m.PlaticaID, &m.Author, &m.Kind, &m.Provider, &m.Model, &m.KeyID, &m.Body, &m.Read, &m.CostMicros, &m.Note, &m.CreatedAt)
	if m.Read == nil {
		m.Read = []string{}
	}
	return m, err
}

// MensajesDePlatica are a conversation's messages after the one numbered
// after (0: from the first), oldest first, at most limit (0: 500).
func (s *Store) MensajesDePlatica(ctx context.Context, platicaID, after int64, limit int) ([]PlaticaMensaje, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `SELECT `+mensajeCols+` FROM platica_mensajes WHERE platica_id = $1 AND id > $2 ORDER BY id LIMIT $3`, platicaID, after, limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PlaticaMensaje, error) { return scanMensaje(r) })
	if out == nil {
		out = []PlaticaMensaje{}
	}
	return out, err
}

// AgregarMensaje keeps a signed message and adds what it cost to the
// conversation's spending, at once.
func (s *Store) AgregarMensaje(ctx context.Context, m PlaticaMensaje) (PlaticaMensaje, error) {
	if m.Read == nil {
		m.Read = []string{}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return m, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, err := scanMensaje(tx.QueryRow(ctx, `INSERT INTO platica_mensajes (platica_id, author, kind, provider, model, key_id, body, read, cost_micros, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING `+mensajeCols,
		m.PlaticaID, m.Author, m.Kind, m.Provider, m.Model, m.KeyID, m.Body, m.Read, m.CostMicros, m.Note))
	if err != nil {
		return out, err
	}
	tag, err := tx.Exec(ctx, `UPDATE platicas SET spent_micros = spent_micros + $2, updated_at = now() WHERE id = $1`, m.PlaticaID, m.CostMicros)
	if err != nil {
		return out, err
	}
	if tag.RowsAffected() == 0 {
		return out, ErrNotFound
	}
	return out, tx.Commit(ctx)
}

// GastoPlatica adds what a call about a conversation cost that left no
// message (the proposal of encargos).
func (s *Store) GastoPlatica(ctx context.Context, id, micros int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE platicas SET spent_micros = spent_micros + $2, updated_at = now() WHERE id = $1`, id, micros)
	return err
}

// ElegirIAPlatica is who answers a conversation from now on.
func (s *Store) ElegirIAPlatica(ctx context.Context, id int64, iaChoice string) error {
	return s.platicaSet(ctx, `UPDATE platicas SET ia = $2, updated_at = now() WHERE id = $1`, id, iaChoice)
}

// GuardarIdea keeps «la idea hasta ahora», by who wrote it (a person, or
// the AI's model).
func (s *Store) GuardarIdea(ctx context.Context, id int64, idea, who string) error {
	return s.platicaSet(ctx, `UPDATE platicas SET idea = $2, idea_by = $3, idea_at = now(), updated_at = now() WHERE id = $1`, id, idea, who)
}

// PermitirMasPlatica lets a conversation spend micros more than its cap:
// a person said to go on.
func (s *Store) PermitirMasPlatica(ctx context.Context, id, micros int64) error {
	return s.platicaSet(ctx, `UPDATE platicas SET allowed_micros = allowed_micros + $2, updated_at = now() WHERE id = $1`, id, micros)
}

// PlaticaHecha notes what came out of a conversation: the decision entry
// it was saved as (nil: none) and the encargos made of it.
func (s *Store) PlaticaHecha(ctx context.Context, id int64, decision *int64, encargos []int64) error {
	if encargos == nil {
		encargos = []int64{}
	}
	return s.platicaSet(ctx, `UPDATE platicas SET decision_id = COALESCE($2, decision_id), encargos = encargos || $3::bigint[], updated_at = now() WHERE id = $1`,
		id, decision, encargos)
}

func (s *Store) platicaSet(ctx context.Context, sql string, args ...any) error {
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TomarTurnoPlatica holds the AI's turn on a conversation: false when
// another answer is being written (that one answers what came meanwhile).
func (s *Store) TomarTurnoPlatica(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE platicas SET thinking_since = now() WHERE id = $1
		AND (thinking_since IS NULL OR thinking_since < now() - make_interval(secs => $2))`, id, platicaThinking.Seconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// SoltarTurnoPlatica ends the AI's turn.
func (s *Store) SoltarTurnoPlatica(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE platicas SET thinking_since = NULL WHERE id = $1`, id)
	return err
}

// PlaticasEsperando are the organization's conversations that chose the
// person's own agent and whose last message is a person's: waiting for an
// agent to answer over MCP. Oldest wait first.
func (s *Store) PlaticasEsperando(ctx context.Context, orgID int64, limit int) ([]Platica, error) {
	rows, err := s.pool.Query(ctx, platicaSelect()+`
		WHERE p.org_id = $1 AND p.ia = $2
		  AND (SELECT m.kind FROM platica_mensajes m WHERE m.platica_id = p.id ORDER BY m.id DESC LIMIT 1) = $3
		ORDER BY p.updated_at LIMIT $4`, orgOr(orgID), PlaticaMiAgente, PlaticaPersona, limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Platica, error) { return scanPlatica(r) })
	if out == nil {
		out = []Platica{}
	}
	return out, err
}

// PlaticaTope is what one conversation of the organization may spend on
// paid models before it asks whether to go on.
func (s *Store) PlaticaTope(ctx context.Context, orgID int64) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT platica_cap_micros FROM ai_settings WHERE org_id = $1`, orgOr(orgID)).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultPlaticaCap, nil
	}
	return n, err
}

// SetPlaticaTope is what one conversation may spend before it asks.
func (s *Store) SetPlaticaTope(ctx context.Context, orgID, micros int64, by string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO ai_settings (org_id, platica_cap_micros, updated_by, updated_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (org_id) DO UPDATE SET platica_cap_micros = $2, updated_by = $3, updated_at = now()`, orgOr(orgID), micros, by)
	return err
}

// PlaticaLlegoAlTope says whether a conversation spent its cap and what
// people allowed past it.
func PlaticaLlegoAlTope(p Platica, cap int64) bool {
	return p.SpentMicros >= cap+p.AllowedMicros
}
