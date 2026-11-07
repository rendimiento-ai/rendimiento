package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The states of an aprobación. Vencida is not stored: a pendiente one past
// its Vence reads as vencida.
const (
	AprobacionPendiente = "pendiente"
	AprobacionAprobada  = "aprobada"
	AprobacionRechazada = "rechazada"
	AprobacionVencida   = "vencida"
	AprobacionFallida   = "fallida"
)

// AccionRevertir is going back to an earlier release: the first change an
// agent may ask for.
const AccionRevertir = "revertir"

// ErrNoPendiente: the aprobación was already decided, or it expired.
var ErrNoPendiente = errors.New("this request is no longer waiting: it was decided already, or it expired")

// Aprobacion is a change to production an agent asked for, and what a
// person decided.
type Aprobacion struct {
	ID          int64      `json:"id"`
	OrgID       int64      `json:"orgId"`
	App         string     `json:"app"`
	Accion      string     `json:"accion"`
	Version     int64      `json:"version"`
	Motivo      string     `json:"motivo"`
	PedidaPor   string     `json:"pedidaPor"`
	KeyID       int64      `json:"-"`
	Estado      string     `json:"estado"`
	DecididaPor string     `json:"decididaPor,omitempty"`
	Comentario  string     `json:"comentario,omitempty"`
	Resultado   string     `json:"resultado,omitempty"`
	Creada      time.Time  `json:"creada"`
	Decidida    *time.Time `json:"decidida,omitempty"`
	Vence       time.Time  `json:"vence"`
}

const aprobacionCols = `id, org_id, app, accion, version, motivo, pedida_por, key_id,
	CASE WHEN estado = 'pendiente' AND vence <= now() THEN 'vencida' ELSE estado END,
	decidida_por, comentario, resultado, creada, decidida, vence`

func scanAprobacion(row pgx.Row) (Aprobacion, error) {
	var a Aprobacion
	err := row.Scan(&a.ID, &a.OrgID, &a.App, &a.Accion, &a.Version, &a.Motivo, &a.PedidaPor, &a.KeyID,
		&a.Estado, &a.DecididaPor, &a.Comentario, &a.Resultado, &a.Creada, &a.Decidida, &a.Vence)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// CrearAprobacion records a request, waiting until a.Vence.
func (s *Store) CrearAprobacion(ctx context.Context, a Aprobacion) (Aprobacion, error) {
	return scanAprobacion(s.pool.QueryRow(ctx, `INSERT INTO aprobaciones (org_id, app, accion, version, motivo, pedida_por, key_id, vence)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+aprobacionCols,
		a.OrgID, a.App, a.Accion, a.Version, a.Motivo, a.PedidaPor, a.KeyID, a.Vence))
}

// GetAprobacion is one request, of any organization (callers check).
func (s *Store) GetAprobacion(ctx context.Context, id int64) (Aprobacion, error) {
	return scanAprobacion(s.pool.QueryRow(ctx, `SELECT `+aprobacionCols+` FROM aprobaciones WHERE id = $1`, id))
}

// ListarAprobaciones is an organization's requests, newest first: only
// those still waiting, or the last 50.
func (s *Store) ListarAprobaciones(ctx context.Context, orgID int64, pendientes bool) ([]Aprobacion, error) {
	q := `SELECT ` + aprobacionCols + ` FROM aprobaciones WHERE org_id = $1`
	if pendientes {
		q += ` AND estado = 'pendiente' AND vence > now()`
	}
	rows, err := s.pool.Query(ctx, q+` ORDER BY id DESC LIMIT 50`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Aprobacion{}
	for rows.Next() {
		a, err := scanAprobacion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DecidirAprobacion approves or rejects a waiting request of the
// organization: ErrNotFound when it is not the organization's, and
// ErrNoPendiente when it was decided already or expired. Only one decision
// wins when two people decide at once.
func (s *Store) DecidirAprobacion(ctx context.Context, orgID, id int64, aprobar bool, por, comentario string) (Aprobacion, error) {
	estado := AprobacionRechazada
	if aprobar {
		estado = AprobacionAprobada
	}
	a, err := scanAprobacion(s.pool.QueryRow(ctx, `UPDATE aprobaciones SET estado = $3, decidida_por = $4, comentario = $5, decidida = now()
		WHERE id = $1 AND org_id = $2 AND estado = 'pendiente' AND vence > now() RETURNING `+aprobacionCols,
		id, orgID, estado, por, comentario))
	if errors.Is(err, ErrNotFound) {
		if prev, gerr := s.GetAprobacion(ctx, id); gerr == nil && prev.OrgID == orgID {
			return prev, ErrNoPendiente
		}
	}
	return a, err
}

// TerminarAprobacion records how an approved request went: done, with what
// came of it, or failed, with why.
func (s *Store) TerminarAprobacion(ctx context.Context, id int64, fallo bool, resultado string) error {
	estado := AprobacionAprobada
	if fallo {
		estado = AprobacionFallida
	}
	_, err := s.pool.Exec(ctx, `UPDATE aprobaciones SET estado = $2, resultado = $3 WHERE id = $1`, id, estado, resultado)
	return err
}
