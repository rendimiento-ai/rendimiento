package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/problems"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// capture keeps the records a logger was given.
type capture struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (c *capture) Enabled(context.Context, slog.Level) bool { return true }
func (c *capture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, r)
	return nil
}
func (c *capture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capture) WithGroup(string) slog.Handler      { return c }

func attrsOf(r slog.Record) []slog.Attr {
	var as []slog.Attr
	r.Attrs(func(a slog.Attr) bool { as = append(as, a); return true })
	return as
}

func attr(r slog.Record, key string) string {
	for _, a := range attrsOf(r) {
		if a.Key == key {
			return a.Value.String()
		}
	}
	return ""
}

// Slow requests and 5xx answers are noted with the route's pattern, never
// the path or its query; live streams are not slow; only a 500 reaches the
// Problems page, one problem per route.
func TestRequestLines(t *testing.T) {
	c := &capture{}
	s := &Server{Log: slog.New(c)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/apps/{app}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fail") != "" {
			httpError(w, http.StatusInternalServerError, "boom")
			return
		}
		time.Sleep(SlowRequest + 50*time.Millisecond)
	})
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) { httpError(w, http.StatusInternalServerError, "boom") })
	mux.HandleFunc("GET /api/environment", func(w http.ResponseWriter, r *http.Request) {
		httpError(w, http.StatusServiceUnavailable, "no cluster")
	})
	mux.HandleFunc("GET /api/runs/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		time.Sleep(SlowRequest + 50*time.Millisecond)
	})
	mux.HandleFunc("GET /api/fast", func(w http.ResponseWriter, r *http.Request) {})
	srv := httptest.NewServer(s.registrando(mux))
	defer srv.Close()
	for _, p := range []string{"/api/apps/tienda?token=abc", "/api/apps/caja?fail=1", "/api/runs/7", "/api/environment", "/api/runs/7/events", "/api/fast"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	if len(c.recs) != 4 {
		for _, r := range c.recs {
			t.Log(r.Message, attrsOf(r))
		}
		t.Fatalf("%d lines, want 4 (slow, two 500s, a 503)", len(c.recs))
	}
	slow, fail, fail2, unavailable := c.recs[0], c.recs[1], c.recs[2], c.recs[3]
	if slow.Level != slog.LevelInfo || slow.Message != "slow request" || attr(slow, "route") != "GET /api/apps/{app}" || attr(slow, "status") != "200" {
		t.Errorf("slow = %s %v", slow.Message, attrsOf(slow))
	}
	for _, r := range c.recs {
		for _, a := range attrsOf(r) {
			if strings.Contains(a.Value.String(), "tienda") || strings.Contains(a.Value.String(), "token") || strings.Contains(a.Value.String(), "fail=") {
				t.Errorf("%s keeps the path or query: %v", r.Message, a)
			}
		}
	}
	if fail.Level != slog.LevelWarn || attr(fail, "status") != "500" || attr(fail, "method") != "GET" || attr(fail, "ms") == "" {
		t.Errorf("fail = %s %v", fail.Message, attrsOf(fail))
	}
	p1, ok1 := problems.FromRecord(fail.Level, fail.Message, fail.Time, attrsOf(fail))
	p2, ok2 := problems.FromRecord(fail2.Level, fail2.Message, fail2.Time, attrsOf(fail2))
	if !ok1 || !ok2 || p1.Fingerprint == p2.Fingerprint {
		t.Errorf("a 500 is a problem of its route: %v %v %+v %+v", ok1, ok2, p1, p2)
	}
	again, _ := problems.FromRecord(fail.Level, fail.Message, time.Now(), attrsOf(fail))
	if again.Fingerprint != p1.Fingerprint {
		t.Error("the same route's 500 must fold into one problem")
	}
	if _, ok := problems.FromRecord(unavailable.Level, unavailable.Message, unavailable.Time, attrsOf(unavailable)); ok || attr(unavailable, "status") != "503" {
		t.Errorf("a 503 stays in the log: %v", attrsOf(unavailable))
	}
}

// The Log page's API: platform administrators only, filters and a cursor.
func TestRegistroAPI(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)
	var lines []store.LogLine
	for i := range 5 {
		lines = append(lines, store.LogLine{At: base.Add(time.Duration(i) * time.Second), Level: "INFO", Process: "worker", Message: "step done", App: "tienda"})
	}
	lines = append(lines, store.LogLine{At: base.Add(time.Minute), Level: "ERROR", Process: "control", Message: "sync failed", App: "caja", Attrs: map[string]any{"err": "bad yaml"}})
	if err := e.store.AddLogLines(ctx, lines); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.do(t, "GET", "/api/registro", "", nil, nil); r.StatusCode != 401 {
		t.Fatalf("without a session: %d", r.StatusCode)
	}
	if err := e.store.SetMember(ctx, store.DefaultOrg, "colabora-registro", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.do(t, "GET", "/api/registro", "", e.session(t, "colabora-registro"), nil); r.StatusCode != 403 {
		t.Fatalf("a collaborator reading the log: %d", r.StatusCode)
	}
	me := e.session(t, "p0dxD")
	get := func(q string) LogPage {
		t.Helper()
		r, body := e.do(t, "GET", "/api/registro"+q, "", me, map[string]string{"Accept-Language": "es-MX"})
		var page LogPage
		if r.StatusCode != 200 || json.Unmarshal([]byte(body), &page) != nil {
			t.Fatalf("%s: %d %s", q, r.StatusCode, body)
		}
		return page
	}
	all := get("")
	if len(all.Lines) != 6 || all.Lines[0].Message != "sync failed" || all.Lines[0].Attrs["err"] != "bad yaml" || all.Next != "" {
		t.Fatalf("all = %+v", all)
	}
	if p := get("?nivel=WARN"); len(p.Lines) != 1 || p.Lines[0].Process != "control" {
		t.Fatalf("at least WARN = %+v", p)
	}
	if p := get("?proceso=worker&app=tienda&q=STEP"); len(p.Lines) != 5 {
		t.Fatalf("filtered = %d", len(p.Lines))
	}
	if p := get("?desde=" + url.QueryEscape(base.Add(30*time.Second).UTC().Format(time.RFC3339))); len(p.Lines) != 1 {
		t.Fatalf("since = %d", len(p.Lines))
	}
	first := get("?limite=4")
	if len(first.Lines) != 4 || first.Next == "" {
		t.Fatalf("first page = %+v", first)
	}
	if rest := get("?limite=4&antes=" + url.QueryEscape(first.Next)); len(rest.Lines) != 2 || rest.Next != "" {
		t.Fatalf("older page = %+v", rest)
	}
	if r, _ := e.do(t, "GET", "/api/registro?antes=nonsense", "", me, nil); r.StatusCode != 400 {
		t.Fatalf("bad cursor: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "GET", "/api/registro?desde=yesterday", "", me, nil); r.StatusCode != 400 {
		t.Fatalf("bad time: %d", r.StatusCode)
	}
}
