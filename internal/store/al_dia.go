package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- Poner al día (decision 191) ----
//
// Each run of a dependency update's pull request (Renovate's, Dependabot's)
// gets a report: what would fail, what would change and how much, the risk
// and what putting the app up to date would cost. «Poner la app al día»
// opens the encargo that does it, on the same branch.

// The states of a report.
const (
	AlDiaPendiente  = "pendiente"   // asked for when its run ended
	AlDiaListo      = "listo"       // written: something to change
	AlDiaSinCambios = "sin_cambios" // written: the update needs nothing of the app
	AlDiaFallo      = "fallo"       // it could not be written (nota says why)
)

// The risks a report gives.
const (
	RiesgoBajo  = "bajo"
	RiesgoMedio = "medio"
	RiesgoAlto  = "alto"
)

// Who puts an app up to date, and when a report is made.
const (
	AlDiaQuienPlataforma = "platform" // the platform's coding agent
	AlDiaQuienPersona    = "local"    // the person's own agent (an encargo for it)
	AlDiaCuandoToda      = "toda"     // every update's run
	AlDiaCuandoFalla     = "falla"    // only a run that failed
)

// AlDiaTopeDefault is the most a report's work may cost for it to start
// without asking, when that is on: 25 cents.
const AlDiaTopeDefault = 250_000

// AlDiaCambio is one file a report says would change, and about how many
// lines would be added and taken out.
type AlDiaCambio struct {
	Archivo string `json:"archivo"`
	Mas     int    `json:"mas"`
	Menos   int    `json:"menos"`
}

// AlDia is the report on one run of a dependency update's pull request.
type AlDia struct {
	ID            int64         `json:"id"`
	OrgID         int64         `json:"-"`
	AppID         int64         `json:"-"`
	Pull          int           `json:"pull"`
	Titulo        string        `json:"titulo"`
	Notas         string        `json:"-"` // the pull request's text, for the model
	Branch        string        `json:"branch"`
	SHA           string        `json:"sha"`
	RunID         int64         `json:"runId"`
	RunFailed     bool          `json:"runFailed"`
	Estado        string        `json:"estado"`
	Falla         []string      `json:"falla"`
	Cambia        []AlDiaCambio `json:"cambia"`
	Riesgo        string        `json:"riesgo,omitempty"`
	Dificultad    int           `json:"dificultad,omitempty"`
	CostoMicros   int64         `json:"costoMicros"`
	Resumen       string        `json:"resumen,omitempty"`
	Nota          string        `json:"nota,omitempty"`
	Provider      string        `json:"provider,omitempty"`
	Model         string        `json:"model,omitempty"`
	InformeMicros int64         `json:"informeMicros"`
	EncargoID     *int64        `json:"encargoId,omitempty"`
	PuestoPor     string        `json:"puestoPor,omitempty"`
	// EncargoStatus is its encargo's state (pending, taken, delivered...)
	// and EncargoRoute who does it, when it has one.
	EncargoStatus string    `json:"encargoStatus,omitempty"`
	EncargoRoute  string    `json:"encargoRoute,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Archivos is how many files the report says would change.
func (a AlDia) Archivos() int { return len(a.Cambia) }

// Lineas is how many lines would be added and taken out, all together.
func (a AlDia) Lineas() int {
	n := 0
	for _, c := range a.Cambia {
		n += c.Mas + c.Menos
	}
	return n
}

const alDiaCols = `d.id, d.org_id, d.app_id, d.pull, d.titulo, d.notas, d.branch, d.sha, d.run_id, d.run_failed, d.estado, d.falla, d.cambia,
	d.riesgo, d.dificultad, d.costo_micros, d.resumen, d.nota, d.provider, d.model, d.informe_micros, d.encargo_id, d.puesto_por,
	COALESCE(e.status, '') AS encargo_status, COALESCE(e.route, '') AS encargo_route, d.created_at, d.updated_at`

const alDiaFrom = ` FROM al_dia d LEFT JOIN encargos e ON e.id = d.encargo_id`

func scanAlDia(row pgx.Row) (AlDia, error) {
	var a AlDia
	err := row.Scan(&a.ID, &a.OrgID, &a.AppID, &a.Pull, &a.Titulo, &a.Notas, &a.Branch, &a.SHA, &a.RunID, &a.RunFailed, &a.Estado, &a.Falla, &a.Cambia,
		&a.Riesgo, &a.Dificultad, &a.CostoMicros, &a.Resumen, &a.Nota, &a.Provider, &a.Model, &a.InformeMicros, &a.EncargoID, &a.PuestoPor,
		&a.EncargoStatus, &a.EncargoRoute, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if a.Falla == nil {
		a.Falla = []string{}
	}
	if a.Cambia == nil {
		a.Cambia = []AlDiaCambio{}
	}
	return a, err
}

// PedirAlDia asks for the report on a run of a dependency update's pull
// request. Nothing is asked when the run has one already, or when an
// earlier run of the same pull request is being (or was) put up to date:
// its encargo's own runs answer for it. false when nothing was asked.
func (s *Store) PedirAlDia(ctx context.Context, a AlDia) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO al_dia (org_id, app_id, pull, titulo, notas, branch, sha, run_id, run_failed)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9
		WHERE NOT EXISTS (SELECT 1 FROM al_dia WHERE app_id = $2 AND pull = $3 AND encargo_id IS NOT NULL)
		ON CONFLICT (run_id) DO NOTHING`,
		orgOr(a.OrgID), a.AppID, a.Pull, a.Titulo, a.Notas, a.Branch, a.SHA, a.RunID, a.RunFailed)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// AlDiaPendientes are the reports still to write, the oldest first.
func (s *Store) AlDiaPendientes(ctx context.Context, limit int) ([]AlDia, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+alDiaCols+alDiaFrom+` WHERE d.estado = 'pendiente' ORDER BY d.created_at, d.id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AlDia, error) { return scanAlDia(r) })
}

// AlDiaPorID is one report.
func (s *Store) AlDiaPorID(ctx context.Context, id int64) (AlDia, error) {
	return scanAlDia(s.pool.QueryRow(ctx, `SELECT `+alDiaCols+alDiaFrom+` WHERE d.id = $1`, id))
}

// GuardarInformeAlDia keeps a written report (or why it could not be).
func (s *Store) GuardarInformeAlDia(ctx context.Context, a AlDia) error {
	if a.Falla == nil {
		a.Falla = []string{}
	}
	if a.Cambia == nil {
		a.Cambia = []AlDiaCambio{}
	}
	_, err := s.pool.Exec(ctx, `UPDATE al_dia SET estado = $2, falla = $3, cambia = $4, riesgo = $5, dificultad = $6, costo_micros = $7,
		resumen = $8, nota = $9, provider = $10, model = $11, informe_micros = $12, updated_at = now() WHERE id = $1`,
		a.ID, a.Estado, a.Falla, a.Cambia, a.Riesgo, a.Dificultad, a.CostoMicros, a.Resumen, a.Nota, a.Provider, a.Model, a.InformeMicros)
	return err
}

// AlDiaDeApp is the latest report of each of an app's dependency updates
// lately, the newest first: what its Caminos tab shows.
func (s *Store) AlDiaDeApp(ctx context.Context, appID int64, limit int) ([]AlDia, error) {
	rows, err := s.pool.Query(ctx, `SELECT * FROM (SELECT DISTINCT ON (d.pull) `+alDiaCols+alDiaFrom+`
		WHERE d.app_id = $1 AND d.created_at > now() - interval '30 days' ORDER BY d.pull, d.id DESC) x ORDER BY x.id DESC LIMIT $2`, appID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AlDia, error) { return scanAlDia(r) })
}

// PonerAlDia links a report to the encargo that puts its app up to date,
// once: ErrEncargoState when it has one already.
func (s *Store) PonerAlDia(ctx context.Context, id, encargoID int64, who string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE al_dia SET encargo_id = $2, puesto_por = $3, updated_at = now() WHERE id = $1 AND encargo_id IS NULL`, id, encargoID, who)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrEncargoState
	}
	return nil
}

// AlDiaAjustes is what an organization chose for its dependency updates.
type AlDiaAjustes struct {
	Encendido  bool      `json:"encendido"`
	Quien      string    `json:"quien"`
	Cuando     string    `json:"cuando"`
	Sola       bool      `json:"sola"`
	TopeMicros int64     `json:"topeMicros"`
	UpdatedBy  string    `json:"updatedBy,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// AlDiaAjustesDe is an organization's choices, or the defaults: on, the
// platform's agent, every update, asking first.
func (s *Store) AlDiaAjustesDe(ctx context.Context, orgID int64) (AlDiaAjustes, error) {
	var a AlDiaAjustes
	err := s.pool.QueryRow(ctx, `SELECT encendido, quien, cuando, sola, tope_micros, updated_by, updated_at FROM al_dia_ajustes WHERE org_id = $1`, orgOr(orgID)).
		Scan(&a.Encendido, &a.Quien, &a.Cuando, &a.Sola, &a.TopeMicros, &a.UpdatedBy, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AlDiaAjustes{Encendido: true, Quien: AlDiaQuienPlataforma, Cuando: AlDiaCuandoToda, TopeMicros: AlDiaTopeDefault}, nil
	}
	return a, err
}

// GuardarAlDiaAjustes keeps an organization's choices.
func (s *Store) GuardarAlDiaAjustes(ctx context.Context, orgID int64, a AlDiaAjustes, by string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO al_dia_ajustes (org_id, encendido, quien, cuando, sola, tope_micros, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (org_id) DO UPDATE SET encendido = $2, quien = $3, cuando = $4, sola = $5, tope_micros = $6, updated_by = $7, updated_at = now()`,
		orgOr(orgID), a.Encendido, a.Quien, a.Cuando, a.Sola, a.TopeMicros, by)
	return err
}
