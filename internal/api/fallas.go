package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rendimiento-ai/rendimiento/internal/problems"
)

// FallaUI is what the UI reports when it shows one of its error pages
// (web/src/components/falla.tsx): which one, on what page, from which page
// the person came, and the UI's version; for a page that failed drawing,
// the error and the top of its component stack. Every field is text from
// a browser: bounded, checked, and stored as data. Who sent it is not
// stored anywhere: not in the problem, not in the bitácora.
type FallaUI struct {
	Kind    string `json:"kind"`              // not_found | forbidden | crashed | unreachable
	Path    string `json:"path"`              // the page's path, "/apps/x"
	From    string `json:"from,omitempty"`    // the in-app path before it
	Version string `json:"version,omitempty"` // the UI's version
	Message string `json:"message,omitempty"` // crashed: the error
	Stack   string `json:"stack,omitempty"`   // crashed: the top of the component stack
	Code    string `json:"code,omitempty"`    // crashed: the short code the page showed
}

// The Problems page's message for each kind (Spanish in internal/i18n).
var fallaMensaje = map[string]string{
	"not_found":   "page not found",
	"forbidden":   "page refused: no access",
	"crashed":     "a page failed to show",
	"unreachable": "the UI could not reach rendimiento",
}

const (
	maxFallaRuta    = 300
	maxFallaMensaje = 300
	maxFallaPila    = 1000
	// fallasPorVentana reports per session in fallasVentana: enough for a
	// person clicking around broken links, not for a loop filling the table.
	fallasPorVentana = 20
	fallasVentana    = 10 * time.Minute
)

var (
	fallaVersion = regexp.MustCompile(`^[0-9A-Za-z.+_-]{0,40}$`)
	fallaCodigo  = regexp.MustCompile(`^[0-9a-f]{4,8}$`)
)

// rutaValida: a path of this site, as a browser's location.pathname gives
// it (percent-encoded): starts with one "/", no spaces or control
// characters, no query, bounded.
func rutaValida(p string) bool {
	if p == "" || len(p) > maxFallaRuta || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || !utf8.ValidString(p) {
		return false
	}
	for _, r := range p {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '?' || r == '#' || r == '\\' {
			return false
		}
	}
	return true
}

// texto keeps text from a browser printable: control characters become
// spaces (newlines kept when lineas), then it is cut to n characters.
func texto(s string, n int, lineas bool) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\n' && lineas {
			return r
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n])
	}
	return s
}

// ritmoFallas counts reports per session, in fixed windows.
type ritmoFallas struct {
	mu  sync.Mutex
	por map[string]ventanaFallas
}

type ventanaFallas struct {
	desde time.Time
	n     int
}

// deja reports whether the session may report one more now.
func (r *ritmoFallas) deja(sesion string, ahora time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.por == nil {
		r.por = map[string]ventanaFallas{}
	}
	if len(r.por) > 10000 { // forget the windows that ended
		for k, v := range r.por {
			if ahora.Sub(v.desde) > fallasVentana {
				delete(r.por, k)
			}
		}
	}
	v := r.por[sesion]
	if ahora.Sub(v.desde) > fallasVentana {
		v = ventanaFallas{desde: ahora}
	}
	if v.n >= fallasPorVentana {
		return false
	}
	v.n++
	r.por[sesion] = v
	return true
}

// recibirFalla records an error page the UI showed (POST /api/fallas) on
// the Problems page: grouped by kind and path, with where the person came
// from. It takes a session, but neither the login nor the session is kept.
func (s *Server) recibirFalla(w http.ResponseWriter, r *http.Request, _ string) {
	sesion := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		sesion = hashToken(c.Value)
	}
	if !s.fallas.deja(sesion, time.Now()) {
		httpError(w, http.StatusTooManyRequests, "too many reports for now")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<10))
	if err != nil {
		httpError(w, http.StatusBadRequest, "unreadable")
		return
	}
	var in FallaUI
	if err := json.Unmarshal(raw, &in); err != nil {
		httpError(w, http.StatusBadRequest, "not a report")
		return
	}
	msg, ok := fallaMensaje[in.Kind]
	switch {
	case !ok:
		httpError(w, http.StatusBadRequest, "unknown kind of page")
		return
	case !rutaValida(in.Path):
		httpError(w, http.StatusBadRequest, "a page's path starts with / and has at most 300 characters")
		return
	case in.From != "" && !rutaValida(in.From):
		httpError(w, http.StatusBadRequest, "a page's path starts with / and has at most 300 characters")
		return
	case !fallaVersion.MatchString(in.Version):
		httpError(w, http.StatusBadRequest, "not a version")
		return
	case in.Code != "" && !fallaCodigo.MatchString(in.Code):
		httpError(w, http.StatusBadRequest, "not a code")
		return
	}
	// The problem is grouped by its detail: the page, and for a page that
	// failed drawing, the error too (numbers and hashes in it count as one).
	detalle := in.Path
	attrs := []slog.Attr{slog.String("component", "ui")}
	level := slog.LevelWarn
	if in.Kind == "crashed" {
		level = slog.LevelError
		if m := texto(in.Message, maxFallaMensaje, false); m != "" {
			detalle += "\n" + m
		}
		if p := texto(in.Stack, maxFallaPila, true); p != "" {
			detalle += "\n" + p
		}
	}
	attrs = append(attrs, slog.String("err", detalle))
	if in.From != "" {
		attrs = append(attrs, slog.String("from", in.From))
	}
	if in.Version != "" {
		attrs = append(attrs, slog.String("version", in.Version))
	}
	if in.Kind == "crashed" && in.Code != "" {
		attrs = append(attrs, slog.String("code", in.Code))
	}
	p, ok := problems.FromRecord(level, msg, time.Now(), attrs)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.Store.RecordProblem(r.Context(), p); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
