package platform

import (
	"bytes"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// hunk is one change of a side against the base: base lines [from, to)
// become lines. A pure insertion has from == to.
type hunk struct {
	from, to int
	lines    []string
}

// hunks are the changes that turn base into side, in base order.
func hunks(base, side []string) []hunk {
	var out []hunk
	for _, op := range difflib.NewMatcherWithJunk(base, side, false, nil).GetOpCodes() {
		if op.Tag == 'e' {
			continue
		}
		out = append(out, hunk{from: op.I1, to: op.I2, lines: side[op.J1:op.J2]})
	}
	return out
}

func sameHunk(a, b hunk) bool {
	return a.from == b.from && a.to == b.to && strings.Join(a.lines, "") == strings.Join(b.lines, "")
}

// mergeAddOnly joins two sides of a file changed from base, the way git's
// three-way merge does, and says whether it could without a person: where
// both sides changed the same place, it can only when both only added
// lines there (as two branches each adding a menu entry or a translation
// after the same line), and keeps both, theirs (the default branch's)
// first. Changes in different places, or the same change on both sides,
// join as git joins them. Any other overlap, or a binary file, is not
// joined. Lines keep their endings.
func mergeAddOnly(base, ours, theirs []byte) ([]byte, bool) {
	if bytes.IndexByte(base, 0) >= 0 || bytes.IndexByte(ours, 0) >= 0 || bytes.IndexByte(theirs, 0) >= 0 {
		return nil, false
	}
	b, o, t := splitLines(base), splitLines(ours), splitLines(theirs)
	ho, ht := hunks(b, o), hunks(b, t)
	var out []string
	pos, i, j := 0, 0, 0
	emit := func(h hunk) {
		out = append(out, b[pos:h.from]...)
		out = append(out, h.lines...)
		pos = h.to
	}
	for i < len(ho) || j < len(ht) {
		switch {
		case j == len(ht) || (i < len(ho) && before(ho[i], ht[j])):
			emit(ho[i])
			i++
		case i == len(ho) || before(ht[j], ho[i]):
			emit(ht[j])
			j++
		case sameHunk(ho[i], ht[j]):
			emit(ho[i])
			i, j = i+1, j+1
		case ho[i].from == ho[i].to && ht[j].from == ht[j].to && ho[i].from == ht[j].from:
			// Both only added lines at the same place: keep both.
			emit(hunk{from: ht[j].from, to: ht[j].to, lines: append(append([]string{}, ht[j].lines...), ho[i].lines...)})
			i, j = i+1, j+1
		default:
			return nil, false
		}
	}
	out = append(out, b[pos:]...)
	return []byte(strings.Join(out, "")), true
}

// before says change a ends before change b starts, with at least one
// base line between them that neither touches. Changes that touch (one
// ends where the other starts) are a conflict, as for git; two insertions
// at the same place are handled apart.
func before(a, b hunk) bool { return a.to < b.from }

// splitLines splits text into lines that keep their "\n".
func splitLines(s []byte) []string {
	if len(s) == 0 {
		return nil
	}
	lines := strings.SplitAfter(string(s), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
