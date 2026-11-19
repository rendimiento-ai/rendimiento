package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// The Holder is GitHub's forja.
var _ forja.Forja = (*Holder)(nil)

// Name is the forja's short name.
func (h *Holder) Name() string { return "github" }

// Ready is nil once the GitHub App is set up (ErrNotConfigured before).
func (h *Holder) Ready() error {
	_, err := h.Get()
	return err
}

// Allows says whether the App serves an account (see App.Allows); no
// account is served before setup.
func (h *Holder) Allows(account string) bool {
	a, err := h.Get()
	return err == nil && a.Allows(account)
}

// AuthorizeURL is where "Sign in with GitHub" sends people.
func (h *Holder) AuthorizeURL(state, redirect string) (string, error) {
	a, err := h.Get()
	if err != nil {
		return "", err
	}
	return a.AuthorizeURL(state, redirect), nil
}

// SignIn trades a sign-in code for the person's GitHub login.
func (h *Holder) SignIn(ctx context.Context, code string) (string, error) {
	a, err := h.Get()
	if err != nil {
		return "", err
	}
	return a.Login(ctx, code)
}

// ParseWebhook checks a delivery's X-Hub-Signature-256 with the App's
// webhook secret and reads the events rendimiento acts on: a push to a
// branch, and a pull request opened, reopened or closed. Any other event
// comes back with only its Kind.
func (h *Holder) ParseWebhook(header http.Header, body []byte) (*forja.Event, error) {
	a, err := h.Get()
	if err != nil {
		return nil, err
	}
	if !VerifyWebhook(a.Credentials().WebhookSecret, body, header.Get("X-Hub-Signature-256")) {
		return nil, forja.ErrBadSignature
	}
	return parseWebhook(header.Get("X-GitHub-Event"), body)
}

// parseWebhook reads a delivery already verified.
func parseWebhook(kind string, body []byte) (*forja.Event, error) {
	var payload struct {
		Action      string `json:"action"` // pull_request: opened, closed...
		Number      int    `json:"number"`
		PullRequest *struct {
			Merged   bool       `json:"merged"`
			MergedAt *time.Time `json:"merged_at"`
			Head     struct {
				Ref  string `json:"ref"`
				SHA  string `json:"sha"`
				Repo *struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"head"`
		} `json:"pull_request"`
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Deleted    bool   `json:"deleted"`
		HeadCommit *struct {
			Message string `json:"message"`
		} `json:"head_commit"`
		Commits []struct {
			Message string `json:"message"`
		} `json:"commits"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, forja.ErrBadPayload
	}
	ev := &forja.Event{Kind: kind}
	switch kind {
	case "push":
		if branch, ok := strings.CutPrefix(payload.Ref, "refs/heads/"); ok {
			p := &forja.Push{Installation: payload.Installation.ID, Repo: payload.Repository.FullName, Branch: branch, SHA: payload.After, Deleted: payload.Deleted,
				SkipCI: payload.HeadCommit != nil && forja.SaysSkipCI(payload.HeadCommit.Message)}
			for _, c := range payload.Commits {
				p.Messages = append(p.Messages, c.Message)
			}
			if len(p.Messages) == 0 && payload.HeadCommit != nil {
				p.Messages = []string{payload.HeadCommit.Message}
			}
			ev.Push = p
		}
	case "pull_request":
		if payload.Number > 0 {
			c := &forja.ChangeEvent{Repo: payload.Repository.FullName, Number: payload.Number, Action: payload.Action}
			if pr := payload.PullRequest; pr != nil {
				c.Merged, c.MergedAt = pr.Merged, pr.MergedAt
				c.HeadRef, c.HeadSHA = pr.Head.Ref, pr.Head.SHA
				if pr.Head.Repo != nil {
					c.HeadRepo = pr.Head.Repo.FullName
				}
			}
			ev.Change = c
		}
	}
	return ev, nil
}
