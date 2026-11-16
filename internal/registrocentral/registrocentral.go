// Package registrocentral keeps the platform's own log in one place: a slog
// handler passes every record on to the real log (stdout) and also queues
// each one at INFO and above, its secrets removed; a Recorder stores them in
// Postgres for the Log page, so a restarted pod no longer loses its lines
// and every process can be read together. It never blocks or fails the
// code that logs: when the database is behind or down, lines are dropped
// and counted, and the count is written once it answers again.
package registrocentral

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/problems"
	"github.com/rendimiento-ai/rendimiento/internal/ruta"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Keep is how long a line is kept.
const Keep = 30 * 24 * time.Hour

// Bounds on what one line keeps.
const (
	MaxMessage   = 2000 // bytes of the message
	MaxValue     = 1000 // bytes of each attribute's value
	MaxAttrs     = 40   // attributes
	MaxAttrBytes = 8000 // bytes of all attributes together
)

// Removed replaces what looked like a secret.
const Removed = "[removed: looked like a secret]"

// Store is where lines are kept: *store.Store, or a stand-in in tests.
type Store interface {
	AddLogLines(ctx context.Context, lines []store.LogLine) error
	PruneLog(ctx context.Context, before time.Time) (int64, error)
}

// Recorder queues lines from its Handler and stores them in Run.
type Recorder struct {
	ch      chan store.LogLine
	dropped atomic.Int64
	process string
}

// NewRecorder returns a Recorder whose lines say they come from process
// unless they carry a role attribute. It buffers up to 1024 lines until Run
// starts storing them; beyond that, lines are counted and dropped rather
// than ever slowing the logging caller.
func NewRecorder(process string) *Recorder { return newRecorder(process, 1024) }

func newRecorder(process string, size int) *Recorder {
	return &Recorder{ch: make(chan store.LogLine, size), process: process}
}

// Handler wraps h: every record still goes to h, and those at INFO and
// above are also queued for the Log page.
func (r *Recorder) Handler(h slog.Handler) slog.Handler { return &handler{inner: h, rec: r} }

// Dropped is how many lines were dropped and not yet reported.
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// add queues a line; when the queue is full it is counted as dropped.
func (r *Recorder) add(l store.LogLine) {
	select {
	case r.ch <- l:
	default:
		r.dropped.Add(1)
	}
}

// Run stores queued lines until ctx ends, and deletes the ones older than
// Keep every hour. log must not be built on Handler, so the recorder's own
// troubles are never queued as lines (they would loop while the database
// is down).
func (r *Recorder) Run(ctx context.Context, st Store, log *slog.Logger) {
	prune := func() {
		if _, err := st.PruneLog(ctx, time.Now().Add(-Keep)); err != nil && ctx.Err() == nil {
			log.Warn("log: could not delete old lines", "err", err)
		}
	}
	prune()
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	down := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			prune()
		case l := <-r.ch:
			batch := []store.LogLine{l}
		more:
			for len(batch) < 100 {
				select {
				case l := <-r.ch:
					batch = append(batch, l)
				default:
					break more
				}
			}
			down = r.write(ctx, st, log, batch, down)
		}
	}
}

// write stores a batch; down says the last write failed. A failed batch is
// dropped and counted; after a write that worked, the lines dropped since
// are reported, as a line of the log and on stdout. It returns whether the
// database is down.
func (r *Recorder) write(ctx context.Context, st Store, log *slog.Logger, batch []store.LogLine, down bool) bool {
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := st.AddLogLines(sctx, batch); err != nil {
		if ctx.Err() != nil {
			return down
		}
		r.dropped.Add(int64(len(batch)))
		if !down {
			log.Warn("log: could not write lines; dropping them until the database answers", "err", err)
		}
		return true
	}
	if n := r.dropped.Swap(0); n > 0 {
		log.Warn("log: some lines were not kept", "dropped", n)
		note := store.LogLine{At: time.Now(), Level: "WARN", Process: r.process, Message: "log: some lines were not kept",
			Attrs: map[string]any{"dropped": n}}
		if err := st.AddLogLines(sctx, []store.LogLine{note}); err != nil {
			r.dropped.Add(n)
		}
	}
	return false
}

type handler struct {
	inner  slog.Handler
	rec    *Recorder
	attrs  []slog.Attr // from WithAttrs, keys already prefixed with their group
	prefix string      // the current group, "a.b."
}

// Enabled is the wrapped handler's.
func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.inner.Enabled(ctx, l) }

// WithAttrs keeps the attributes for the log too.
func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.inner = h.inner.WithAttrs(as)
	c.attrs = append(append([]slog.Attr{}, h.attrs...), prefixed(h.prefix, as)...)
	return &c
}

// WithGroup prefixes later attributes with the group's name, as slog does.
func (h *handler) WithGroup(name string) slog.Handler {
	c := *h
	c.inner = h.inner.WithGroup(name)
	c.prefix = h.prefix + name + "."
	return &c
}

// Handle logs the record as usual and queues it for the Log page.
func (h *handler) Handle(ctx context.Context, rec slog.Record) error {
	err := h.inner.Handle(ctx, rec)
	if rec.Level >= slog.LevelInfo {
		all := append([]slog.Attr{}, h.attrs...)
		rec.Attrs(func(a slog.Attr) bool {
			all = append(all, prefixed(h.prefix, []slog.Attr{a})...)
			return true
		})
		h.rec.add(FromRecord(h.rec.process, rec.Level, rec.Message, rec.Time, all))
	}
	return err
}

// prefixed adds a group prefix to attribute keys.
func prefixed(prefix string, as []slog.Attr) []slog.Attr {
	if prefix == "" {
		return as
	}
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = slog.Attr{Key: prefix + a.Key, Value: a.Value}
	}
	return out
}

// FromRecord turns a record into a line of the log: the role attribute is
// its process (else process), app and run have their own columns, the rest
// are its attributes. Secrets are removed and sizes bounded (Clean, value).
func FromRecord(process string, level slog.Level, msg string, at time.Time, attrs []slog.Attr) store.LogLine {
	l := store.LogLine{At: at, Level: levelName(level), Process: process, Message: Clean(msg, MaxMessage)}
	if l.At.IsZero() {
		l.At = time.Now()
	}
	size, omitted := 0, 0
	var put func(key string, v slog.Value)
	put = func(key string, v slog.Value) {
		v = v.Resolve()
		if v.Kind() == slog.KindGroup {
			for _, a := range v.Group() {
				k := a.Key
				if key != "" {
					k = key + "." + k
				}
				put(k, a.Value)
			}
			return
		}
		switch key {
		case "":
			return
		case "role":
			l.Process = clip(v.String(), 40)
			return
		case "app":
			if l.App == "" {
				l.App = clip(v.String(), 200)
				return
			}
		case "run":
			if n, err := strconv.ParseInt(v.String(), 10, 64); err == nil && l.Run == nil {
				l.Run = &n
				return
			}
		case problems.LogOnly:
			return
		}
		val := value(key, v)
		b, _ := json.Marshal(val)
		if len(l.Attrs) >= MaxAttrs || size+len(key)+len(b) > MaxAttrBytes {
			omitted++
			return
		}
		if l.Attrs == nil {
			l.Attrs = map[string]any{}
		}
		size += len(key) + len(b)
		l.Attrs[key] = val
	}
	for _, a := range attrs {
		put(a.Key, a.Value)
	}
	if omitted > 0 {
		if l.Attrs == nil {
			l.Attrs = map[string]any{}
		}
		l.Attrs["omitted"] = omitted
	}
	return l
}

func levelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN"
	}
	return "INFO"
}

// value is what an attribute keeps: numbers and booleans as they are, the
// rest as cleaned text; nothing at all of a key that names a credential.
func value(key string, v slog.Value) any {
	if Sensitive(key) {
		return Removed
	}
	switch v.Kind() {
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().UTC().Format(time.RFC3339Nano)
	}
	s := v.String()
	if v.Kind() == slog.KindAny {
		if e, ok := v.Any().(error); ok && e != nil {
			s = e.Error()
		}
	}
	return Clean(s, MaxValue)
}

// credentialWords in an attribute's key mean its value is a credential.
var credentialWords = []string{"token", "secret", "password", "passwd", "contraseña", "clave", "llave",
	"authorization", "cookie", "credential", "apikey", "api_key", "api-key", "private", "dsn"}

// keyParts splits "agent.signing_key" into its words.
var keyParts = regexp.MustCompile(`[^a-z0-9ñ]+`)

// Sensitive reports whether an attribute's key names a credential: token,
// secret, password, authorization, cookie, key and the like ("key",
// "signing_key", "privateKey").
func Sensitive(key string) bool {
	k := strings.ToLower(key)
	for _, w := range credentialWords {
		if strings.Contains(k, w) {
			return true
		}
	}
	for _, p := range keyParts.Split(k, -1) {
		if p == "key" || p == "keys" || p == "pw" || p == "pass" {
			return true
		}
	}
	return strings.HasSuffix(key, "Key")
}

// userinfo is the user and password in a URL ("postgres://u:p@host").
var userinfo = regexp.MustCompile(`://[^/\s:@]+:[^/\s@]+@`)

// Clean is text as the log keeps it: a password in a URL taken out, all of
// it replaced when the Ruta's check (ruta.LooksSecret, the one that refuses
// entries with secrets) says it seems to hold a credential, and cut to n
// bytes.
func Clean(s string, n int) string {
	s = userinfo.ReplaceAllString(s, "://[removed]@")
	if ruta.LooksSecret(s) {
		return Removed
	}
	return clip(s, n)
}

// clip cuts s to n bytes (at a character's start), saying how much was left out.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + fmt.Sprintf("… (%d more bytes)", len(s)-cut)
}
