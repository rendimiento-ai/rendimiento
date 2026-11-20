package bitbucket

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// Sign is a delivery's signature with a webhook's secret, as Bitbucket
// writes it in X-Hub-Signature: sha256=<hex of HMAC-SHA256>.
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// ParseWebhook checks a delivery's X-Hub-Signature against each
// connection's webhook secret (in constant time) and reads the events
// rendimiento acts on: a push to a branch (repo:push) and a pull request
// opened, merged or declined (pullrequest:created, :fulfilled, :rejected).
// Any other event comes back with only its Kind.
func (f *Forja) ParseWebhook(header http.Header, body []byte) (*forja.Event, error) {
	c, err := f.deliveredBy(context.Background(), header.Get("X-Hub-Signature"), body)
	if err != nil {
		return nil, err
	}
	ev, err := parseWebhook(c, header.Get("X-Event-Key"), body)
	if err == nil && ev.Change != nil {
		f.fullHead(c, ev.Change)
	}
	return ev, err
}

// fullHead writes the whole commit of a pull request's head: Bitbucket's
// webhook cuts it to 12 characters, and the libreta notes a merge with the
// whole one (as runs have it). When Bitbucket cannot say, the short one
// stays.
func (f *Forja) fullHead(c *Connection, ch *forja.ChangeEvent) {
	if ch.HeadSHA == "" || len(ch.HeadSHA) == 40 || (ch.Action != "closed" && ch.Action != "opened") {
		return
	}
	repo := ch.Repo
	if ch.HeadRepo != "" {
		repo = ch.HeadRepo
	}
	if !InWorkspace(c, repo) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if cm, err := f.commit(ctx, c.ID, repo, ch.HeadSHA); err == nil && strings.HasPrefix(cm.Hash, ch.HeadSHA) {
		ch.HeadSHA = cm.Hash
	}
}

// deliveredBy is the connection whose webhook secret signed a delivery.
func (f *Forja) deliveredBy(ctx context.Context, signature string, body []byte) (*Connection, error) {
	if f == nil || f.Connections == nil {
		return nil, ErrNotConnected
	}
	list, err := f.Connections.BitbucketConnections(ctx)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotConnected
	}
	var found *Connection
	for _, c := range list {
		sec, err := f.secret(ctx, c.ID, false)
		if err != nil || sec.WebhookSecret == "" {
			continue
		}
		// Every connection is compared, so the time taken says nothing of
		// which one matched.
		if hmac.Equal([]byte(signature), []byte(Sign(sec.WebhookSecret, body))) && found == nil {
			found = c
		}
	}
	if signature == "" || found == nil {
		return nil, forja.ErrBadSignature
	}
	return found, nil
}

// bbRef is a branch (or tag) in a push.
type bbRef struct {
	Type   string `json:"type"` // branch, tag
	Name   string `json:"name"`
	Target struct {
		Hash    string `json:"hash"`
		Message string `json:"message"`
	} `json:"target"`
}

// parseWebhook reads a delivery of connection c, already checked. A push
// of several branches at once is read for each: the first in Push, the
// rest in More.
func parseWebhook(c *Connection, kind string, body []byte) (*forja.Event, error) {
	var payload struct {
		Push *struct {
			Changes []struct {
				New     *bbRef `json:"new"`
				Old     *bbRef `json:"old"`
				Commits []struct {
					Hash    string `json:"hash"`
					Message string `json:"message"`
				} `json:"commits"`
			} `json:"changes"`
		} `json:"push"`
		PullRequest *bbPR `json:"pullrequest"`
		Repository  struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, forja.ErrBadPayload
	}
	ev := &forja.Event{Kind: kind}
	repo := payload.Repository.FullName
	if repo == "" || !InWorkspace(c, RepoOf(repo)) {
		return ev, nil // not the workspace's: nothing to act on
	}
	switch {
	case kind == "repo:push" && payload.Push != nil:
		for _, ch := range payload.Push.Changes {
			p := &forja.Push{Installation: c.ID, Repo: RepoOf(repo)}
			switch {
			case ch.New != nil && ch.New.Type == "branch":
				p.Branch, p.SHA = ch.New.Name, ch.New.Target.Hash
				p.SkipCI = forja.SaysSkipCI(ch.New.Target.Message)
			case ch.New == nil && ch.Old != nil && ch.Old.Type == "branch":
				p.Branch, p.Deleted = ch.Old.Name, true
			default:
				continue // a tag
			}
			// Bitbucket lists the newest first; the platform reads them
			// oldest first, as GitHub sends them.
			for i := len(ch.Commits) - 1; i >= 0; i-- {
				p.Messages = append(p.Messages, ch.Commits[i].Message)
			}
			if len(p.Messages) == 0 && ch.New != nil && ch.New.Target.Message != "" {
				p.Messages = []string{ch.New.Target.Message}
			}
			if ev.Push == nil {
				ev.Push = p
			} else {
				ev.More = append(ev.More, p)
			}
		}
	case strings.HasPrefix(kind, "pullrequest:") && payload.PullRequest != nil && payload.PullRequest.ID > 0:
		pr := payload.PullRequest
		target := pr.Destination.Repository.FullName
		if target == "" {
			target = repo
		}
		ch := &forja.ChangeEvent{Repo: RepoOf(target), Number: pr.ID, HeadRef: pr.Source.Branch.Name, HeadSHA: pr.Source.Commit.Hash}
		if src := pr.Source.Repository.FullName; src != "" {
			ch.HeadRepo = RepoOf(src)
		}
		switch kind {
		case "pullrequest:created":
			ch.Action = "opened"
		case "pullrequest:rejected":
			ch.Action = "closed"
		case "pullrequest:fulfilled":
			ch.Action, ch.Merged = "closed", true
			if !pr.UpdatedOn.IsZero() {
				at := pr.UpdatedOn.UTC().Truncate(time.Second)
				ch.MergedAt = &at
			}
		default:
			ch.Action = strings.TrimPrefix(kind, "pullrequest:") // updated, approved...: ignored
		}
		ev.Change = ch
	}
	return ev, nil
}
