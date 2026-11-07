package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Who did an action in the bitácora.
const (
	QuienPersona    = "persona"
	QuienAgente     = "agente"
	QuienPlataforma = "plataforma"
)

// Bitacora is one action in the log: who did what, on what, through where,
// and how it ended. Each organization's rows form a chain: Huella is the
// hash of the row and of the previous row's Huella, so changing or removing
// a row breaks every one after it (VerificarBitacora).
type Bitacora struct {
	ID        int64             `json:"id"`
	OrgID     int64             `json:"orgId"`
	Cuando    time.Time         `json:"cuando"`
	QuienTipo string            `json:"quienTipo"`
	Quien     string            `json:"quien"`
	Por       string            `json:"por"`
	Accion    string            `json:"accion"`
	Sobre     map[string]string `json:"sobre"`
	Resultado string            `json:"resultado"`
	Detalle   map[string]any    `json:"detalle"`
	// HuellaAnterior is the previous row's Huella ("" for the first).
	HuellaAnterior string `json:"huellaAnterior"`
	Huella         string `json:"huella"`
}

// huella is the row's hash: sha256 over the previous hash and the row's
// fields as canonical JSON (Go sorts map keys). Times are UTC, to the
// microsecond Postgres keeps.
func (b Bitacora) huella() (string, error) {
	sobre, detalle := b.Sobre, b.Detalle
	if sobre == nil {
		sobre = map[string]string{}
	}
	if detalle == nil {
		detalle = map[string]any{}
	}
	campos, err := json.Marshal([]any{b.OrgID, b.Cuando.UTC().Format(time.RFC3339Nano), b.QuienTipo, b.Quien, b.Por, b.Accion, sobre, b.Resultado, detalle})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte(b.HuellaAnterior+"\n"), campos...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// AnotarBitacora adds an action to its organization's chain: it takes the
// organization's lock, reads the last hash, and writes the row with its own.
func (s *Store) AnotarBitacora(ctx context.Context, b Bitacora) (Bitacora, error) {
	if b.Cuando.IsZero() {
		b.Cuando = time.Now()
	}
	b.Cuando = b.Cuando.UTC().Truncate(time.Microsecond)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('bitacora'), $1::int)`, b.OrgID); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT huella FROM bitacora WHERE org_id = $1 ORDER BY id DESC LIMIT 1`, b.OrgID).Scan(&b.HuellaAnterior)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if b.Huella, err = b.huella(); err != nil {
			return err
		}
		sobre, _ := json.Marshal(nonNil(b.Sobre))
		detalle, _ := json.Marshal(nonNilAny(b.Detalle))
		return tx.QueryRow(ctx, `INSERT INTO bitacora (org_id, cuando, quien_tipo, quien, por, accion, sobre, resultado, detalle, huella_anterior, huella)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
			b.OrgID, b.Cuando, b.QuienTipo, b.Quien, b.Por, b.Accion, sobre, b.Resultado, detalle, b.HuellaAnterior, b.Huella).Scan(&b.ID)
	})
	return b, err
}

func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilAny(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// FiltroBitacora narrows a listing: by who, by action (a substring), by app
// (sobre.app), and before an id (to page back). Empty fields don't filter.
type FiltroBitacora struct {
	Quien, Accion, App string
	Antes              int64
	Limite             int
}

// ListarBitacora is an organization's actions, newest first.
func (s *Store) ListarBitacora(ctx context.Context, orgID int64, f FiltroBitacora) ([]Bitacora, error) {
	if f.Limite <= 0 || f.Limite > 500 {
		f.Limite = 100
	}
	q := `SELECT id, org_id, cuando, quien_tipo, quien, por, accion, sobre, resultado, detalle, huella_anterior, huella
		FROM bitacora WHERE org_id = $1`
	args := []any{orgID}
	add := func(cond string, v any) {
		args = append(args, v)
		q += fmt.Sprintf(" AND "+cond, len(args))
	}
	if f.Quien != "" {
		add("quien = $%d", f.Quien)
	}
	if f.Accion != "" {
		add("accion ILIKE '%%' || $%d || '%%'", f.Accion)
	}
	if f.App != "" {
		add("sobre->>'app' = $%d", f.App)
	}
	if f.Antes > 0 {
		add("id < $%d", f.Antes)
	}
	args = append(args, f.Limite)
	q += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Bitacora{}
	for rows.Next() {
		b, err := scanBitacora(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func scanBitacora(rows pgx.Rows) (Bitacora, error) {
	var b Bitacora
	var sobre, detalle []byte
	if err := rows.Scan(&b.ID, &b.OrgID, &b.Cuando, &b.QuienTipo, &b.Quien, &b.Por, &b.Accion, &sobre, &b.Resultado, &detalle, &b.HuellaAnterior, &b.Huella); err != nil {
		return b, err
	}
	_ = json.Unmarshal(sobre, &b.Sobre)
	_ = json.Unmarshal(detalle, &b.Detalle)
	b.Cuando = b.Cuando.UTC()
	return b, nil
}

// VerificacionBitacora is the result of checking a chain: how many rows were
// checked, and the first that does not hold (0 when all do), with why.
type VerificacionBitacora struct {
	Revisados int64  `json:"revisados"`
	RotaEn    int64  `json:"rotaEn,omitempty"`
	Motivo    string `json:"motivo,omitempty"`
}

// VerificarBitacora walks an organization's chain from its first row and
// recomputes every hash: a row changed in the database no longer matches
// its hash, and a row removed leaves the next one pointing at a hash that
// is not the one before it.
func (s *Store) VerificarBitacora(ctx context.Context, orgID int64) (VerificacionBitacora, error) {
	var v VerificacionBitacora
	rows, err := s.pool.Query(ctx, `SELECT id, org_id, cuando, quien_tipo, quien, por, accion, sobre, resultado, detalle, huella_anterior, huella
		FROM bitacora WHERE org_id = $1 ORDER BY id`, orgID)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	anterior := ""
	for rows.Next() {
		b, err := scanBitacora(rows)
		if err != nil {
			return v, err
		}
		v.Revisados++
		guardada := b.Huella
		switch calc, err := b.huella(); {
		case err != nil:
			return v, err
		case b.HuellaAnterior != anterior:
			v.RotaEn, v.Motivo = b.ID, "the row before it was changed or removed"
		case calc != guardada:
			v.RotaEn, v.Motivo = b.ID, "this row was changed"
		}
		if v.RotaEn != 0 {
			return v, nil
		}
		anterior = guardada
	}
	return v, rows.Err()
}
