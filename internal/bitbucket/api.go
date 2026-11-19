package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// APIError is Bitbucket answering with a status of 300 or more.
type APIError struct {
	Status       int
	Method, Path string
	Body         string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("bitbucket %s %s: %d", e.Method, e.Path, e.Status)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// Is makes errors.Is find forja.ErrNotFound in a 404, and forja.ErrConflict
// in a 409.
func (e *APIError) Is(target error) bool {
	switch target {
	case forja.ErrNotFound:
		return e.Status == http.StatusNotFound
	case forja.ErrConflict:
		return e.Status == http.StatusConflict
	}
	return false
}

// Says is the sentence Bitbucket gave for refusing ("" when none): its
// error's message (and detail), or OAuth's error_description.
func (e *APIError) Says() string {
	var body struct {
		Error json.RawMessage `json:"error"`
		Desc  string          `json:"error_description"`
	}
	if json.Unmarshal([]byte(e.Body), &body) != nil {
		return ""
	}
	var inner struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(body.Error, &inner) == nil && inner.Message != "" {
		says := strings.TrimSuffix(inner.Message, ".")
		if inner.Detail != "" && inner.Detail != inner.Message {
			says += ": " + strings.TrimSuffix(inner.Detail, ".")
		}
		return says
	}
	if body.Desc != "" {
		return body.Desc
	}
	var s string
	if json.Unmarshal(body.Error, &s) == nil {
		return s
	}
	return ""
}

// api calls Bitbucket's API 2.0 as connection c: path starts with "/" and
// is relative to /2.0. A 401 refreshes the token once.
func (f *Forja) api(ctx context.Context, c *Connection, method, path string, body, out any) error {
	return f.apiRaw(ctx, c, method, f.apiURL()+path, body, out)
}

// apiRaw is api on a whole address (a page's "next", an archive).
func (f *Forja) apiRaw(ctx context.Context, c *Connection, method, rawURL string, body, out any) error {
	tok, err := f.token(ctx, c, false)
	if err != nil {
		return err
	}
	err = f.call(ctx, tok, method, rawURL, body, out)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
		if tok, err = f.token(ctx, c, true); err != nil {
			return err
		}
		return f.call(ctx, tok, method, rawURL, body, out)
	}
	return err
}

// form is a body sent as multipart/form-data (Bitbucket's /src takes
// files that way).
type form struct {
	contentType string
	data        []byte
}

// call is one request with a bearer token; out, when not nil, is a
// *[]byte for the raw answer or anything JSON decodes into.
func (f *Forja) call(ctx context.Context, token, method, rawURL string, body, out any) error {
	var rd io.Reader
	contentType := ""
	switch b := body.(type) {
	case nil:
	case form:
		rd, contentType = bytes.NewReader(b.data), b.contentType
	default:
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd, contentType = bytes.NewReader(raw), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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

// AuthorizeURLFor is where a person authorizes the connection's consumer
// on Bitbucket: it sends them back to the consumer's callback URL with
// state and a code. Bitbucket asks for the permissions the consumer has;
// it takes no scope here.
func (f *Forja) AuthorizeURLFor(c *Connection, state string) string {
	q := url.Values{"client_id": {c.ConsumerKey}, "response_type": {"code"}, "state": {state}}
	return f.siteURL() + "/site/oauth2/authorize?" + q.Encode()
}

// Tokens is Bitbucket's answer to /site/oauth2/access_token.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scopes       string `json:"scopes"`
}

// expires is when the access token stops working (two hours when
// Bitbucket does not say).
func (t Tokens) expires() time.Time {
	if t.ExpiresIn <= 0 {
		return time.Now().Add(2 * time.Hour)
	}
	return time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
}

// oauthToken posts a grant to Bitbucket's token endpoint, with the
// consumer's key and secret as basic auth.
func (f *Forja) oauthToken(ctx context.Context, c *Connection, secret string, grant url.Values) (*Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.siteURL()+"/site/oauth2/access_token", strings.NewReader(grant.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.ConsumerKey, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Method: http.MethodPost, Path: "/site/oauth2/access_token", Body: strings.TrimSpace(string(raw))}
	}
	var t Tokens
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	if t.AccessToken == "" {
		return nil, errors.New("bitbucket gave no token")
	}
	return &t, nil
}

// User is a Bitbucket account: its account id (or uuid) never changes,
// its nickname may.
type User struct {
	UUID        string `json:"uuid"`
	AccountID   string `json:"account_id"`
	Nickname    string `json:"nickname"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// ID is the account's id that never changes.
func (u *User) ID() string {
	if u.AccountID != "" {
		return u.AccountID
	}
	return u.UUID
}

// notName is what a nickname may have that a login may not.
var notName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// Name is the account's name as rendimiento writes it: its nickname (or
// username), with what a login cannot have as dashes.
func (u *User) Name() string {
	n := u.Nickname
	if n == "" {
		n = u.Username
	}
	if n == "" {
		n = u.DisplayName
	}
	n = strings.Trim(notName.ReplaceAllString(n, "-"), "-.")
	if n == "" {
		n = strings.Trim(u.UUID, "{}")
	}
	return n
}

// Exchange trades a code for the account that authorized it and its
// tokens.
func (f *Forja) Exchange(ctx context.Context, c *Connection, secret, code string) (*User, *Tokens, error) {
	t, err := f.oauthToken(ctx, c, secret, url.Values{"grant_type": {"authorization_code"}, "code": {code}})
	if err != nil {
		return nil, nil, err
	}
	var u User
	if err := f.call(ctx, t.AccessToken, http.MethodGet, f.apiURL()+"/user", nil, &u); err != nil {
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
// has five minutes left, else a refreshed one, which is kept each time
// (with the refresh token Bitbucket answers). force refreshes it anyway.
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
	if f.Keys == nil {
		return "", ErrNotConnected
	}
	// Another process may have refreshed it already: what is kept wins.
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
		return "", fmt.Errorf("the Bitbucket workspace %s was not authorized yet: connect it again on the Environment page", c.Workspace)
	}
	t, err := f.oauthToken(ctx, c, kept.ConsumerSecret, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {kept.RefreshToken}})
	if err != nil {
		return "", fmt.Errorf("refresh the Bitbucket token of %s: %w", c.Workspace, err)
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

// first is the oldest connection: the one forja.SignIn signs in with.
func (f *Forja) first(ctx context.Context) (*Connection, *Secret, error) {
	if f == nil || f.Connections == nil {
		return nil, nil, ErrNotConnected
	}
	list, err := f.Connections.BitbucketConnections(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(list) == 0 {
		return nil, nil, ErrNotConnected
	}
	return f.connection(ctx, list[0].ID)
}

// AuthorizeURL is where "Sign in with Bitbucket" sends people, on the
// oldest connection (the API signs in on the one a person picks:
// AuthorizeURLFor). Bitbucket sends them back to the consumer's callback
// URL, whatever redirect says.
func (f *Forja) AuthorizeURL(state, _ string) (string, error) {
	c, _, err := f.first(context.Background())
	if err != nil {
		return "", err
	}
	return f.AuthorizeURLFor(c, state), nil
}

// SignIn trades a sign-in code of the oldest connection for the account's
// name in rendimiento's words (Login).
func (f *Forja) SignIn(ctx context.Context, code string) (string, error) {
	c, sec, err := f.first(ctx)
	if err != nil {
		return "", err
	}
	u, _, err := f.Exchange(ctx, c, sec.ConsumerSecret, code)
	if err != nil {
		return "", err
	}
	return Login(u), nil
}

// Login is how a Bitbucket account not linked to anyone is named in
// rendimiento: its nickname at bitbucket.org (ana@bitbucket.org). GitHub
// logins have no "@", so the two never mix.
func Login(u *User) string { return u.Name() + "@" + Host }

// IdentityBase is the address forge_identities keeps Bitbucket accounts
// under: one for every workspace, since an account is Bitbucket's.
const IdentityBase = DefaultSiteURL
