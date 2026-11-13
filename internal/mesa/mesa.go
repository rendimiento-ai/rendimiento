// Package mesa is an app's work board: each line of work (a branch with its
// pull request, its last run and the encargos about it, or an encargo with
// no branch) with how it stands and who it waits for. Build is pure: the
// platform hands it what the store and GitHub know.
package mesa

import (
	"regexp"
	"sort"
	"strconv"
	"time"
)

// Who a line waits for: the groups of the board, in the order it shows them.
const (
	GroupYou     = "you"     // a person: merge, review, look at a failure
	GroupWorking = "working" // a run or an agent is on it
	GroupSpare   = "spare"   // can be closed: a copy, or forgotten
	GroupToday   = "today"   // merged in the last day
)

// Why a line is in its group.
const (
	WhyPassed    = "passed"    // its last run passed: it can be merged
	WhyBlocked   = "blocked"   // it passed, but GitHub refuses to merge it
	WhyConflict  = "conflict"  // it conflicts with the default branch
	WhyFailed    = "failed"    // its last run failed and nobody has it
	WhyCancelled = "cancelled" // its last run was cancelled
	WhyUntested  = "untested"  // an open pull request with no run here
	WhyReview    = "review"    // an encargo was delivered: it waits for a review
	WhyRunning   = "running"   // its run is queued or running
	WhyTaken     = "taken"     // an agent has its encargo
	WhyCopy      = "copy"      // its pull request repeats another (CopyOf)
	WhyStale     = "stale"     // failed long ago, with no pull request
	WhyMerged    = "merged"
)

// StaleAfter is how long a failed branch with no pull request waits before
// the board calls it forgotten; TodayFor how long a merge stays in Today.
const (
	StaleAfter = 7 * 24 * time.Hour
	TodayFor   = 24 * time.Hour
)

// Pull is a pull request as the board needs it.
type Pull struct {
	Number   int
	Title    string
	Branch   string // its head branch
	SHA      string // its head commit
	Open     bool
	MergedAt *time.Time
}

// Run is a run of the app.
type Run struct {
	ID      int64
	Branch  string
	SHA     string
	Status  string // queued, running, succeeded, failed, cancelled
	Merge   string // what became of its pull request when it passed
	Failed  []string
	Release int64
	At      time.Time // when it finished, or was created
}

// Heads is what GitHub says of the repository's branches now: the commit
// each one points at, and which commits are already in the default branch.
// With Known false (GitHub not asked, or too many branches to list) the
// board keeps every branch it sees in the runs.
type Heads struct {
	Known  bool
	SHA    map[string]string // branch → its commit
	InMain map[string]bool   // commit → it is in the default branch
}

// Encargo is an encargo about the app.
type Encargo struct {
	ID     int64
	Title  string
	Status string // pending, taken, waiting, delivered
	Agent  string // the agent that has it
	RunID  int64  // the run it is about, or 0
	Links  []string
	At     time.Time
}

// Line is one line of work.
type Line struct {
	Group string `json:"group"`
	Why   string `json:"why"`
	// Branch is "" for an encargo with no branch.
	Branch    string   `json:"branch,omitempty"`
	Pull      int      `json:"pull,omitempty"`
	PullTitle string   `json:"pullTitle,omitempty"`
	PullOpen  bool     `json:"pullOpen,omitempty"`
	Run       int64    `json:"run,omitempty"`
	RunStatus string   `json:"runStatus,omitempty"`
	Failed    []string `json:"failed,omitempty"`
	Release   int64    `json:"release,omitempty"`
	// Encargos are the open ones about this line; Agent who has one.
	Encargos []LineEncargo `json:"encargos,omitempty"`
	Agent    string        `json:"agent,omitempty"`
	// CopyOf is the pull request this line's repeats.
	CopyOf int       `json:"copyOf,omitempty"`
	At     time.Time `json:"at"`
}

// LineEncargo is an encargo on a line.
type LineEncargo struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// Board is the work board, its lines sorted by group then newest first.
type Board struct {
	Lines []Line `json:"lines"`
	// GitHub is false when it could not be asked: the lines then come from
	// the runs and encargos only.
	GitHub bool `json:"github"`
}

// You counts the lines that wait for a person.
func (b Board) You() int {
	n := 0
	for _, l := range b.Lines {
		if l.Group == GroupYou {
			n++
		}
	}
	return n
}

// branchLink finds a branch in an encargo's link: a "pull/new/<branch>"
// or "tree/<branch>" on GitHub.
var branchLink = regexp.MustCompile(`github\.com/[^/]+/[^/]+/(?:pull/new|tree)/([^?#\s]+)`)

// pullLink finds a pull request's number in a link.
var pullLink = regexp.MustCompile(`github\.com/[^/]+/[^/]+/pull/(\d+)`)

type line struct {
	branch   string
	pull     *Pull // the open one, or else the latest
	run      *Run  // the last one
	encargos []Encargo
}

// Build makes the board from the app's pull requests (the most recently
// updated first), its recent runs (any order) and its open encargos.
func Build(defaultBranch string, pulls []Pull, github bool, heads Heads, runs []Run, encargos []Encargo, now time.Time) Board {
	byBranch := map[string]*line{}
	get := func(b string) *line {
		l := byBranch[b]
		if l == nil {
			l = &line{branch: b}
			byBranch[b] = l
		}
		return l
	}
	byNumber := map[int]*Pull{}
	for i := range pulls {
		p := &pulls[i]
		byNumber[p.Number] = p
		if p.Branch == defaultBranch || p.Branch == "" {
			continue
		}
		l := get(p.Branch)
		if l.pull == nil || (!l.pull.Open && p.Open) {
			l.pull = p
		}
	}
	runByID := map[int64]*Run{}
	for i := range runs {
		r := &runs[i]
		runByID[r.ID] = r
		if r.Branch == defaultBranch || r.Branch == "" {
			continue
		}
		l := get(r.Branch)
		if l.run == nil || r.ID > l.run.ID {
			l.run = r
		}
	}

	// An encargo's branch: from its links, else from its run.
	var loose []Encargo
	for _, e := range encargos {
		b := ""
		for _, link := range e.Links {
			if m := branchLink.FindStringSubmatch(link); m != nil {
				b = m[1]
				break
			}
			if m := pullLink.FindStringSubmatch(link); m != nil {
				n, _ := strconv.Atoi(m[1])
				if p := byNumber[n]; p != nil {
					b = p.Branch
					break
				}
			}
		}
		if r := runByID[e.RunID]; b == "" && r != nil {
			b = r.Branch
		}
		if b == "" || b == defaultBranch {
			loose = append(loose, e)
			continue
		}
		get(b).encargos = append(get(b).encargos, e)
	}

	// Copies: an open pull request with the head commit of another, the
	// original being the merged one, or else the oldest.
	original := map[string]*Pull{}
	for i := range pulls {
		p := &pulls[i]
		if p.SHA == "" || p.Branch == defaultBranch || (!p.Open && p.MergedAt == nil) {
			continue
		}
		o := original[p.SHA]
		if o == nil || (p.MergedAt != nil && o.MergedAt == nil) ||
			((p.MergedAt != nil) == (o.MergedAt != nil) && p.Number < o.Number) {
			original[p.SHA] = p
		}
	}

	var out []Line
	for _, l := range byBranch {
		if ln, ok := l.judge(original, heads, now); ok {
			out = append(out, ln)
		}
	}
	for _, e := range loose {
		ln := Line{Encargos: []LineEncargo{{ID: e.ID, Title: e.Title, Status: e.Status}}, At: e.At}
		switch e.Status {
		case "delivered":
			ln.Group, ln.Why = GroupYou, WhyReview
		case "taken", "waiting":
			ln.Group, ln.Why, ln.Agent = GroupWorking, WhyTaken, e.Agent
		default:
			continue // pending: on the encargos page, not anyone's yet
		}
		out = append(out, ln)
	}
	order := map[string]int{GroupYou: 0, GroupWorking: 1, GroupSpare: 2, GroupToday: 3}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return order[out[i].Group] < order[out[j].Group]
		}
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].Branch < out[j].Branch
	})
	if out == nil {
		out = []Line{}
	}
	return Board{Lines: out, GitHub: github}
}

// judge says where a branch's line goes, and false when it is not on the
// board (closed, or merged more than a day ago).
func (l *line) judge(original map[string]*Pull, heads Heads, now time.Time) (Line, bool) {
	ln := Line{Branch: l.branch}
	if p := l.pull; p != nil {
		ln.Pull, ln.PullTitle, ln.PullOpen = p.Number, p.Title, p.Open
	}
	if r := l.run; r != nil {
		ln.Run, ln.RunStatus, ln.Failed, ln.Release, ln.At = r.ID, r.Status, r.Failed, r.Release, r.At
	}
	delivered, taken := false, false
	for _, e := range l.encargos {
		ln.Encargos = append(ln.Encargos, LineEncargo{ID: e.ID, Title: e.Title, Status: e.Status})
		switch e.Status {
		case "delivered":
			delivered = true
		case "taken", "waiting":
			taken = true
			ln.Agent = e.Agent
		}
		if e.At.After(ln.At) {
			ln.At = e.At
		}
	}
	set := func(group, why string) (Line, bool) {
		ln.Group, ln.Why = group, why
		return ln, true
	}

	p, r := l.pull, l.run
	open := p != nil && p.Open
	head, exists := heads.SHA[l.branch]
	// A branch already in the default branch is merged, by its own pull
	// request or by another built on it.
	inMain := heads.Known && exists && heads.InMain[head]
	merged := (p != nil && p.MergedAt != nil) || (r != nil && r.Merge == "merged") || inMain
	if merged && !open {
		at := ln.At
		if p != nil && p.MergedAt != nil {
			at = *p.MergedAt
		}
		if now.Sub(at) > TodayFor {
			return ln, false
		}
		ln.At = at
		return set(GroupToday, WhyMerged)
	}
	if p != nil && !p.Open && !taken && !delivered {
		return ln, false // closed without merging
	}
	if heads.Known && !exists && !open && !taken {
		return ln, false // the branch is gone
	}
	if p != nil && p.Open {
		if first := original[p.SHA]; first != nil && first.Number != p.Number {
			ln.CopyOf = first.Number
			return set(GroupSpare, WhyCopy)
		}
	}
	if r != nil && (r.Status == "queued" || r.Status == "running") {
		return set(GroupWorking, WhyRunning)
	}
	if taken {
		return set(GroupWorking, WhyTaken)
	}
	// What can be merged comes before a review: merging is what is asked.
	switch {
	case r != nil && r.Merge == "conflict":
		return set(GroupYou, WhyConflict)
	case r != nil && r.Status == "succeeded" && r.Merge == "blocked":
		return set(GroupYou, WhyBlocked)
	case r != nil && r.Status == "succeeded":
		return set(GroupYou, WhyPassed)
	case delivered:
		return set(GroupYou, WhyReview)
	case r == nil && p != nil && p.Open:
		return set(GroupYou, WhyUntested)
	case r == nil:
		return ln, false
	case r.Status == "cancelled":
		return set(GroupYou, WhyCancelled)
	case p == nil && now.Sub(r.At) > StaleAfter:
		return set(GroupSpare, WhyStale)
	default:
		return set(GroupYou, WhyFailed)
	}
}
