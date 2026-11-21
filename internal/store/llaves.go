package store

import (
	"context"
	"time"
)

// Llave is a key changed on the Environment page: who changed it, when,
// and when it expires (nil: it does not, or its service does not say).
// Never its value, which is kept in the Kubernetes Secret that uses it.
// Version goes up with each change: processes that see a newer one read
// the value again.
type Llave struct {
	Llave       string     `json:"llave"`
	Version     int64      `json:"version"`
	CambiadaPor string     `json:"cambiadaPor"`
	CambiadaEn  time.Time  `json:"cambiadaEn"`
	VenceEn     *time.Time `json:"venceEn,omitempty"`
}

// Llaves are the keys changed here, by their name.
func (s *Store) Llaves(ctx context.Context) (map[string]Llave, error) {
	rows, err := s.pool.Query(ctx, `SELECT llave, version, cambiada_por, cambiada_en, vence_en FROM llaves`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Llave{}
	for rows.Next() {
		var l Llave
		if err := rows.Scan(&l.Llave, &l.Version, &l.CambiadaPor, &l.CambiadaEn, &l.VenceEn); err != nil {
			return nil, err
		}
		out[l.Llave] = l
	}
	return out, rows.Err()
}

// AnotarLlave records that por changed a key now, with its expiry, and
// returns the row with its new version.
func (s *Store) AnotarLlave(ctx context.Context, llave, por string, venceEn *time.Time) (Llave, error) {
	l := Llave{Llave: llave}
	err := s.pool.QueryRow(ctx, `INSERT INTO llaves (llave, cambiada_por, vence_en) VALUES ($1, $2, $3)
		ON CONFLICT (llave) DO UPDATE SET version = llaves.version + 1, cambiada_por = $2, cambiada_en = now(), vence_en = $3
		RETURNING version, cambiada_por, cambiada_en, vence_en`, llave, por, venceEn).Scan(&l.Version, &l.CambiadaPor, &l.CambiadaEn, &l.VenceEn)
	return l, err
}
