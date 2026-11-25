package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rendimiento-ai/rendimiento/internal/tachar"
)

// ReleaseEvidence is what was seen of a release when it failed
// verification, kept before the rollback removes its pods (or its
// containers): each service's new pods with their state, the events about
// them and the last lines they wrote. Kubernetes keeps events about an hour
// and a rolled-back release's pods not at all, so without it nobody can say
// later why the release failed.
type ReleaseEvidence struct {
	CapturedAt time.Time         `json:"capturedAt"`
	Where      string            `json:"where"` // the destination: kubernetes, local
	Services   []ServiceEvidence `json:"services"`
	Events     []EvidenceEvent   `json:"events,omitempty"`
	// Notes say what could not be read (a list the platform may not read).
	Notes []string `json:"notes,omitempty"`
	// Truncated: logs were shortened to fit MaxEvidence.
	Truncated bool `json:"truncated,omitempty"`
}

// ServiceEvidence is one service of the failed release: its new
// ReplicaSet and pods (in a computer, its container).
type ServiceEvidence struct {
	Name       string `json:"name"`
	ReplicaSet string `json:"replicaSet,omitempty"`
	// Message is why its Deployment is not progressing or available.
	Message string        `json:"message,omitempty"`
	Pods    []PodEvidence `json:"pods"`
}

// PodEvidence is a pod (or a container of a computer) of the release.
type PodEvidence struct {
	Name  string `json:"name"`
	Phase string `json:"phase"`
	Node  string `json:"node,omitempty"`
	// Conditions are the ones not true: "Ready: ContainersNotReady (…)".
	Conditions []string            `json:"conditions,omitempty"`
	Containers []ContainerEvidence `json:"containers"`
}

// ContainerEvidence is one container's state and its last lines.
type ContainerEvidence struct {
	Name     string `json:"name"`
	Ready    bool   `json:"ready"`
	Restarts int32  `json:"restarts"`
	// State is waiting, running or terminated, with its reason and message.
	State    string `json:"state"`
	Reason   string `json:"reason,omitempty"`
	Message  string `json:"message,omitempty"`
	ExitCode *int32 `json:"exitCode,omitempty"`
	// The last time it ended (before its restart).
	LastReason     string     `json:"lastReason,omitempty"`
	LastExitCode   *int32     `json:"lastExitCode,omitempty"`
	LastFinishedAt *time.Time `json:"lastFinishedAt,omitempty"`
	// Log is the end of what it wrote; PreviousLog, of the run before its
	// last restart.
	Log         string `json:"log,omitempty"`
	PreviousLog string `json:"previousLog,omitempty"`
}

// EvidenceEvent is one Kubernetes event about the release's Deployment,
// ReplicaSet or pods.
type EvidenceEvent struct {
	At      time.Time `json:"at"`
	Type    string    `json:"type"` // Normal, Warning
	Reason  string    `json:"reason"`
	Object  string    `json:"object"` // Pod/web-6d9f…
	Message string    `json:"message"`
	Count   int32     `json:"count,omitempty"`
}

// MaxEvidence is the most kept of one release's evidence, in bytes of JSON.
const MaxEvidence = 64 << 10

// EvidenceDays is how long a release's evidence is kept.
const EvidenceDays = 30

// Redact leaves out every line that looks like a secret, in the logs and
// in the messages (an app may print its DATABASE_URL as it starts), and
// what Postgres cannot keep in JSON (NUL bytes, invalid UTF-8).
func (e *ReleaseEvidence) Redact() {
	clean := func(s string) string {
		return tachar.Lineas(strings.ReplaceAll(strings.ToValidUTF8(s, "\uFFFD"), "\x00", ""))
	}
	for i := range e.Services {
		s := &e.Services[i]
		s.Message = clean(s.Message)
		for j := range s.Pods {
			p := &s.Pods[j]
			for k := range p.Conditions {
				p.Conditions[k] = clean(p.Conditions[k])
			}
			for k := range p.Containers {
				c := &p.Containers[k]
				c.Message, c.Log, c.PreviousLog = clean(c.Message), clean(c.Log), clean(c.PreviousLog)
			}
		}
	}
	for i := range e.Events {
		e.Events[i].Message = clean(e.Events[i].Message)
	}
	for i := range e.Notes {
		e.Notes[i] = clean(e.Notes[i])
	}
}

// Cap shortens the logs, the longest first and keeping their ends (where
// the error is), until the evidence is at most limit bytes of JSON; past
// that, it drops the oldest events.
func (e *ReleaseEvidence) Cap(limit int) {
	size := func() int {
		b, _ := json.Marshal(e)
		return len(b)
	}
	for n := size(); n > limit; n = size() {
		longest := (*string)(nil)
		for i := range e.Services {
			for j := range e.Services[i].Pods {
				for k := range e.Services[i].Pods[j].Containers {
					c := &e.Services[i].Pods[j].Containers[k]
					for _, l := range []*string{&c.Log, &c.PreviousLog} {
						if longest == nil || len(*l) > len(*longest) {
							longest = l
						}
					}
				}
			}
		}
		e.Truncated = true
		over := n - limit
		switch {
		case longest != nil && len(*longest) > 256:
			// At most half of it at a time, so the logs are shortened
			// alike; a little more than over, as JSON escapes count too.
			*longest = tailFrom(*longest, min(over+64, len(*longest)/2))
		case len(e.Events) > 0:
			e.Events = e.Events[1:]
		case longest != nil && *longest != "":
			*longest = ""
		default:
			return // states and notes only: nothing more to shorten
		}
	}
}

// tailFrom is s from byte i on, starting at the next whole line.
func tailFrom(s string, i int) string {
	if i <= 0 {
		return s
	}
	if i >= len(s) {
		return ""
	}
	rest := s[i:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 && nl < len(rest)-1 {
		rest = rest[nl+1:]
	}
	return "…\n" + strings.ToValidUTF8(rest, "")
}

// Summary is the evidence in a few lines for agents (app_status): each
// pod's state and its containers', the warnings among the events, and the
// last lines of each container that is not ready (of every container when
// all were: the release failed its checks), at most lines of each.
func (e *ReleaseEvidence) Summary(lines int) string {
	allReady := true
	for _, s := range e.Services {
		for _, p := range s.Pods {
			for _, c := range p.Containers {
				allReady = allReady && c.Ready
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "What was seen when it failed (%s, %s):\n", e.Where, e.CapturedAt.UTC().Format("2006-01-02 15:04"))
	for _, s := range e.Services {
		fmt.Fprintf(&b, "- service %s", s.Name)
		if s.ReplicaSet != "" {
			fmt.Fprintf(&b, ", ReplicaSet %s", s.ReplicaSet)
		}
		if s.Message != "" {
			fmt.Fprintf(&b, ": %s", s.Message)
		}
		if len(s.Pods) == 0 {
			b.WriteString(" (no pods)")
		}
		b.WriteString("\n")
		for _, p := range s.Pods {
			fmt.Fprintf(&b, "  %s %s", p.Name, p.Phase)
			if len(p.Conditions) > 0 {
				fmt.Fprintf(&b, " (%s)", strings.Join(p.Conditions, "; "))
			}
			b.WriteString("\n")
			for _, c := range p.Containers {
				fmt.Fprintf(&b, "    %s: %s", c.Name, c.Describe())
				b.WriteString("\n")
				if c.Ready && !allReady {
					continue
				}
				log := c.Log
				if strings.TrimSpace(log) == "" {
					log = c.PreviousLog
				}
				if t := lastLines(log, lines); t != "" {
					b.WriteString("      last lines:\n")
					for _, l := range strings.Split(t, "\n") {
						b.WriteString("      | " + l + "\n")
					}
				}
			}
		}
	}
	var warn []EvidenceEvent
	for _, ev := range e.Events {
		if ev.Type != "Normal" {
			warn = append(warn, ev)
		}
	}
	if len(warn) > 10 {
		warn = warn[len(warn)-10:]
	}
	if len(warn) > 0 {
		b.WriteString("Warning events:\n")
		for _, ev := range warn {
			fmt.Fprintf(&b, "- %s %s %s: %s", ev.At.UTC().Format("15:04:05"), ev.Object, ev.Reason, ev.Message)
			if ev.Count > 1 {
				fmt.Fprintf(&b, " (%d times)", ev.Count)
			}
			b.WriteString("\n")
		}
	}
	for _, n := range e.Notes {
		fmt.Fprintf(&b, "Note: %s\n", n)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Describe is a container's state in a line: "waiting CrashLoopBackOff, 5
// restarts, last exited 1 (Error)".
func (c ContainerEvidence) Describe() string {
	parts := []string{c.State}
	if c.Reason != "" {
		parts[0] += " " + c.Reason
	}
	if c.ExitCode != nil {
		parts[0] += fmt.Sprintf(" (exit code %d)", *c.ExitCode)
	}
	if c.Message != "" {
		parts[0] += ": " + strings.SplitN(c.Message, "\n", 2)[0]
	}
	if c.Ready {
		parts = append(parts, "ready")
	}
	if c.Restarts > 0 {
		parts = append(parts, fmt.Sprintf("%d restarts", c.Restarts))
	}
	if c.LastExitCode != nil {
		last := fmt.Sprintf("last exited %d", *c.LastExitCode)
		if c.LastReason != "" {
			last += " (" + c.LastReason + ")"
		}
		parts = append(parts, last)
	}
	return strings.Join(parts, ", ")
}

// lastLines is the last n lines of s.
func lastLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" || n <= 0 {
		return ""
	}
	l := strings.Split(s, "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

// SaveReleaseEvidence keeps a release's evidence (replacing any kept
// before), redacted and capped to MaxEvidence.
func (s *Store) SaveReleaseEvidence(ctx context.Context, releaseID int64, e *ReleaseEvidence) error {
	e.Redact()
	e.Cap(MaxEvidence)
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO release_evidence (release_id, captured_at, evidence) VALUES ($1, $2, $3)
		ON CONFLICT (release_id) DO UPDATE SET captured_at = $2, evidence = $3`, releaseID, e.CapturedAt, b)
	return err
}

// ReleaseEvidence is what was kept of an app's release when it failed
// (ErrNotFound: nothing was kept, or it is older than EvidenceDays).
func (s *Store) ReleaseEvidence(ctx context.Context, appID, number int64) (*ReleaseEvidence, error) {
	var b []byte
	err := s.pool.QueryRow(ctx, `SELECT e.evidence FROM release_evidence e JOIN releases r ON r.id = e.release_id
		WHERE r.app_id = $1 AND r.number = $2`, appID, number).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var e ReleaseEvidence
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// ReleasesWithEvidence says which of the releases have evidence kept.
func (s *Store) ReleasesWithEvidence(ctx context.Context, releaseIDs []int64) (map[int64]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT release_id FROM release_evidence WHERE release_id = ANY($1)`, releaseIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// PruneReleaseEvidence deletes the evidence captured before `before`.
func (s *Store) PruneReleaseEvidence(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM release_evidence WHERE captured_at < $1`, before)
	return tag.RowsAffected(), err
}
