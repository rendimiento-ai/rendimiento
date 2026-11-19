package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// GitLabConnection is one GitLab rendimiento works with: gitlab.com or a
// GitLab of one's own (migration 0060). Its OAuth application's secret and
// the tokens rendimiento acts with are not here but with the platform's
// other secrets (see gitlab.Keys).
type GitLabConnection struct {
	ID            int64     `json:"id"`
	BaseURL       string    `json:"baseUrl"`       // https://gitlab.com
	ApplicationID string    `json:"applicationId"` // the OAuth application's id
	Username      string    `json:"username"`      // who rendimiento acts as ("" until authorized)
	AddedBy       string    `json:"addedBy"`
	CreatedAt     time.Time `json:"createdAt"`
}

const gitlabCols = `id, base_url, application_id, username, added_by, created_at`

func scanGitLab(row pgx.Row) (*GitLabConnection, error) {
	var c GitLabConnection
	if err := row.Scan(&c.ID, &c.BaseURL, &c.ApplicationID, &c.Username, &c.AddedBy, &c.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

// AddGitLabConnection keeps a new GitLab, or the same address again with a
// new application (it is reconnected, keeping its id).
func (s *Store) AddGitLabConnection(ctx context.Context, baseURL, applicationID, by string) (*GitLabConnection, error) {
	return scanGitLab(s.pool.QueryRow(ctx, `INSERT INTO gitlab_connections (base_url, application_id, added_by) VALUES ($1, $2, $3)
		ON CONFLICT (lower(base_url)) DO UPDATE SET application_id = EXCLUDED.application_id, added_by = EXCLUDED.added_by
		RETURNING `+gitlabCols, baseURL, applicationID, by))
}

// GitLabConnection is one connection, or ErrNotFound.
func (s *Store) GitLabConnection(ctx context.Context, id int64) (*GitLabConnection, error) {
	return scanGitLab(s.pool.QueryRow(ctx, `SELECT `+gitlabCols+` FROM gitlab_connections WHERE id = $1`, id))
}

// GitLabConnections lists every connection, the oldest first.
func (s *Store) GitLabConnections(ctx context.Context) ([]*GitLabConnection, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+gitlabCols+` FROM gitlab_connections ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*GitLabConnection{}
	for rows.Next() {
		c, err := scanGitLab(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetGitLabUsername records who rendimiento acts as on the connection,
// once a person authorized it.
func (s *Store) SetGitLabUsername(ctx context.Context, id int64, username string) error {
	_, err := s.pool.Exec(ctx, `UPDATE gitlab_connections SET username = $2 WHERE id = $1`, id, username)
	return err
}

// DeleteGitLabConnection forgets a connection; its apps, if any, stay and
// cannot build until it is connected again.
func (s *Store) DeleteGitLabConnection(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM gitlab_connections WHERE id = $1`, id)
	return err
}

// Identity is a person's account on another forja, linked to them: signing
// in with it is signing in as Login.
type Identity struct {
	Login      string    `json:"login"`
	Forja      string    `json:"forja"`
	BaseURL    string    `json:"baseUrl"`
	ExternalID string    `json:"externalId"`
	Username   string    `json:"username"`
	CreatedAt  time.Time `json:"createdAt"`
}

// ErrIdentityTaken: the account is already linked to another person.
var ErrIdentityTaken = errors.New("that account is already linked to another person")

// LinkIdentity links an account to login. Linking it again to the same
// person only refreshes its name; to another person, it is refused.
func (s *Store) LinkIdentity(ctx context.Context, id Identity) error {
	var owner string
	err := s.pool.QueryRow(ctx, `INSERT INTO forge_identities (login, forja, base_url, external_id, username) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (forja, base_url, external_id) DO UPDATE SET username = EXCLUDED.username
		RETURNING login`, id.Login, id.Forja, id.BaseURL, id.ExternalID, id.Username).Scan(&owner)
	if err != nil {
		return err
	}
	if owner != id.Login {
		return ErrIdentityTaken
	}
	return nil
}

// IdentityLogin is the person an account is linked to ("" when none).
func (s *Store) IdentityLogin(ctx context.Context, forja, baseURL, externalID string) (string, error) {
	var login string
	err := s.pool.QueryRow(ctx, `SELECT login FROM forge_identities WHERE forja = $1 AND base_url = $2 AND external_id = $3`, forja, baseURL, externalID).Scan(&login)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return login, err
}

// Identities lists the accounts linked to login.
func (s *Store) Identities(ctx context.Context, login string) ([]Identity, error) {
	rows, err := s.pool.Query(ctx, `SELECT login, forja, base_url, external_id, username, created_at FROM forge_identities WHERE lower(login) = lower($1) ORDER BY created_at`, login)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Identity{}
	for rows.Next() {
		var i Identity
		if err := rows.Scan(&i.Login, &i.Forja, &i.BaseURL, &i.ExternalID, &i.Username, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// UnlinkIdentity removes one of login's accounts.
func (s *Store) UnlinkIdentity(ctx context.Context, login, forja, baseURL, externalID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM forge_identities WHERE lower(login) = lower($1) AND forja = $2 AND base_url = $3 AND external_id = $4`, login, forja, baseURL, externalID)
	return err
}
