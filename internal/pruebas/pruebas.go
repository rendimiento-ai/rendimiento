// Package pruebas judges an app's own tests after a release: it reads the
// line each test printed, and keeps for each test whether it only reports
// (advisory), may roll a release back (blocking), or is set aside
// (quarantined) because it fails and then passes on its own. A test that
// cries wolf must never roll back a good release. Pure: the platform reads
// and saves the states.
package pruebas

import (
	"sort"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/spec"

	"github.com/rendimiento-ai/rendimiento/internal/i18n"
)

// What a test may do to a release.
const (
	Advisory    = "advisory"    // a failure is reported; every new test starts here
	Blocking    = "blocking"    // a failure rolls the release back
	Quarantined = "quarantined" // not counted: it failed and passed on its own too often
)

// FlakesToQuarantine is how many flaky runs set a test aside.
const FlakesToQuarantine = 3

// What Playwright says of a test.
const (
	Passed  = "expected"
	Failed  = "unexpected"
	Flaky   = "flaky" // failed, then passed on its retry
	Skipped = "skipped"
)

// Result is one test's outcome in a release.
type Result struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// State is what is kept of a test between releases.
type State struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Streak int    `json:"streak"` // releases in a row it passed
	Flakes int    `json:"flakes"`
	Last   string `json:"last"` // its status in the last release
}

// Parse reads the tests' lines from the task's log; reported is false when
// the tests left no report (they did not run).
func Parse(log string) (results []Result, reported bool) {
	seen := map[string]bool{}
	for _, line := range strings.Split(log, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == spec.TestsNoReport {
			return nil, false
		}
		rest, ok := strings.CutPrefix(line, spec.TestsReportLine+"\t")
		if !ok {
			continue
		}
		status, name, ok := strings.Cut(rest, "\t")
		if !ok || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		results = append(results, Result{Name: name, Status: status})
	}
	return results, len(results) > 0
}

// Judge moves each test's state with this release's results: failures are
// the blocking tests that failed (they roll the release back), warnings
// what a person should know. blockAfter is how many clean releases make a
// test blocking. A quarantined test that passes that many times in a row
// is counted again, as advisory.
func Judge(prev map[string]State, results []Result, blockAfter int) (next []State, failures, warnings []string) {
	for _, r := range results {
		st, ok := prev[r.Name]
		if !ok {
			st = State{Name: r.Name, State: Advisory}
		}
		st.Last = r.Status
		switch r.Status {
		case Passed:
			st.Streak++
			switch {
			case st.State == Advisory && st.Streak >= blockAfter:
				st.State = Blocking
				warnings = append(warnings, i18n.M("test %s now blocks: it passed %d releases in a row", r.Name, st.Streak))
			case st.State == Quarantined && st.Streak >= blockAfter:
				st.State, st.Flakes = Advisory, 0
				warnings = append(warnings, i18n.M("test %s is counted again: it passed %d releases in a row", r.Name, st.Streak))
			}
		case Failed:
			st.Streak = 0
			switch st.State {
			case Blocking:
				failures = append(failures, i18n.M("test %s failed, and it blocks", r.Name))
			case Advisory:
				warnings = append(warnings, i18n.M("test %s failed (it only reports until it passes %d releases in a row)", r.Name, blockAfter))
			}
		case Flaky:
			st.Streak = 0
			st.Flakes++
			if st.State != Quarantined && st.Flakes >= FlakesToQuarantine {
				st.State = Quarantined
				warnings = append(warnings, i18n.M("test %s set aside: it failed and then passed on its own %d times", r.Name, st.Flakes))
			} else if st.State != Quarantined {
				warnings = append(warnings, i18n.M("test %s failed and then passed on its retry", r.Name))
			}
		default: // skipped: nothing changes
			continue
		}
		next = append(next, st)
	}
	sort.Slice(next, func(i, j int) bool { return next[i].Name < next[j].Name })
	return next, failures, warnings
}
