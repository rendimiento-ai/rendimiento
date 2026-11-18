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
