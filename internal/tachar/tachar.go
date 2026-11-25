// Package tachar finds what looks like a credential in text, and strikes
// out the lines that hold one. The Ruta refuses entries with one
// (ruta.LooksSecret), and what rendimiento keeps from an app's pods and
// containers (a failed release's evidence) leaves those lines out.
package tachar

import (
	"regexp"
	"strings"
)

// PrefijoLlave starts every agent key (ruta.TokenPrefix).
const PrefijoLlave = "rnd_"

// Omitida is what a line that looks like a secret becomes.
const Omitida = "(a line that looks like a secret was left out)"

var patrones = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr|github_pat)_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bre_[A-Za-z0-9]{8,}_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`\b` + PrefijoLlave + `[A-Za-z0-9_-]{40,}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
}

// conClave is a URL with a password in it (postgresql://app:…@host/db):
// an app's log that prints its DATABASE_URL. Only Lineas looks for it, so
// what the Ruta refuses (Parece) stays as it was.
var conClave = regexp.MustCompile(`[a-z][a-z0-9+.-]*://[^/\s:@]+:[^/\s@]{6,}@`)

// asignacion catches "password: …" style values; a value counts only if it
// looks random (letters and digits, 16+ characters), so "token: the one in
// the rendimiento-github Secret" is fine.
var asignacion = regexp.MustCompile(`(?i)\b(password|passwd|contraseña|secret|token|api[_-]?key)\s*[:=]\s*["']?([A-Za-z0-9+/_=.-]{16,})`)

// Parece reports whether text seems to contain a credential.
func Parece(text string) bool {
	for _, p := range patrones {
		if p.MatchString(text) {
			return true
		}
	}
	for _, m := range asignacion.FindAllStringSubmatch(text, -1) {
		v := m[2]
		if strings.ContainsAny(v, "0123456789") && strings.IndexFunc(v, func(r rune) bool { return r >= 'A' && r <= 'z' }) >= 0 {
			return true
		}
	}
	return false
}

// Lineas is s with every line that looks like a secret (or has a URL
// with a password) replaced by Omitida.
func Lineas(s string) string {
	if !Parece(s) && !conClave.MatchString(s) {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if Parece(l) || conClave.MatchString(l) {
			lines[i] = Omitida
		}
	}
	return strings.Join(lines, "\n")
}
