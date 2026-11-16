package registrocentral

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/problems"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

func queued(r *Recorder) []store.LogLine {
	var out []store.LogLine
	for {
		select {
		case l := <-r.ch:
			out = append(out, l)
		default:
			return out
		}
	}
}

// Every record at INFO and above is queued with its process, app, run and
// attributes; debug is not; everything still reaches the real log.
func TestHandlerQueuesInfoAndAbove(t *testing.T) {
	var buf bytes.Buffer
	r := NewRecorder("all")
	log := slog.New(r.Handler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	log.Debug("noisy detail")
	log.Info("startup")
	log.With("role", "worker").WithGroup("step").Info("step done", "app", "tienda", "run", int64(42), "name", "test", "took", 3*time.Second)
	log.Warn("slow", "ms", 1500, "ok", true)
	log.Error("sync failed", "app", "caja", "err", errors.New("bad yaml"), slog.Group("pod", "name", "caja-1"))

	if strings.Count(buf.String(), "\n") != 5 {
		t.Fatalf("the real log lost records: %s", buf.String())
	}
	ls := queued(r)
	if len(ls) != 4 {
		t.Fatalf("queued %d lines, want 4: %+v", len(ls), ls)
	}
	if ls[0].Process != "all" || ls[0].Level != "INFO" || ls[0].Message != "startup" || ls[0].Attrs != nil {
		t.Errorf("startup = %+v", ls[0])
	}
	// The group prefixes keys, as slog does; role, app and run are columns.
	step := ls[1]
	if step.Process != "worker" || step.App != "" || step.Run != nil || step.Attrs["step.app"] != "tienda" || step.Attrs["step.took"] != "3s" {
		t.Errorf("grouped step = %+v", step)
	}
	if ls[2].Level != "WARN" || ls[2].Attrs["ms"] != int64(1500) || ls[2].Attrs["ok"] != true {
		t.Errorf("warn = %+v", ls[2])
	}
	e := ls[3]
	if e.Level != "ERROR" || e.App != "caja" || e.Attrs["err"] != "bad yaml" || e.Attrs["pod.name"] != "caja-1" {
		t.Errorf("error = %+v", e)
	}

	log.Info("run started", "app", "tienda", "run", 7, problems.LogOnly, true)
	l := queued(r)[0]
	if l.App != "tienda" || l.Run == nil || *l.Run != 7 || l.Attrs != nil {
		t.Errorf("app and run columns, log_only left out: %+v", l)
	}
}

// Secrets never reach the table: values the Ruta's check flags, values of
// keys that name credentials, passwords in URLs, and messages too.
func TestRedaction(t *testing.T) {
	r := NewRecorder("api")
	log := slog.New(r.Handler(slog.NewJSONHandler(io.Discard, nil)))
	gh := "ghp_" + strings.Repeat("a1B2", 8)
	log.Info("configured",
		"setupToken", "abc",
		"Authorization", "Bearer x",
		"cookie", "rendimiento_session=1",
		"signing_key", "k",
		"privateKey", "k",
		"note", "the token is "+gh,
		"assign", "password: Sup3rS3cretValue123",
		"err", errors.New(`dial postgres://rendimiento:hunter2@db:5432/x: refused`),
		"secret", "rendimiento-github", // even a Secret's name: the key says secret
		"keyChanged", false,
		"branch", "main",
	)
	log.Warn("pushed with " + gh)
	ls := queued(r)
	a := ls[0].Attrs
	for _, k := range []string{"setupToken", "Authorization", "cookie", "signing_key", "privateKey", "note", "assign", "secret"} {
		if a[k] != Removed {
			t.Errorf("%s = %v, want it removed", k, a[k])
		}
	}
	if a["err"] != "dial postgres://[removed]@db:5432/x: refused" {
		t.Errorf("err = %v", a["err"])
	}
	if a["branch"] != "main" || a["keyChanged"] != false {
		t.Errorf("ordinary attributes changed: %v", a)
	}
	if ls[1].Message != Removed {
		t.Errorf("message = %q", ls[1].Message)
	}
	if !Sensitive("api_key") || !Sensitive("agent.keys") || !Sensitive("x-api-key") || Sensitive("monkey") || Sensitive("branch") {
		t.Error("Sensitive is wrong about some keys")
	}
}

// Messages, values and attributes are bounded.
func TestBounds(t *testing.T) {
	r := NewRecorder("api")
	log := slog.New(r.Handler(slog.NewJSONHandler(io.Discard, nil)))
	var args []any
	for i := range 60 {
		args = append(args, "k"+strings.Repeat("x", i), strings.Repeat("é", 400))
	}
	log.Info(strings.Repeat("m", 5000), args...)
	l := queued(r)[0]
	if len(l.Message) > MaxMessage+40 || !strings.Contains(l.Message, "more bytes") {
		t.Errorf("message is %d bytes", len(l.Message))
	}
	size := 0
	for k, v := range l.Attrs {
		s, _ := v.(string)
		if len(s) > MaxValue+40 {
			t.Errorf("%s is %d bytes", k, len(s))
		}
		size += len(k) + len(s)
	}
	if len(l.Attrs) > MaxAttrs+1 || size > MaxAttrBytes+200 || l.Attrs["omitted"] == nil {
		t.Errorf("%d attributes, %d bytes, omitted %v", len(l.Attrs), size, l.Attrs["omitted"])
	}
	if got := clip("añb", 2); got != "a… (3 more bytes)" {
		t.Errorf("clip cut a character: %q", got)
	}
}

// When the writer is behind, lines are dropped and counted, never waited for.
func TestDropsWhenFull(t *testing.T) {
	r := newRecorder("api", 3)
	log := slog.New(r.Handler(slog.NewJSONHandler(io.Discard, nil)))
	done := make(chan struct{})
	go func() {
		for range 10 {
			log.Info("line")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("logging blocked on a full queue")
	}
	if len(queued(r)) != 3 || r.Dropped() != 7 {
		t.Fatalf("dropped %d", r.Dropped())
	}
}

type fakeStore struct {
	mu    sync.Mutex
	down  bool
	lines []store.LogLine
}

func (f *fakeStore) AddLogLines(_ context.Context, ls []store.LogLine) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return errors.New("connection refused")
	}
	f.lines = append(f.lines, ls...)
	return nil
}

func (f *fakeStore) PruneLog(context.Context, time.Time) (int64, error) { return 0, nil }

func (f *fakeStore) set(down bool) {
	f.mu.Lock()
	f.down = down
	f.mu.Unlock()
}

func (f *fakeStore) kept() []store.LogLine {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.LogLine{}, f.lines...)
}

// While the database is down, lines are dropped (said once on stdout); when
// it answers again, how many were dropped is written as a line.
func TestDatabaseDownThenBack(t *testing.T) {
	r := NewRecorder("control")
	log := slog.New(r.Handler(slog.NewJSONHandler(io.Discard, nil)))
	st := &fakeStore{down: true}
	var plain bytes.Buffer
	var mu sync.Mutex
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx, st, slog.New(slog.NewJSONHandler(&lockedWriter{w: &plain, mu: &mu}, nil)))

	wait := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting: %s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	log.Info("one")
	wait("the first drop", func() bool { return r.Dropped() == 1 })
	log.Info("two")
	wait("the second drop", func() bool { return r.Dropped() == 2 })
	mu.Lock()
	if n := strings.Count(plain.String(), "could not write"); n != 1 {
		t.Errorf("said %d times that it could not write", n)
	}
	mu.Unlock()

	st.set(false)
	log.Info("three")
	wait("the report", func() bool { return len(st.kept()) == 2 })
	ls := st.kept()
	if ls[0].Message != "three" || ls[1].Message != "log: some lines were not kept" || ls[1].Attrs["dropped"] != int64(2) || ls[1].Process != "control" {
		t.Fatalf("kept %+v", ls)
	}
	if r.Dropped() != 0 {
		t.Fatalf("still counting %d", r.Dropped())
	}
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}
