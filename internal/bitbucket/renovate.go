package bitbucket

import (
	"context"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// Renovate is what a Renovate run on the workspace's repositories needs:
// the API (without its /2.0, which Renovate adds), a token that lasts at least last (refreshed first when the kept
// one would not), and each repository's full name (workspace/repo).
func (f *Forja) Renovate(ctx context.Context, installation int64, repos []string, last time.Duration) (*forja.Renovate, error) {
	c, _, err := f.connection(ctx, installation)
	if err != nil {
		return nil, err
	}
	tok, err := f.tokenLasting(ctx, c, last)
	if err != nil {
		return nil, err
	}
	out := &forja.Renovate{Platform: "bitbucket", Endpoint: strings.TrimSuffix(f.apiURL(), "/2.0") + "/", Token: tok, Repos: map[string]string{}}
	for _, r := range repos {
		out.Repos[fullName(r)] = r
	}
	return out, nil
}

// tokenLasting is a token with at least last left: the kept one, or a
// refreshed one.
func (f *Forja) tokenLasting(ctx context.Context, c *Connection, last time.Duration) (string, error) {
	sec, err := f.secret(ctx, c.ID, true)
	if err != nil {
		return "", err
	}
	return f.token(ctx, c, sec.AccessToken == "" || time.Until(sec.Expires) < last+5*time.Minute)
}
