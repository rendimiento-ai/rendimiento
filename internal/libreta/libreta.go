// Package libreta is an app's notebook (Ruta 150): what was asked for the
// app and whether it is done. It is a view, never a second memory: each
// line is a Ruta entry about the app (a to-do or a decision), an encargo
// about it, or a pull request of Renovate's, and its state is computed from
// what the platform already knows (the encargo's state, the runs of its
// branches, their pull requests and the releases of the default branch);
// nobody sets it by hand. Build is pure: the platform hands it what the
// store and GitHub know.
package libreta

import (
	"sort"
	"time"
)

// States of a line, in the order a request goes through them, and the two
// apart: Esperando (it waits on something that has not happened yet) and
// Descartado (set aside, with its reason). Hecho is done with no branch to
// say more: a to-do ticked in the Ruta, a decision, an accepted encargo.
const (
	Pedido     = "pedido"     // asked for
	EnCurso    = "en_curso"   // an agent took it, or a branch is linked
	Listo      = "listo"      // its branch's run passed: it can be merged
	Integrado  = "integrado"  // merged into the default branch
	Publicado  = "publicado"  // a release with it passed its verification
	Esperando  = "esperando"  // waiting on something else
	Descartado = "descartado" // set aside
	Hecho      = "hecho"      // done, with no branch to tell more
)

// Open reports whether a state is still open (not done or set aside).
func Open(status string) bool {
	switch status {
	case Pedido, EnCurso, Listo, Esperando:
		return true
	}
	return false
}

// Done reports whether a state is done.
func Done(status string) bool {
	return status == Integrado || status == Publicado || status == Hecho
}

// Where a line comes from.
const (
	FromRuta     = "ruta"
	FromEncargo  = "encargo"
	FromRenovate = "renovate"
	FromPruebas  = "pruebas" // the app's own tests, until they may roll a release back
)

// Why a line is in its state.
const (
	WhyBranch       = "branch"        // a branch, with no run yet
	WhyRunning      = "running"       // its run is queued or running
	WhyFailed       = "failed"        // its last run failed
	WhyCancelled    = "cancelled"     // its last run was cancelled
	WhyConflict     = "conflict"      // it conflicts with the default branch
	WhyBlocked      = "blocked"       // it passed, but GitHub will not merge it yet
	WhyPassed       = "passed"        // its last run passed
	WhyClosed       = "closed"        // its pull request was closed without merging
	WhyTaken        = "taken"         // an agent has its encargo
	WhyReview       = "review"        // its encargo was delivered: it waits for a person's review
	WhyMerged       = "merged"        // merged; no release made with it yet
	WhyVerifying    = "verifying"     // its release is being verified
	WhyVerifyFailed = "verify-failed" // its release did not pass, and no later one has yet
	WhyMarked       = "marked"        // ticked as done in the Ruta
	WhyDecided      = "decided"       // a decision
	WhyAccepted     = "accepted"      // its encargo was reviewed and accepted
	WhyTests        = "tests"         // the app's tests only report until they pass enough releases
)

// Entry is a Ruta entry about the app: a to-do or a decision.
type Entry struct {
	ID        int64
	Kind      string // pendiente or decision
	Title     string
	Author    string
	ByAgent   bool
	Done      bool
	Discarded string // why it was set aside, "" while it stands
	Cargo     int64
	Branches  []string // the branches it is worked on
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Encargo is an encargo about the app.
type Encargo struct {
	ID         int64
	Title      string
	Status     string // pending, taken, waiting, delivered, reviewed, cancelled
	CreatedBy  string
	ByAgent    bool
	Agent      string   // the agent that has it
	Branches   []string // its branch by name, its run's, those in its links
	Pulls      []int    // the pull requests in its links
	CancelNote string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Pull is a pull request of the app's repository.
type Pull struct {
	Number    int
	Title     string
	Branch    string // its head branch
	Open      bool
	Deps      bool // Renovate's or Dependabot's
	Author    string
	MergedAt  *time.Time
	ClosedAt  *time.Time
	CreatedAt time.Time
}

// Run is a run of the app.
type Run struct {
	ID        int64
	Branch    string
	SHA       string
	Status    string // queued, running, succeeded, failed, cancelled
	Merge     string // what became of its pull request when it passed
	Failed    []string
	Release   int64 // the release it made, if any
	CreatedAt time.Time
}

// Release is a release of the app, with its verification.
type Release struct {
	Number   int64
	RunID    int64
	Verify   string // store.Verify*
	Rollback bool
}

// Tests are the app's own tests that only report yet: each one blocks once
// it passes BlockAfter releases in a row.
type Tests struct {
	BlockAfter int
	Advisory   map[string]int // test → releases in a row it passed
}

// Input is everything a libreta is made of.
type Input struct {
	DefaultBranch string
	Entries       []Entry
	Encargos      []Encargo
	Pulls         []Pull // the most recently updated first
	GitHub        bool   // false when GitHub could not be asked
	Runs          []Run  // the app's recent runs, any order
	Releases      []Release
	// InMain says, for a branch, that its head commit is already in the
	// default branch (merged by another pull request, say).
	InMain map[string]bool
	Tests  *Tests
}

// Progress is how far a waiting line has gone.
type Progress struct {
	Done int `json:"done"`
	Of   int `json:"of"`
}

// Item is one line of the libreta.
type Item struct {
	Source    string     `json:"source"`
	ID        int64      `json:"id"` // the entry's, the encargo's or the pull request's number
	Kind      string     `json:"kind,omitempty"`
	Title     string     `json:"title"`
	By        string     `json:"by,omitempty"`
	ByAgent   bool       `json:"byAgent,omitempty"`
	Agent     string     `json:"agent,omitempty"`
	Status    string     `json:"status"`
	Why       string     `json:"why,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	Cargo     int64      `json:"cargo,omitempty"`
	Branch    string     `json:"branch,omitempty"`
	Pull      int        `json:"pull,omitempty"`
	Run       int64      `json:"run,omitempty"`
	RunStatus string     `json:"runStatus,omitempty"`
	Failed    []string   `json:"failed,omitempty"`
	Release   int64      `json:"release,omitempty"`
	At        time.Time  `json:"at"`               // when it was asked
	DoneAt    *time.Time `json:"doneAt,omitempty"` // when it was merged, ticked or set aside
	Progress  *Progress  `json:"progress,omitempty"`
	Tests     []string   `json:"tests,omitempty"`
}

// Libreta is the app's notebook: open lines first, newest first, then the
// done and set-aside ones, most recently finished first.
type Libreta struct {
	DefaultBranch string `json:"defaultBranch"`
	Items         []Item `json:"items"`
	// GitHub is false when it could not be asked: merges are then known
	// only from the runs, and Renovate's pull requests are missing.
	GitHub bool `json:"github"`
}

// rank orders the states a branch can give, so the most advanced of an
// item's branches speaks for it.
var rank = map[string]int{Pedido: 0, EnCurso: 2, Esperando: 3, Listo: 4, Integrado: 5, Publicado: 6}

// branchState is what one branch says of the line it belongs to.
type branchState struct {
	branch   string
	status   string
	why      string
	pull     int
	run      *Run
	release  int64
	mergedAt *time.Time
	closedAt *time.Time
	lastAt   time.Time
}

type book struct {
	in       Input
	pulls    map[string]*Pull // branch → its open pull request, or else the latest
	byNumber map[int]*Pull
	runs     map[string]*Run // branch → its last run
	main     []*Run          // the default branch's runs, oldest first
	releases map[int64]Release
}

// Build makes the libreta.
func Build(in Input) Libreta {
	b := &book{in: in, pulls: map[string]*Pull{}, byNumber: map[int]*Pull{}, runs: map[string]*Run{}, releases: map[int64]Release{}}
	for i := range in.Pulls {
		p := &in.Pulls[i]
		b.byNumber[p.Number] = p
		if p.Branch == "" || p.Branch == in.DefaultBranch {
			continue
		}
		if had := b.pulls[p.Branch]; had == nil || (!had.Open && (p.Open || p.Number > had.Number)) {
			b.pulls[p.Branch] = p
		}
	}
	for i := range in.Runs {
		r := &in.Runs[i]
		if r.Branch == in.DefaultBranch {
			b.main = append(b.main, r)
			continue
		}
		if had := b.runs[r.Branch]; had == nil || r.ID > had.ID {
			b.runs[r.Branch] = r
		}
	}
	sort.Slice(b.main, func(i, j int) bool { return b.main[i].ID < b.main[j].ID })
	for _, rel := range in.Releases {
		if !rel.Rollback && rel.RunID != 0 {
			b.releases[rel.RunID] = rel
		}
	}

	claimed := map[string]bool{} // branches that belong to an entry or an encargo
	items := []Item{}
	for _, e := range in.Entries {
		it := Item{Source: FromRuta, ID: e.ID, Kind: e.Kind, Title: e.Title, By: e.Author, ByAgent: e.ByAgent, Cargo: e.Cargo, At: e.CreatedAt}
		bs, ok := b.best(e.Branches, true)
		for _, br := range e.Branches {
			claimed[br] = true
		}
		upd := e.UpdatedAt
		switch {
		case ok && (bs.status == Integrado || bs.status == Publicado):
			b.apply(&it, bs)
		case e.Discarded != "":
			it.Status, it.Reason, it.DoneAt = Descartado, e.Discarded, &upd
			if ok {
				b.apply(&it, bs)
				it.Status, it.Why = Descartado, ""
			}
		case ok && bs.status != Pedido:
			b.apply(&it, bs)
		case e.Kind == "pendiente" && e.Done:
			it.Status, it.Why, it.DoneAt = Hecho, WhyMarked, &upd
		case e.Kind == "decision":
			it.Status, it.Why, it.DoneAt = Hecho, WhyDecided, &e.CreatedAt
		default:
			it.Status = Pedido
			if ok {
				b.apply(&it, bs) // its branch closed without merging: the request stands
				it.Status = Pedido
			}
		}
		items = append(items, it)
	}

	for _, e := range in.Encargos {
		it := Item{Source: FromEncargo, ID: e.ID, Title: e.Title, By: e.CreatedBy, ByAgent: e.ByAgent, At: e.CreatedAt}
		branches := append([]string(nil), e.Branches...)
		for _, n := range e.Pulls {
			if p := b.byNumber[n]; p != nil && p.Branch != "" {
				branches = append(branches, p.Branch)
			}
		}
		bs, ok := b.best(branches, false)
		for _, br := range branches {
			if b.seen(br) {
				claimed[br] = true
			}
		}
		upd := e.UpdatedAt
		switch {
		case ok && (bs.status == Integrado || bs.status == Publicado):
			b.apply(&it, bs)
		case e.Status == "cancelled":
			it.Status, it.Reason, it.DoneAt = Descartado, e.CancelNote, &upd
		case ok && bs.status != Pedido:
			b.apply(&it, bs)
		default:
			switch e.Status {
			case "taken", "waiting":
				it.Status, it.Why = EnCurso, WhyTaken
			case "delivered":
				it.Status, it.Why = Esperando, WhyReview
			case "reviewed":
				it.Status, it.Why, it.DoneAt = Hecho, WhyAccepted, &upd
			default:
				it.Status = Pedido
			}
			if ok {
				it.Branch, it.Pull = bs.branch, bs.pull
			}
		}
		if e.Status == "taken" || e.Status == "waiting" {
			it.Agent = e.Agent
		}
		items = append(items, it)
	}

	// Renovate's (and Dependabot's) pull requests no entry or encargo has.
	for i := range in.Pulls {
		p := &in.Pulls[i]
		if !p.Deps || claimed[p.Branch] || b.pulls[p.Branch] != p {
			continue
		}
		claimed[p.Branch] = true
		it := Item{Source: FromRenovate, ID: int64(p.Number), Title: p.Title, By: p.Author, At: p.CreatedAt}
		bs, _ := b.best([]string{p.Branch}, false)
		b.apply(&it, bs)
		if bs.status == Pedido { // closed without merging
			it.Status, it.Why, it.DoneAt = Descartado, WhyClosed, p.ClosedAt
		}
		items = append(items, it)
	}

	// The app's tests that only report yet: they wait for clean releases.
	if t := in.Tests; t != nil && len(t.Advisory) > 0 && t.BlockAfter > 0 {
		least := -1
		names := make([]string, 0, len(t.Advisory))
		for n, streak := range t.Advisory {
			names = append(names, n)
			if least < 0 || streak < least {
				least = streak
			}
		}
		sort.Strings(names)
		items = append(items, Item{Source: FromPruebas, Status: Esperando, Why: WhyTests, Tests: names,
			Progress: &Progress{Done: min(least, t.BlockAfter), Of: t.BlockAfter}})
	}

	sort.SliceStable(items, func(i, j int) bool {
		a, c := items[i], items[j]
		if Open(a.Status) != Open(c.Status) {
			return Open(a.Status)
		}
		if Open(a.Status) {
			return a.At.After(c.At)
		}
		return finished(a).After(finished(c))
	})
	return Libreta{DefaultBranch: in.DefaultBranch, Items: items, GitHub: in.GitHub}
}

// finished is when a done or set-aside line ended (when it was asked, if
// that is not known).
func finished(it Item) time.Time {
	if it.DoneAt != nil {
		return *it.DoneAt
	}
	return it.At
}

// seen reports whether anything is known of a branch: a run or a pull
// request.
func (b *book) seen(branch string) bool {
	return b.runs[branch] != nil || b.pulls[branch] != nil || b.in.InMain[branch]
}

// best is what the most advanced of the branches says (false when none is
// known). A branch named by hand (explicit) counts even with no run yet.
func (b *book) best(branches []string, explicit bool) (branchState, bool) {
	var out branchState
	found := false
	done := map[string]bool{}
	for _, br := range branches {
		if br == "" || br == b.in.DefaultBranch || done[br] {
			continue
		}
		done[br] = true
		if !b.seen(br) && !explicit {
			continue
		}
		s := b.state(br)
		if !found || rank[s.status] > rank[out.status] || rank[s.status] == rank[out.status] && s.lastAt.After(out.lastAt) {
			out, found = s, true
		}
	}
	return out, found
}

// state is what one branch says: its pull request, its last run, whether
// it was merged and in which release it went out.
func (b *book) state(branch string) branchState {
	s := branchState{branch: branch}
	p, r := b.pulls[branch], b.runs[branch]
	if p != nil {
		s.pull, s.closedAt = p.Number, p.ClosedAt
		s.lastAt = p.CreatedAt
	}
	if r != nil {
		s.run = r
		if r.CreatedAt.After(s.lastAt) {
			s.lastAt = r.CreatedAt
		}
	}
	merged := p != nil && p.MergedAt != nil || r != nil && r.Merge == "merged" || b.in.InMain[branch]
	if merged && !(p != nil && p.Open) {
		if p != nil && p.MergedAt != nil {
			s.mergedAt = p.MergedAt
		} else if r != nil && r.Merge == "merged" {
			at := r.CreatedAt
			s.mergedAt = &at
		}
		s.status, s.why = Integrado, WhyMerged
		b.published(&s)
		return s
	}
	switch {
	case p != nil && !p.Open:
		s.status, s.why = Pedido, WhyClosed
	case r == nil:
		s.status, s.why = EnCurso, WhyBranch
	case r.Status == "queued" || r.Status == "running":
		s.status, s.why = EnCurso, WhyRunning
	case r.Status == "succeeded" && r.Merge == "conflict":
		s.status, s.why = EnCurso, WhyConflict
	case r.Status == "succeeded" && r.Merge == "blocked":
		s.status, s.why = Esperando, WhyBlocked
	case r.Status == "succeeded":
		s.status, s.why = Listo, WhyPassed
	case r.Status == "cancelled":
		s.status, s.why = EnCurso, WhyCancelled
	default:
		s.status, s.why = EnCurso, WhyFailed
	}
	return s
}

// published finds the release a merged branch went out in: the first one
// made by a run of the default branch created after the merge (so it holds
// it) whose verification passed. Until one passes, the first such release
// is named, and why it is not published yet.
func (b *book) published(s *branchState) {
	if s.mergedAt == nil {
		return
	}
	var first *Release
	for _, r := range b.main {
		if r.CreatedAt.Before(*s.mergedAt) {
			continue
		}
		rel, ok := b.releases[r.ID]
		if !ok {
			continue
		}
		switch rel.Verify {
		case "passed", "skipped", "":
			s.status, s.why, s.release = Publicado, "", rel.Number
			return
		}
		if first == nil {
			r := rel
			first = &r
		}
	}
	if first != nil {
		s.release = first.Number
		if first.Verify == "verifying" {
			s.why = WhyVerifying
		} else {
			s.why = WhyVerifyFailed
		}
	}
}

// apply puts what a branch says on a line.
func (b *book) apply(it *Item, s branchState) {
	it.Status, it.Why, it.Branch, it.Pull, it.Release = s.status, s.why, s.branch, s.pull, s.release
	if s.run != nil {
		it.Run, it.RunStatus, it.Failed = s.run.ID, s.run.Status, s.run.Failed
	}
	if s.mergedAt != nil {
		it.DoneAt = s.mergedAt
	}
}
