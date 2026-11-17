package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The lookout (el vigía, internal/agentes/vigia.go) reads what the platform
// recorded in a week, added up here first: step durations and failures,
// retries and stumbles, each app's runs, the log's warnings and errors by
// their message, the slow requests by route and the open problems. Only
// counts, times and message templates: never a log line's attributes, a
// repository's contents or what people wrote.

// VigiaPaso is one step of one app over the week, and over the three weeks
// before it (Antes).
type VigiaPaso struct {
	App   string `json:"app"`
	Paso  string `json:"paso"`
	Veces int    `json:"veces"` // runs it finished in
	// Fallas is how many times it failed, Reintentos how many it ran more
	// than once, and Ultimo how many times it was the last of its run to
	// finish (it decided how long the run took).
	Fallas     int     `json:"fallas"`
	Reintentos int     `json:"reintentos"`
	Ultimo     int     `json:"ultimo"`
	Segundos   float64 `json:"segundos"` // on average
	P90        float64 `json:"p90"`
	// VecesAntes and SegundosAntes are the same over the three weeks before.
	VecesAntes    int     `json:"vecesAntes"`
	SegundosAntes float64 `json:"segundosAntes"`
}

// VigiaApp is one app's runs over the week, and the three weeks before.
type VigiaApp struct {
	App              string  `json:"app"`
	Ejecuciones      int     `json:"ejecuciones"`
	Fallidas         int     `json:"fallidas"`
	Canceladas       int     `json:"canceladas"`
	Reintentos       int     `json:"reintentos"`
	Segundos         float64 `json:"segundos"` // a finished run, on average
	EjecucionesAntes int     `json:"ejecucionesAntes"`
	FallidasAntes    int     `json:"fallidasAntes"`
}

// VigiaTropiezo is a step that failed and then passed on the same commit.
type VigiaTropiezo struct {
	App   string `json:"app"`
	Paso  string `json:"paso"`
	Veces int    `json:"veces"`
}

// VigiaLinea is one message of the platform's log at WARN or ERROR: how many
// times, on how many days, since when, and how many times in the three
// weeks before.
type VigiaLinea struct {
	Nivel      string    `json:"nivel"`
	Proceso    string    `json:"proceso"`
	App        string    `json:"app"`
	Mensaje    string    `json:"mensaje"`
	Veces      int       `json:"veces"`
	Dias       int       `json:"dias"`
	VecesAntes int       `json:"vecesAntes"`
	Primera    time.Time `json:"primera"`
	Ultima     time.Time `json:"ultima"`
}

// VigiaRuta is a route of the API that answered slowly (over a second).
type VigiaRuta struct {
	Ruta  string  `json:"ruta"`
	Veces int     `json:"veces"`
	Ms    float64 `json:"ms"` // on average
}

// VigiaProblema is a problem still open that happened this week.
type VigiaProblema struct {
	ID         int64     `json:"id"`
	Nivel      string    `json:"nivel"`
	Componente string    `json:"componente"`
	App        string    `json:"app"`
	Mensaje    string    `json:"mensaje"`
	Veces      int       `json:"veces"`
	Desde      time.Time `json:"desde"`
}

// VigiaDatos is what the lookout reads of a week.
type VigiaDatos struct {
	Desde     time.Time       `json:"desde"`
	Hasta     time.Time       `json:"hasta"`
	Pasos     []VigiaPaso     `json:"pasos"`
	Apps      []VigiaApp      `json:"apps"`
	Tropiezos []VigiaTropiezo `json:"tropiezos"`
	Lineas    []VigiaLinea    `json:"lineas"`
	Lentas    []VigiaRuta     `json:"lentas"`
	Problemas []VigiaProblema `json:"problemas"`
}

// LeerVigia adds up the week from desde to hasta, and the three weeks before
// it for comparison (the log keeps 30 days).
func (s *Store) LeerVigia(ctx context.Context, desde, hasta time.Time) (VigiaDatos, error) {
	d := VigiaDatos{Desde: desde, Hasta: hasta}
	antes := desde.Add(-3 * hasta.Sub(desde))
	var err error
	// Steps: each step's runs that finished, with its time, whether it was
	// the last of its run, and the same before the week.
	d.Pasos, err = collect(ctx, s, `
		WITH s AS (
			SELECT a.name AS app, st.step_id, st.status, st.tries, r.created_at >= $2 AS semana,
				EXTRACT(EPOCH FROM st.finished_at - st.started_at)::float8 AS secs,
				st.finished_at = max(st.finished_at) OVER (PARTITION BY st.run_id) AS ultimo
			FROM steps st JOIN runs r ON r.id = st.run_id JOIN apps a ON a.id = r.app_id
			WHERE r.created_at >= $1 AND r.created_at < $3
				AND st.started_at IS NOT NULL AND st.finished_at IS NOT NULL AND st.status IN ('succeeded', 'failed'))
		SELECT app, step_id,
			count(*) FILTER (WHERE semana)::int,
			count(*) FILTER (WHERE semana AND status = 'failed')::int,
			count(*) FILTER (WHERE semana AND tries > 1)::int,
			count(*) FILTER (WHERE semana AND ultimo)::int,
			COALESCE(avg(secs) FILTER (WHERE semana), 0)::float8,
			COALESCE(percentile_cont(0.9) WITHIN GROUP (ORDER BY secs) FILTER (WHERE semana), 0)::float8,
			count(*) FILTER (WHERE NOT semana)::int,
			COALESCE(avg(secs) FILTER (WHERE NOT semana), 0)::float8
		FROM s GROUP BY app, step_id
		HAVING count(*) FILTER (WHERE semana) > 0
		ORDER BY COALESCE(avg(secs) FILTER (WHERE semana), 0) * count(*) FILTER (WHERE semana) DESC LIMIT 80`,
		[]any{antes, desde, hasta}, func(r pgx.CollectableRow) (VigiaPaso, error) {
			var p VigiaPaso
			err := r.Scan(&p.App, &p.Paso, &p.Veces, &p.Fallas, &p.Reintentos, &p.Ultimo, &p.Segundos, &p.P90, &p.VecesAntes, &p.SegundosAntes)
			return p, err
		})
	if err != nil {
		return d, err
	}
	d.Apps, err = collect(ctx, s, `
		SELECT a.name,
			count(*) FILTER (WHERE r.created_at >= $2)::int,
			count(*) FILTER (WHERE r.created_at >= $2 AND r.status = 'failed')::int,
			count(*) FILTER (WHERE r.created_at >= $2 AND r.status = 'cancelled')::int,
			COALESCE(sum(r.retries) FILTER (WHERE r.created_at >= $2), 0)::int,
			COALESCE(avg(EXTRACT(EPOCH FROM r.finished_at - r.started_at)) FILTER (WHERE r.created_at >= $2 AND r.status = 'succeeded'), 0)::float8,
			count(*) FILTER (WHERE r.created_at < $2)::int,
			count(*) FILTER (WHERE r.created_at < $2 AND r.status = 'failed')::int
		FROM runs r JOIN apps a ON a.id = r.app_id
		WHERE r.created_at >= $1 AND r.created_at < $3
		GROUP BY a.name ORDER BY 2 DESC, 1 LIMIT 60`,
		[]any{antes, desde, hasta}, func(r pgx.CollectableRow) (VigiaApp, error) {
			var a VigiaApp
			err := r.Scan(&a.App, &a.Ejecuciones, &a.Fallidas, &a.Canceladas, &a.Reintentos, &a.Segundos, &a.EjecucionesAntes, &a.FallidasAntes)
			return a, err
		})
	if err != nil {
		return d, err
	}
	d.Tropiezos, err = collect(ctx, s, `
		SELECT a.name, t.step_id, count(*)::int FROM step_stumbles t JOIN apps a ON a.id = t.app_id
		WHERE t.at >= $1 AND t.at < $2 GROUP BY a.name, t.step_id ORDER BY 3 DESC LIMIT 30`,
		[]any{desde, hasta}, func(r pgx.CollectableRow) (VigiaTropiezo, error) {
			var t VigiaTropiezo
			err := r.Scan(&t.App, &t.Paso, &t.Veces)
			return t, err
		})
	if err != nil {
		return d, err
	}
	// The log's warnings and errors by message: the message is the line's
	// template (the values are attributes, which are not read).
	d.Lineas, err = collect(ctx, s, `
		SELECT level, process, app, message,
			count(*) FILTER (WHERE at >= $2)::int,
			count(DISTINCT date_trunc('day', at)) FILTER (WHERE at >= $2)::int,
			count(*) FILTER (WHERE at < $2)::int,
			min(at) FILTER (WHERE at >= $2), max(at) FILTER (WHERE at >= $2)
		FROM registro WHERE at >= $1 AND at < $3 AND level IN ('WARN', 'ERROR')
		GROUP BY level, process, app, message
		HAVING count(*) FILTER (WHERE at >= $2) > 0
		ORDER BY level = 'ERROR' DESC, 5 DESC LIMIT 40`,
		[]any{antes, desde, hasta}, func(r pgx.CollectableRow) (VigiaLinea, error) {
			var l VigiaLinea
			err := r.Scan(&l.Nivel, &l.Proceso, &l.App, &l.Mensaje, &l.Veces, &l.Dias, &l.VecesAntes, &l.Primera, &l.Ultima)
			return l, err
		})
	if err != nil {
		return d, err
	}
	// Slow requests by route: the route is a pattern, never a path.
	d.Lentas, err = collect(ctx, s, `
		SELECT attrs->>'route', count(*)::int, COALESCE(avg((attrs->>'ms')::float8), 0)::float8
		FROM registro WHERE at >= $1 AND at < $2 AND message = 'slow request' AND attrs ? 'route'
		GROUP BY 1 ORDER BY 2 DESC LIMIT 20`,
		[]any{desde, hasta}, func(r pgx.CollectableRow) (VigiaRuta, error) {
			var v VigiaRuta
			err := r.Scan(&v.Ruta, &v.Veces, &v.Ms)
			return v, err
		})
	if err != nil {
		return d, err
	}
	d.Problemas, err = collect(ctx, s, `
		SELECT id, level, component, app, message, count, first_at FROM problems
		WHERE dismissed_at IS NULL AND resolved_at IS NULL AND last_at >= $1
		ORDER BY count DESC LIMIT 20`,
		[]any{desde}, func(r pgx.CollectableRow) (VigiaProblema, error) {
			var p VigiaProblema
			err := r.Scan(&p.ID, &p.Nivel, &p.Componente, &p.App, &p.Mensaje, &p.Veces, &p.Desde)
			return p, err
		})
	return d, err
}

// collect runs a query and reads each row with read.
func collect[T any](ctx context.Context, s *Store, q string, args []any, read func(pgx.CollectableRow) (T, error)) ([]T, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, read)
	if out == nil {
		out = []T{}
	}
	return out, err
}

// The states of a review, and of a finding.
const (
	RevisionEnCurso = "revisando"
	RevisionHecha   = "hecha"
	RevisionFallida = "fallida"

	HallazgoNuevo      = "nuevo"
	HallazgoEncargo    = "encargo"
	HallazgoRuta       = "ruta"
	HallazgoDescartado = "descartado"
)

// VigiaRevision is one review by the lookout, and its findings.
type VigiaRevision struct {
	ID          int64           `json:"id"`
	At          time.Time       `json:"at"`
	Desde       time.Time       `json:"desde"`
	PedidaPor   string          `json:"pedidaPor,omitempty"`
	Estado      string          `json:"estado"`
	Nota        string          `json:"nota,omitempty"`
	Proveedor   string          `json:"proveedor,omitempty"`
	Modelo      string          `json:"modelo,omitempty"`
	CostoMicros int64           `json:"costoMicros"`
	Senales     int             `json:"senales"`
	Hallazgos   []VigiaHallazgo `json:"hallazgos"`
}

// VigiaHallazgo is one finding: what the lookout saw, its evidence, and what
// a person decided.
type VigiaHallazgo struct {
	ID          int64      `json:"id"`
	RevisionID  int64      `json:"revisionId"`
	Clave       string     `json:"clave"`
	Titulo      string     `json:"titulo"`
	Evidencia   string     `json:"evidencia"`
	Accionable  bool       `json:"accionable"`
	App         string     `json:"app,omitempty"`
	Estado      string     `json:"estado"`
	DecididoPor string     `json:"decididoPor,omitempty"`
	DecididoAt  *time.Time `json:"decididoAt,omitempty"`
	EncargoID   *int64     `json:"encargoId,omitempty"`
	RutaID      *int64     `json:"rutaId,omitempty"`
}

// ErrVigiaOcupado: a review is going on already.
var ErrVigiaOcupado = errors.New("the lookout is reviewing already")

// ErrVigiaDecidido: someone decided that finding already.
var ErrVigiaDecidido = errors.New("that finding was decided already")

// vigiaOcupado is how long a review may be going on before another may
// start (one whose process died never finishes).
const vigiaOcupado = 15 * time.Minute

// EmpezarRevision starts a review of the week since desde, asked by por (""
// for the weekly one), unless one is going on.
func (s *Store) EmpezarRevision(ctx context.Context, desde time.Time, por string) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('vigia'))`); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM vigia_revisiones WHERE estado = 'revisando' AND at > $1`,
			time.Now().Add(-vigiaOcupado)).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrVigiaOcupado
		}
		return tx.QueryRow(ctx, `INSERT INTO vigia_revisiones (desde, pedida_por) VALUES ($1, $2) RETURNING id`, desde, por).Scan(&id)
	})
	return id, err
}

// TerminarRevision keeps how a review ended and its findings.
func (s *Store) TerminarRevision(ctx context.Context, r VigiaRevision) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE vigia_revisiones SET estado = $2, nota = $3, proveedor = $4, modelo = $5, costo_micros = $6, senales = $7 WHERE id = $1`,
			r.ID, r.Estado, r.Nota, r.Proveedor, r.Modelo, r.CostoMicros, r.Senales); err != nil {
			return err
		}
		for _, h := range r.Hallazgos {
			if _, err := tx.Exec(ctx, `INSERT INTO vigia_hallazgos (revision_id, clave, titulo, evidencia, accionable, app) VALUES ($1, $2, $3, $4, $5, $6)`,
				r.ID, h.Clave, h.Titulo, h.Evidencia, h.Accionable, h.App); err != nil {
				return err
			}
		}
		return nil
	})
}

const hallazgoCols = `id, revision_id, clave, titulo, evidencia, accionable, app, estado, decidido_por, decidido_at, encargo_id, ruta_id`

func leerHallazgo(r pgx.Row) (VigiaHallazgo, error) {
	var h VigiaHallazgo
	err := r.Scan(&h.ID, &h.RevisionID, &h.Clave, &h.Titulo, &h.Evidencia, &h.Accionable, &h.App, &h.Estado, &h.DecididoPor, &h.DecididoAt, &h.EncargoID, &h.RutaID)
	return h, err
}

// UltimaRevision is the latest review, finished or not, with its findings
// (ErrNotFound before the first).
func (s *Store) UltimaRevision(ctx context.Context) (VigiaRevision, error) {
	var r VigiaRevision
	err := s.pool.QueryRow(ctx, `SELECT id, at, desde, pedida_por, estado, nota, proveedor, modelo, costo_micros, senales
		FROM vigia_revisiones ORDER BY at DESC, id DESC LIMIT 1`).Scan(&r.ID, &r.At, &r.Desde, &r.PedidaPor, &r.Estado, &r.Nota, &r.Proveedor, &r.Modelo, &r.CostoMicros, &r.Senales)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.Hallazgos, err = collect(ctx, s, `SELECT `+hallazgoCols+` FROM vigia_hallazgos WHERE revision_id = $1 ORDER BY id`,
		[]any{r.ID}, func(row pgx.CollectableRow) (VigiaHallazgo, error) { return leerHallazgo(row) })
	return r, err
}

// UltimaTerminada is when the latest review that finished ended well or
// not: the weekly one waits a week after one that was done, a day after
// one that failed. Zero before the first.
func (s *Store) UltimaTerminada(ctx context.Context) (time.Time, string, error) {
	var at time.Time
	var estado string
	err := s.pool.QueryRow(ctx, `SELECT at, estado FROM vigia_revisiones WHERE estado <> 'revisando' ORDER BY at DESC LIMIT 1`).Scan(&at, &estado)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, "", nil
	}
	return at, estado, err
}

// VigiaRecuerdo is how long a finding turned into an encargo or a note is
// not proposed again; one a person dismissed is never proposed again.
const VigiaRecuerdo = 90 * 24 * time.Hour

// VigiaDecididos are the findings people decided that the lookout must not
// propose again, the latest of each key first.
func (s *Store) VigiaDecididos(ctx context.Context, now time.Time) ([]VigiaHallazgo, error) {
	return collect(ctx, s, `SELECT `+hallazgoCols+` FROM (
			SELECT DISTINCT ON (clave) * FROM vigia_hallazgos
			WHERE estado = 'descartado' OR (estado IN ('encargo', 'ruta') AND decidido_at > $1)
			ORDER BY clave, decidido_at DESC) d
		ORDER BY decidido_at DESC LIMIT 200`,
		[]any{now.Add(-VigiaRecuerdo)}, func(row pgx.CollectableRow) (VigiaHallazgo, error) { return leerHallazgo(row) })
}

// Hallazgo is one finding (ErrNotFound if there is none).
func (s *Store) Hallazgo(ctx context.Context, id int64) (VigiaHallazgo, error) {
	h, err := leerHallazgo(s.pool.QueryRow(ctx, `SELECT `+hallazgoCols+` FROM vigia_hallazgos WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return h, ErrNotFound
	}
	return h, err
}

// DecidirHallazgo marks a new finding as decided (encargo, ruta or
// descartado) by por, once: ErrVigiaDecidido when someone did already.
func (s *Store) DecidirHallazgo(ctx context.Context, id int64, estado, por string) (VigiaHallazgo, error) {
	h, err := leerHallazgo(s.pool.QueryRow(ctx, `UPDATE vigia_hallazgos SET estado = $2, decidido_por = $3, decidido_at = now()
		WHERE id = $1 AND estado = 'nuevo' RETURNING `+hallazgoCols, id, estado, por))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := s.Hallazgo(ctx, id); err != nil {
			return h, err
		}
		return h, ErrVigiaDecidido
	}
	return h, err
}

// EnlazarHallazgo keeps the encargo or the Ruta entry a finding became.
func (s *Store) EnlazarHallazgo(ctx context.Context, id int64, encargo, ruta *int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE vigia_hallazgos SET encargo_id = COALESCE($2, encargo_id), ruta_id = COALESCE($3, ruta_id) WHERE id = $1`, id, encargo, ruta)
	return err
}

// DeshacerHallazgo puts a finding back to new, when what it was to become
// could not be made.
func (s *Store) DeshacerHallazgo(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE vigia_hallazgos SET estado = 'nuevo', decidido_por = '', decidido_at = NULL WHERE id = $1 AND encargo_id IS NULL AND ruta_id IS NULL`, id)
	return err
}

// VigiaNuevos is how many findings of the latest review nobody decided yet.
func (s *Store) VigiaNuevos(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM vigia_hallazgos WHERE estado = 'nuevo'
		AND revision_id = (SELECT id FROM vigia_revisiones WHERE estado = 'hecha' ORDER BY at DESC, id DESC LIMIT 1)`).Scan(&n)
	return n, err
}

// VigiaTope is the lookout's monthly cap when none was set: 50 cents. A
// review costs about a cent with Claude Haiku, nothing on the local model.
const VigiaTope = 500_000

// VigiaAjuste is whether the lookout reviews each week (on unless an
// administrator turned it off), what it may spend a month, and who changed
// it last.
type VigiaAjuste struct {
	Encendido  bool      `json:"encendido"`
	TopeMicros int64     `json:"topeMicros"`
	UpdatedBy  string    `json:"updatedBy,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt,omitzero"`
}

// GetVigia reads the lookout's settings (on, with VigiaTope, when never set).
func (s *Store) GetVigia(ctx context.Context) (VigiaAjuste, error) {
	a := VigiaAjuste{Encendido: true, TopeMicros: VigiaTope}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value, updated_by, updated_at FROM platform_settings WHERE key = 'vigia'`).Scan(&raw, &a.UpdatedBy, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	var v struct {
		Apagado bool   `json:"apagado"`
		Tope    *int64 `json:"topeMicros"`
	}
	err = json.Unmarshal(raw, &v)
	a.Encendido = !v.Apagado
	if v.Tope != nil {
		a.TopeMicros = *v.Tope
	}
	return a, err
}

// SetVigia keeps the lookout's settings.
func (s *Store) SetVigia(ctx context.Context, a VigiaAjuste, by string) error {
	raw, err := json.Marshal(map[string]any{"apagado": !a.Encendido, "topeMicros": a.TopeMicros})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO platform_settings (key, value, updated_by, updated_at) VALUES ('vigia', $1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = $1, updated_by = $2, updated_at = now()`, raw, by)
	return err
}

// AISpentBy is what one source spent of an organization's AI this month.
func (s *Store) AISpentBy(ctx context.Context, orgID int64, source string, now time.Time) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(sum(cost_micros), 0) FROM ai_usage WHERE org_id = $1 AND source = $2 AND at >= $3`,
		orgID, source, monthStart(now)).Scan(&n)
	return n, err
}
