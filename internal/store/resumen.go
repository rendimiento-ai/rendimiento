package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// DayFact is one thing that happened on a day, as data for the summary:
// the app it is about ("" for the organization), and what happened, in
// plain English. Commit messages and logs never go in: only what the
// platform itself records.
type DayFact struct {
	App  string `json:"app"`
	Text string `json:"text"`
}

// DayCounts are a day's numbers, for a summary without a model.
type DayCounts struct {
	App       string `json:"app"`
	Releases  int    `json:"releases"`
	Rollbacks int    `json:"rollbacks"`
	Failed    int    `json:"failed"`
	Problems  int    `json:"problems"`
}

// DayFacts is what an organization's apps, people and agents did between
// from and to.
func (s *Store) DayFacts(ctx context.Context, orgID int64, from, to time.Time) ([]DayFact, []DayCounts, error) {
	var facts []DayFact
	counts := map[string]*DayCounts{}
	count := func(app string) *DayCounts {
		if counts[app] == nil {
			counts[app] = &DayCounts{App: app}
		}
		return counts[app]
	}
	// Releases and rollbacks, with how their verification went.
	rows, err := s.pool.Query(ctx, `SELECT a.name, r.number, r.rollback_of, r.verify_status FROM releases r JOIN apps a ON a.id = r.app_id
		WHERE a.org_id = $1 AND r.created_at >= $2 AND r.created_at < $3 ORDER BY r.id`, orgID, from, to)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var app, verify string
		var n int
		var back *int
		if err := rows.Scan(&app, &n, &back, &verify); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if back != nil {
			count(app).Rollbacks++
			facts = append(facts, DayFact{App: app, Text: fmt.Sprintf("rolled back to release #%d (as #%d)", *back, n)})
			continue
		}
		count(app).Releases++
		facts = append(facts, DayFact{App: app, Text: fmt.Sprintf("release #%d, verification %s", n, verify)})
	}
	rows.Close()
	// Runs: how many passed and failed, per app and branch kind.
	rows, err = s.pool.Query(ctx, `SELECT a.name, r.deploy, r.status, count(*) FROM runs r JOIN apps a ON a.id = r.app_id
		WHERE a.org_id = $1 AND r.created_at >= $2 AND r.created_at < $3 AND r.status IN ('succeeded', 'failed')
		GROUP BY 1, 2, 3 ORDER BY 1, 2, 3`, orgID, from, to)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var app, status string
		var deploy bool
		var n int
		if err := rows.Scan(&app, &deploy, &status, &n); err != nil {
			rows.Close()
			return nil, nil, err
		}
		where := "branch"
		if deploy {
			where = "main-branch"
		}
		if status == RunFailed {
			count(app).Failed += n
		}
		facts = append(facts, DayFact{App: app, Text: fmt.Sprintf("%d %s runs %s", n, where, status)})
	}
	rows.Close()
	// Problems that started or were fixed.
	rows, err = s.pool.Query(ctx, `SELECT p.app, p.message, p.first_at >= $2, p.resolved_at IS NOT NULL AND p.resolved_at >= $2 AND p.resolved_at < $3
		FROM problems p JOIN apps a ON a.name = p.app
		WHERE a.org_id = $1 AND ((p.first_at >= $2 AND p.first_at < $3) OR (p.resolved_at >= $2 AND p.resolved_at < $3))
		ORDER BY p.id LIMIT 20`, orgID, from, to)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var app, msg string
		var started, fixed bool
		if err := rows.Scan(&app, &msg, &started, &fixed); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if started {
			count(app).Problems++
			facts = append(facts, DayFact{App: app, Text: "a problem started: " + clip(msg, 140)})
		}
		if fixed {
			facts = append(facts, DayFact{App: app, Text: "a problem was fixed: " + clip(msg, 140)})
		}
	}
	rows.Close()
	// What people and agents did (the bitácora), by who and what, per app.
	rows, err = s.pool.Query(ctx, `SELECT COALESCE(sobre->>'app', ''), quien_tipo, quien, accion, count(*) FROM bitacora
		WHERE org_id = $1 AND cuando >= $2 AND cuando < $3 AND resultado LIKE '2%'
		GROUP BY 1, 2, 3, 4 ORDER BY 5 DESC LIMIT 25`, orgID, from, to)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var app, kind, who, action string
		var n int
		if err := rows.Scan(&app, &kind, &who, &action, &n); err != nil {
			rows.Close()
			return nil, nil, err
		}
		facts = append(facts, DayFact{App: app, Text: fmt.Sprintf("%s %s did %q %d times", kind, who, action, n)})
	}
	rows.Close()
	// Encargos: made that day, and those waiting for a person's review now.
	var made, delivered int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE created_at >= $2 AND created_at < $3), count(*) FILTER (WHERE status = $4)
		FROM encargos WHERE org_id = $1`, orgID, from, to, EncargoDelivered).Scan(&made, &delivered); err != nil {
		return nil, nil, err
	}
	if made > 0 {
		facts = append(facts, DayFact{Text: fmt.Sprintf("%d encargos were made", made)})
	}
	if delivered > 0 {
		facts = append(facts, DayFact{Text: fmt.Sprintf("%d encargos are delivered and wait for a person's review", delivered)})
	}
	out := make([]DayCounts, 0, len(counts))
	for _, c := range counts {
		out = append(out, *c)
	}
	return facts, out, nil
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// SummaryLine is one line of a day's summary, about one app ("" for the
// organization).
type SummaryLine struct {
	App  string `json:"app"`
	Text string `json:"text"`
}

// DaySummary is what was written of one day, in one language.
type DaySummary struct {
	Day      string        `json:"day"` // 2006-01-02
	Lines    []SummaryLine `json:"lines"`
	Source   string        `json:"source"` // ia, counts or empty
	Provider string        `json:"provider,omitempty"`
}

// The ways a summary was written.
const (
	SummaryIA     = "ia"
	SummaryCounts = "counts"
	SummaryEmpty  = "empty"
)

// GetDaySummary is a day's summary in a language (ErrNotFound when none
// was written yet).
func (s *Store) GetDaySummary(ctx context.Context, orgID int64, day, lang string) (DaySummary, error) {
	out := DaySummary{Day: day}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT lines, source, provider FROM day_summaries WHERE org_id = $1 AND day = $2 AND lang = $3`,
		orgID, day, lang).Scan(&raw, &out.Source, &out.Provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(raw, &out.Lines)
}

// SaveDaySummary keeps a day's summary, replacing one already written
// (Rehacer).
func (s *Store) SaveDaySummary(ctx context.Context, orgID int64, lang string, d DaySummary) error {
	raw, err := json.Marshal(d.Lines)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO day_summaries (org_id, day, lang, lines, source, provider) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (org_id, day, lang) DO UPDATE SET lines = $4, source = $5, provider = $6, created_at = now()`,
		orgID, d.Day, lang, raw, d.Source, d.Provider)
	return err
}

// SummaryVisit is the day a person last opened the summary in an
// organization ("" the first time), and records today as their visit.
func (s *Store) SummaryVisit(ctx context.Context, orgID int64, login, today string) (string, error) {
	var last string
	err := s.pool.QueryRow(ctx, `SELECT seen_day::text FROM summary_visits WHERE org_id = $1 AND login = $2`, orgID, login).Scan(&last)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO summary_visits (org_id, login, seen_day) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, login) DO UPDATE SET seen_day = $3`, orgID, login, today)
	return last, err
}
