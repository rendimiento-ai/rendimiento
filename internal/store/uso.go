package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// How rendimiento's own interface is used: totals per day (UTC), never per
// person. The API checks what comes in (route patterns from a list, actions
// from a closed list); the store adds it up.

// UsoMaxMs is the longest time to data counted; longer is not kept as a
// time (the view still counts).
const UsoMaxMs = 60000

// UsoDias is how many days of use are kept.
const UsoDias = 90

// UsoLimites are the upper bounds of the time-to-data buckets, in
// milliseconds; a fifth bucket holds the rest (4 s or more).
var UsoLimites = [4]int{500, 1000, 2000, 4000}

// VistaUso is one page shown: its route pattern, the pattern it came from
// ("" on the first of a visit) and, when it could be measured, how long it
// took to show its data.
type VistaUso struct {
	Ruta  string
	Desde string
	Ms    *int
}

// AccionUso is one main action taken on a page.
type AccionUso struct {
	Ruta   string
	Accion string
}

// AnotarUso adds views and actions to the day's totals (dia in UTC).
// Nothing about who did them is passed or kept.
func (s *Store) AnotarUso(ctx context.Context, dia time.Time, vistas []VistaUso, acciones []AccionUso) error {
	if len(vistas) == 0 && len(acciones) == 0 {
		return nil
	}
	d := dia.UTC().Format("2006-01-02")
	type pagina struct {
		vistas, medidas, ms int64
		h                   [5]int64
	}
	paginas := map[string]*pagina{}
	pasos := map[[2]string]int64{}
	for _, v := range vistas {
		p := paginas[v.Ruta]
		if p == nil {
			p = &pagina{}
			paginas[v.Ruta] = p
		}
		p.vistas++
		if v.Ms != nil && *v.Ms >= 0 && *v.Ms <= UsoMaxMs {
			p.medidas++
			p.ms += int64(*v.Ms)
			p.h[cubeta(*v.Ms)]++
		}
		pasos[[2]string{v.Desde, v.Ruta}]++
	}
	acc := map[[2]string]int64{}
	for _, a := range acciones {
		acc[[2]string{a.Ruta, a.Accion}]++
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Always in the same order, so two requests at once never deadlock.
		for _, r := range ordenadas(paginas) {
			p := paginas[r]
			if _, err := tx.Exec(ctx, `INSERT INTO uso_paginas (dia, ruta, vistas, medidas, ms_total, menos_500, menos_1000, menos_2000, menos_4000, mas_4000)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				ON CONFLICT (dia, ruta) DO UPDATE SET vistas = uso_paginas.vistas + $3, medidas = uso_paginas.medidas + $4,
					ms_total = uso_paginas.ms_total + $5, menos_500 = uso_paginas.menos_500 + $6, menos_1000 = uso_paginas.menos_1000 + $7,
					menos_2000 = uso_paginas.menos_2000 + $8, menos_4000 = uso_paginas.menos_4000 + $9, mas_4000 = uso_paginas.mas_4000 + $10`,
				d, r, p.vistas, p.medidas, p.ms, p.h[0], p.h[1], p.h[2], p.h[3], p.h[4]); err != nil {
				return err
			}
		}
		for _, k := range ordenadasPar(pasos) {
			if _, err := tx.Exec(ctx, `INSERT INTO uso_pasos (dia, desde, hacia, veces) VALUES ($1, $2, $3, $4)
				ON CONFLICT (dia, desde, hacia) DO UPDATE SET veces = uso_pasos.veces + $4`, d, k[0], k[1], pasos[k]); err != nil {
				return err
			}
		}
		for _, k := range ordenadasPar(acc) {
			if _, err := tx.Exec(ctx, `INSERT INTO uso_acciones (dia, ruta, accion, veces) VALUES ($1, $2, $3, $4)
				ON CONFLICT (dia, ruta, accion) DO UPDATE SET veces = uso_acciones.veces + $4`, d, k[0], k[1], acc[k]); err != nil {
				return err
			}
		}
		return nil
	})
}

// cubeta is the time-to-data bucket of ms (0 to 4).
func cubeta(ms int) int {
	for i, l := range UsoLimites {
		if ms < l {
			return i
		}
	}
	return len(UsoLimites)
}

func ordenadas[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func ordenadasPar(m map[[2]string]int64) [][2]string {
	out := make([][2]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}
		return out[i][1] < out[j][1]
	})
	return out
}

// PaginaUso is a page's totals over a period.
type PaginaUso struct {
	Ruta    string `json:"ruta"`
	Vistas  int64  `json:"vistas"`
	Medidas int64  `json:"medidas"`
	MsTotal int64  `json:"msTotal"`
	// Cubetas count the measured views under 0.5, 1, 2 and 4 s, and 4 s or more.
	Cubetas [5]int64 `json:"cubetas"`
}

// PasoUso is how many times people went from one page to another.
type PasoUso struct {
	Desde string `json:"desde"`
	Hacia string `json:"hacia"`
	Veces int64  `json:"veces"`
}

// AccionUsada is how many times an action was taken on a page.
type AccionUsada struct {
	Ruta   string `json:"ruta"`
	Accion string `json:"accion"`
	Veces  int64  `json:"veces"`
}

// ResumenUso is the use since a day: the pages, the moves between them
// and the actions, the most frequent first (at most 100 of each).
type ResumenUso struct {
	Paginas  []PaginaUso   `json:"paginas"`
	Pasos    []PasoUso     `json:"pasos"`
	Acciones []AccionUsada `json:"acciones"`
}

// LeerUso adds up the use from desde (a UTC day, included) to today.
func (s *Store) LeerUso(ctx context.Context, desde time.Time) (ResumenUso, error) {
	d := desde.UTC().Format("2006-01-02")
	out := ResumenUso{Paginas: []PaginaUso{}, Pasos: []PasoUso{}, Acciones: []AccionUsada{}}
	rows, err := s.pool.Query(ctx, `SELECT ruta, sum(vistas), sum(medidas), sum(ms_total), sum(menos_500), sum(menos_1000), sum(menos_2000), sum(menos_4000), sum(mas_4000)
		FROM uso_paginas WHERE dia >= $1 GROUP BY ruta ORDER BY 2 DESC, ruta LIMIT 100`, d)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p PaginaUso
		if err := rows.Scan(&p.Ruta, &p.Vistas, &p.Medidas, &p.MsTotal, &p.Cubetas[0], &p.Cubetas[1], &p.Cubetas[2], &p.Cubetas[3], &p.Cubetas[4]); err != nil {
			rows.Close()
			return out, err
		}
		out.Paginas = append(out.Paginas, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	rows, err = s.pool.Query(ctx, `SELECT desde, hacia, sum(veces) FROM uso_pasos WHERE dia >= $1 GROUP BY desde, hacia ORDER BY 3 DESC, desde, hacia LIMIT 100`, d)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p PasoUso
		if err := rows.Scan(&p.Desde, &p.Hacia, &p.Veces); err != nil {
			rows.Close()
			return out, err
		}
		out.Pasos = append(out.Pasos, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	rows, err = s.pool.Query(ctx, `SELECT ruta, accion, sum(veces) FROM uso_acciones WHERE dia >= $1 GROUP BY ruta, accion ORDER BY 3 DESC, ruta, accion LIMIT 100`, d)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var a AccionUsada
		if err := rows.Scan(&a.Ruta, &a.Accion, &a.Veces); err != nil {
			rows.Close()
			return out, err
		}
		out.Acciones = append(out.Acciones, a)
	}
	rows.Close()
	return out, rows.Err()
}

// PruneUso deletes the days before `before` (a UTC day).
func (s *Store) PruneUso(ctx context.Context, before time.Time) error {
	d := before.UTC().Format("2006-01-02")
	for _, q := range []string{`DELETE FROM uso_paginas WHERE dia < $1`, `DELETE FROM uso_pasos WHERE dia < $1`, `DELETE FROM uso_acciones WHERE dia < $1`} {
		if _, err := s.pool.Exec(ctx, q, d); err != nil {
			return err
		}
	}
	return nil
}

// UsoAjuste is whether the interface's use is measured (on unless an
// administrator turned it off), and who changed it last.
type UsoAjuste struct {
	Encendido bool      `json:"encendido"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// GetUso reads the switch (on when never set).
func (s *Store) GetUso(ctx context.Context) (UsoAjuste, error) {
	a := UsoAjuste{Encendido: true}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value, updated_by, updated_at FROM platform_settings WHERE key = 'uso'`).Scan(&raw, &a.UpdatedBy, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return UsoAjuste{Encendido: true}, nil
	}
	if err != nil {
		return a, err
	}
	var v struct {
		Apagado bool `json:"apagado"`
	}
	err = json.Unmarshal(raw, &v)
	a.Encendido = !v.Apagado
	return a, err
}

// SetUso turns measuring on or off.
func (s *Store) SetUso(ctx context.Context, on bool, by string) error {
	raw, err := json.Marshal(map[string]bool{"apagado": !on})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO platform_settings (key, value, updated_by, updated_at) VALUES ('uso', $1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = $1, updated_by = $2, updated_at = now()`, raw, by)
	return err
}
