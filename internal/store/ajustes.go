package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Ajuste is a platform setting saved on the Environment page, by its
// environment variable. It is kept in platform_settings beside the
// capacity, as "env:<variable>" with the value as a JSON string, so the
// previous version, which reads only "capacity", ignores it.
type Ajuste struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedBy string    `json:"updatedBy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const ajustePrefix = "env:"

// GetAjustes are the settings saved on the Environment page.
func (s *Store) GetAjustes(ctx context.Context) (map[string]Ajuste, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value, updated_by, updated_at FROM platform_settings WHERE starts_with(key, $1)`, ajustePrefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Ajuste{}
	for rows.Next() {
		var a Ajuste
		var raw []byte
		if err := rows.Scan(&a.Key, &raw, &a.UpdatedBy, &a.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &a.Value); err != nil {
			continue // not one of ours
		}
		a.Key = strings.TrimPrefix(a.Key, ajustePrefix)
		out[a.Key] = a
	}
	return out, rows.Err()
}

// SaveAjuste keeps a setting's value; it wins over the environment's.
func (s *Store) SaveAjuste(ctx context.Context, key, value, by string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO platform_settings (key, value, updated_by, updated_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_by = $3, updated_at = now()`, ajustePrefix+key, raw, by)
	return err
}

// DeleteAjuste forgets a saved setting: the environment's value holds again.
// It reports whether there was one.
func (s *Store) DeleteAjuste(ctx context.Context, key string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM platform_settings WHERE key = $1`, ajustePrefix+key)
	return tag.RowsAffected() > 0, err
}
