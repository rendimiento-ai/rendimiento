package agentes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Terminal is the coding agent's run tool (Ruta 178): any shell command,
// in a sandbox pod with the repository at its branch and its uncommitted
// changes (pipeline.Sandboxes). The files the command changes come back as
// its changes, as run_fix's do. It spends no AI money, only the job's time.
type Terminal struct {
	Sandbox interface {
		Run(ctx context.Context, req pipeline.SandboxRun) (pipeline.SandboxResult, error)
		Close(key string)
	}
	Store interface {
		AISettingsOf(ctx context.Context, orgID int64) (store.AISettings, error)
		NoteEncargoRun(ctx context.Context, id int64, who, note string) error
	}
	// Clone gives the sandbox what it clones the app's repository with: the
	// context its token comes from and the address (the platform's
	// ForClone). Without it the sandbox clones app.Repo from GitHub without
	// a token, which a private repository refuses.
	Clone func(ctx context.Context, app *store.App) (context.Context, string)

	mu       sync.Mutex
	watching map[int64]bool
}

// maxCommand is the longest command the run tool takes.
const maxCommand = 8000

var runTool = ia.Tool{Name: "run", Description: "Run a shell command (sh -c) in a sandbox: a pod with the repository at your branch plus your uncommitted changes, in the app's test image when it has one. Use it to install dependencies, run the tests touching your change, a linter, a build, or git. The sandbox stays for this encargo, so what one command installs is there for the next. It answers the exit code, how long it took and the end of stdout and stderr; files it changes in the repository (not ignored ones such as node_modules) become your changes, and commit_and_test commits them. It reaches the public internet (package registries), never the cluster or the home network; each command stops after 15 minutes.",
	InputSchema: schema(map[string]any{"command": str("the command, as for sh -c, e.g. npm ci && npm test"), "folder": str("the folder it runs in, from the repository's root; empty for the root")}, "command")}

// offered says whether the job may use run: the platform has a sandbox and
// the organization did not turn it off (on the AI page).
func (j *job) offered() bool {
	t := j.c.Terminal
	if t == nil || t.Sandbox == nil || t.Store == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	set, err := t.Store.AISettingsOf(ctx, j.e.OrgID)
	return err == nil && set.CodeRun
}

// withTerminal is the tools with run when the job may use it, without it
// when not.
func (j *job) withTerminal(tools []ia.Tool) []ia.Tool {
	on := j.offered()
	out := make([]ia.Tool, 0, len(tools))
	for _, t := range tools {
		if t.Name != "run" || on {
			out = append(out, t)
		}
	}
	return out
}

// terminalRule tells the model whether it can run code.
func (j *job) terminalRule() string {
	if !j.offered() {
		return "\n- You cannot run code: commit_and_test is how your change gets tested."
	}
	return "\n- You can run code: run executes a shell command in a sandbox with your branch and your uncommitted changes. Before commit_and_test, run the tests touching your change (and its linter or type check, if the repository has one), fix what fails, and only then commit; a run that passes in the sandbox still needs commit_and_test, which is the proof the person sees. Install dependencies the way the repository's CI does (npm ci, go mod download). Files a command changes become your changes: do not leave build output or caches the repository does not ignore. The sandbox runs as a non-root user without sudo, and reaches only the public internet."
}

// run runs a command in the job's sandbox.
func (j *job) run(ctx context.Context, input json.RawMessage) (string, bool) {
	c := j.c
	var in struct{ Command, Folder string }
	if err := json.Unmarshal(input, &in); err != nil {
		return "the input is not valid JSON: " + err.Error(), true
	}
	in.Command = strings.TrimSpace(in.Command)
	folder := strings.TrimPrefix(path.Clean("/"+in.Folder), "/")
	if folder == "" {
		folder = "."
	}
	switch {
	case !j.offered():
		return "running commands is turned off for this organization", true
	case in.Command == "":
		return "give a command", true
	case len(in.Command) > maxCommand:
		return fmt.Sprintf("the command has %d characters, more than the %d it may have; put a long script in a file first", len(in.Command), maxCommand), true
	}
	j.doing(ctx, "run", truncate(in.Command, 80))
	t := c.Terminal
	key := fmt.Sprintf("encargo-%d", j.e.ID)
	t.watch(ctx, j.e.ID, key)
	var removed []string
	for p, gone := range j.removed {
		if gone {
			removed = append(removed, p)
		}
	}
	sort.Strings(removed)
	image, from := sandboxImage(j.app.Spec, folder)
	rctx, repo := ctx, j.app.Repo
	if t.Clone != nil {
		rctx, repo = t.Clone(ctx, j.app)
	}
	src := pipeline.Source{Repo: repo, Branch: j.branch, SHA: j.at()}
	res, err := t.Sandbox.Run(rctx, pipeline.SandboxRun{Key: key, Src: src, Image: image, ImageFrom: from,
		Folder: folder, Command: in.Command, Pending: j.changed, Removed: removed})
	if err != nil {
		_ = t.Store.NoteEncargoRun(context.WithoutCancel(ctx), j.e.ID, CoderName,
			c.say("%s: no se pudo correr: %v", "%s: could not run: %v", truncate(in.Command, 200), err))
		return "the command could not run: " + err.Error(), true
	}
	var came, gone, binary []string
	for _, ch := range res.Changes {
		switch {
		case ch.Deleted:
			delete(j.changed, ch.Path)
			j.removed[ch.Path] = true
			gone = append(gone, ch.Path)
		case len(ch.Data) > maxBigFile || !utf8.Valid(ch.Data) || bytes.ContainsRune(ch.Data, 0):
			binary = append(binary, ch.Path)
		default:
			j.changed[ch.Path], j.removed[ch.Path] = ch.Data, false
			came = append(came, ch.Path)
		}
	}
	took := res.Took.Round(time.Second)
	status := c.say("salió con %d", "exit %d", res.ExitCode)
	if res.TimedOut {
		status = c.say("se pasó de su tiempo", "timed out")
	}
	note := c.say("%s (en %s): %s · %s · %d archivo(s)", "%s (in %s): %s · %s · %d file(s)",
		truncate(in.Command, 200), folder, status, took, len(came)+len(gone))
	_ = t.Store.NoteEncargoRun(context.WithoutCancel(ctx), j.e.ID, CoderName, note)

	var b strings.Builder
	fresh := "the same sandbox as before"
	if res.Fresh {
		fresh = "a new sandbox"
	}
	fmt.Fprintf(&b, "$ %s\n(in %s, image %s (%s), %s)\n", in.Command, folder, res.Image, res.ImageFrom, fresh)
	if res.TimedOut {
		fmt.Fprintf(&b, "TIMED OUT: it ran %s and was stopped; the sandbox was removed (the next run starts a fresh one) and nothing it changed came back.\n", took)
	} else {
		fmt.Fprintf(&b, "exit code %d, in %s\n", res.ExitCode, took)
	}
	stream := func(name, text string, total int64) {
		text = ansi.ReplaceAllString(text, "")
		if strings.TrimSpace(text) == "" {
			return
		}
		if int64(len(text)) < total {
			fmt.Fprintf(&b, "\n%s (the last %d of %d bytes):\n", name, len(text), total)
		} else {
			fmt.Fprintf(&b, "\n%s:\n", name)
		}
		b.WriteString(strings.TrimRight(text, "\n") + "\n")
	}
	stream("stdout", res.Stdout, res.StdoutBytes)
	stream("stderr", res.Stderr, res.StderrBytes)
	switch {
	case res.TooMany > 0:
		fmt.Fprintf(&b, "\nIt changed %d files in the repository, too many to bring back: none did, and the next run sets them back to your changes. If it wrote build output or a cache, make the repository ignore it or write it elsewhere.\n", res.TooMany)
	case res.TooBig:
		b.WriteString("\nThe files it changed are too big to bring back: none did, and the next run sets them back to your changes.\n")
	}
	if len(came) > 0 {
		fmt.Fprintf(&b, "\nFiles it changed, now among your changes (commit_and_test commits them): %s\n", strings.Join(came, ", "))
	}
	if len(gone) > 0 {
		fmt.Fprintf(&b, "Files it deleted, now deleted among your changes: %s\n", strings.Join(gone, ", "))
	}
	if left := append(binary, res.Skipped...); len(left) > 0 {
		fmt.Fprintf(&b, "Changed but not brought back (binary, too big, a link or an odd name; the next run sets them back): %s\n", strings.Join(left, ", "))
	}
	return truncate(b.String(), 40000), res.TimedOut || res.ExitCode != 0
}

// watch removes the encargo's sandbox once its job ends (ctx is the job's:
// it ends when the job delivers, gives up, is stopped or runs out of time).
func (t *Terminal) watch(ctx context.Context, id int64, key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.watching == nil {
		t.watching = map[int64]bool{}
	}
	if t.watching[id] {
		return
	}
	t.watching[id] = true
	go func() {
		<-ctx.Done()
		t.Sandbox.Close(key)
		t.mu.Lock()
		delete(t.watching, id)
		t.mu.Unlock()
	}()
}

// sandboxImage is the image a command in folder runs in: the image of the
// test or build task whose folder holds it most closely (as a step there
// would run), or "" (the sandbox's general image) when none does.
func sandboxImage(sp spec.Spec, folder string) (string, string) {
	best, image, from := -1, "", ""
	consider := func(dir, img, what string) {
		dir = strings.Trim(path.Clean("/"+dir), "/")
		if img == "" || (dir != "" && folder != dir && !strings.HasPrefix(folder, dir+"/")) {
			return
		}
		if n := len(dir); n > best {
			best, image, from = n, img, what
		}
	}
	for _, s := range sp.Services {
		if s.Test != nil {
			consider(s.Path, s.Test.Image, "the test of service "+s.Name)
		}
	}
	for _, b := range sp.Builds {
		if b.Test != nil {
			consider(b.Path, b.Test.Image, "the test of build "+b.Name)
		}
	}
	for _, tk := range sp.Tasks {
		if tk.Stage == "" || tk.Stage == "build" {
			consider(tk.Path, tk.Image, "task "+tk.Name)
		}
	}
	return image, from
}
