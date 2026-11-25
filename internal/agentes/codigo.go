package agentes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rendimiento-ai/rendimiento/internal/arreglos"
	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/mezcla"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// CoderName is the agent key the coding agent signs with, and CoderSource
// how it signs its calls to the model.
const (
	CoderName   = "agente de código"
	CoderSource = "agent: coder"
)

// CoderStore is what the coding agent reads and keeps.
type CoderStore interface {
	CoderCandidates(ctx context.Context, limit int) ([]store.CoderCandidate, error)
	SetEncargoDoing(ctx context.Context, id int64, doing string) error
	ShouldStop(ctx context.Context, id int64) (bool, error)
	EncargosOfSystemKey(ctx context.Context, name string) ([]store.Encargo, error)
	SystemKey(ctx context.Context, orgID int64, name string) (store.AgentKey, error)
	TakeCargo(ctx context.Context, keyID int64, purpose string) (store.Cargo, error)
	OpenCargo(ctx context.Context, keyID int64) (store.Cargo, error)
	HandOver(ctx context.Context, id int64, entrega, pendientes string) error
	TakeEncargo(ctx context.Context, id, keyID, cargoID int64, who string) (store.Encargo, error)
	WaitEncargo(ctx context.Context, id, keyID, runID int64, who string) (store.Encargo, error)
	ResumeEncargo(ctx context.Context, id, keyID int64, who, note string) (store.Encargo, error)
	DeliverEncargo(ctx context.Context, id, keyID int64, who, result string, links []string) (store.Encargo, error)
	GiveBackEncargo(ctx context.Context, id, keyID int64, who, note string) (store.Encargo, error)
	GiveBackEncargoCause(ctx context.Context, id, keyID int64, who, note, cause string) (store.Encargo, error)
	AddEncargoSpent(ctx context.Context, id, micros int64) error
	GetApp(ctx context.Context, name string) (*store.App, error)
	ListRuns(ctx context.Context, appID int64, limit int) ([]*store.Run, error)
	GetRun(ctx context.Context, id int64) (*store.Run, error)
	GetDiagnosis(ctx context.Context, runID int64) (store.Diagnosis, error)
	NoteEncargoFix(ctx context.Context, id int64, who, note string) error
}

// Fixes runs the app's declared fixes (arreglos.Service).
type Fixes interface {
	Declared(ctx context.Context, app *store.App) ([]spec.Fix, error)
	Run(ctx context.Context, req arreglos.Request) (arreglos.Result, error)
}

// Repos is the apps' repositories on their forja (GitHub, GitLab or
// Bitbucket: the platform's Repos sends each call to its own).
type Repos interface {
	Files(ctx context.Context, installation int64, repo, ref string, maxFile int) (map[string][]byte, error)
	BranchSHA(ctx context.Context, installation int64, repo, branch string) (string, error)
	CreateBranch(ctx context.Context, installation int64, repo, branch, sha string) error
	CommitFiles(ctx context.Context, installation int64, repo, branch, message string, files map[string][]byte, remove []string) (string, error)
	// For merging the default branch into the encargo's (merge_default_branch):
	// the commit both come from, the files each changed since, a file at a
	// commit, and a merge commit with both as parents.
	MergeBase(ctx context.Context, installation int64, repo, base, head string) (string, time.Time, error)
	ChangedFiles(ctx context.Context, installation int64, repo, base, head string) ([]string, bool, error)
	FileAt(ctx context.Context, installation int64, repo, path, ref string) ([]byte, error)
	CommitMerge(ctx context.Context, installation int64, repo, branch, head, other, message string, files map[string][]byte, remove []string) (string, error)
}

// Coder is the platform's coding agent: it does encargos on a branch of the
// app's repository, through its forja's API, and tests them with the platform's own
// CI, the way a person's agent does; it runs code only in its sandbox (run)
// and the app's declared fixes, and never touches the default branch. It stops at twice the encargo's estimate and
// gives it to the person's own agent with what it did and what is left.
type Coder struct {
	Store CoderStore
	Repos Repos
	Model interface {
		CompleteTools(ctx context.Context, orgID int64, source string, req ia.ToolRequest) (ia.ToolResponse, error)
	}
	// Logs is a step's whole log.
	Logs func(ctx context.Context, runID int64, step string) (string, error)
	// BaseURL is the platform's address, for links to runs.
	BaseURL string
	// Lang is the language it writes to people in: es or en.
	Lang string
	Log  *slog.Logger
	// Poll is how often it looks at a run it waits for (15 seconds).
	Poll time.Duration
	// AlEntregar opens the pull request of the branch it delivers (nil: the
	// link to open one stays).
	AlEntregar func(ctx context.Context, orgID, encargo int64, title, result string, links []string) []string
	// Fixes runs the app's declared fixes for run_fix (nil: the tool is
	// not offered).
	Fixes Fixes
	// Terminal runs commands in a sandbox for run (nil: the tool is not
	// offered; see terminal.go).
	Terminal *Terminal
	// Plataforma is what slow_requests and app_status read (nil: they are
	// not offered); PlatformApp the app that is rendimiento itself
	// (SELF_APP), whose slow requests are the platform's own.
	Plataforma  CoderPlataforma
	PlatformApp string
	// RetryRun runs again what failed in a run the cluster failed (nil: an
	// empty commit runs it again).
	RetryRun func(ctx context.Context, id int64) (*store.Run, error)

	// wrap makes Repos try a call once more when the forja's failure passes.
	wrap      sync.Once
	lastSweep time.Time

	// The encargos it does now, and the cargo each organization's key has
	// open while it does them (several at once share it, see Pick).
	mu      sync.Mutex
	running map[int64]Slot
	cargos  map[int64]*sharedCargo
}

// sharedCargo is a key's open cargo while the coding agent does one or
// more encargos in it: handed over when the last one ends, with what
// each became.
type sharedCargo struct {
	id       int64
	jobs     int
	entregas []string
}

// runWait is how long runs usually take at most: past it the agent keeps
// waiting, spending nothing, up to runWaitMax (variables for tests).
var (
	runWait    = 50 * time.Minute
	runWaitMax = 6 * time.Hour
)

// Limits on the work: the turns, the wait for a push to start a run, and
// what a file, a read and a search may be.
const (
	maxTurns    = 80
	noRunWait   = 5 * time.Minute // for the push to start a run
	maxFileSize = 256 << 10       // a file in the branch's snapshot (listed and searched)
	maxBigFile  = 8 << 20         // a bigger one (a lockfile), read only when a tool names it
	readChars   = 40_000
	maxMatches  = 120
	minBudget   = 300_000 // 30 cents, for an encargo with no estimate
	defaultCost = 500_000 // what an encargo with no estimate is taken to cost
)

// Run does the encargos it should until ctx ends: each organization's
// queue in its order, as many at once as the organization lets it (one by
// default), never two of the same app (Pick). First it gives back any it
// had when the platform stopped.
func (c *Coder) Run(ctx context.Context) {
	if c.Poll == 0 {
		c.Poll = 15 * time.Second
	}
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	c.recover(ctx)
	wake := make(chan struct{}, 1)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		if time.Since(c.lastSweep) > 5*time.Minute {
			c.lastSweep = time.Now()
			c.passUnanswered(ctx, c.lastSweep)
		}
		cands, err := c.Store.CoderCandidates(ctx, 200)
		switch {
		case err == nil:
			c.mu.Lock()
			if c.running == nil {
				c.running = map[int64]Slot{}
			}
			picked := Pick(cands, c.running)
			for _, e := range picked {
				c.running[e.ID] = Slot{Org: e.OrgID, App: e.App}
			}
			c.mu.Unlock()
			for _, e := range picked {
				wg.Add(1)
				go func(e store.Encargo) {
					defer wg.Done()
					// maxWork bounds the work (job.work); this, the waits for runs too.
					wctx, cancel := context.WithTimeout(ctx, maxWork+2*runWaitMax)
					if err := c.Do(wctx, e); err != nil {
						c.Log.Warn("coder: an encargo", "encargo", e.ID, "err", err)
					}
					cancel()
					c.mu.Lock()
					delete(c.running, e.ID)
					c.mu.Unlock()
					select { // the next one at once
					case wake <- struct{}{}:
					default:
					}
				}(e)
			}
		case ctx.Err() == nil:
			c.Log.Warn("coder: looking for encargos", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-tick.C:
		}
	}
}

// Slot is an encargo the coding agent does: its organization and app.
type Slot struct {
	Org int64
	App string
}

// Pick chooses which candidates to start now, given the ones running (by
// encargo): each organization's queue in its order, while it does fewer
// than its organization lets it at once (1 to store.MaxCodeConcurrency),
// and never an encargo of an app it already does one of, in the same
// organization: two branches of one repository at once would step on each
// other's runs. A candidate whose app is busy waits; the next one, of
// another app, may go first.
func Pick(cands []store.CoderCandidate, running map[int64]Slot) []store.Encargo {
	inFlight := map[int64]int{}
	busy := map[Slot]bool{}
	for _, s := range running {
		inFlight[s.Org]++
		busy[s] = true
	}
	var out []store.Encargo
	for _, c := range cands {
		n := min(max(c.Concurrency, 1), store.MaxCodeConcurrency)
		s := Slot{Org: c.OrgID, App: c.App}
		if _, ok := running[c.ID]; ok || c.App == "" || inFlight[c.OrgID] >= n || busy[s] {
			continue
		}
		out = append(out, c.Encargo)
		inFlight[c.OrgID]++
		busy[s] = true
	}
	return out
}

// takeCargo is the cargo an encargo is done in: the key's open one when
// the coding agent does another of that organization now, else a new one.
func (c *Coder) takeCargo(ctx context.Context, keyID int64, purpose string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cargos == nil {
		c.cargos = map[int64]*sharedCargo{}
	}
	if sc, ok := c.cargos[keyID]; ok {
		sc.jobs++
		return sc.id, nil
	}
	cg, err := c.Store.TakeCargo(ctx, keyID, purpose)
	if errors.Is(err, store.ErrCargoOpen) {
		cg, err = c.Store.OpenCargo(ctx, keyID)
	}
	if err != nil {
		return 0, err
	}
	c.cargos[keyID] = &sharedCargo{id: cg.ID, jobs: 1}
	return cg.ID, nil
}

// handOver adds what became of an encargo to its cargo, and hands the
// cargo over when it was the last one in it.
func (c *Coder) handOver(ctx context.Context, keyID int64, entrega string) error {
	c.mu.Lock()
	sc, ok := c.cargos[keyID]
	if !ok {
		c.mu.Unlock()
		return nil
	}
	sc.entregas = append(sc.entregas, entrega)
	if sc.jobs--; sc.jobs > 0 {
		c.mu.Unlock()
		return nil
	}
	delete(c.cargos, keyID)
	c.mu.Unlock()
	return c.Store.HandOver(ctx, sc.id, strings.Join(sc.entregas, "\n\n"), "")
}

// errStopped is a person stopping the encargo the coding agent does.
var errStopped = errors.New("stopped by a person")

// watchStop cancels the work on an encargo when a person asks to stop it,
// or it is dropped (store.ShouldStop), looking every Poll.
func (c *Coder) watchStop(ctx context.Context, id int64, cancel context.CancelCauseFunc) {
	t := time.NewTicker(max(c.Poll, 50*time.Millisecond))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if stop, err := c.Store.ShouldStop(ctx, id); err == nil && stop {
				cancel(errStopped)
				return
			}
		}
	}
}

// recover gives back the encargos the coding agent had when the platform
// stopped (an update): their branch keeps what it committed.
func (c *Coder) recover(ctx context.Context) {
	list, err := c.Store.EncargosOfSystemKey(ctx, CoderName)
	if err != nil {
		return
	}
	for _, e := range list {
		note := c.say("La plataforma se reinició mientras lo hacía; lo que alcanzó a probar está en la rama %s.",
			"The platform restarted while it worked on it; what it got to test is on branch %s.", branchFor(e))
		if _, err := c.Store.GiveBackEncargoCause(ctx, e.ID, *e.KeyID, CoderName, note, store.CauseRestart); err == nil {
			if cg, err := c.Store.OpenCargo(ctx, *e.KeyID); err == nil {
				_ = c.Store.HandOver(ctx, cg.ID, note, "")
			}
		}
	}
}

func (c *Coder) say(es, en string, args ...any) string {
	if c.Lang == "es" {
		return fmt.Sprintf(es, args...)
	}
	return fmt.Sprintf(en, args...)
}

// branchFor is an encargo's branch (store.RamaDeEncargo): fix/ for one
// about a failed run, feature/ otherwise.
func branchFor(e store.Encargo) string { return store.RamaDeEncargo(e) }

// job is one encargo being done: the repository as it is on the branch
// with what changed since the last commit, and what it spent.
type job struct {
	c      *Coder
	e      store.Encargo
	app    *store.App
	key    store.AgentKey
	branch string
	from   string // the branch it was made from: the default one, or the failed run's
	same   bool   // it works on the failed run's branch itself
	// merging is the default branch's commit a merge_default_branch joined,
	// and mergeHead the branch's commit it joined it to: the next commit is
	// a merge, with both as parents.
	merging, mergeHead string
	files              map[string][]byte
	changed            map[string][]byte
	removed            map[string]bool
	spent              int64
	budget             int64
	costs              []int64 // what each call to the model cost
	lastSHA            string
	lastRun            *store.Run
	finished           bool
	outcome            string // delivered, gave back
	cause              string // why it gave it back (store.Cause*; "": read from the note)
	// base is the commit the branch stood at when the job read its files:
	// what a sandbox or a fix starts from before its first commit.
	base string
	// fixes are the app's declared fixes, from the default branch.
	fixes []spec.Fix
	// resumed is the answered question it goes on from (nil: a new start);
	// start is when it began, and waited how long it waited for runs.
	resumed *store.EncargoPregunta
	start   time.Time
	waited  time.Duration
}

// Do does one encargo: takes it in a cargo, works on its branch, and
// delivers it or gives it back.
func (c *Coder) Do(ctx context.Context, e store.Encargo) error {
	if c.Poll == 0 {
		c.Poll = 15 * time.Second
	}
	c.wrap.Do(func() {
		if _, ok := c.Repos.(retryRepos); !ok && c.Repos != nil {
			c.Repos = retryRepos{c.Repos}
		}
	})
	key, err := c.Store.SystemKey(ctx, e.OrgID, CoderName)
	if err != nil {
		return err
	}
	cargo, err := c.takeCargo(ctx, key.ID, c.say("Encargo %d: %s", "Encargo %d: %s", e.ID, e.Title))
	if err != nil {
		return err
	}
	budget := 2 * int64(defaultCost)
	if e.EstimateMicros != nil {
		budget = max(2**e.EstimateMicros, minBudget)
	}
	id := e.ID
	if e, err = c.Store.TakeEncargo(ctx, id, key.ID, cargo, CoderName); err != nil {
		_ = c.handOver(context.WithoutCancel(ctx), key.ID, c.say("Encargo %d: otro lo tomó antes.", "Encargo %d: someone took it first.", id))
		return err
	}
	wctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go c.watchStop(wctx, e.ID, cancel)
	j := &job{c: c, e: e, key: key, branch: branchFor(e), changed: map[string][]byte{}, removed: map[string]bool{}, budget: budget}
	err = j.work(wctx)
	if !j.finished {
		why := c.say("No pude terminarlo", "Could not finish it")
		if errors.Is(context.Cause(wctx), errStopped) {
			why, j.cause = c.say("Una persona lo detuvo", "A person stopped it"), store.CauseStopped
			if sha := j.commitLeft(context.WithoutCancel(ctx)); sha != "" {
				why += c.say(". Lo que llevaba quedó SIN PROBAR en el commit %s", ". What I had is in commit %s, UNTESTED", sha[:min(7, len(sha))])
			}
		} else if err != nil {
			why, j.cause = why+": "+err.Error(), j.errCause(err)
		}
		j.giveBack(context.WithoutCancel(ctx), why)
	}
	entrega := c.say("Encargo %d (%s): %s. Gastó %s de %s.", "Encargo %d (%s): %s. Spent %s of %s.",
		e.ID, e.Title, j.outcome, ia.Dollars(j.spent), ia.Dollars(j.budget))
	return c.handOver(context.WithoutCancel(ctx), key.ID, entrega)
}

func (j *job) giveBack(ctx context.Context, why string) {
	c := j.c
	note := why + ". " + c.say("Lo que hice está en la rama %s; gasté %s de %s.", "What I did is on branch %s; spent %s of %s.",
		j.branch, ia.Dollars(j.spent), ia.Dollars(j.budget))
	if _, err := c.Store.GiveBackEncargoCause(ctx, j.e.ID, j.key.ID, CoderName, note, j.cause); err != nil {
		c.Log.Warn("coder: giving an encargo back", "encargo", j.e.ID, "err", err)
	}
	j.finished, j.outcome = true, c.say("lo devolvió a su agente", "gave it back to your agent")
}

// work prepares the branch and talks with the model until it delivers,
// gives up, or runs out of turns or budget.
func (j *job) work(ctx context.Context) error {
	c := j.c
	app, err := c.Store.GetApp(ctx, j.e.App)
	if err != nil || app.OrgID != j.e.OrgID {
		return fmt.Errorf("no app %q", j.e.App)
	}
	j.app = app
	j.start = time.Now()
	if st, ok := c.asks(); ok {
		if p, err := st.ResumeAnswer(ctx, j.e.ID); err == nil {
			j.resumed, j.spent = &p, p.SpentMicros // the brake counts what it spent before asking
		}
	}
	// An encargo about a run that failed on another branch is about that
	// branch's code: the error lives there, not on the default branch.
	j.from = app.DefaultBranch
	if j.e.RunID != nil {
		if run, err := c.Store.GetRun(ctx, *j.e.RunID); err == nil && run.AppID == app.ID && run.Branch != "" {
			j.from = run.Branch
		}
	}
	// A fix of a branch that is not the default one goes on that branch,
	// not on a copy of it (Ruta 130): the fix stays where the problem is,
	// and its pull request is tested again by itself. Only a failure of the
	// default branch gets a branch of its own, since nobody pushes to it.
	if j.from != app.DefaultBranch {
		j.branch, j.same = j.from, true
	} else if head, ok := j.resumedHead(ctx); ok {
		j.lastSHA = head // it goes on from what it committed before asking
	} else {
		base, err := c.Repos.BranchSHA(ctx, app.InstallationID, app.Repo, j.from)
		if err != nil {
			return err
		}
		if err := c.Repos.CreateBranch(ctx, app.InstallationID, app.Repo, j.branch, base); err != nil {
			return err
		}
		j.base = base
	}
	if head, ok := j.resumedHead(ctx); ok && j.same {
		j.lastSHA = head
	}
	if j.base == "" && j.lastSHA == "" {
		if head, err := c.Repos.BranchSHA(ctx, app.InstallationID, app.Repo, j.branch); err == nil {
			j.base = head // the branch of another run, as it is now
		}
	}
	if j.files, err = c.Repos.Files(ctx, app.InstallationID, app.Repo, j.branch, maxFileSize); err != nil {
		return err
	}
	if c.Fixes != nil {
		if fixes, err := c.Fixes.Declared(ctx, app); err == nil {
			j.fixes = fixes
		}
	}
	turns := []ia.Turn{ia.UserText(j.brief())}
	nudged := 0
	for turn := 0; turn < maxTurns; turn++ {
		if ctx.Err() != nil { // a person stopped it, or its time ran out
			return context.Cause(ctx)
		}
		if time.Since(j.start)-j.waited > maxWork {
			j.commitLeft(ctx)
			return errors.New(c.say("se acabó su tiempo de trabajo", "its working time ran out"))
		}
		// The brake looks ahead (Ruta 77): it stops before a call that would
		// take it past twice the estimate, not after, even in the middle of
		// an edit; what it has is kept as an untested commit.
		if next := j.nextCost(); j.spent+next > j.budget {
			sha := j.commitLeft(ctx)
			why := c.say("Me detuve antes de pasar del doble de lo estimado: la siguiente vuelta costaría unos %s y llevo %s de %s",
				"Stopped before going past twice the estimate: the next turn would cost about %s, and I have spent %s of %s",
				ia.Dollars(next), ia.Dollars(j.spent), ia.Dollars(j.budget))
			if sha != "" {
				why += c.say(". Lo que llevaba quedó SIN PROBAR en el commit %s", ". What I had is in commit %s, UNTESTED", sha[:min(7, len(sha))])
			}
			j.cause = store.CauseBudget
			j.giveBack(context.WithoutCancel(ctx), why)
			return nil
		}
		j.doing(ctx, "think", "")
		res, err := j.complete(ctx, ia.ToolRequest{System: j.system(), Tools: j.tools(), Turns: turns,
			Model: WorkModel, MaxTokens: 16000})
		if err != nil {
			return err
		}
		if res.Stop == "refusal" {
			return errors.New(c.say("el modelo se negó a seguir", "the model refused to go on"))
		}
		turns = append(turns, ia.Turn{Role: "assistant", Content: res.Content})
		if len(res.Calls) == 0 {
			if nudged++; nudged > 2 {
				return errors.New(c.say("el modelo dejó de usar sus herramientas", "the model stopped using its tools"))
			}
			turns = append(turns, ia.UserText("Keep going with your tools. When the run passed, call deliver; if you cannot finish, call give_up."))
			continue
		}
		var results []ia.ToolResult
		for _, call := range res.Calls {
			out, isErr := j.tool(ctx, call)
			results = append(results, ia.ToolResult{ToolUseID: call.ID, Content: out, IsError: isErr})
		}
		if j.finished {
			return nil
		}
		turns = append(turns, ia.UserResults(results))
	}
	j.commitLeft(ctx)
	return errors.New(c.say("se acabaron los turnos", "ran out of turns"))
}

// nextCost is what the next call to the model would cost, by the ones
// before: each reads all the conversation so far, so it costs about the
// last one plus what the last one grew. 0 before the first.
func (j *job) nextCost() int64 {
	n := len(j.costs)
	if n == 0 {
		return 0
	}
	last := j.costs[n-1]
	if n == 1 {
		return last
	}
	return last + max(0, last-j.costs[n-2])
}

// commitLeft keeps what changed since the last commit on the branch, for
// the person's agent to go on from, marked as untested; it answers the
// commit ("" when there was nothing to keep, or it may not).
func (j *job) commitLeft(ctx context.Context) string {
	if len(j.changed) == 0 && len(j.removed) == 0 && j.merging == "" {
		return ""
	}
	// Never half a merge, and never unfinished work on a person's own
	// branch: its pull request is theirs. It stays on an encargo's branch.
	if len(j.marked()) > 0 || (j.same && store.EncargoOfBranch(j.branch) == 0) {
		return ""
	}
	// [skip ci]: unfinished work is not worth a run; whoever goes on from it
	// pushes a commit of their own, and that one runs.
	sha, _ := j.commit(context.WithoutCancel(ctx), j.c.say("Encargo %d: SIN PROBAR, trabajo sin terminar [skip ci]\n\nEl agente de código se detuvo a la mitad; nada de esto se ha probado.",
		"Encargo %d: UNTESTED, unfinished work [skip ci]\n\nThe coding agent stopped halfway; none of this has been tested.", j.e.ID))
	return sha
}

// brief is the first message: the encargo, the app, and the repository's
// own instructions for agents.
func (j *job) brief() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Encargo %d: %s\n\nApp: %s (repository %s, default branch %s). You work on branch %s, made from %s.\n",
		j.e.ID, j.e.Title, j.app.Name, j.app.Repo, j.app.DefaultBranch, j.branch, j.from)
	switch {
	case j.same:
		fmt.Fprintf(&b, "That is the branch of run #%d: you commit on it directly, and its pull request is what gets merged. If it conflicts with %s, call merge_default_branch first.\n", *j.e.RunID, j.app.DefaultBranch)
	}
	if j.e.Body != "" {
		fmt.Fprintf(&b, "\nThe detail:\n%s\n", truncate(j.e.Body, 8000))
	}
	if j.e.Comment != "" {
		fmt.Fprintf(&b, "\nA person sent it back before, saying:\n%s\n", j.e.Comment)
	}
	// Given back before (by this agent or another): why it stopped, and
	// what the person said when sending it again.
	why, told := j.e.GaveBack()
	if why != "" {
		fmt.Fprintf(&b, "\nIt was given back to the person before. Last time you stopped because:\n%s\n", truncate(why, 4000))
	}
	for _, n := range told {
		fmt.Fprintf(&b, "\nThe person answered, sending it again:\n%s\n", truncate(n, 4000))
	}
	b.WriteString(j.resumeBrief())
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		if f, ok := j.files[name]; ok {
			fmt.Fprintf(&b, "\nThe repository's %s:\n%s\n", name, truncate(string(f), 8000))
		}
	}
	fmt.Fprintf(&b, "\nThe repository has %d text files. Top level: %s\n", len(j.files), strings.Join(j.top(), ", "))
	if len(j.fixes) > 0 {
		fmt.Fprintf(&b, "\nThe app declares fixes you can run with run_fix (they regenerate files you cannot write by hand, such as a lockfile):\n%s", j.fixList())
	}
	return b.String()
}

// fixList is the app's declared fixes, one a line: name, folder, what it
// runs and what it may write.
func (j *job) fixList() string {
	var b strings.Builder
	for _, f := range j.fixes {
		writes, what := f.Writes, f.Command
		if f.Recipe != "" {
			writes, what = arreglos.RecipeWrites(f.Recipe), f.Recipe+" recipe"
		}
		fmt.Fprintf(&b, "- %s: in %s, %s; writes %s\n", f.Name, f.Path, what, strings.Join(writes, ", "))
	}
	return b.String()
}

// tools are the agent's tools: run_fix only when the app declares fixes,
// with them in its description.
func (j *job) tools() []ia.Tool {
	tools := append(j.withTerminal(coderTools), j.moreTools()...)
	if len(j.fixes) == 0 {
		return tools
	}
	names := make([]string, 0, len(j.fixes))
	for _, f := range j.fixes {
		names = append(names, f.Name)
	}
	fix := ia.Tool{Name: "run_fix", Description: "Run one of the app's declared fixes on your branch, with your uncommitted changes: a command (such as npm install --package-lock-only or go mod tidy) that regenerates files in a pod of the platform's CI. The files it writes join your changes; commit_and_test commits them. Use it when a lockfile or go.sum no longer matches. The app's fixes:\n" + j.fixList(),
		InputSchema: map[string]any{"type": "object", "required": []string{"name"}, "properties": map[string]any{
			"name": map[string]any{"type": "string", "enum": names, "description": "the fix's name"}}}}
	out := make([]ia.Tool, 0, len(tools)+1)
	for _, t := range tools {
		if t.Name == "commit_and_test" {
			out = append(out, fix)
		}
		out = append(out, t)
	}
	return out
}

// top lists the repository's top-level files and folders.
func (j *job) top() []string {
	seen := map[string]bool{}
	for p := range j.files {
		first, rest, dir := strings.Cut(p, "/")
		if dir && rest != "" {
			first += "/"
		}
		seen[first] = true
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func (j *job) system() string {
	lang := "English"
	if j.c.Lang == "es" {
		lang = "Mexican Spanish, using usted"
	}
	return fmt.Sprintf(`You are rendimiento's coding agent. rendimiento is a platform that builds, tests and deploys apps. A person asked for an encargo (a piece of work) on one app, and you do it on your own branch of the app's repository, through tools: you read and change files, then commit and the platform's CI tests your branch.

How to work:
- Read before you change: find the files involved (search, list_files, read_file) and follow the repository's conventions and its AGENTS.md or CLAUDE.md, if it has them.
- Make the smallest change that does what was asked, in the style of the code around it. Change or add tests when behavior changes.
- commit_and_test commits your changes to your branch and waits for the platform's CI run on it, then tells you how it ended and, if it failed, the end of the failed steps' logs. Fix and test again until it passes.
- Never write secrets (passwords, tokens, keys) into files. Never touch any branch but yours.
- When the run passed, call deliver with a short summary for the person, in %s: what you changed and why, how it was tested, and what they should look at. If you cannot finish (the encargo is unclear, needs a decision, or keeps failing), call give_up with the reason and what is left, in %s.
- Work efficiently: this encargo was estimated at about %s, and you stop at %s, before a turn that would pass it, even in the middle of a change (what you have is then committed untested). Commit and test as soon as a change is whole; prefer search and reading parts of files over reading everything.`,
		lang, lang, ia.Dollars(j.budget/2), ia.Dollars(j.budget)) + j.fixRule() + j.terminalRule() + j.seeRule()
}

// fixRule tells the model of run_fix, when the app declares fixes.
func (j *job) fixRule() string {
	if len(j.fixes) == 0 {
		return ""
	}
	return "\n- The one thing you can run: the app's declared fixes, with run_fix, when a lockfile or go.sum no longer matches its manifest (npm's \"package.json and package-lock.json are not in sync\", \"missing go.sum entry\"). Never edit a lockfile by hand when a fix can regenerate it."
}

var coderTools = []ia.Tool{
	{Name: "list_files", Description: "List the repository's files under a folder (all of them when it is empty), optionally only those whose name matches a glob like *.go.",
		InputSchema: schema(map[string]any{"folder": str("a folder, like web/src; empty for all"), "glob": str("a name pattern, like *.tsx")})},
	{Name: "read_file", Description: "Read a file as it is now on your branch (with your changes), whole or from line to line; with ref, as it is on another branch or commit (the default branch, say). Files over 256 KB (a package-lock.json, say) are not listed or searched, but can be read and edited by their path.",
		InputSchema: schema(map[string]any{"path": str("the file's path"), "from_line": num("first line, from 1"), "to_line": num("last line"), "ref": str("a branch or commit; empty for yours")}, "path")},
	{Name: "search", Description: "Search the repository's files for a regular expression (Go syntax); answers path:line: text, up to 120 matches.",
		InputSchema: schema(map[string]any{"pattern": str("a regular expression"), "folder": str("only under this folder")}, "pattern")},
	{Name: "write_file", Description: "Write a whole file (a new one, or replacing it).",
		InputSchema: schema(map[string]any{"path": str("the file's path"), "content": str("the whole content")}, "path", "content")},
	{Name: "edit_file", Description: "Replace one exact piece of a file with another; old_text must appear exactly once.",
		InputSchema: schema(map[string]any{"path": str("the file's path"), "old_text": str("the exact text to replace"), "new_text": str("what replaces it")}, "path", "old_text", "new_text")},
	{Name: "delete_file", Description: "Delete a file.",
		InputSchema: schema(map[string]any{"path": str("the file's path")}, "path")},
	{Name: "merge_default_branch", Description: "Merge the default branch into yours, as git merge would: the files only it changed come in, and where both changed the same place the file gets git's conflict marks (<<<<<<< your branch, =======, >>>>>>> the default branch) for you to resolve, keeping what both sides meant. Your next commit_and_test is the merge commit; it refuses while a mark is left.",
		InputSchema: schema(map[string]any{})},
	{Name: "commit_and_test", Description: "Commit your changes to your branch with a message, and wait for the platform's CI run on it (minutes): how it ended and, if it failed, the end of the failed steps' logs.",
		InputSchema: schema(map[string]any{"message": str("the commit message, in the repository's style")}, "message")},
	{Name: "deliver", Description: "Hand the encargo over to the person, once the latest run passed.",
		InputSchema: schema(map[string]any{"summary": str("what you changed and why, how it was tested, what to look at")}, "summary")},
	{Name: "give_up", Description: "Give the encargo back to the person's own agent, when you cannot finish it.",
		InputSchema: schema(map[string]any{"reason": str("why, and what is left"), "cause": giveUpCause}, "reason")},
	runTool,
}

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
func num(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }

// tool runs one tool call: its answer, and whether it is an error.
func (j *job) tool(ctx context.Context, call ia.ToolCall) (string, bool) {
	var in struct {
		Folder, Glob, Path, Pattern, Content, Message, Summary, Reason, Ref, Name string
		OldText                                                                   string `json:"old_text"`
		NewText                                                                   string `json:"new_text"`
		FromLine                                                                  int    `json:"from_line"`
		ToLine                                                                    int    `json:"to_line"`
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return "the input is not valid JSON: " + err.Error(), true
	}
	in.Path = strings.TrimPrefix(path.Clean("/"+in.Path), "/")
	switch call.Name {
	case "list_files":
		j.doing(ctx, call.Name, in.Folder)
	case "search":
		j.doing(ctx, call.Name, in.Pattern)
	case "run_fix":
		j.doing(ctx, call.Name, in.Name)
	case "read_file", "write_file", "edit_file", "delete_file":
		j.doing(ctx, call.Name, in.Path)
	default:
		j.doing(ctx, call.Name, "")
	}
	switch call.Name {
	case "read_file", "edit_file", "delete_file":
		if in.Ref == "" || call.Name != "read_file" {
			if msg := j.fetchBig(ctx, in.Path); msg != "" {
				return msg, true
			}
		}
	}
	switch call.Name {
	case "list_files":
		return j.list(in.Folder, in.Glob), false
	case "read_file":
		if in.Ref != "" {
			return j.readAt(ctx, in.Path, in.Ref, in.FromLine, in.ToLine)
		}
		return j.read(in.Path, in.FromLine, in.ToLine)
	case "merge_default_branch":
		return j.mergeDefault(ctx)
	case "search":
		return j.search(in.Pattern, in.Folder)
	case "write_file":
		if in.Path == "" || strings.HasPrefix(in.Path, ".git/") {
			return "not a file you may write", true
		}
		j.changed[in.Path], j.removed[in.Path] = []byte(in.Content), false
		return fmt.Sprintf("wrote %s (%d bytes)", in.Path, len(in.Content)), false
	case "edit_file":
		f, ok := j.file(in.Path)
		if !ok {
			return "no such file: " + in.Path, true
		}
		if n := strings.Count(string(f), in.OldText); n != 1 || in.OldText == "" {
			return fmt.Sprintf("old_text appears %d times in %s; it must appear exactly once", n, in.Path), true
		}
		j.changed[in.Path] = []byte(strings.Replace(string(f), in.OldText, in.NewText, 1))
		return "edited " + in.Path, false
	case "delete_file":
		if _, ok := j.file(in.Path); !ok {
			return "no such file: " + in.Path, true
		}
		delete(j.changed, in.Path)
		j.removed[in.Path] = true
		return "deleted " + in.Path, false
	case "run_fix":
		return j.runFix(ctx, in.Name)
	case "run":
		return j.run(ctx, call.Input)
	case "commit_and_test":
		return j.commitAndTest(ctx, in.Message)
	case "deliver":
		return j.deliver(ctx, in.Summary)
	case "give_up":
		j.cause = causeOfGiveUp(call.Input)
		j.commitLeft(ctx)
		j.giveBack(context.WithoutCancel(ctx), in.Reason)
		return "given back", false
	}
	if out, isErr, ok := j.moreTool(ctx, call); ok {
		return out, isErr
	}
	return "no such tool: " + call.Name, true
}

// doing keeps what it does now ("tool:what") for the queue's page: the
// interface says it in the person's language (web/src/components/cola.tsx).
func (j *job) doing(ctx context.Context, tool, what string) {
	if what == "" && tool == "list_files" {
		what = "."
	}
	_ = j.c.Store.SetEncargoDoing(context.WithoutCancel(ctx), j.e.ID, tool+":"+what)
}

// fetchBig brings in a file the branch's snapshot left out for its size
// (Files skips those over maxFileSize), so read_file and edit_file find it
// by its path. It answers why it could not, or "" (also when there is no
// such file: the tool then says so).
func (j *job) fetchBig(ctx context.Context, p string) string {
	if p == "" || j.removed[p] {
		return ""
	}
	if _, ok := j.changed[p]; ok {
		return ""
	}
	if _, ok := j.files[p]; ok {
		return ""
	}
	f, err := j.c.Repos.FileAt(ctx, j.app.InstallationID, j.app.Repo, p, j.branch)
	switch {
	case forja.IsNotFound(err):
		return ""
	case err != nil:
		return fmt.Sprintf("could not read %s: %v", p, err)
	case len(f) > maxBigFile:
		return fmt.Sprintf("%s has %d bytes, more than the %d MB a file may have here", p, len(f), maxBigFile>>20)
	case !utf8.Valid(f) || bytes.ContainsRune(f, 0):
		return p + " is not a text file"
	}
	if j.files == nil {
		j.files = map[string][]byte{}
	}
	j.files[p] = f
	return ""
}

// file is a file as it is now: with the changes, without the removed.
func (j *job) file(p string) ([]byte, bool) {
	if j.removed[p] {
		return nil, false
	}
	if f, ok := j.changed[p]; ok {
		return f, true
	}
	f, ok := j.files[p]
	return f, ok
}

// paths are the files as they are now, sorted.
func (j *job) paths() []string {
	seen := map[string]bool{}
	for p := range j.files {
		seen[p] = true
	}
	for p := range j.changed {
		seen[p] = true
	}
	out := []string{}
	for p := range seen {
		if !j.removed[p] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func under(p, folder string) bool {
	folder = strings.Trim(folder, "/")
	return folder == "" || folder == "." || strings.HasPrefix(p, folder+"/")
}

func (j *job) list(folder, glob string) string {
	var out []string
	for _, p := range j.paths() {
		if !under(p, folder) {
			continue
		}
		if ok, _ := path.Match(glob, path.Base(p)); glob != "" && !ok {
			continue
		}
		out = append(out, p)
	}
	if len(out) > 400 {
		return strings.Join(out[:400], "\n") + fmt.Sprintf("\n… and %d more; narrow it with folder or glob", len(out)-400)
	}
	if len(out) == 0 {
		return "no files"
	}
	return strings.Join(out, "\n")
}

func (j *job) read(p string, from, to int) (string, bool) {
	f, ok := j.file(p)
	if !ok {
		return "no such file: " + p, true
	}
	return lines(f, from, to)
}

// readAt reads a file as it is at ref (another branch, or a commit).
func (j *job) readAt(ctx context.Context, p, ref string, from, to int) (string, bool) {
	f, err := j.c.Repos.FileAt(ctx, j.app.InstallationID, j.app.Repo, p, ref)
	if err != nil {
		return fmt.Sprintf("could not read %s at %s: %v", p, ref, err), true
	}
	return lines(f, from, to)
}

// lines is a file whole, or from line to line, cut to readChars.
func lines(f []byte, from, to int) (string, bool) {
	if from <= 0 && to <= 0 {
		if len(f) > readChars {
			return string(f[:readChars]) + fmt.Sprintf("\n… the file has %d bytes; read the rest with from_line and to_line", len(f)), false
		}
		return string(f), false
	}
	lines := strings.Split(string(f), "\n")
	from = max(from, 1)
	if to <= 0 || to > len(lines) {
		to = len(lines)
	}
	if from > to {
		return fmt.Sprintf("the file has %d lines", len(lines)), true
	}
	text := strings.Join(lines[from-1:to], "\n")
	if len(text) > readChars {
		text = text[:readChars] + "\n… (cut)"
	}
	return text, false
}

func (j *job) search(pattern, folder string) (string, bool) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "not a valid regular expression: " + err.Error(), true
	}
	var out []string
	for _, p := range j.paths() {
		if !under(p, folder) {
			continue
		}
		f, _ := j.file(p)
		for i, line := range strings.Split(string(f), "\n") {
			if re.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d: %s", p, i+1, truncate(strings.TrimSpace(line), 200)))
				if len(out) >= maxMatches {
					return strings.Join(out, "\n") + "\n… more; narrow the pattern or the folder", false
				}
			}
		}
	}
	if len(out) == 0 {
		return "no matches", false
	}
	return strings.Join(out, "\n"), false
}

// marked are the changed files that still have a conflict's marks.
func (j *job) marked() []string {
	var out []string
	for p, f := range j.changed {
		if mezcla.HasMarks(f) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// mergeDefault merges the default branch into the branch, in the job's
// copy: what only the default branch changed comes in; where both changed
// a file, it is joined (mezcla.Merge), with git's marks where both changed
// the same place, for the model to resolve. The next commit is the merge.
func (j *job) mergeDefault(ctx context.Context) (string, bool) {
	c, inst, repo := j.c, j.app.InstallationID, j.app.Repo
	if j.branch == j.app.DefaultBranch {
		return "you are on the default branch", true
	}
	if len(j.changed) > 0 || len(j.removed) > 0 {
		return "commit_and_test your changes first, then merge", true
	}
	head, err := c.Repos.BranchSHA(ctx, inst, repo, j.branch)
	if err != nil {
		return "could not read your branch: " + err.Error(), true
	}
	main, err := c.Repos.BranchSHA(ctx, inst, repo, j.app.DefaultBranch)
	if err != nil {
		return "could not read the default branch: " + err.Error(), true
	}
	base, _, err := c.Repos.MergeBase(ctx, inst, repo, main, head)
	if err != nil || base == "" {
		return fmt.Sprintf("no common commit of %s and %s: %v", j.app.DefaultBranch, j.branch, err), true
	}
	if base == main {
		return j.app.DefaultBranch + " has nothing your branch lacks: no merge needed", false
	}
	theirs, ok1, err := c.Repos.ChangedFiles(ctx, inst, repo, base, main)
	if err != nil {
		return "could not compare: " + err.Error(), true
	}
	ours, ok2, err := c.Repos.ChangedFiles(ctx, inst, repo, base, head)
	if err != nil {
		return "could not compare: " + err.Error(), true
	}
	if !ok1 || !ok2 {
		return "too many files changed to merge without git; give_up so a person's agent merges it", true
	}
	mine := map[string]bool{}
	for _, f := range ours {
		mine[f] = true
	}
	read := func(p, ref string) ([]byte, bool, error) {
		b, err := c.Repos.FileAt(ctx, inst, repo, p, ref)
		if forja.IsNotFound(err) {
			return nil, false, nil
		}
		return b, err == nil, err
	}
	var came, joined, conflicts, notes []string
	for _, f := range theirs {
		t, tok, err := read(f, main)
		if err != nil {
			return "could not read " + f + ": " + err.Error(), true
		}
		if !mine[f] {
			if tok {
				j.changed[f] = t
			} else {
				j.removed[f] = true
			}
			came = append(came, f)
			continue
		}
		o, ook := j.file(f)
		switch {
		case !ook && !tok:
			continue
		case !ook:
			notes = append(notes, f+": your branch deleted it and "+j.app.DefaultBranch+" changed it; it stays deleted unless you write it back (read it with ref)")
			continue
		case !tok:
			notes = append(notes, f+": "+j.app.DefaultBranch+" deleted it and your branch changed it; it stays as yours unless you delete it")
			continue
		case string(o) == string(t):
			continue
		}
		b, _, err := read(f, base)
		if err != nil {
			return "could not read " + f + ": " + err.Error(), true
		}
		merged, conflict, ok := mezcla.Merge(b, o, t, j.branch, j.app.DefaultBranch)
		if !ok {
			notes = append(notes, f+": a binary file both changed; it stays as yours")
			continue
		}
		j.changed[f] = merged
		if conflict {
			conflicts = append(conflicts, f)
		} else {
			joined = append(joined, f)
		}
	}
	j.merging, j.mergeHead = main, head
	var out strings.Builder
	fmt.Fprintf(&out, "merged %s (%s) into %s in your copy; your next commit_and_test is the merge commit.\n", j.app.DefaultBranch, main[:min(7, len(main))], j.branch)
	if len(came) > 0 {
		fmt.Fprintf(&out, "Came in as %s has them: %s\n", j.app.DefaultBranch, strings.Join(came, ", "))
	}
	if len(joined) > 0 {
		fmt.Fprintf(&out, "Joined without conflict: %s\n", strings.Join(joined, ", "))
	}
	if len(conflicts) > 0 {
		fmt.Fprintf(&out, "CONFLICTS to resolve (read each, keep what both sides meant, remove every mark): %s\n", strings.Join(conflicts, ", "))
	}
	for _, n := range notes {
		out.WriteString(n + "\n")
	}
	return out.String(), false
}

// at is the commit the job's changes lie over: its last commit, or the
// one its branch started from.
func (j *job) at() string {
	if j.lastSHA != "" {
		return j.lastSHA
	}
	return j.base
}

// runFix runs a declared fix on the branch with the uncommitted changes:
// what it writes joins them, for the next commit_and_test.
func (j *job) runFix(ctx context.Context, name string) (string, bool) {
	c := j.c
	if c.Fixes == nil || len(j.fixes) == 0 {
		return "this app declares no fixes", true
	}
	if _, ok := (&spec.Spec{Fixes: j.fixes}).FixNamed(name); !ok {
		return "no such fix: " + name + "; the app's are:\n" + j.fixList(), true
	}
	if left := j.marked(); len(left) > 0 {
		return "these files still have conflict marks; resolve them first: " + strings.Join(left, ", "), true
	}
	var removed []string
	for p, gone := range j.removed {
		if gone {
			removed = append(removed, p)
		}
	}
	sort.Strings(removed)
	res, err := c.Fixes.Run(ctx, arreglos.Request{App: j.app, Branch: j.branch, SHA: j.at(), Fix: name,
		Pending: j.changed, Removed: removed, AskedKind: store.FixByAgent, AskedBy: CoderName, Encargo: j.e.ID})
	if err != nil {
		return "the fix could not run: " + err.Error(), true
	}
	took := res.Took.Round(time.Second)
	note := ""
	switch res.Status {
	case store.FixSucceeded:
		for p, f := range res.Files {
			j.changed[p], j.removed[p] = f, false
		}
		for _, p := range res.Removed {
			delete(j.changed, p)
			j.removed[p] = true
		}
		all := append(append([]string{}, res.Written...), res.Removed...)
		note = c.say("run_fix %s: %d archivo(s), %s · %s", "run_fix %s: %d file(s), %s · %s", name, len(all), strings.Join(all, ", "), took)
	case store.FixUnchanged:
		note = c.say("run_fix %s: sin cambios · %s", "run_fix %s: nothing changed · %s", name, took)
	default:
		note = c.say("run_fix %s: falló · %s", "run_fix %s: failed · %s", name, took)
	}
	_ = c.Store.NoteEncargoFix(context.WithoutCancel(ctx), j.e.ID, CoderName, note)
	var b strings.Builder
	fmt.Fprintf(&b, "fix %s ran in %s (image %s, %s), %s.\n", name, res.Plan.Path, res.Plan.Image, res.Plan.ImageFrom, took)
	switch res.Status {
	case store.FixSucceeded:
		fmt.Fprintf(&b, "It wrote %s; they are now among your changes: commit_and_test commits them.\n", strings.Join(append(append([]string{}, res.Written...), res.Removed...), ", "))
	case store.FixUnchanged:
		b.WriteString("Nothing it may write changed.\n")
	default:
		fmt.Fprintf(&b, "It failed: %s\nThe end of its log:\n", res.Message)
		lines := strings.Split(strings.TrimRight(ansi.ReplaceAllString(res.Log, ""), "\n"), "\n")
		b.WriteString(strings.Join(lines[max(0, len(lines)-60):], "\n") + "\n")
	}
	if len(res.Ignored) > 0 {
		fmt.Fprintf(&b, "It also changed files it may not write; they were left out: %s\n", strings.Join(res.Ignored, ", "))
	}
	return truncate(b.String(), 12000), res.Status == store.FixFailed
}

// commit commits what changed to the branch.
func (j *job) commit(ctx context.Context, message string) (string, error) {
	var remove []string
	for p, gone := range j.removed {
		if gone {
			remove = append(remove, p)
		}
	}
	sort.Strings(remove)
	var sha string
	var err error
	if j.merging != "" {
		sha, err = j.c.Repos.CommitMerge(ctx, j.app.InstallationID, j.app.Repo, j.branch, j.mergeHead, j.merging, message, j.changed, remove)
	} else {
		sha, err = j.c.Repos.CommitFiles(ctx, j.app.InstallationID, j.app.Repo, j.branch, message, j.changed, remove)
	}
	if err != nil {
		return "", err
	}
	j.merging, j.mergeHead = "", ""
	for p, f := range j.changed {
		j.files[p] = f
	}
	for _, p := range remove {
		delete(j.files, p)
	}
	j.changed, j.removed, j.lastSHA, j.lastRun = map[string][]byte{}, map[string]bool{}, sha, nil
	return sha, nil
}

// commitAndTest commits and waits for the platform's run on the commit.
func (j *job) commitAndTest(ctx context.Context, message string) (string, bool) {
	c := j.c
	nothing := len(j.changed) == 0 && len(j.removed) == 0 && j.merging == ""
	if nothing && (j.resumed == nil || j.lastSHA == "" || j.lastRun != nil) {
		return "nothing changed since the last commit", true
	}
	if left := j.marked(); len(left) > 0 {
		return "these files still have conflict marks; resolve them first: " + strings.Join(left, ", "), true
	}
	sha := j.lastSHA // resumed with nothing new: the run of what it committed before asking
	if !nothing {
		var err error
		if sha, err = j.commit(ctx, strings.TrimSpace(message)+"\n\n"+c.say("Encargo %d, hecho por el agente de código de rendimiento.", "Encargo %d, done by rendimiento's coding agent.", j.e.ID)); err != nil {
			return "the commit failed: " + err.Error(), true
		}
	}
	run, err := j.waitRun(ctx, sha)
	if err != nil {
		return fmt.Sprintf("committed %s, but: %s", sha[:min(7, len(sha))], err.Error()), true
	}
	// A run the cluster failed (etcd, an evicted pod, an image pull) says
	// nothing of the code: it runs once more before it counts.
	again := ""
	if run.Status == store.RunFailed {
		if sign := j.clusterSign(ctx, run); sign != "" {
			if nsha, nrun, err := j.rerun(ctx, sha, run); err == nil {
				again = fmt.Sprintf("run #%d failed for the cluster (%s), not the code, so it ran again. ", run.ID, sign)
				sha, run = nsha, nrun
			} else {
				again = fmt.Sprintf("run #%d seems to have failed for the cluster (%s), not the code, but it could not run again (%v). ", run.ID, sign, err)
			}
		}
	}
	j.lastRun = run
	if run.Status == store.RunSucceeded {
		return again + fmt.Sprintf("committed %s; run #%d passed. You may deliver.", sha[:min(7, len(sha))], run.ID), false
	}
	return again + j.failure(ctx, sha, run), true
}

// waitRun waits for the run on a commit to start (noRunWait) and end
// (waitEnd), marking the encargo as waiting on it meanwhile.
func (j *job) waitRun(ctx context.Context, sha string) (*store.Run, error) {
	c := j.c
	start := time.Now()
	for {
		runs, err := c.Store.ListRuns(ctx, j.app.ID, 30)
		if err != nil {
			return nil, err
		}
		for _, r := range runs {
			if r.SHA == sha {
				return j.waitEnd(ctx, r)
			}
		}
		if time.Since(start) > noRunWait {
			return nil, errors.New("no run started for the commit; the app may not build this branch")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.Poll):
		}
	}
}

// failure is how a run failed: its failed steps with the end of their logs,
// and the diagnostician's explanation if it has one.
func (j *job) failure(ctx context.Context, sha string, run *store.Run) string {
	var b strings.Builder
	fmt.Fprintf(&b, "committed %s; run #%d %s.\n", sha[:7], run.ID, run.Status)
	shown := 0
	for _, s := range run.Steps {
		if s.Status != store.RunFailed || shown == 3 {
			continue
		}
		shown++
		fmt.Fprintf(&b, "\nStep %s failed: %s\n", s.ID, s.Message)
		if j.c.Logs != nil {
			if text, err := j.c.Logs(ctx, run.ID, s.ID); err == nil {
				lines := strings.Split(strings.TrimRight(ansi.ReplaceAllString(text, ""), "\n"), "\n")
				b.WriteString(strings.Join(lines[max(0, len(lines)-80):], "\n"))
				b.WriteString("\n")
			}
		}
	}
	if d, err := j.c.Store.GetDiagnosis(ctx, run.ID); err == nil && d.Text != "" {
		fmt.Fprintf(&b, "\nThe platform's diagnostician says:\n%s\n", d.Text)
	}
	return truncate(b.String(), 20000)
}

// deliver hands the encargo over, once the latest commit's run passed.
func (j *job) deliver(ctx context.Context, summary string) (string, bool) {
	c := j.c
	if len(j.changed) > 0 || len(j.removed) > 0 {
		return "you have changes not committed; commit_and_test them first", true
	}
	if j.lastRun == nil || j.lastRun.Status != store.RunSucceeded {
		return "the latest commit has no passing run yet; commit_and_test first", true
	}
	result := strings.TrimSpace(summary) + "\n\n" + c.say("Gastó %s (estimado: %s).", "Spent %s (estimated: %s).", ia.Dollars(j.spent), ia.Dollars(j.budget/2))
	// The pull request goes into the branch it was made from.
	into := ""
	if j.from != j.app.DefaultBranch && !j.same {
		into = j.from
	}
	links := []string{forja.NewPullURL(j.app.ForjaOf(), j.app.Repo, into, j.branch)}
	if c.BaseURL != "" {
		links = append(links, fmt.Sprintf("%s/apps/%s/runs/%d", strings.TrimRight(c.BaseURL, "/"), j.app.Name, j.lastRun.ID))
	}
	if c.AlEntregar != nil {
		links = c.AlEntregar(context.WithoutCancel(ctx), j.e.OrgID, j.e.ID, j.e.Title, result, links)
	}
	if _, err := c.Store.DeliverEncargo(context.WithoutCancel(ctx), j.e.ID, j.key.ID, CoderName, result, links); err != nil {
		return "could not deliver: " + err.Error(), true
	}
	j.finished, j.outcome = true, c.say("lo entregó", "delivered it")
	return "delivered", false
}
