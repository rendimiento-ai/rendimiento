package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ARatosNodo is what a node «a ratos» does while it is on, as its drawer on
// the Environment page sets it (Ruta pendiente 199). The zero value is the
// default: it builds the other half of images, runs tests and moves a step
// it shut down under to another node; with it off, images are of one
// architecture without waiting.
type ARatosNodo struct {
	// SinOtraMitad: it builds no half of images.
	SinOtraMitad bool `json:"sinOtraMitad,omitempty"`
	// SinPruebas: no test steps go to it.
	SinPruebas bool `json:"sinPruebas,omitempty"`
	// SinReintento: a step it shut down under fails instead of running again
	// on another node.
	SinReintento bool `json:"sinReintento,omitempty"`
	// Esperar: while it is off, a build waits for it up to EsperaARatos, so
	// its image has both architectures.
	Esperar bool `json:"esperar,omitempty"`
}

// EsperaARatos is the longest a build waits for an off node «a ratos».
const EsperaARatos = 30 * time.Minute

// MarcarEncendido notes whether a node «a ratos» is on now: a stretch opens
// when it comes on and closes when it goes off. Calling it again with the
// same state changes nothing, from any number of processes.
func (s *Store) MarcarEncendido(ctx context.Context, nodo string, on bool) error {
	if on {
		_, err := s.pool.Exec(ctx, `INSERT INTO nodos_encendidos (nodo) VALUES ($1) ON CONFLICT (nodo) WHERE hasta IS NULL DO NOTHING`, nodo)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE nodos_encendidos SET hasta = now() WHERE nodo = $1 AND hasta IS NULL`, nodo)
	return err
}

// CifrasNodo is what a node did, for its drawer on the Environment page and
// the data by node (Ruta pendiente 201).
type CifrasNodo struct {
	Nodo string `json:"nodo"`
	// PrendidaDesde is when its stretch on began (nodes «a ratos» only).
	PrendidaDesde *time.Time `json:"prendidaDesde,omitempty"`
	// HorasSemana are the hours it was on in the last 7 days (nodes «a
	// ratos» only).
	HorasSemana float64 `json:"horasSemana"`
	// Today: the steps that ran on it since midnight (UTC), and of them the
	// builds (an image, or one of its halves).
	PasosHoy    int `json:"pasosHoy"`
	ImagenesHoy int `json:"imagenesHoy"`
	// The last 7 days: its steps, how many failed, and the minutes they took.
	PasosSemana   int         `json:"pasosSemana"`
	FallasSemana  int         `json:"fallasSemana"`
	MinutosSemana float64     `json:"minutosSemana"`
	Apps          []CifrasApp `json:"apps"`
}

// CifrasApp is one app's steps on a node in the last 7 days.
type CifrasApp struct {
	App     string  `json:"app"`
	Pasos   int     `json:"pasos"`
	Fallas  int     `json:"fallas"`
	Minutos float64 `json:"minutos"`
}

// CifrasDeNodo counts what ran on a node: today, and the last 7 days by app.
func (s *Store) CifrasDeNodo(ctx context.Context, nodo string, now time.Time) (CifrasNodo, error) {
	c := CifrasNodo{Nodo: nodo, Apps: []CifrasApp{}}
	semana, hoy := now.Add(-7*24*time.Hour), now.UTC().Truncate(24*time.Hour)
	b, _ := json.Marshal([]map[string]string{{"nodo": nodo}})
	donde := string(b)
	err := s.pool.QueryRow(ctx, `SELECT
			count(*) FILTER (WHERE st.started_at >= $3),
			count(*) FILTER (WHERE st.started_at >= $3 AND st.kind = 'build'),
			count(*),
			count(*) FILTER (WHERE st.status = 'failed'),
			coalesce(sum(extract(epoch FROM st.finished_at - st.started_at)) FILTER (WHERE st.finished_at IS NOT NULL), 0) / 60
		FROM steps st WHERE st.lugares @> $1::jsonb AND st.started_at >= $2`, donde, semana, hoy).
		Scan(&c.PasosHoy, &c.ImagenesHoy, &c.PasosSemana, &c.FallasSemana, &c.MinutosSemana)
	if err != nil {
		return c, err
	}
	rows, err := s.pool.Query(ctx, `SELECT a.name, count(*), count(*) FILTER (WHERE st.status = 'failed'),
			coalesce(sum(extract(epoch FROM st.finished_at - st.started_at)) FILTER (WHERE st.finished_at IS NOT NULL), 0) / 60
		FROM steps st JOIN runs r ON r.id = st.run_id JOIN apps a ON a.id = r.app_id
		WHERE st.lugares @> $1::jsonb AND st.started_at >= $2
		GROUP BY a.name ORDER BY count(*) DESC, a.name LIMIT 10`, donde, semana)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var a CifrasApp
		if err := rows.Scan(&a.App, &a.Pasos, &a.Fallas, &a.Minutos); err != nil {
			rows.Close()
			return c, err
		}
		c.Apps = append(c.Apps, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return c, err
	}
	var desde time.Time
	err = s.pool.QueryRow(ctx, `SELECT desde FROM nodos_encendidos WHERE nodo = $1 AND hasta IS NULL`, nodo).Scan(&desde)
	switch {
	case err == nil:
		c.PrendidaDesde = &desde
	case !errors.Is(err, pgx.ErrNoRows):
		return c, err
	}
	err = s.pool.QueryRow(ctx, `SELECT coalesce(sum(extract(epoch FROM least(coalesce(hasta, $3), $3) - greatest(desde, $2))), 0) / 3600
		FROM nodos_encendidos WHERE nodo = $1 AND coalesce(hasta, $3) > $2`, nodo, semana, now).Scan(&c.HorasSemana)
	return c, err
}
