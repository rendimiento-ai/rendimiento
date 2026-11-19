package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// BitbucketConnection is one Bitbucket Cloud workspace rendimiento works
// with (migration 0061). Its OAuth consumer's secret and the tokens
// rendimiento acts with are not here but with the platform's other secrets
// (see bitbucket.Keys).
type BitbucketConnection struct {
	ID          int64     `json:"id"`
	Workspace   string    `json:"workspace"`   // the workspace's slug
	ConsumerKey string    `json:"consumerKey"` // the OAuth consumer's key
	Username    string    `json:"username"`    // who rendimiento acts as ("" until authorized)
	AddedBy     string    `json:"addedBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

const bitbucketCols = `id, workspace, consumer_key, username, added_by, created_at`

func scanBitbucket(row pgx.Row) (*BitbucketConnection, error) {
	var c BitbucketConnection
	if err := row.Scan(&c.ID, &c.Workspace, &c.ConsumerKey, &c.Username, &c.AddedBy, &c.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

// AddBitbucketConnection keeps a new workspace, or the same workspace again
// with a new consumer (it is reconnected, keeping its id).
func (s *Store) AddBitbucketConnection(ctx context.Context, workspace, consumerKey, by string) (*BitbucketConnection, error) {
	return scanBitbucket(s.pool.QueryRow(ctx, `INSERT INTO bitbucket_connections (workspace, consumer_key, added_by) VALUES ($1, $2, $3)
		ON CONFLICT (lower(workspace)) DO UPDATE SET consumer_key = EXCLUDED.consumer_key, added_by = EXCLUDED.added_by
		RETURNING `+bitbucketCols, workspace, consumerKey, by))
}

// BitbucketConnection is one connection, or ErrNotFound.
func (s *Store) BitbucketConnection(ctx context.Context, id int64) (*BitbucketConnection, error) {
	return scanBitbucket(s.pool.QueryRow(ctx, `SELECT `+bitbucketCols+` FROM bitbucket_connections WHERE id = $1`, id))
}

// BitbucketConnections lists every connection, the oldest first.
func (s *Store) BitbucketConnections(ctx context.Context) ([]*BitbucketConnection, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+bitbucketCols+` FROM bitbucket_connections ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*BitbucketConnection{}
	for rows.Next() {
		c, err := scanBitbucket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetBitbucketUsername records who rendimiento acts as on the connection,
// once a person authorized it.
func (s *Store) SetBitbucketUsername(ctx context.Context, id int64, username string) error {
	_, err := s.pool.Exec(ctx, `UPDATE bitbucket_connections SET username = $2 WHERE id = $1`, id, username)
	return err
}

// DeleteBitbucketConnection forgets a connection; its apps, if any, stay
// and cannot build until it is connected again.
func (s *Store) DeleteBitbucketConnection(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM bitbucket_connections WHERE id = $1`, id)
	return err
}
