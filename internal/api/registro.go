package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/problems"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// SlowRequest is how long a request may take before the log notes it.
const SlowRequest = time.Second

// timedWriter remembers the status, and whether the answer was streamed.
type timedWriter struct {
	statusWriter
	streamed bool
}

// Flush marks the answer as a stream (live events), which is long on purpose.
func (w *timedWriter) Flush() {
	w.streamed = true
	w.statusWriter.Flush()
}

// registrando notes in the platform's log the requests nobody saw before:
// one slower than SlowRequest (INFO) and one that failed with a 5xx (WARN),
// with the route's pattern (never the path itself, its query, body or
// headers), the method, the status and the milliseconds. Live-event
// streams are long on purpose and are never slow. A 500 also reaches the
// Problems page, one problem per route; the other 5xx (a service upstream
// down, a page this computer does not have) say so to the person and stay
// in the log only.
func (s *Server) registrando(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		tw := &timedWriter{statusWriter: statusWriter{ResponseWriter: w, status: http.StatusOK}}
		next.ServeHTTP(tw, r)
		// The mux has set the pattern on r by now.
		s.noteRequest(r.Method, r.Pattern, tw.status, time.Since(start), tw.streamed || streaming(r, tw.Header()))
	})
}

// streaming is a live-event answer.
func streaming(r *http.Request, h http.Header) bool {
	return strings.HasSuffix(r.Pattern, "/events") || strings.HasSuffix(r.Pattern, "/stream") ||
		strings.Contains(h.Get("Content-Type"), "text/event-stream")
}

// noteRequest writes a request's line, if it deserves one.
func (s *Server) noteRequest(method, pattern string, status int, took time.Duration, stream bool) {
	log := s.Log
	if log == nil {
		return
	}
	route := pattern
	if route == "" {
		route = "(no route)"
	} else if !strings.Contains(route, " ") {
		route = method + " " + route // a pattern without a method ("/")
	}
	attrs := []any{"component", "http", "route", route, "method", method, "status", status, "ms", took.Milliseconds()}
	switch {
	case status == http.StatusInternalServerError:
		// The route is the problem's detail, so each route is one problem.
		log.Warn("request failed", append(attrs, "err", route+" answered 500")...)
	case status >= 500:
		log.Warn("request failed", append(attrs, problems.LogOnly, true)...)
	case took >= SlowRequest && !stream:
		log.Info("slow request", attrs...)
	}
}

// LogPage is a page of the platform's log, and the cursor of the next.
type LogPage struct {
	Lines []store.LogLine `json:"lines"`
	Next  string          `json:"next,omitempty"`
}

// levelsFrom are the levels shown for ?nivel=: that one and the worse.
func levelsFrom(l string) []string {
	switch strings.ToUpper(l) {
	case "WARN":
		return []string{"WARN", "ERROR"}
	case "ERROR":
		return []string{"ERROR"}
	}
	return nil
}

// listRegistro is the platform's log, newest first: ?proceso=, ?app=,
// ?nivel= (at least), ?desde= and ?hasta= (RFC 3339), ?q= (in the
// message), ?antes= (the cursor of the page before) and ?limite= (up to
// 200). Platform administrators only.
func (s *Server) listRegistro(w http.ResponseWriter, r *http.Request, _ string) {
	q := r.URL.Query()
	f := store.LogFilter{Process: q.Get("proceso"), App: q.Get("app"), Levels: levelsFrom(q.Get("nivel")),
		Text: strings.TrimSpace(q.Get("q")), Before: q.Get("antes"), Limit: 100}
	if len(f.Text) > 200 {
		f.Text = f.Text[:200]
	}
	if n, err := strconv.Atoi(q.Get("limite")); err == nil && n > 0 {
		f.Limit = min(n, 200)
	}
	for _, p := range []struct {
		key string
		to  *time.Time
	}{{"desde", &f.Since}, {"hasta", &f.Until}} {
		if v := q.Get(p.key); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				httpError(w, http.StatusBadRequest, i18n.M("a time is like 2026-11-16T09:00:00Z"))
				return
			}
			*p.to = t
		}
	}
	lines, next, err := s.Store.LogLines(r.Context(), f)
	if errors.Is(err, store.ErrBadCursor) {
		httpError(w, http.StatusBadRequest, i18n.M("that is not a page of the log"))
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, LogPage{Lines: lines, Next: next})
}
