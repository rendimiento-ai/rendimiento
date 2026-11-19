package arreglos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Limits: what the files a fix writes may add up to, and its log.
const (
	MaxBytes = 20 << 20
	maxLog   = 2 << 20
)

// Executor runs a fix's pod (pipeline.KubeExecutor; faked in tests).
type Executor interface {
	Fix(ctx context.Context, src pipeline.Source, job pipeline.FixJob, w io.Writer) (pipeline.FixOutput, error)
}

// Repos is what a fix reads and writes on GitHub.
type Repos interface {
	FileAt(ctx context.Context, installation int64, repo, path, ref string) ([]byte, error)
	BranchSHA(ctx context.Context, installation int64, repo, branch string) (string, error)
	CommitFiles(ctx context.Context, installation int64, repo, branch, message string, files map[string][]byte, remove []string) (string, error)
}

// Store keeps the fix runs.
type Store interface {
	StartFixRun(ctx context.Context, f store.FixRun) (store.FixRun, error)
	FinishFixRun(ctx context.Context, f store.FixRun, log string) error
	SetFixRunCommit(ctx context.Context, id int64, sha string) error
}

// Service runs declared fixes.
type Service struct {
	Exec  Executor
	Repos Repos
	Store Store
	// MaxTimeout is the platform's step timeout: no fix runs longer.
	MaxTimeout time.Duration
	// Parallel is how many fixes run at once (2).
	Parallel int

	once sync.Once
	sem  chan struct{}
}

// Request is a fix to run.
type Request struct {
	App    *store.App
	Branch string
	// SHA is the commit to fix (empty: the branch's head).
	SHA string
	Fix string
	// Pending and Removed are the caller's uncommitted changes (the coding
	// agent's), laid over the commit.
	Pending map[string][]byte
	Removed []string
	// AskedKind and AskedBy say who asked (store.FixBy*), and Encargo and
	// Run what for.
	AskedKind, AskedBy string
	Encargo, Run       int64
	// Commit commits the files to the branch (the run page's button and
	// Renovate's branches); without it they are only returned (the coding
	// agent takes them into its changes). CoAuthor, a person's GitHub
	// login, signs the commit with them.
	Commit   bool
	CoAuthor string
}

// Result is how a fix ran.
type Result struct {
	ID   int64 `json:"id"`
	Plan Plan  `json:"plan"`
	// Status is store.FixSucceeded, FixUnchanged or FixFailed.
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	// Files are what it wrote (and Removed what it deleted); Ignored what it
	// changed without being allowed to, left out.
	Files   map[string][]byte `json:"-"`
	Written []string          `json:"files"`
	Removed []string          `json:"removed,omitempty"`
	Ignored []string          `json:"ignored,omitempty"`
	Commit  string            `json:"commit,omitempty"`
	Log     string            `json:"-"`
	Took    time.Duration     `json:"took"`
}

// Errors a request can get before anything runs.
var (
	ErrDefaultBranch = errors.New(i18n.M("a fix never runs on the default branch: it only gets what a person merges"))
	ErrNoFix         = errors.New(i18n.M("no such fix in the default branch's rendimiento.yaml"))
)

// Declared are the fixes in the default branch's rendimiento.yaml: the only
// ones that run, whatever the branch being fixed says.
func (s *Service) Declared(ctx context.Context, app *store.App) ([]spec.Fix, error) {
	raw, err := s.Repos.FileAt(ctx, app.InstallationID, app.Repo, spec.FileName, app.DefaultBranch)
	if err != nil {
		return nil, fmt.Errorf("read %s on %s: %w", spec.FileName, app.DefaultBranch, err)
	}
	sp, err := spec.Parse(raw)
	if err != nil {
		return nil, err
	}
	return sp.Fixes, nil
}

// Run runs one declared fix on a branch: it resolves its recipe (the image
// from the folder's version files), runs it in a pod, keeps only the files
// it may write, records it, and commits them when asked.
func (s *Service) Run(ctx context.Context, req Request) (Result, error) {
	app := req.App
	if req.Branch == "" || req.Branch == app.DefaultBranch {
		return Result{}, ErrDefaultBranch
	}
	if app.ForjaOf() != store.ForjaGitHub {
		return Result{}, fmt.Errorf("declared fixes run on GitHub apps only yet: %s is on %s", app.Name, app.ForjaOf())
	}
	fixes, err := s.Declared(ctx, app)
	if err != nil {
		return Result{}, err
	}
	var fix spec.Fix
	found := false
	for _, f := range fixes {
		if f.Name == req.Fix {
			fix, found = f, true
		}
	}
	if !found {
		return Result{}, fmt.Errorf("%w: %q", ErrNoFix, req.Fix)
	}
	sha := req.SHA
	if sha == "" {
		if sha, err = s.Repos.BranchSHA(ctx, app.InstallationID, app.Repo, req.Branch); err != nil {
			return Result{}, err
		}
	}
	removed := map[string]bool{}
	for _, p := range req.Removed {
		removed[p] = true
	}
	read := func(p string) ([]byte, bool) {
		if removed[p] {
			return nil, false
		}
		if b, ok := req.Pending[p]; ok {
			return b, true
		}
		if !versionFile(path.Base(p)) {
			return nil, false
		}
		b, err := s.Repos.FileAt(ctx, app.InstallationID, app.Repo, p, sha)
		return b, err == nil
	}
	plan, err := Resolve(fix, read, s.MaxTimeout)
	if err != nil {
		return Result{}, err
	}
	rec := store.FixRun{AppID: app.ID, Branch: req.Branch, SHA: sha, Fix: fix.Name, AskedKind: req.AskedKind, AskedBy: req.AskedBy,
		Image: plan.Image, ImageFrom: plan.ImageFrom}
	if req.Encargo != 0 {
		rec.EncargoID = &req.Encargo
	}
	if req.Run != 0 {
		rec.RunID = &req.Run
	}
	if rec, err = s.Store.StartFixRun(ctx, rec); err != nil {
		return Result{}, err
	}
	s.once.Do(func() {
		n := s.Parallel
		if n < 1 {
			n = 2
		}
		s.sem = make(chan struct{}, n)
	})
	var log bytes.Buffer
	lw := &capped{w: &log, n: maxLog}
	fmt.Fprintf(lw, "rendimiento: fix %s on %s at %s, in %s\n", fix.Name, req.Branch, short(sha), plan.Path)
	fmt.Fprintf(lw, "rendimiento: image %s (%s)\nrendimiento: $ %s\n", plan.Image, imageFromText(plan.ImageFrom), plan.Command)
	start := time.Now()
	res := Result{ID: rec.ID, Plan: plan}
	finish := func(status, msg string) (Result, error) {
		res.Status, res.Message, res.Took = status, msg, time.Since(start).Round(time.Second)
		if msg != "" {
			fmt.Fprintln(lw, "rendimiento: "+msg)
		}
		res.Log = log.String()
		rec.Status, rec.Message, rec.Files, rec.Ignored, rec.CommitSHA = status, msg, append(append([]string{}, res.Written...), res.Removed...), res.Ignored, res.Commit
		if err := s.Store.FinishFixRun(context.WithoutCancel(ctx), rec, res.Log); err != nil {
			return res, err
		}
		return res, nil
	}
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return finish(store.FixFailed, i18n.M("cancelled before it started"))
	}
	out, err := s.Exec.Fix(ctx, pipeline.Source{Repo: app.Repo, SHA: sha, Branch: req.Branch},
		pipeline.FixJob{ID: strconv.FormatInt(rec.ID, 10), Image: plan.Image, Command: plan.Command, Path: plan.Path, Writes: plan.Writes,
			Timeout: plan.Timeout, Pending: req.Pending, Removed: req.Removed, MaxBytes: MaxBytes}, lw)
	<-s.sem
	switch {
	case err != nil:
		return finish(store.FixFailed, err.Error())
	case out.Failed:
		return finish(store.FixFailed, out.Message)
	}
	kept, ignored, err := Keep(plan, out, MaxBytes)
	res.Ignored = ignored
	if len(ignored) > 0 {
		fmt.Fprintf(lw, "rendimiento: it also changed files it may not write; they were left out: %s\n", strings.Join(ignored, ", "))
	}
	if err != nil {
		return finish(store.FixFailed, err.Error())
	}
	res.Files = map[string][]byte{}
	for _, c := range kept {
		if c.Deleted {
			res.Removed = append(res.Removed, c.Path)
			continue
		}
		if old, ok := req.Pending[c.Path]; ok && bytes.Equal(old, c.Data) {
			continue
		}
		res.Files[c.Path] = c.Data
		res.Written = append(res.Written, c.Path)
	}
	if len(res.Written) == 0 && len(res.Removed) == 0 {
		return finish(store.FixUnchanged, i18n.M("it ran, and nothing it may write changed"))
	}
	fmt.Fprintf(lw, "rendimiento: it wrote %s\n", strings.Join(append(append([]string{}, res.Written...), res.Removed...), ", "))
	if req.Commit {
		msg := CommitMessage(plan, append(append([]string{}, res.Written...), res.Removed...), req.AskedKind, req.AskedBy, req.CoAuthor)
		c, err := s.Repos.CommitFiles(ctx, app.InstallationID, app.Repo, req.Branch, msg, res.Files, res.Removed)
		if err != nil {
			return finish(store.FixFailed, i18n.M("the commit failed: %s", err.Error()))
		}
		res.Commit = c
		fmt.Fprintf(lw, "rendimiento: committed %s to %s\n", short(c), req.Branch)
	}
	return finish(store.FixSucceeded, "")
}

// CommitMessage is the message of a fix's commit: what it regenerated,
// which fix, who asked, and the person as a co-author.
func CommitMessage(plan Plan, files []string, kind, by, coAuthor string) string {
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, path.Base(f))
	}
	sort.Strings(names)
	names = compactStrings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "Regenerate %s", strings.Join(names, ", "))
	if plan.Path != "." {
		fmt.Fprintf(&b, " in %s", plan.Path)
	}
	fmt.Fprintf(&b, "\n\nRun by rendimiento's declared fix %s (%s)", plan.Name, plan.Command)
	switch kind {
	case store.FixByPerson:
		fmt.Fprintf(&b, ", asked by %s from the run page.", by)
	case store.FixByRenovate:
		b.WriteString(", by itself: Renovate's lockfile did not match.")
	default:
		b.WriteString(".")
	}
	if githubLogin.MatchString(coAuthor) {
		fmt.Fprintf(&b, "\n\nCo-authored-by: %s <%s@users.noreply.github.com>", coAuthor, coAuthor)
	}
	return b.String() + "\n"
}

var githubLogin = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)

func compactStrings(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// Keep is what of a fix's output comes back: the changed files under its
// folder that match its patterns exactly, up to max bytes in all. The
// others it changed are ignored, and listed.
func Keep(plan Plan, out pipeline.FixOutput, max int) ([]pipeline.FixChange, []string, error) {
	var kept []pipeline.FixChange
	ignored := append([]string{}, out.Others...)
	total := 0
	for _, c := range out.Changes {
		if !Writes(plan, c.Path) {
			ignored = append(ignored, c.Path)
			continue
		}
		total += len(c.Data)
		kept = append(kept, c)
	}
	sort.Strings(ignored)
	if total > max {
		return nil, ignored, errors.New(i18n.M("the files it wrote add up to %.1f MB, more than the %d MB a fix may write", float64(total)/(1<<20), max>>20))
	}
	return kept, ignored, nil
}

// Writes reports whether a fix may write a file (by its path from the repo
// root): under the fix's folder, matching one of its patterns, where * and
// ? stay within a folder and ** spans any number of them.
func Writes(plan Plan, file string) bool {
	file = path.Clean(file)
	if strings.HasPrefix(file, "../") || path.IsAbs(file) || file == ".git" || strings.HasPrefix(file, ".git/") {
		return false
	}
	rel := file
	if dir := path.Clean(plan.Path); dir != "." {
		var ok bool
		if rel, ok = strings.CutPrefix(file, dir+"/"); !ok {
			return false
		}
	}
	for _, w := range plan.Writes {
		if matchGlob(strings.Split(w, "/"), strings.Split(rel, "/")) {
			return true
		}
	}
	return false
}

func matchGlob(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(name); i++ {
				if matchGlob(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

func versionFile(base string) bool {
	for _, f := range VersionFiles {
		if f == base {
			return true
		}
	}
	return false
}

func imageFromText(from string) string {
	switch from {
	case "default":
		return "no version found in the folder: the default"
	case "declared":
		return "declared by the fix"
	}
	return "version from " + from
}

func short(sha string) string { return sha[:min(7, len(sha))] }

// capped writes at most n bytes, then a note that the rest was cut.
type capped struct {
	w   io.Writer
	n   int
	cut bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.n <= 0 {
		if !c.cut {
			c.cut = true
			_, _ = io.WriteString(c.w, "\nrendimiento: the log is cut here\n")
		}
		return len(p), nil
	}
	q := p
	if len(q) > c.n {
		q = q[:c.n]
	}
	c.n -= len(q)
	_, err := c.w.Write(q)
	return len(p), err
}
