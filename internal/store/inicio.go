package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SeccionInicio is one section of the home page and whether it shows. A
// list of them, in order, is how a person laid out their home.
type SeccionInicio struct {
	ID string `json:"id"`
	On bool   `json:"on"`
}

// InicioPredeterminado is the home page an administrator of the platform
// set for whoever never chose theirs.
type InicioPredeterminado struct {
	Secciones []SeccionInicio `json:"secciones"`
	UpdatedBy string          `json:"updatedBy"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// GetInicio is the person's own home page, or nil when they never chose one.
func (s *Store) GetInicio(ctx context.Context, login string) ([]SeccionInicio, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT secciones FROM inicio_preferencias WHERE login = $1`, login).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SeccionInicio
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil // unreadable: as if never chosen
	}
	return out, nil
}

// SaveInicio keeps the person's home page.
func (s *Store) SaveInicio(ctx context.Context, login string, secciones []SeccionInicio) error {
	raw, err := json.Marshal(secciones)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO inicio_preferencias (login, secciones, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (login) DO UPDATE SET secciones = $2, updated_at = now()`, login, raw)
	return err
}

// DeleteInicio forgets the person's home page: they see the default again.
func (s *Store) DeleteInicio(ctx context.Context, login string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM inicio_preferencias WHERE login = $1`, login)
	return err
}

// GetInicioPredeterminado is the default an administrator set, or nil.
func (s *Store) GetInicioPredeterminado(ctx context.Context) (*InicioPredeterminado, error) {
	var raw []byte
	p := &InicioPredeterminado{}
	err := s.pool.QueryRow(ctx, `SELECT value, updated_by, updated_at FROM platform_settings WHERE key = 'inicio'`).Scan(&raw, &p.UpdatedBy, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &p.Secciones); err != nil {
		return nil, nil
	}
	return p, nil
}

// SaveInicioPredeterminado sets the home page of everyone who never chose theirs.
func (s *Store) SaveInicioPredeterminado(ctx context.Context, secciones []SeccionInicio, by string) error {
	raw, err := json.Marshal(secciones)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO platform_settings (key, value, updated_by, updated_at) VALUES ('inicio', $1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = $1, updated_by = $2, updated_at = now()`, raw, by)
	return err
}

// DeleteInicioPredeterminado goes back to the built-in default.
func (s *Store) DeleteInicioPredeterminado(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM platform_settings WHERE key = 'inicio'`)
	return err
}
