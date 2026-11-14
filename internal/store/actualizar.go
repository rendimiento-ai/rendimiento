package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ActualizarSolo is whether rendimiento installs fix releases by itself on
// a computer (decision 138: off until an administrator turns it on).
type ActualizarSolo struct {
	On        bool      `json:"on"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// GetActualizarSolo reads the switch (off when never set).
func (s *Store) GetActualizarSolo(ctx context.Context) (ActualizarSolo, error) {
	var a ActualizarSolo
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value, updated_by, updated_at FROM platform_settings WHERE key = 'actualizar-solo'`).Scan(&raw, &a.UpdatedBy, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActualizarSolo{}, nil
	}
	if err != nil {
		return a, err
	}
	var v struct {
		On bool `json:"on"`
	}
	err = json.Unmarshal(raw, &v)
	a.On = v.On
	return a, err
}

// SetActualizarSolo turns the switch on or off.
func (s *Store) SetActualizarSolo(ctx context.Context, on bool, by string) error {
	raw, err := json.Marshal(map[string]bool{"on": on})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO platform_settings (key, value, updated_by, updated_at) VALUES ('actualizar-solo', $1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = $1, updated_by = $2, updated_at = now()`, raw, by)
	return err
}
