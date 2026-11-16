// Package mezcla joins two versions of a file changed from a common base,
// the way git's three-way merge does, for whoever merges without git: the
// coding agent, which has no shell. Where both sides changed the same
// place it writes git's conflict marks, for the agent to resolve as a
// person would.
package mezcla

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

// before: change a ends before b starts, with a base line between them
// that neither touches. Changes that touch are a conflict, as for git.
func before(a, b hunk) bool { return a.to < b.from }

// Merge joins ours and theirs, both changed from base. Changes in
// different places, the same change on both sides, and lines both only
// added at the same place (theirs first) join without a person. Any other
// place both changed is written between git's marks, ours (labelled
// oursName) above theirs (theirsName), and conflict is true. A binary file
// (a NUL byte) is not merged: ok is false.
func Merge(base, ours, theirs []byte, oursName, theirsName string) (merged []byte, conflict, ok bool) {
	if bytes.IndexByte(base, 0) >= 0 || bytes.IndexByte(ours, 0) >= 0 || bytes.IndexByte(theirs, 0) >= 0 {
		return nil, false, false
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
			emit(hunk{from: ht[j].from, to: ht[j].to, lines: append(append([]string{}, ht[j].lines...), ho[i].lines...)})
			i, j = i+1, j+1
		default:
			// A conflict: every change of either side that overlaps the
			// place, grown until no change of either side touches it.
			from, to := min(ho[i].from, ht[j].from), max(ho[i].to, ht[j].to)
			i0, j0 := i, j
			for {
				grew := false
				for i < len(ho) && !before(hunk{from: from, to: to}, ho[i]) {
					from, to, i, grew = min(from, ho[i].from), max(to, ho[i].to), i+1, true
				}
				for j < len(ht) && !before(hunk{from: from, to: to}, ht[j]) {
					from, to, j, grew = min(from, ht[j].from), max(to, ht[j].to), j+1, true
				}
				if !grew {
					break
				}
			}
			out = append(out, b[pos:from]...)
			out = append(out, "<<<<<<< "+oursName+"\n")
			out = appendEnded(out, apply(b, from, to, ho[i0:i]))
			out = append(out, "=======\n")
			out = appendEnded(out, apply(b, from, to, ht[j0:j]))
			out = append(out, ">>>>>>> "+theirsName+"\n")
			pos, conflict = to, true
		}
	}
	out = append(out, b[pos:]...)
	return []byte(strings.Join(out, "")), conflict, true
}

// apply is base lines [from, to) with a side's changes in that place.
func apply(b []string, from, to int, hs []hunk) []string {
	var out []string
	pos := from
	for _, h := range hs {
		out = append(out, b[pos:h.from]...)
		out = append(out, h.lines...)
		pos = h.to
	}
	return append(out, b[pos:to]...)
}

// appendEnded adds lines, the last one ended with "\n" so a mark that
// follows starts a line of its own.
func appendEnded(out, lines []string) []string {
	if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
		lines = append(append([]string{}, lines[:n-1]...), lines[n-1]+"\n")
	}
	return append(out, lines...)
}

// HasMarks says a file still has a conflict's marks, at the start of a line.
func HasMarks(f []byte) bool {
	for _, line := range strings.Split(string(f), "\n") {
		if strings.HasPrefix(line, "<<<<<<< ") || line == "=======" || strings.HasPrefix(line, ">>>>>>> ") {
			return true
		}
	}
	return false
}

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
