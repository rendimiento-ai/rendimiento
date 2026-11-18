// Package arreglos runs an app's declared fixes (rendimiento.yaml's
// fixes:): a command that regenerates files, such as a lockfile, on a
// branch, in a pod of the build namespace. Only the files the fix may
// write come back, and they are committed to the same branch (or join the
// coding agent's changes), where the branch's normal run tests them.
//
// The fix's definition is always read from the default branch: a branch
// cannot add a command and have it run.
package arreglos

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// Plan is what a fix runs, with its recipe resolved.
type Plan struct {
	Name    string   `json:"name"`
	Recipe  string   `json:"recipe,omitempty"`
	Path    string   `json:"path"`
	Image   string   `json:"image"`
	Command string   `json:"command"`
	Writes  []string `json:"writes"`
	// ImageFrom says where the image's version came from: a file of the
	// folder (".nvmrc", "go.mod", …), "declared" for a fix of the app's own,
	// or "default" when nothing said.
	ImageFrom string        `json:"imageFrom"`
	Timeout   time.Duration `json:"timeout"`
}

// Read reads a file of the branch, with the caller's uncommitted changes,
// by its path from the repo root; false when there is none.
type Read func(p string) ([]byte, bool)

// Default images' versions, when the folder does not say.
const (
	DefaultNode   = "22"
	DefaultGo     = "1.24"
	DefaultPython = "3.12"
)

// recipe is a known fix: its command (given the version files found), the
// files it may write, and the image's language.
type recipe struct {
	writes []string
	lang   string // node, go, python
	cmd    func(read func(string) ([]byte, bool)) string
}

var recipes = map[string]recipe{
	// npm: only the lockfile, without running the packages' install scripts.
	"npm": {writes: []string{"package-lock.json"}, lang: "node", cmd: func(read func(string) ([]byte, bool)) string {
		c := "npm install --package-lock-only --ignore-scripts --no-audit --no-fund"
		if v := packageManager(read, "npm"); v != "" {
			c = "npm install -g npm@" + v + " --no-audit --no-fund >/dev/null && " + c
		}
		return c
	}},
	"pnpm": {writes: []string{"pnpm-lock.yaml"}, lang: "node", cmd: func(read func(string) ([]byte, bool)) string {
		return corepack(read, "pnpm") + "pnpm install --lockfile-only --ignore-scripts"
	}},
	// Yarn 2 and later update only the lockfile; Yarn 1 has no such mode,
	// so it installs, without scripts, and only yarn.lock is kept.
	"yarn": {writes: []string{"yarn.lock"}, lang: "node", cmd: func(read func(string) ([]byte, bool)) string {
		v := packageManager(read, "yarn")
		lock, _ := read("yarn.lock")
		if strings.HasPrefix(v, "1.") || (v == "" && strings.Contains(string(lock), "yarn lockfile v1")) {
			return corepack(read, "yarn") + "yarn install --ignore-scripts --non-interactive --no-progress"
		}
		return corepack(read, "yarn") + "YARN_ENABLE_SCRIPTS=0 yarn install --mode update-lockfile"
	}},
	"go": {writes: []string{"go.mod", "go.sum"}, lang: "go", cmd: func(func(string) ([]byte, bool)) string {
		return "go mod tidy"
	}},
	"uv": {writes: []string{"uv.lock"}, lang: "uv", cmd: func(func(string) ([]byte, bool)) string {
		return "uv lock"
	}},
	"poetry": {writes: []string{"poetry.lock"}, lang: "python", cmd: func(func(string) ([]byte, bool)) string {
		return "pip install --quiet --disable-pip-version-check poetry && poetry lock"
	}},
	"pip-tools": {writes: []string{"requirements.txt"}, lang: "python", cmd: func(func(string) ([]byte, bool)) string {
		return "pip install --quiet --disable-pip-version-check pip-tools && pip-compile --quiet"
	}},
}

// RecipeWrites are the files a recipe may write, relative to its path.
func RecipeWrites(name string) []string { return append([]string{}, recipes[name].writes...) }

// corepack enables the package manager the folder names (packageManager),
// installing corepack first where Node no longer brings it.
func corepack(read func(string) ([]byte, bool), tool string) string {
	c := "{ command -v corepack >/dev/null || npm install -g corepack >/dev/null; } && corepack enable && "
	if v := packageManager(read, tool); v == "" {
		c += "corepack prepare " + tool + "@latest --activate && "
	}
	return c
}

var safeVersion = regexp.MustCompile(`^[0-9][0-9A-Za-z.+-]*$`)

// packageManager is the version package.json's packageManager names for
// tool ("pnpm@9.1.0+sha…" → 9.1.0), or "".
func packageManager(read func(string) ([]byte, bool), tool string) string {
	raw, ok := read("package.json")
	if !ok {
		return ""
	}
	var pkg struct {
		PackageManager string `json:"packageManager"`
	}
	if json.Unmarshal(raw, &pkg) != nil {
		return ""
	}
	name, v, ok := strings.Cut(pkg.PackageManager, "@")
	if !ok || name != tool {
		return ""
	}
	v, _, _ = strings.Cut(v, "+")
	if !safeVersion.MatchString(v) {
		return ""
	}
	return v
}

// Resolve turns a declared fix into what runs: a recipe's command, files
// and image (its version from the folder's files, read through read), or
// the fix's own. Its timeout is the fix's (DefaultFixTimeout seconds when
// it gives none), at most max.
func Resolve(f spec.Fix, read Read, max time.Duration) (Plan, error) {
	dir := path.Clean(f.Path)
	if dir == "" {
		dir = "."
	}
	in := func(p string) ([]byte, bool) {
		if read == nil {
			return nil, false
		}
		return read(path.Join(dir, p))
	}
	timeout := time.Duration(f.Timeout) * time.Second
	if timeout <= 0 {
		timeout = spec.DefaultFixTimeout * time.Second
	}
	if max > 0 && timeout > max {
		timeout = max
	}
	p := Plan{Name: f.Name, Recipe: f.Recipe, Path: dir, Timeout: timeout}
	if f.Recipe == "" {
		if f.Image == "" || strings.TrimSpace(f.Command) == "" || len(f.Writes) == 0 {
			return p, fmt.Errorf("fix %s needs a recipe, or image, command and writes", f.Name)
		}
		p.Image, p.Command, p.Writes, p.ImageFrom = f.Image, f.Command, append([]string{}, f.Writes...), "declared"
		return p, nil
	}
	r, ok := recipes[f.Recipe]
	if !ok {
		return p, fmt.Errorf("fix %s: no recipe %q", f.Name, f.Recipe)
	}
	p.Command, p.Writes = r.cmd(in), append([]string{}, r.writes...)
	switch r.lang {
	case "node":
		v, from := nodeVersion(in)
		p.Image, p.ImageFrom = "node:"+v+"-bookworm-slim", from
	case "go":
		v, from := goVersion(in)
		p.Image, p.ImageFrom = "golang:"+v, from
	case "uv":
		v, from := pythonVersion(in)
		p.Image, p.ImageFrom = "ghcr.io/astral-sh/uv:python"+v+"-bookworm-slim", from
	case "python":
		v, from := pythonVersion(in)
		p.Image, p.ImageFrom = "python:"+v+"-slim", from
	}
	return p, nil
}

var (
	leadingMajor  = regexp.MustCompile(`([0-9]+)`)
	fromImage     = regexp.MustCompile(`(?im)^\s*FROM\s+(?:--\S+\s+)*(?:[a-z0-9.-]+(?::[0-9]+)?/)?(?:library/)?([a-z0-9._/-]+):([0-9][0-9A-Za-z.-]*)`)
	goDirective   = regexp.MustCompile(`(?m)^go\s+([0-9]+\.[0-9]+)(\.[0-9]+)?\s*$`)
	toolchainLine = regexp.MustCompile(`(?m)^toolchain\s+go([0-9]+\.[0-9]+(?:\.[0-9]+)?)\s*$`)
	minorVersion  = regexp.MustCompile(`([0-9]+)\.([0-9]+)`)
)

// dockerfileVersion is the version of image (node, golang, python) in the
// folder's Dockerfile's first FROM of it, or "".
func dockerfileVersion(in Read, image string) string {
	raw, ok := in("Dockerfile")
	if !ok {
		return ""
	}
	for _, m := range fromImage.FindAllStringSubmatch(string(raw), -1) {
		if path.Base(m[1]) == image {
			return m[2]
		}
	}
	return ""
}

// nodeVersion is the Node major the folder uses: .nvmrc, .node-version,
// package.json's engines.node, its Dockerfile's FROM node, else DefaultNode.
func nodeVersion(in Read) (string, string) {
	for _, f := range []string{".nvmrc", ".node-version"} {
		if raw, ok := in(f); ok {
			if m := leadingMajor.FindString(strings.TrimSpace(string(raw))); m != "" {
				return m, f
			}
		}
	}
	if raw, ok := in("package.json"); ok {
		var pkg struct {
			Engines struct {
				Node string `json:"node"`
			} `json:"engines"`
		}
		if json.Unmarshal(raw, &pkg) == nil {
			if m := leadingMajor.FindString(pkg.Engines.Node); m != "" {
				return m, "package.json engines.node"
			}
		}
	}
	if v := dockerfileVersion(in, "node"); v != "" {
		if m := leadingMajor.FindString(v); m != "" {
			return m, "Dockerfile"
		}
	}
	return DefaultNode, "default"
}

// goVersion is the Go the folder's go.mod asks for (its toolchain line,
// else its go line as major.minor), its Dockerfile's FROM golang, else DefaultGo.
func goVersion(in Read) (string, string) {
	if raw, ok := in("go.mod"); ok {
		if m := toolchainLine.FindStringSubmatch(string(raw)); m != nil {
			return m[1], "go.mod toolchain"
		}
		if m := goDirective.FindStringSubmatch(string(raw)); m != nil {
			return m[1] + m[2], "go.mod"
		}
	}
	if v := dockerfileVersion(in, "golang"); v != "" {
		if m := minorVersion.FindString(v); m != "" {
			return m, "Dockerfile"
		}
	}
	return DefaultGo, "default"
}

var requiresPython = regexp.MustCompile(`(?m)^\s*requires-python\s*=\s*["'][^"'0-9]*([0-9]+\.[0-9]+)`)

// pythonVersion is the folder's Python (major.minor): .python-version,
// pyproject.toml's requires-python, its Dockerfile's FROM python, else
// DefaultPython.
func pythonVersion(in Read) (string, string) {
	if raw, ok := in(".python-version"); ok {
		if m := minorVersion.FindString(string(raw)); m != "" {
			return m, ".python-version"
		}
	}
	if raw, ok := in("pyproject.toml"); ok {
		if m := requiresPython.FindStringSubmatch(string(raw)); m != nil {
			return m[1], "pyproject.toml requires-python"
		}
	}
	if v := dockerfileVersion(in, "python"); v != "" {
		if m := minorVersion.FindString(v); m != "" {
			return m, "Dockerfile"
		}
	}
	return DefaultPython, "default"
}

// VersionFiles are the files Resolve may read in a fix's folder.
var VersionFiles = []string{".nvmrc", ".node-version", "package.json", "yarn.lock", "go.mod", "Dockerfile", ".python-version", "pyproject.toml"}
