package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The states of rendimiento's sleep on a computer (Ruta 92).
const (
	Despierto   = "despierto"
	Durmiendose = "durmiendose" // waiting for a publish to finish before sleeping
	Dormido     = "dormido"
)

// Sueno is where rendimiento's sleep stands: one row, so a restart stays
// asleep (platform.Sueno keeps it).
type Sueno struct {
	Estado string `json:"estado"`
	// Apagar: «Apagar del todo» was asked; rendimiento stops once asleep.
	Apagar bool       `json:"apagar"`
	Desde  *time.Time `json:"desde,omitempty"`
	Por    string     `json:"por,omitempty"`
	// Contenedores are the containers sleep stopped, started again on waking.
	Contenedores []string `json:"-"`
	// Cortadas are the runs sleep cut, repeated on waking (never failed).
	Cortadas []int64 `json:"-"`
	// Esperada is the publish sleep waited for ("stockpulse #214"), or ""
	// when there was none.
	Esperada string `json:"esperada,omitempty"`
	// DespertarA is the hour it wakes at by itself ("08:00"; "" off).
	DespertarA string `json:"despertarA"`
	// AlArrancar wakes it when rendimiento starts again (set by «Apagar del
	// todo»).
	AlArrancar bool `json:"alArrancar"`
	// Tapa is «Dormir al cerrar la tapa», which the desktop app reads.
	Tapa bool `json:"tapa"`
	// Resumen is what the last wake did.
	Resumen *Despertar `json:"resumen,omitempty"`
}

// Despertar is what a wake did, for the summary people see after it.
type Despertar struct {
	Desde time.Time `json:"desde"`
	Hasta time.Time `json:"hasta"`
	Por   string    `json:"por,omitempty"`
	// Apps are the apps whose containers started again.
	Apps []string `json:"apps"`
	// Esperada is the publish sleep waited for before sleeping ("" none).
	Esperada string `json:"esperada,omitempty"`
	// Avisos is how many stored webhooks were handled, and Fallidos how many
	// of them could not be.
	Avisos   int `json:"avisos"`
	Fallidos int `json:"fallidos,omitempty"`
	// Repetidas are the runs cut by sleep that run again.
	Repetidas []int64 `json:"repetidas"`
	// Problema is what could not be started again, in words.
	Problema string `json:"problema,omitempty"`
}

// GetSueno reads where rendimiento's sleep stands.
func (s *Store) GetSueno(ctx context.Context) (Sueno, error) {
	var z Sueno
	var resumen []byte
	err := s.pool.QueryRow(ctx, `SELECT estado, apagar, desde, por, contenedores, cortadas, esperada, despertar_a, al_arrancar, tapa, resumen
		FROM dormir WHERE id = 1`).Scan(&z.Estado, &z.Apagar, &z.Desde, &z.Por, &z.Contenedores, &z.Cortadas, &z.Esperada,
		&z.DespertarA, &z.AlArrancar, &z.Tapa, &resumen)
	if errors.Is(err, pgx.ErrNoRows) {
		return Sueno{Estado: Despierto}, nil
	}
	if err != nil {
		return z, err
	}
	if len(resumen) > 0 {
		var d Despertar
		if json.Unmarshal(resumen, &d) == nil {
			z.Resumen = &d
		}
	}
	return z, nil
}

// GuardarSueno keeps where rendimiento's sleep stands.
func (s *Store) GuardarSueno(ctx context.Context, z Sueno) error {
	var resumen []byte
	if z.Resumen != nil {
		resumen, _ = json.Marshal(z.Resumen)
	}
	if z.Contenedores == nil {
		z.Contenedores = []string{}
	}
	if z.Cortadas == nil {
		z.Cortadas = []int64{}
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO dormir (id, estado, apagar, desde, por, contenedores, cortadas, esperada, despertar_a, al_arrancar, tapa, resumen, actualizado_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
		ON CONFLICT (id) DO UPDATE SET estado = $1, apagar = $2, desde = $3, por = $4, contenedores = $5, cortadas = $6, esperada = $7,
			despertar_a = $8, al_arrancar = $9, tapa = $10, resumen = $11, actualizado_at = now()`,
		z.Estado, z.Apagar, z.Desde, z.Por, z.Contenedores, z.Cortadas, z.Esperada, z.DespertarA, z.AlArrancar, z.Tapa, resumen)
	return err
}

// AvisoGuardado is a forja's webhook received while rendimiento slept: the
// event it carried, already checked (its signature), kept to be handled
// on waking.
type AvisoGuardado struct {
	ID       int64           `json:"id"`
	Forja    string          `json:"forja"`
	Evento   json.RawMessage `json:"evento"`
	Recibido time.Time       `json:"recibido"`
}

// GuardarAviso keeps a webhook's event to handle it on waking.
func (s *Store) GuardarAviso(ctx context.Context, forja string, evento any) error {
	raw, err := json.Marshal(evento)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO dormir_avisos (forja, evento) VALUES ($1, $2)`, forja, raw)
	return err
}

// AvisosGuardados are the webhooks kept while asleep, in the order they came.
func (s *Store) AvisosGuardados(ctx context.Context) ([]AvisoGuardado, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, forja, evento, recibido_at FROM dormir_avisos ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AvisoGuardado, error) {
		var a AvisoGuardado
		err := r.Scan(&a.ID, &a.Forja, &a.Evento, &a.Recibido)
		return a, err
	})
}

// ContarAvisos is how many webhooks wait for the wake.
func (s *Store) ContarAvisos(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM dormir_avisos`).Scan(&n)
	return n, err
}

// BorrarAviso forgets a webhook once handled.
func (s *Store) BorrarAviso(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM dormir_avisos WHERE id = $1`, id)
	return err
}

// EmpezarPeriodo notes that rendimiento fell asleep at desde.
func (s *Store) EmpezarPeriodo(ctx context.Context, desde time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO dormir_periodos (desde) SELECT $1
		WHERE NOT EXISTS (SELECT 1 FROM dormir_periodos WHERE hasta IS NULL)`, desde)
	return err
}

// TerminarPeriodo notes that it woke at hasta.
func (s *Store) TerminarPeriodo(ctx context.Context, hasta time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE dormir_periodos SET hasta = $1 WHERE hasta IS NULL`, hasta)
	return err
}

// Periodo is a span rendimiento slept (Hasta nil: it sleeps still).
type Periodo struct {
	Desde time.Time  `json:"desde"`
	Hasta *time.Time `json:"hasta,omitempty"`
}

// PeriodosDormidos are the spans it slept that touch desde..hasta.
func (s *Store) PeriodosDormidos(ctx context.Context, desde, hasta time.Time) ([]Periodo, error) {
	return collect(ctx, s, `SELECT desde, hasta FROM dormir_periodos
		WHERE desde < $2 AND (hasta IS NULL OR hasta > $1) ORDER BY desde`,
		[]any{desde, hasta}, func(r pgx.CollectableRow) (Periodo, error) {
			var p Periodo
			err := r.Scan(&p.Desde, &p.Hasta)
			return p, err
		})
}

// RepetirAlDespertar puts a run that sleep cut back in the queue, from its
// first step: it is not failed nor cancelled, and runs again on waking.
func (s *Store) RepetirAlDespertar(ctx context.Context, runID int64, message string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE steps SET status = 'pending', digest = '', message = '', started_at = NULL, finished_at = NULL, log = ''
			WHERE run_id = $1`, runID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE runs SET status = 'queued', started_at = NULL, finished_at = NULL, message = $2 WHERE id = $1`, runID, message)
		return err
	})
}
