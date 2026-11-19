package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// APIError is GitLab answering with a status of 300 or more.
type APIError struct {
	Status       int
	Method, Path string
	Body         string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("gitlab %s %s: %d", e.Method, e.Path, e.Status)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// Is makes errors.Is find forja.ErrNotFound in a 404, and forja.ErrConflict
// in a 409 (a merge whose head moved).
func (e *APIError) Is(target error) bool {
	switch target {
	case forja.ErrNotFound:
		return e.Status == http.StatusNotFound
	case forja.ErrConflict:
		return e.Status == http.StatusConflict
	}
	return false
}

// Says is the sentence GitLab gave for refusing ("" when none): its
// "message" (a text, a list or an object of lists) or its "error".
func (e *APIError) Says() string {
	var body struct {
		Message json.RawMessage `json:"message"`
		Error   string          `json:"error"`
		Desc    string          `json:"error_description"`
	}
	if json.Unmarshal([]byte(e.Body), &body) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(body.Message, &s) == nil && s != "" {
		return strings.TrimSuffix(s, ".")
	}
	var list []string
	if json.Unmarshal(body.Message, &list) == nil && len(list) > 0 {
		return strings.Join(list, "; ")
	}
	var fields map[string][]string
	if json.Unmarshal(body.Message, &fields) == nil && len(fields) > 0 {
		var parts []string
		for k, v := range fields {
			parts = append(parts, k+" "+strings.Join(v, ", "))
		}
		return strings.Join(parts, "; ")
	}
	if body.Desc != "" {
		return body.Desc
	}
	return body.Error
}

// api calls GitLab's API v4 on connection c with its token: path starts
// with "/" and is relative to /api/v4. A 401 refreshes the token once.
func (f *Forja) api(ctx context.Context, c *Connection, method, path string, body, out any) error {
	tok, err := f.token(ctx, c, false)
	if err != nil {
		return err
	}
	err = f.call(ctx, tok, method, strings.TrimRight(c.BaseURL, "/")+"/api/v4"+path, body, out)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
		if tok, err = f.token(ctx, c, true); err != nil {
			return err
		}
		return f.call(ctx, tok, method, strings.TrimRight(c.BaseURL, "/")+"/api/v4"+path, body, out)
	}
	return err
}

// call is one request with a bearer token; out, when not nil, is a
// *[]byte for the raw answer or anything JSON decodes into.
func (f *Forja) call(ctx context.Context, token, method, rawURL string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := f.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{Status: resp.StatusCode, Method: method, Path: req.URL.Path, Body: strings.TrimSpace(string(raw))}
	}
	switch o := out.(type) {
	case nil:
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	case *[]byte:
		*o, err = io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		return err
	default:
		return json.NewDecoder(resp.Body).Decode(out)
	}
}

// ---- OAuth ----

// AuthorizeURLFor is where a person authorizes the connection's
// application on its GitLab, with the scopes asked: GitLab sends them back
// to redirect with state and a code.
func AuthorizeURLFor(c *Connection, state, redirect, scope string) string {
	q := url.Values{"client_id": {c.ApplicationID}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {state}, "scope": {scope}}
	return strings.TrimRight(c.BaseURL, "/") + "/oauth/authorize?" + q.Encode()
}

// Tokens is GitLab's answer to /oauth/token.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	CreatedAt    int64  `json:"created_at"`
}

// expires is when the access token stops working (two hours when GitLab
// does not say).
func (t Tokens) expires() time.Time {
	from := time.Now()
	if t.CreatedAt > 0 {
		from = time.Unix(t.CreatedAt, 0)
	}
	if t.ExpiresIn <= 0 {
		return from.Add(2 * time.Hour)
	}
	return from.Add(time.Duration(t.ExpiresIn) * time.Second)
}

// oauthToken posts a grant to the connection's /oauth/token.
func (f *Forja) oauthToken(ctx context.Context, c *Connection, secret string, form url.Values) (*Tokens, error) {
	form.Set("client_id", c.ApplicationID)
	form.Set("client_secret", secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Method: http.MethodPost, Path: "/oauth/token", Body: strings.TrimSpace(string(raw))}
	}
	var t Tokens
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	if t.AccessToken == "" {
		return nil, errors.New("gitlab gave no token")
	}
	return &t, nil
}

// User is a GitLab account: its id never changes, its username may.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// Exchange trades a code for the account that authorized it and its
// tokens. redirect must be the one the authorization was asked with.
func (f *Forja) Exchange(ctx context.Context, c *Connection, secret, code, redirect string) (*User, *Tokens, error) {
	t, err := f.oauthToken(ctx, c, secret, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}})
	if err != nil {
		return nil, nil, err
	}
	var u User
	if err := f.call(ctx, t.AccessToken, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+"/api/v4/user", nil, &u); err != nil {
		return nil, nil, err
	}
	return &u, t, nil
}

// Authorized keeps the tokens a person's authorization gave the
// connection: rendimiento acts with them from now on.
func (f *Forja) Authorized(ctx context.Context, id int64, t *Tokens) error {
	sec, err := f.secret(ctx, id, true)
	if err != nil {
		return err
	}
	next := *sec
	next.AccessToken, next.RefreshToken, next.Expires = t.AccessToken, t.RefreshToken, t.expires()
	if err := f.Keys.Save(ctx, id, &next); err != nil {
		return err
	}
	f.remember(id, &next)
	return nil
}

// token is a valid access token for the connection: the kept one while it
// has five minutes left, else a refreshed one (which is kept: GitLab
// replaces the refresh token on each use). force refreshes it anyway.
func (f *Forja) token(ctx context.Context, c *Connection, force bool) (string, error) {
	sec, err := f.secret(ctx, c.ID, false)
	if err != nil {
		return "", err
	}
	if !force && sec.AccessToken != "" && time.Until(sec.Expires) > 5*time.Minute {
		return sec.AccessToken, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// Another process may have refreshed it already: what is kept wins.
	if f.Keys == nil {
		return "", ErrNotConnected
	}
	kept, err := f.Keys.Load(ctx, c.ID)
	if err != nil {
		return "", err
	}
	if kept == nil {
		return "", ErrNotConnected
	}
	if kept.AccessToken != sec.AccessToken && time.Until(kept.Expires) > 5*time.Minute {
		f.cache(c.ID, kept)
		return kept.AccessToken, nil
	}
	if kept.RefreshToken == "" {
		return "", fmt.Errorf("GitLab at %s was not authorized yet: connect it again on the Environment page", c.BaseURL)
	}
	t, err := f.oauthToken(ctx, c, kept.ApplicationSecret, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {kept.RefreshToken},
		"redirect_uri": {strings.TrimRight(f.BaseURL, "/") + CallbackPath}})
	if err != nil {
		return "", fmt.Errorf("refresh the GitLab token of %s: %w", c.BaseURL, err)
	}
	next := *kept
	next.AccessToken, next.Expires = t.AccessToken, t.expires()
	if t.RefreshToken != "" {
		next.RefreshToken = t.RefreshToken
	}
	if err := f.Keys.Save(ctx, c.ID, &next); err != nil {
		return "", err
	}
	f.cache(c.ID, &next)
	return next.AccessToken, nil
}

// cache is remember with f.mu already held.
func (f *Forja) cache(id int64, sec *Secret) {
	if f.secrets == nil {
		f.secrets = map[int64]*Secret{}
	}
	f.secrets[id] = sec
}

// first is the oldest connection: the one forja.SignIn signs in with.
func (f *Forja) first(ctx context.Context) (*Connection, *Secret, error) {
	if f == nil || f.Connections == nil {
		return nil, nil, ErrNotConnected
	}
	list, err := f.Connections.GitLabConnections(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(list) == 0 {
		return nil, nil, ErrNotConnected
	}
	return f.connection(ctx, list[0].ID)
}

// AuthorizeURL is where "Sign in with GitLab" sends people, on the oldest
// connection (the API signs in on the one a person picks: AuthorizeURLFor).
func (f *Forja) AuthorizeURL(state, redirect string) (string, error) {
	c, _, err := f.first(context.Background())
	if err != nil {
		return "", err
	}
	return AuthorizeURLFor(c, state, redirect, ScopeSignIn), nil
}

// SignIn trades a sign-in code of the oldest connection for the account's
// name in rendimiento's words (Login). The redirect is the callback.
func (f *Forja) SignIn(ctx context.Context, code string) (string, error) {
	c, sec, err := f.first(ctx)
	if err != nil {
		return "", err
	}
	u, _, err := f.Exchange(ctx, c, sec.ApplicationSecret, code, strings.TrimRight(f.BaseURL, "/")+CallbackPath)
	if err != nil {
		return "", err
	}
	return Login(c, u.Username), nil
}

// Login is how a GitLab account not linked to anyone is named in
// rendimiento: its username at its GitLab's address (ana@gitlab.com).
// GitHub logins have no "@", so the two never mix.
func Login(c *Connection, username string) string {
	host := Host(c.BaseURL)
	if u, err := url.Parse(c.BaseURL); err == nil && u.Host != "" {
		host = strings.ToLower(u.Host)
	}
	return username + "@" + host
}
