package wsl

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf16"
)

// texto reads what wsl.exe printed. Its own messages are UTF-16LE (UTF-8
// with WSL_UTF8=1, which only newer versions honour); what Linux programs
// print inside the distribution is UTF-8; and one answer can carry both: a
// warning of wsl.exe before a file's content.
func texto(b []byte) string {
	var s string
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		s = utf16le(b[2:])
	case bytes.IndexByte(b, 0) < 0:
		s = string(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}))
	case mezclado(b):
		s = mezcla(b)
	default:
		s = utf16le(b)
	}
	s = strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1 // \r, and what a wrong guess leaves
		}
		return r
	}, s)
	return strings.ToValidUTF8(s, "?")
}

func utf16le(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

// mezclado reports whether b, which has NULs, also has UTF-8 text: four
// ASCII bytes in a row (printable, or the end of a line), which UTF-16
// gives only for two uncommon characters together.
func mezclado(b []byte) bool {
	n := 0
	for _, c := range b {
		if (c < 0x20 && c != '\n') || c > 0x7E {
			n = 0
			continue
		}
		if n++; n == 4 {
			return true
		}
	}
	return false
}

// mezcla reads UTF-16 and UTF-8 from one answer: a byte before a NUL is a
// UTF-16 character (ASCII and Latin-1, so wsl.exe's error codes and
// Spanish survive); the rest is taken as UTF-8.
func mezcla(b []byte) string {
	var s strings.Builder
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == 0:
		case i+1 < len(b) && b[i+1] == 0:
			s.WriteRune(rune(b[i]))
			i++
		default:
			s.WriteByte(b[i])
		}
	}
	return s.String()
}

// lineas are the lines of s with something in them, trimmed.
func lineas(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// cola is the end of s: its last lines, up to n bytes.
func cola(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToValidUTF8(s, "")
}
