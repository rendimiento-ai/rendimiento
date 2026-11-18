package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/arreglos"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// fixOffer is what a failed run's page offers when a step's log says a
// lockfile is stale: the declared fix that regenerates it, or, when the
// app declares none, the recipe to add to rendimiento.yaml. Runs are the
// fixes already run for this run.
type fixOffer struct {
	Step   string          `json:"step,omitempty"`
	Folder string          `json:"folder,omitempty"`
	Stale  *arreglos.Stale `json:"stale,omitempty"`
	// Declared is the app's fix that covers the step's folder (from the
	// default branch), and Writes what it may write.
	Declared *spec.Fix `json:"declared,omitempty"`
	Writes   []string  `json:"writes,omitempty"`
	// Add is the recipe fix to declare, when the app has none for it.
	Add  *spec.Fix      `json:"add,omitempty"`
	Runs []store.FixRun `json:"runs"`
	// Available is false where fixes cannot run (no build namespace).
	Available bool `json:"available"`
}

// fixRunApp is a run's fix runs with the run and its app, after the access
// checks; false means the answer was written.
func (s *Server) fixRunApp(w http.ResponseWriter, r *http.Request) (*store.Run, *store.App, bool) {
	id, ok := s.runID(w, r)
	if !ok {
		return nil, nil, false
	}
	run, err := s.Store.GetRun(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such run")
		return nil, nil, false
	}
	app, err := s.Store.GetAppByID(r.Context(), run.AppID)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such run")
		return nil, nil, false
	}
	return run, app, true
}

// offer reads the run's failed steps' logs for a stale lockfile, and finds
// the declared fix (from the default branch) that covers the step's folder.
func (s *Server) offer(ctx context.Context, run *store.Run, app *store.App) fixOffer {
	out := fixOffer{Runs: []store.FixRun{}, Available: s.Platform.Fixes != nil}
	if runs, err := s.Store.FixRuns(ctx, app.ID, run.ID, 20); err == nil {
		out.Runs = runs
	}
	if run.Status != store.RunFailed || run.Deploy || run.Branch == app.DefaultBranch {
		return out
	}
	folders := map[string]string{}
	for _, st := range pipeline.Plan(app.Name, s.Platform.Config.Registry, app.Spec) {
		folders[st.ID] = st.Path
	}
	for _, st := range run.Steps {
		if st.Status != store.RunFailed {
			continue
		}
		log, err := s.StepLogText(ctx, run.ID, st.ID)
		if err != nil {
			continue
		}
		stale, ok := arreglos.Signature(log)
		if !ok {
			continue
		}
		folder := folders[st.ID]
		if folder == "" {
			folder = "."
		}
		out.Step, out.Folder, out.Stale = st.ID, path.Clean(folder), &stale
		break
	}
	if out.Stale == nil || s.Platform.Fixes == nil {
		return out
	}
	declared, err := s.Platform.Fixes.Declared(ctx, app)
	if err != nil {
		return out
	}
	if f, ok := arreglos.Covering(declared, *out.Stale, out.Folder, false); ok {
		out.Declared, out.Writes = &f, f.Writes
		if f.Recipe != "" {
			out.Writes = arreglos.RecipeWrites(f.Recipe)
		}
		return out
	}
	add := spec.Fix{Name: fixName(out.Stale.Recipe, out.Folder, declared, app.Spec), Recipe: out.Stale.Recipe, Path: out.Folder}
	out.Add = &add
	return out
}

var notName = regexp.MustCompile(`[^a-z0-9-]+`)

// fixName is a name for a fix to declare: its recipe and folder (npm-web),
// not taken by anything else of the app.
func fixName(recipe, folder string, fixes []spec.Fix, sp spec.Spec) string {
	n := recipe
	if folder != "." {
		n += "-" + strings.ReplaceAll(folder, "/", "-")
	}
	n = strings.Trim(notName.ReplaceAllString(strings.ToLower(n), "-"), "-")
	if len(n) > 36 {
		n = strings.TrimRight(n[:36], "-")
	}
	taken := map[string]bool{}
	for _, f := range fixes {
		taken[f.Name] = true
	}
	for _, x := range sp.Services {
		taken[x.Name] = true
	}
	for _, x := range sp.Jobs {
		taken[x.Name] = true
	}
	for _, x := range sp.Builds {
		taken[x.Name] = true
	}
	for _, x := range sp.Tasks {
		taken[x.Name] = true
	}
	for base, i := n, 2; taken[n]; i++ {
		n = fmt.Sprintf("%s-%d", base, i)
	}
	return n
}

// getRunFix is what a failed run's page offers to regenerate a stale
// lockfile, and the fixes already run for it.
func (s *Server) getRunFix(w http.ResponseWriter, r *http.Request, _ string) {
	run, app, ok := s.fixRunApp(w, r)
	if !ok {
		return
	}
	writeJSON(w, s.offer(r.Context(), run, app))
}

// runFix runs the declared fix the offer names on the run's branch, and
// commits what it writes there with the person as a co-author: the branch's
// new run tests it. Never on the default branch; no AI is involved.
func (s *Server) runFix(w http.ResponseWriter, r *http.Request, login string) {
	run, app, ok := s.fixRunApp(w, r)
	if !ok {
		return
	}
	if run.Branch == app.DefaultBranch || run.Deploy {
		httpError(w, http.StatusConflict, i18n.M("a fix never runs on the default branch: it only gets what a person merges"))
		return
	}
	o := s.offer(r.Context(), run, app)
	switch {
	case !o.Available:
		httpError(w, http.StatusConflict, i18n.M("fixes cannot run on this platform"))
		return
	case o.Declared == nil:
		httpError(w, http.StatusConflict, i18n.M("this run's failure has no declared fix"))
		return
	}
	for _, f := range o.Runs {
		if f.Status == store.FixRunning && time.Since(f.StartedAt) < time.Hour {
			httpError(w, http.StatusConflict, i18n.M("fix run #%d is still running", f.ID))
			return
		}
	}
	req := arreglos.Request{App: app, Branch: run.Branch, SHA: run.SHA, Fix: o.Declared.Name, AskedKind: store.FixByPerson,
		AskedBy: login, CoAuthor: login, Run: run.ID, Commit: true}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		if _, err := s.Platform.Fixes.Run(ctx, req); err != nil {
			s.Log.Warn("a fix from the run page", "app", app.Name, "run", run.ID, "fix", req.Fix, "err", err)
		}
	}()
	anotarDetalle(r, "arreglo", o.Declared.Name)
	writeJSONStatus(w, http.StatusAccepted, map[string]any{"fix": o.Declared.Name})
}

// addFix opens a branch from the default one that declares the recipe the
// offer proposes in rendimiento.yaml; the person opens its pull request
// and merges it.
func (s *Server) addFix(w http.ResponseWriter, r *http.Request, _ string) {
	run, app, ok := s.fixRunApp(w, r)
	if !ok {
		return
	}
	o := s.offer(r.Context(), run, app)
	if o.Add == nil {
		httpError(w, http.StatusConflict, i18n.M("this run's failure has no fix to declare"))
		return
	}
	ctx := r.Context()
	raw, err := s.GitHub.FileAt(ctx, app.InstallationID, app.Repo, spec.FileName, app.DefaultBranch)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	text, err := addFixYAML(raw, *o.Add)
	if err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	branch := "fix/declare-" + o.Add.Name
	base, err := s.GitHub.BranchSHA(ctx, app.InstallationID, app.Repo, app.DefaultBranch)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := s.GitHub.CreateBranch(ctx, app.InstallationID, app.Repo, branch, base); err != nil {
		httpError(w, http.StatusConflict, i18n.M("could not open branch %s: %s", branch, err.Error()))
		return
	}
	msg := fmt.Sprintf("Declare the %s fix in rendimiento.yaml\n\nIt regenerates %s in %s (the %s recipe): run #%d failed because it no longer matched.\n",
		o.Add.Name, o.Stale.File, o.Add.Path, o.Add.Recipe, run.ID)
	if _, err := s.GitHub.CommitFiles(ctx, app.InstallationID, app.Repo, branch, msg, map[string][]byte{spec.FileName: text}, nil); err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	anotarDetalle(r, "arreglo_declarado", o.Add.Name)
	writeJSON(w, map[string]string{"branch": branch, "link": "https://github.com/" + app.Repo + "/pull/new/" + branch})
}

var (
	topFixes   = regexp.MustCompile(`(?m)^fixes:[ \t]*(#.*)?$`)
	topFlow    = regexp.MustCompile(`(?m)^fixes:[ \t]*\S`)
	topKey     = regexp.MustCompile(`^[A-Za-z_"']`)
	itemIndent = regexp.MustCompile(`^([ \t]*)- `)
)

// addFixYAML adds a fix to rendimiento.yaml as text, so its comments and
// order stay: at the end of its fixes: list, or as a new one at the end.
// The result must parse and hold the fix.
func addFixYAML(raw []byte, f spec.Fix) ([]byte, error) {
	src := string(raw)
	entry := func(indent string) string {
		pad := indent + "  "
		e := indent + "- name: " + f.Name + "\n" + pad + "recipe: " + f.Recipe + "\n"
		if f.Path != "." && f.Path != "" {
			e += pad + "path: " + f.Path + "\n"
		}
		return e
	}
	var out string
	switch loc := topFixes.FindStringIndex(src); {
	case loc != nil:
		lines := strings.SplitAfter(src[loc[1]:], "\n")
		at, indent := loc[1], "  "
		if len(lines) > 0 {
			at += len(lines[0]) // the rest of the fixes: line
			lines = lines[1:]
		}
		found := false
		for _, l := range lines {
			if strings.TrimSpace(l) != "" && topKey.MatchString(l) {
				break
			}
			if m := itemIndent.FindStringSubmatch(l); m != nil && !found {
				indent, found = m[1], true
			}
			at += len(l)
		}
		head := src[:at]
		// Blank lines before the next key stay after the new entry.
		trimmed := strings.TrimRight(head, "\n")
		gap := head[len(trimmed):]
		if !strings.HasSuffix(trimmed, "\n") {
			trimmed += "\n"
		}
		if gap == "\n" || gap == "" {
			gap = ""
		} else {
			gap = gap[1:]
		}
		out = trimmed + entry(indent) + gap + src[at:]
	case topFlow.MatchString(src):
		return nil, errors.New(i18n.M("rendimiento.yaml writes fixes: on one line; add the fix by hand"))
	default:
		if src != "" && !strings.HasSuffix(src, "\n") {
			src += "\n"
		}
		out = src + "\n# Commands that regenerate files when they no longer match (rendimiento's declared fixes).\nfixes:\n" + entry("  ")
	}
	sp, err := spec.Parse([]byte(out))
	if err != nil {
		return nil, err
	}
	if _, ok := sp.FixNamed(f.Name); !ok {
		return nil, errors.New(i18n.M("rendimiento.yaml writes fixes: in a way rendimiento cannot add to; add the fix by hand"))
	}
	return []byte(out), nil
}

// fixRunLog is the whole log of one of a run's fixes.
func (s *Server) fixRunLog(w http.ResponseWriter, r *http.Request, _ string) {
	run, app, ok := s.fixRunApp(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("fix"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad fix run id")
		return
	}
	f, err := s.Store.FixRunByID(r.Context(), app.ID, id)
	if err != nil || f.RunID == nil || *f.RunID != run.ID {
		httpError(w, http.StatusNotFound, "no such fix run")
		return
	}
	log, err := s.Store.FixRunLog(r.Context(), app.ID, id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(log))
}
