package arreglos

import (
	"path"
	"regexp"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// Stale is a failed step's log that says a lockfile no longer matches:
// the recipe that regenerates it, its file, and the line that said so.
type Stale struct {
	Recipe string `json:"recipe"`
	File   string `json:"file"`
	Line   string `json:"line"`
}

// signatures are what package managers print when a lockfile is stale.
var signatures = []struct {
	recipe, file string
	re           *regexp.Regexp
}{
	{"npm", "package-lock.json", regexp.MustCompile(`(?i)can only install packages when your package\.json and package-lock\.json(?: \(?or npm-shrinkwrap\.json\)?)? are in sync`)},
	{"npm", "package-lock.json", regexp.MustCompile(`(?i)lock ?file'?s? .*does not satisfy`)},
	{"npm", "package-lock.json", regexp.MustCompile(`(?i)Missing: \S+@\S+ from lock file`)},
	{"npm", "package-lock.json", regexp.MustCompile(`\bEALLOWREMOTE\b`)},
	{"pnpm", "pnpm-lock.yaml", regexp.MustCompile(`ERR_PNPM_OUTDATED_LOCKFILE|ERR_PNPM_LOCKFILE_CONFIG_MISMATCH`)},
	{"yarn", "yarn.lock", regexp.MustCompile(`(?i)lockfile needs to be updated|lockfile would have been modified by this install|YN0028`)},
	{"go", "go.sum", regexp.MustCompile(`missing go\.sum entry`)},
	{"go", "go.mod", regexp.MustCompile(`updates to go\.(?:mod|sum) needed`)},
	{"uv", "uv.lock", regexp.MustCompile("(?i)the lockfile at `?uv\\.lock`? needs to be updated")},
	{"poetry", "poetry.lock", regexp.MustCompile(`(?i)pyproject\.toml changed significantly since poetry\.lock was last generated|poetry\.lock was last generated|poetry\.lock is not consistent with pyproject\.toml`)},
}

var ansiCodes = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// Signature reads a failed step's log for a stale lockfile: the first known
// message, or false.
func Signature(log string) (Stale, bool) {
	for _, line := range strings.Split(ansiCodes.ReplaceAllString(log, ""), "\n") {
		for _, s := range signatures {
			if s.re.MatchString(line) {
				return Stale{Recipe: s.recipe, File: s.file, Line: strings.TrimSpace(line)}, true
			}
		}
	}
	return Stale{}, false
}

// Contains reports whether a fix's folder holds dir (both relative to the
// repo root): the same folder, or one above it.
func Contains(fixPath, dir string) bool {
	f, d := path.Clean(fixPath), path.Clean(dir)
	return f == "." || f == d || strings.HasPrefix(d, f+"/")
}

// Covering is the declared fix that regenerates what stale names in dir:
// of the same recipe, or of the app's own that writes that file, in dir or
// the nearest folder above it. recipesOnly leaves out fixes of the app's own.
func Covering(fixes []spec.Fix, stale Stale, dir string, recipesOnly bool) (spec.Fix, bool) {
	var best spec.Fix
	found := false
	for _, f := range fixes {
		if !Contains(f.Path, dir) {
			continue
		}
		fits := f.Recipe == stale.Recipe
		if f.Recipe == "" && !recipesOnly {
			for _, w := range f.Writes {
				if ok, _ := path.Match(w, stale.File); ok {
					fits = true
				}
			}
		}
		if !fits {
			continue
		}
		if !found || len(path.Clean(f.Path)) > len(path.Clean(best.Path)) || path.Clean(best.Path) == "." {
			best, found = f, true
		}
	}
	return best, found
}

// Found is a failed step whose log says a lockfile is stale, and the
// step's folder.
type Found struct {
	Step   string
	Folder string
	Stale  Stale
}

// Match is a failed step and the declared fix that covers it.
type Match struct {
	Found
	Fix spec.Fix
}

// Covered is, for each failed step, the declared fix that covers it (see
// Covering), in the steps' order; steps no fix covers are left out.
func Covered(found []Found, fixes []spec.Fix, recipesOnly bool) []Match {
	var out []Match
	for _, f := range found {
		if fix, ok := Covering(fixes, f.Stale, f.Folder, recipesOnly); ok {
			out = append(out, Match{Found: f, Fix: fix})
		}
	}
	return out
}

// deeper reports whether folder a is deeper than b, and, at the same
// depth, whether step a comes first: the same answer in any order.
func deeper(a, b, stepA, stepB string) bool {
	if da, db := depth(a), depth(b); da != db {
		return da > db
	}
	if a != b {
		return a < b
	}
	return stepA < stepB
}

func depth(dir string) int {
	dir = path.Clean(dir)
	if dir == "." {
		return 0
	}
	return strings.Count(dir, "/") + 1
}

// Best is the one failed step a run's page offers to fix, whatever the
// steps' order: of the steps a declared fix covers, the one with the
// deepest fix (then the deepest folder); when no fix covers any, ok is
// false and step is the one in the deepest folder, for the fix to declare.
// found must not be empty.
func Best(found []Found, fixes []spec.Fix, recipesOnly bool) (step Found, fix spec.Fix, ok bool) {
	if ms := Covered(found, fixes, recipesOnly); len(ms) > 0 {
		best := ms[0]
		for _, m := range ms[1:] {
			bp, mp := path.Clean(best.Fix.Path), path.Clean(m.Fix.Path)
			if mp != bp && deeper(mp, bp, "", "") ||
				mp == bp && deeper(m.Folder, best.Folder, m.Step, best.Step) {
				best = m
			}
		}
		return best.Found, best.Fix, true
	}
	best := found[0]
	for _, f := range found[1:] {
		if deeper(f.Folder, best.Folder, f.Step, best.Step) {
			best = f
		}
	}
	return best, spec.Fix{}, false
}
