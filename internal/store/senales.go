package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SenalesAjustes are this installation's own settings for its señales de
// vida (internal/senales): its random identity, whether a person turned
// them off, whether the operators saw the notice that they are sent, and
// when the last one went out.
type SenalesAjustes struct {
	Identidad   string     `json:"identidad"`
	Apagadas    bool       `json:"apagadas"`
	AvisoVisto  bool       `json:"avisoVisto"`
	UltimoEnvio *time.Time `json:"ultimoEnvio,omitempty"`
	UpdatedBy   string     `json:"updatedBy,omitempty"`
}

// GetSenales reads the settings, making the identity the first time: 16
// random bytes, from nothing about the computer, the account or the domain.
func (s *Store) GetSenales(ctx context.Context) (SenalesAjustes, error) {
	var a SenalesAjustes
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM platform_settings WHERE key = 'senales'`).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &a)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return a, err
	}
	if a.Identidad != "" {
		return a, nil
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return a, err
	}
	a.Identidad = hex.EncodeToString(b)
	nuevo, _ := json.Marshal(a)
	// Two processes starting at once keep the same identity: the first wins.
	if _, err := s.pool.Exec(ctx, `INSERT INTO platform_settings (key, value) VALUES ('senales', $1) ON CONFLICT (key) DO NOTHING`, nuevo); err != nil {
		return a, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT value FROM platform_settings WHERE key = 'senales'`).Scan(&raw); err != nil {
		return a, err
	}
	return a, json.Unmarshal(raw, &a)
}

// SaveSenales writes the settings (the identity never changes).
func (s *Store) SaveSenales(ctx context.Context, a SenalesAjustes, by string) error {
	actual, err := s.GetSenales(ctx)
	if err != nil {
		return err
	}
	a.Identidad, a.UpdatedBy = actual.Identidad, by
	raw, _ := json.Marshal(a)
	_, err = s.pool.Exec(ctx, `INSERT INTO platform_settings (key, value, updated_by, updated_at) VALUES ('senales', $1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = $1, updated_by = $2, updated_at = now()`, raw, by)
	return err
}

// ConteosSenales are the counts a señal carries, over the last seven days
// where they move: nothing that names an app, a repository or a person.
type ConteosSenales struct {
	Apps              int `json:"apps"`
	Ejecuciones       int `json:"ejecuciones"`
	Versiones         int `json:"versiones"`
	AgentesConLlave   int `json:"agentes_con_llave"`
	LlamadasDeAgentes int `json:"llamadas_de_agentes"`
	Organizaciones    int `json:"-"` // sent beside the week's counts
	Personas          int `json:"-"` // sent as a range (internal/senales)
}

// Conteos counts what a señal carries.
func (s *Store) Conteos(ctx context.Context) (ConteosSenales, error) {
	var c ConteosSenales
	err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM apps),
		(SELECT count(*) FROM runs WHERE created_at > now() - interval '7 days'),
		(SELECT count(*) FROM releases WHERE created_at > now() - interval '7 days'),
		(SELECT count(*) FROM agent_keys WHERE revoked_at IS NULL AND expires_at > now()),
		(SELECT count(*) FROM agent_actions WHERE at > now() - interval '7 days'),
		(SELECT count(*) FROM orgs),
		(SELECT count(DISTINCT login) FROM org_members WHERE login NOT LIKE 'demo~%')`).
		Scan(&c.Apps, &c.Ejecuciones, &c.Versiones, &c.AgentesConLlave, &c.LlamadasDeAgentes, &c.Organizaciones, &c.Personas)
	return c, err
}

// RecibirSenal keeps a señal from an installation: its first and last time,
// how many came, whether it is the team's own, and its last counts. One
// that came within the hour only moves its last time (no address is kept,
// so this is what limits a sender).
func (s *Store) RecibirSenal(ctx context.Context, instalacion string, equipo bool, ultimo json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO senales (instalacion, equipo, ultimo) VALUES ($1, $2, $3)
		ON CONFLICT (instalacion) DO UPDATE SET ultima = now(), equipo = senales.equipo OR $2,
			envios = senales.envios + CASE WHEN senales.ultima < now() - interval '1 hour' THEN 1 ELSE 0 END,
			ultimo = CASE WHEN senales.ultima < now() - interval '1 hour' THEN $3 ELSE senales.ultimo END`,
		instalacion, equipo, ultimo)
	return err
}

// ResumenSenales is what the received señales say: installations heard of
// in the last 7 and 28 days, outside the team (the number that counts) and
// the team's own, and the outside ones of the last 28 days by mode, engine
// and version.
type ResumenSenales struct {
	Activas7      int            `json:"activas7"`
	Activas28     int            `json:"activas28"`
	Equipo28      int            `json:"equipo28"`
	Total         int            `json:"total"`
	PorModo       map[string]int `json:"porModo"`
	PorMotor      map[string]int `json:"porMotor"`
	PorVersion    map[string]int `json:"porVersion"`
	AppsEnFuera28 int            `json:"appsEnFuera28"`
}

// Resumen sums up the received señales.
func (s *Store) Resumen(ctx context.Context) (ResumenSenales, error) {
	r := ResumenSenales{PorModo: map[string]int{}, PorMotor: map[string]int{}, PorVersion: map[string]int{}}
	err := s.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE NOT equipo AND ultima > now() - interval '7 days'),
		count(*) FILTER (WHERE NOT equipo AND ultima > now() - interval '28 days'),
		count(*) FILTER (WHERE equipo AND ultima > now() - interval '28 days'),
		count(*) FILTER (WHERE NOT equipo),
		coalesce(sum((ultimo->'semana'->>'apps')::int) FILTER (WHERE NOT equipo AND ultima > now() - interval '28 days'), 0)
		FROM senales`).Scan(&r.Activas7, &r.Activas28, &r.Equipo28, &r.Total, &r.AppsEnFuera28)
	if err != nil {
		return r, err
	}
	rows, err := s.pool.Query(ctx, `SELECT coalesce(ultimo->>'modo', ''), coalesce(ultimo->>'motor', ''), coalesce(ultimo->>'version', '')
		FROM senales WHERE NOT equipo AND ultima > now() - interval '28 days'`)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var modo, motor, version string
		if err := rows.Scan(&modo, &motor, &version); err != nil {
			return r, err
		}
		r.PorModo[modo]++
		r.PorMotor[motor]++
		r.PorVersion[version]++
	}
	return r, rows.Err()
}
