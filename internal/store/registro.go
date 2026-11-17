package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LogLine is one line of the platform's own log (the Log page): what one
// process logged at INFO or above, its secrets already removed.
type LogLine struct {
	ID      int64          `json:"id"`
	At      time.Time      `json:"at"`
	Level   string         `json:"level"`
	Process string         `json:"process"`
	Message string         `json:"msg"` // not "message": the log is shown as it was written, untranslated
	App     string         `json:"app,omitempty"`
	Run     *int64         `json:"run,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// AddLogLines writes lines of the platform's log, all at once.
func (s *Store) AddLogLines(ctx context.Context, lines []LogLine) error {
	if len(lines) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(`INSERT INTO registro (at, level, process, message, app, run_id, attrs) VALUES `)
	args := make([]any, 0, len(lines)*7)
	for i, l := range lines {
		attrs := []byte("{}")
		if len(l.Attrs) > 0 {
			b, err := json.Marshal(l.Attrs)
			if err != nil {
				return err
			}
			attrs = b
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		n := i * 7
		fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d, $%d, $%d, $%d)", n+1, n+2, n+3, n+4, n+5, n+6, n+7)
		args = append(args, l.At, l.Level, l.Process, l.Message, l.App, l.Run, attrs)
	}
	_, err := s.pool.Exec(ctx, sb.String(), args...)
	return err
}

// LogFilter picks lines of the platform's log. Empty fields do not filter.
type LogFilter struct {
	Process string
	App     string
	Levels  []string // any of these
	Since   time.Time
	Until   time.Time
	Text    string // in the message, ignoring case
	Before  string // a cursor: LogCursor of the last line already shown
	Limit   int
}

// LogCursor is where a page of the log ends: the next page starts with the
// line just older than this one.
func LogCursor(l LogLine) string {
	return l.At.UTC().Format(time.RFC3339Nano) + "_" + strconv.FormatInt(l.ID, 10)
}

// ErrBadCursor is a cursor that LogCursor did not make.
var ErrBadCursor = errors.New("not a cursor of the log")

func parseLogCursor(c string) (time.Time, int64, error) {
	at, id, ok := strings.Cut(c, "_")
	if !ok {
		return time.Time{}, 0, ErrBadCursor
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, 0, ErrBadCursor
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return time.Time{}, 0, ErrBadCursor
	}
	return t, n, nil
}

// LogLines returns lines of the platform's log, newest first, and the
// cursor of the next (older) page, "" when there is none.
func (s *Store) LogLines(ctx context.Context, f LogFilter) ([]LogLine, string, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	where := []string{"TRUE"}
	args := []any{}
	add := func(cond string, v ...any) {
		for _, x := range v {
			args = append(args, x)
			cond = strings.Replace(cond, "?", "$"+strconv.Itoa(len(args)), 1)
		}
		where = append(where, cond)
	}
	if f.Process != "" {
		add("process = ?", f.Process)
	}
	if f.App != "" {
		add("app = ?", f.App)
	}
	if len(f.Levels) > 0 {
		add("level = ANY(?)", f.Levels)
	}
	if !f.Since.IsZero() {
		add("at >= ?", f.Since)
	}
	if !f.Until.IsZero() {
		add("at < ?", f.Until)
	}
	if f.Text != "" {
		add("strpos(lower(message), lower(?)) > 0", f.Text)
	}
	if f.Before != "" {
		at, id, err := parseLogCursor(f.Before)
		if err != nil {
			return nil, "", err
		}
		add("(at, id) < (?, ?)", at, id)
	}
	args = append(args, f.Limit+1)
	rows, err := s.pool.Query(ctx, `
		SELECT id, at, level, process, message, app, run_id, attrs FROM registro
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY at DESC, id DESC LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []LogLine{}
	for rows.Next() {
		var l LogLine
		var attrs []byte
		if err := rows.Scan(&l.ID, &l.At, &l.Level, &l.Process, &l.Message, &l.App, &l.Run, &attrs); err != nil {
			return nil, "", err
		}
		if len(attrs) > 2 {
			_ = json.Unmarshal(attrs, &l.Attrs)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > f.Limit {
		out = out[:f.Limit]
		next = LogCursor(out[len(out)-1])
	}
	return out, next, nil
}

// PruneLog deletes the lines of the platform's log older than `before`.
func (s *Store) PruneLog(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM registro WHERE at < $1`, before)
	return tag.RowsAffected(), err
}
