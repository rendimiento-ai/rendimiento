package gitlab

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// ParseWebhook checks a delivery's X-Gitlab-Token against each connection's
// webhook token (in constant time) and reads the events rendimiento acts
// on: a push to a branch (Push Hook) and a merge request opened, reopened,
// closed or merged (Merge Request Hook). Any other event comes back with
// only its Kind.
func (f *Forja) ParseWebhook(header http.Header, body []byte) (*forja.Event, error) {
	c, err := f.deliveredBy(context.Background(), header.Get("X-Gitlab-Token"))
	if err != nil {
		return nil, err
	}
	return parseWebhook(c, header.Get("X-Gitlab-Event"), body)
}

// deliveredBy is the connection whose webhook token a delivery carries.
func (f *Forja) deliveredBy(ctx context.Context, token string) (*Connection, error) {
	if f == nil || f.Connections == nil {
		return nil, ErrNotConnected
	}
	list, err := f.Connections.GitLabConnections(ctx)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotConnected
	}
	var found *Connection
	for _, c := range list {
		sec, err := f.secret(ctx, c.ID, false)
		if err != nil || sec.WebhookToken == "" {
			continue
		}
		// Every connection is compared, so the time taken says nothing of
		// which one matched.
		if subtle.ConstantTimeCompare([]byte(token), []byte(sec.WebhookToken)) == 1 && found == nil {
			found = c
		}
	}
	if token == "" || found == nil {
		return nil, forja.ErrBadSignature
	}
	return found, nil
}

// parseWebhook reads a delivery of connection c, already checked.
func parseWebhook(c *Connection, kind string, body []byte) (*forja.Event, error) {
	var payload struct {
		ObjectKind string `json:"object_kind"`
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Commits    []struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		} `json:"commits"`
		Project struct {
			PathWithNamespace string `json:"path_with_namespace"`
		} `json:"project"`
		Attrs *struct {
			IID          int    `json:"iid"`
			Action       string `json:"action"` // open, reopen, close, merge, update...
			SourceBranch string `json:"source_branch"`
			UpdatedAt    string `json:"updated_at"`
			LastCommit   struct {
				ID string `json:"id"`
			} `json:"last_commit"`
			Source struct {
				PathWithNamespace string `json:"path_with_namespace"`
			} `json:"source"`
			Target struct {
				PathWithNamespace string `json:"path_with_namespace"`
			} `json:"target"`
		} `json:"object_attributes"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, forja.ErrBadPayload
	}
	ev := &forja.Event{Kind: kind}
	switch {
	case kind == "Push Hook" || payload.ObjectKind == "push":
		branch, ok := strings.CutPrefix(payload.Ref, "refs/heads/")
		if !ok || payload.Project.PathWithNamespace == "" {
			break
		}
		p := &forja.Push{Installation: c.ID, Repo: RepoOf(c, payload.Project.PathWithNamespace), Branch: branch, SHA: payload.After,
			Deleted: strings.Trim(payload.After, "0") == ""}
		for _, cm := range payload.Commits {
			p.Messages = append(p.Messages, cm.Message)
			if cm.ID == payload.After {
				p.SkipCI = forja.SaysSkipCI(cm.Message)
			}
		}
		ev.Push = p
	case kind == "Merge Request Hook" || payload.ObjectKind == "merge_request":
		a := payload.Attrs
		if a == nil || a.IID == 0 {
			break
		}
		target := a.Target.PathWithNamespace
		if target == "" {
			target = payload.Project.PathWithNamespace
		}
		ch := &forja.ChangeEvent{Repo: RepoOf(c, target), Number: a.IID, HeadRef: a.SourceBranch, HeadSHA: a.LastCommit.ID}
		if a.Source.PathWithNamespace != "" {
			ch.HeadRepo = RepoOf(c, a.Source.PathWithNamespace)
		}
		switch a.Action {
		case "open":
			ch.Action = "opened"
		case "reopen":
			ch.Action = "reopened"
		case "close":
			ch.Action = "closed"
		case "merge":
			ch.Action, ch.Merged = "closed", true
			if at, ok := gitlabTime(a.UpdatedAt); ok {
				ch.MergedAt = &at
			}
		default:
			ch.Action = a.Action // update, approved...: ignored
		}
		ev.Change = ch
	}
	return ev, nil
}

// gitlabTime reads a webhook's time: "2026-11-19 10:00:00 UTC", or RFC 3339.
func gitlabTime(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
