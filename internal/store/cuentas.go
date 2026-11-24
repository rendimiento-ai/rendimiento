package store

import (
	"context"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// Why the coding agent gave an encargo back (encargo_log.cause on its
// "released" entry): recorded when it lets it go, from the place it stopped
// (its brake, a person, a restart, an error) or from what the model said
// when it called give_up.
const (
	CauseBudget        = "budget"         // its brake: the next turn would pass twice the estimate
	CauseNeedsDecision = "needs_decision" // the encargo needs a person's answer
	CauseNoTools       = "no_tools"       // it lacks a tool it needs (git, running a command, reading logs)
	CauseCITimeout     = "ci_timeout"     // the run did not end in time
	CauseAIError       = "ai_error"       // the model failed, refused, or stopped using its tools
	CauseForgeError    = "forge_error"    // the forja (GitHub, GitLab, Bitbucket) answered with an error
	CauseConflict      = "conflict"       // it could not merge the default branch
	CauseStopped       = "stopped"        // a person stopped it
	CauseRestart       = "restart"        // the platform restarted while it worked
	CauseOther         = "other"
)

// Causes are the known causes, in the order the AI page lists them when
// they tie.
var Causes = []string{CauseBudget, CauseNeedsDecision, CauseNoTools, CauseCITimeout, CauseAIError, CauseForgeError, CauseConflict, CauseStopped, CauseRestart, CauseOther}

// KnownCause is cause when it is one of Causes, else "".
func KnownCause(cause string) string {
	cause = strings.ToLower(strings.TrimSpace(cause))
	if slices.Contains(Causes, cause) {
		return cause
	}
	return ""
}

// causeWords are how ClassifyCause reads a release's note: the first that
// matches wins, so the specific ones go first (a forja's error line before
// the word conflict, a conflict before the tools it lacked to resolve it).
// They are the words the coding agent's own messages use (agentes.Coder) and
// the ones its model wrote in Spanish and English.
var causeWords = []struct {
	cause string
	re    *regexp.Regexp
}{
	{CauseStopped, regexp.MustCompile(`una persona lo detuvo|a person stopped it`)},
	{CauseRestart, regexp.MustCompile(`plataforma se reinici|platform restarted`)},
	{CauseBudget, regexp.MustCompile(`doble de lo estimado|twice the estimate|presupuesto|budget`)},
	{CauseCITimeout, regexp.MustCompile(`no terminó en|no termino en|did not end in|no terminó|did not finish`)},
	{CauseAIError, regexp.MustCompile(`anthropic|api\.openai|el modelo|the model|overloaded|rate limit`)},
	{CauseForgeError, regexp.MustCompile(`(github|gitlab|bitbucket) (get|post|put|patch|delete) `)},
	{CauseConflict, regexp.MustCompile(`conflict|fusionar`)},
	{CauseNeedsDecision, regexp.MustCompile(`decisi|visto bueno|aprobaci|approv|unclear|no está claro`)},
	{CauseNoTools, regexp.MustCompile(`herramienta|tools|no tengo git|npm install|no puedo ejecutar|cannot run`)},
}

// ClassifyCause guesses a release's cause from its note, for the ones from
// before the cause was recorded (or whose model gave none): approximate, by
// the words in it; CauseOther when none matches.
func ClassifyCause(note string) string {
	n := strings.ToLower(note)
	for _, w := range causeWords {
		if w.re.MatchString(n) {
			return w.cause
		}
	}
	return CauseOther
}

// CuentaCausa is one cause of the coding agent's releases: how many, and of
// which encargos (each once, the oldest first); Approx is how many of them
// were classified from their note (ClassifyCause), not recorded.
type CuentaCausa struct {
	Cause    string  `json:"cause"`
	N        int     `json:"n"`
	Encargos []int64 `json:"encargos"`
	Approx   int     `json:"approx"`
}

// CuentaRazon is the middle (median) of what the coding agent spent over
// what was estimated, on the encargos it finished of a difficulty band
// (From to To; 0 and 0 for all of them).
type CuentaRazon struct {
	From int     `json:"from"`
	To   int     `json:"to"`
	X    float64 `json:"x"`
	N    int     `json:"n"`
}

// CuentaRacha is the coding agent giving encargos back for the same cause
// N times in a row, its latest outcomes (a delivery ends it; a person's stop
// and a restart are not its own and neither make nor end it).
type CuentaRacha struct {
	Cause    string  `json:"cause"`
	N        int     `json:"n"`
	Encargos []int64 `json:"encargos"`
}

// RachaFrom is how many releases in a row for one cause make a warning.
const RachaFrom = 3

// CoderCuentas is how an organization's coding agent is doing, over the
// encargos it took since a moment (all of them, when it is zero):
//   - Encargos it took; Finished it delivered (FirstTry without giving it
//     back first, AfterRetry after), GaveBack it gave back to the person's
//     own agent and did not finish, Working it has now;
//   - of the ones it finished, Accepted the person accepted at its first
//     delivery, Returned sent back with a comment, ToReview not answered yet;
//   - Causes, why it gave back the GaveBack (each one's last release), and
//     Retried, the releases of encargos it took again afterwards;
//   - Ratios, spent over estimate on what it finished: by difficulty band,
//     and All;
//   - Racha, a warning when its latest outcomes are RachaFrom or more
//     releases for the same cause (over all its record, not the period).
type CoderCuentas struct {
	Since      *time.Time    `json:"since,omitempty"`
	Encargos   int           `json:"encargos"`
	Finished   int           `json:"finished"`
	FirstTry   int           `json:"firstTry"`
	AfterRetry int           `json:"afterRetry"`
	GaveBack   int           `json:"gaveBack"`
	Working    int           `json:"working"`
	Accepted   int           `json:"accepted"`
	Returned   int           `json:"returned"`
	ToReview   int           `json:"toReview"`
	Causes     []CuentaCausa `json:"causes"`
	Retried    []CuentaCausa `json:"retried"`
	Ratios     []CuentaRazon `json:"ratios"`
	All        CuentaRazon   `json:"all"`
	Racha      *CuentaRacha  `json:"racha,omitempty"`
}

// cuentaBands are the difficulty bands the ratios are given by.
var cuentaBands = [][2]int{{1, 3}, {4, 6}, {7, 10}}

// cuentasEncargos is how many of the coding agent's latest encargos the
// counts read, at most.
const cuentasEncargos = 500

type cuentaEvento struct {
	id      int64
	at      time.Time
	who     string
	what    string
	cause   string
	approx  bool // cause read from the note
	encargo int64
}

type cuentaEncargo struct {
	id         int64
	status     string
	difficulty *int
	estimate   *int64
	spent      int64
	log        []cuentaEvento
}

// CoderCuentasOf counts how an organization's coding agent (who: its name,
// agentes.CoderName, as CoderRecord tells it) is doing over the encargos it
// took since since (zero: all of them).
func (s *Store) CoderCuentasOf(ctx context.Context, orgID int64, who string, since time.Time) (CoderCuentas, error) {
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.status, e.difficulty, e.estimate_micros, e.spent_micros,
			l.id, l.at, l.who, l.what, COALESCE(l.cause, ''), CASE WHEN l.what = 'released' AND l.cause IS NULL THEN l.note ELSE '' END
		FROM encargos e JOIN encargo_log l ON l.encargo_id = e.id
		WHERE e.org_id = $1 AND e.id IN (
			SELECT t.encargo_id FROM encargo_log t JOIN encargos te ON te.id = t.encargo_id
			WHERE te.org_id = $1 AND t.who = $2 AND t.what = 'taken'
			GROUP BY t.encargo_id ORDER BY max(t.at) DESC LIMIT $3)
		AND l.what IN ('taken', 'waiting', 'resumed', 'delivered', 'released', 'reviewed', 'returned', 'cancelled')
		ORDER BY e.id, l.at, l.id`, orgOr(orgID), who, cuentasEncargos)
	if err != nil {
		return CoderCuentas{}, err
	}
	defer rows.Close()
	var list []*cuentaEncargo
	for rows.Next() {
		var e cuentaEncargo
		var ev cuentaEvento
		var note string
		if err := rows.Scan(&e.id, &e.status, &e.difficulty, &e.estimate, &e.spent, &ev.id, &ev.at, &ev.who, &ev.what, &ev.cause, &note); err != nil {
			return CoderCuentas{}, err
		}
		if ev.what == "released" && ev.cause == "" {
			ev.cause, ev.approx = ClassifyCause(note), true
		}
		ev.encargo = e.id
		if n := len(list); n == 0 || list[n-1].id != e.id {
			list = append(list, &e)
		}
		last := list[len(list)-1]
		last.log = append(last.log, ev)
	}
	if err := rows.Err(); err != nil {
		return CoderCuentas{}, err
	}
	return cuentasFrom(list, who, since), nil
}

// cuentasFrom counts CoderCuentas from the encargos' logs (each in order).
func cuentasFrom(list []*cuentaEncargo, who string, since time.Time) CoderCuentas {
	c := CoderCuentas{Causes: []CuentaCausa{}, Retried: []CuentaCausa{}, Ratios: []CuentaRazon{}}
	if !since.IsZero() {
		c.Since = &since
	}
	causes, retried := map[string]*CuentaCausa{}, map[string]*CuentaCausa{}
	add := func(m map[string]*CuentaCausa, ev cuentaEvento) {
		k := m[ev.cause]
		if k == nil {
			k = &CuentaCausa{Cause: ev.cause, Encargos: []int64{}}
			m[ev.cause] = k
		}
		k.N++
		if ev.approx {
			k.Approx++
		}
		if !slices.Contains(k.Encargos, ev.encargo) {
			k.Encargos = append(k.Encargos, ev.encargo)
		}
	}
	type razon struct {
		d int
		x float64
	}
	var razones []razon
	var outcomes []cuentaEvento // the coder's deliveries and releases, all its record
	for _, e := range list {
		// Whether it took it in the period, where its first delivery is
		// (-1: none), where its releases are, and what it did last on it.
		took, delivered, released, lastCoder := false, -1, []int{}, ""
		for i, ev := range e.log {
			if ev.who != who {
				continue
			}
			switch ev.what {
			case "taken":
				if since.IsZero() || !ev.at.Before(since) {
					took = true
				}
			case "delivered":
				if delivered < 0 {
					delivered = i
				}
				outcomes = append(outcomes, ev)
			case "released":
				released = append(released, i)
				outcomes = append(outcomes, ev)
			}
			lastCoder = ev.what
		}
		if !took {
			continue
		}
		c.Encargos++
		switch {
		case delivered >= 0:
			c.Finished++
			if len(released) > 0 && released[0] < delivered {
				c.AfterRetry++
			} else {
				c.FirstTry++
			}
			// The person's answer to its first delivery.
			answer := ""
			for _, ev := range e.log[delivered+1:] {
				if ev.what == "reviewed" || ev.what == "returned" {
					answer = ev.what
					break
				}
			}
			switch {
			case answer == "reviewed":
				c.Accepted++
			case answer == "returned":
				c.Returned++
			case e.status == EncargoDelivered:
				c.ToReview++
			}
			if e.difficulty != nil && e.estimate != nil && *e.estimate > 0 && e.spent > 0 {
				razones = append(razones, razon{*e.difficulty, float64(e.spent) / float64(*e.estimate)})
			}
		case lastCoder == "released":
			c.GaveBack++
		default:
			c.Working++
		}
		// Why: the last release of one it gave back; the ones before, or
		// before it finished, are the retried.
		for k, i := range released {
			ev := e.log[i]
			if delivered < 0 && lastCoder == "released" && k == len(released)-1 {
				add(causes, ev)
			} else {
				add(retried, ev)
			}
		}
	}
	c.Causes, c.Retried = sortCausas(causes), sortCausas(retried)

	for _, b := range cuentaBands {
		var xs []float64
		for _, r := range razones {
			if r.d >= b[0] && r.d <= b[1] {
				xs = append(xs, r.x)
			}
		}
		if len(xs) > 0 {
			c.Ratios = append(c.Ratios, CuentaRazon{From: b[0], To: b[1], X: mediana(xs), N: len(xs)})
		}
	}
	var all []float64
	for _, r := range razones {
		all = append(all, r.x)
	}
	c.All = CuentaRazon{X: mediana(all), N: len(all)}

	sort.SliceStable(outcomes, func(a, b int) bool {
		if !outcomes[a].at.Equal(outcomes[b].at) {
			return outcomes[a].at.Before(outcomes[b].at)
		}
		return outcomes[a].id < outcomes[b].id
	})
	c.Racha = rachaOf(outcomes)
	return c
}

// rachaOf is the releases for one cause at the end of the outcomes, when
// they are RachaFrom or more.
func rachaOf(outcomes []cuentaEvento) *CuentaRacha {
	var r *CuentaRacha
	for i := len(outcomes) - 1; i >= 0; i-- {
		ev := outcomes[i]
		if ev.what == "released" && (ev.cause == CauseStopped || ev.cause == CauseRestart) {
			continue // not its own doing
		}
		if ev.what != "released" || (r != nil && ev.cause != r.Cause) {
			break
		}
		if r == nil {
			r = &CuentaRacha{Cause: ev.cause, Encargos: []int64{}}
		}
		r.N++
		if !slices.Contains(r.Encargos, ev.encargo) {
			r.Encargos = append([]int64{ev.encargo}, r.Encargos...)
		}
	}
	if r == nil || r.N < RachaFrom {
		return nil
	}
	return r
}

// sortCausas lists the causes, the most first (ties in Causes' order).
func sortCausas(m map[string]*CuentaCausa) []CuentaCausa {
	out := []CuentaCausa{}
	for _, k := range m {
		out = append(out, *k)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].N != out[b].N {
			return out[a].N > out[b].N
		}
		return slices.Index(Causes, out[a].Cause) < slices.Index(Causes, out[b].Cause)
	})
	return out
}

// mediana is the middle of xs (0 when there are none), to two decimals.
func mediana(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := s[len(s)/2]
	if len(s)%2 == 0 {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return math.Round(m*100) / 100
}
