package forja

import "time"

// Check says on a commit, and on its pull, whether it passed (a check run
// on GitHub).
type Check struct {
	Name    string `json:"name,omitempty"`
	HeadSHA string `json:"head_sha,omitempty"`
	Status  string `json:"status,omitempty"` // queued | in_progress | completed
	// Conclusion, once completed: success | failure | cancelled, or
	// action_required when a person must merge it.
	Conclusion string  `json:"conclusion,omitempty"`
	DetailsURL string  `json:"details_url,omitempty"`
	Output     *Output `json:"output,omitempty"`
}

// Output is a check's title and summary.
type Output struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// CheckOutput builds a check's title and summary.
func CheckOutput(title, summary string) *Output { return &Output{title, summary} }

// Opened is a pull opened (or found open) by rendimiento.
type Opened struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

// Branch is a branch and the commit it points at.
type Branch struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// Change is a pull request (a merge request on GitLab): what the Caminos
// tab, previews and merging need of it.
type Change struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	State string `json:"state"`
	Draft bool   `json:"draft"`
	Head  struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"` // a fork's, when the branch is not this repository's
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	MergedAt       *time.Time `json:"merged_at"`
	MergeCommitSHA string     `json:"merge_commit_sha"`
	// CreatedAt and ClosedAt are when it was opened and closed (the
	// libreta's dates for Renovate's).
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	// User is who opened it: a bot (Type "Bot") for Renovate's.
	User struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
	// Mergeable is nil while the forja still works it out; only Pull has
	// it, with MergeableState ("clean", "dirty" for a conflict, "blocked"...).
	Mergeable      *bool  `json:"mergeable"`
	MergeableState string `json:"mergeable_state"`
}

// Event is one webhook delivery, read.
type Event struct {
	// Kind is the forja's name for the event ("push", "pull_request"...),
	// for the log.
	Kind string
	// Push is set for a push to a branch.
	Push *Push
	// Change is set when a pull was opened, reopened or closed.
	Change *ChangeEvent
}

// Push is a push to a branch.
type Push struct {
	Installation int64
	Repo         string
	Branch       string
	SHA          string
	Deleted      bool
	// SkipCI: the head commit's message asks not to run ([skip ci]).
	SkipCI bool
	// Messages are the pushed commits' messages: a «Ruta: N» line in one
	// links the branch to that Ruta entry (its libreta).
	Messages []string
}

// ChangeEvent is a pull opened, reopened or closed.
type ChangeEvent struct {
	Repo   string
	Number int
	Action string // opened | reopened | closed (others are ignored)
	// Merged (with when) is a close that merged it.
	Merged   bool
	MergedAt *time.Time
	// HeadRef and HeadSHA are its branch and commit; HeadRepo is the
	// repository the branch is in ("" when the forja did not say).
	HeadRef, HeadSHA, HeadRepo string
}
