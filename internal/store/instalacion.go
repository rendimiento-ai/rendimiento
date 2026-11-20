package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// A new platform is installed from its setup page: the setup token, the
// forja its code lives on, and who administers it. Once it is, the setup
// page's doors close. "Installed" is kept in platform_settings (the key
// "instalada"), so no table changes; a platform installed before the key
// existed counts as installed when it has an administrator and a forja
// authorized (see EstadoInstalacion).

// EstadoInstalacion is what the setup page needs to know about the platform.
type EstadoInstalacion struct {
	// Marcada: the setup page finished here (someone proved the setup
	// token and became the first administrator).
	Marcada bool
	// Administradores: the platform's organization has an administrator.
	Administradores bool
	// ForjaAutorizada: a GitLab or a Bitbucket workspace is connected and
	// authorized (the GitHub App is not kept here; the caller adds it).
	ForjaAutorizada bool
}

// EstadoInstalacion reads the platform's installation state.
func (s *Store) EstadoInstalacion(ctx context.Context) (EstadoInstalacion, error) {
	var i EstadoInstalacion
	err := s.pool.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM platform_settings WHERE key = 'instalada'),
		EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND role = 'admin'),
		EXISTS (SELECT 1 FROM gitlab_connections WHERE username <> '') OR EXISTS (SELECT 1 FROM bitbucket_connections WHERE username <> '')`,
		DefaultOrg).Scan(&i.Marcada, &i.Administradores, &i.ForjaAutorizada)
	return i, err
}

// MarcarInstalada records that the setup page finished; by is who became
// the first administrator.
func (s *Store) MarcarInstalada(ctx context.Context, by string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO platform_settings (key, value, updated_by, updated_at) VALUES ('instalada', 'true', $1, now())
		ON CONFLICT (key) DO NOTHING`, by)
	return err
}

// Acceso is a person who may sign in: their name in rendimiento, whether
// they administer an organization, and the accounts linked to them on
// other forjas.
type Acceso struct {
	Login      string
	Admin      bool
	Identities []Identity
}

// Accesos lists every member of any organization (demo links' guests
// aside, who come in by their link), with their linked accounts.
func (s *Store) Accesos(ctx context.Context) ([]Acceso, error) {
	rows, err := s.pool.Query(ctx, `SELECT min(login), bool_or(role = 'admin') FROM org_members GROUP BY lower(login) ORDER BY lower(min(login))`)
	if err != nil {
		return nil, err
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Acceso, error) {
		var a Acceso
		err := r.Scan(&a.Login, &a.Admin)
		return a, err
	})
	if err != nil {
		return nil, err
	}
	out := []Acceso{}
	by := map[string]int{}
	for _, a := range all {
		if IsDemoLogin(a.Login) {
			continue
		}
		by[strings.ToLower(a.Login)] = len(out)
		out = append(out, a)
	}
	rows, err = s.pool.Query(ctx, `SELECT login, forja, base_url, external_id, username, created_at FROM forge_identities ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var i Identity
		if err := rows.Scan(&i.Login, &i.Forja, &i.BaseURL, &i.ExternalID, &i.Username, &i.CreatedAt); err != nil {
			return nil, err
		}
		if n, ok := by[strings.ToLower(i.Login)]; ok {
			out[n].Identities = append(out[n].Identities, i)
		}
	}
	return out, rows.Err()
}
