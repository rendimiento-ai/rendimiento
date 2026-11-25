package agentes

// The coding agent asks and sees (Ruta 178): it can ask the person a
// question and stop without spending until they answer (ask), it reads
// its app's runs, step logs, slow requests and status (runs, step_log,
// slow_requests, app_status), and it does not give an encargo up for what
// passes by itself: a dropped connection to the model, a busy forja, a
// long run, or a run the cluster failed.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/ruta"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// AskStore is where the coding agent keeps its questions (store.Store):
// when the coder's Store is one, it offers the ask tool.
type AskStore interface {
	AskEncargo(ctx context.Context, id, keyID int64, who string, q store.EncargoPregunta) (store.Encargo, error)
	ResumeAnswer(ctx context.Context, id int64) (store.EncargoPregunta, error)
	UnansweredSince(ctx context.Context, before time.Time) ([]store.EncargoPregunta, error)
	PassAskedEncargo(ctx context.Context, id, orgID int64, who, note string) (store.Encargo, error)
}

// CoderPlataforma is what the coding agent's read-only tools read beyond
// its store (store.Store): only ever about the encargo's own app.
type CoderPlataforma interface {
	ListReleases(ctx context.Context, appID int64, limit int) ([]*store.Release, error)
	ListProblems(ctx context.Context, app string, since time.Time, withDismissed bool, limit int) ([]store.Problem, error)
	SlowRequests(ctx context.Context, app string, platform bool, since time.Time, limit int) ([]store.SlowRequest, error)
	HallazgosDeApp(ctx context.Context, app string, limit int) ([]store.VigiaHallazgo, error)
	ListTraces(ctx context.Context, q store.TraceQuery) ([]store.Trace, error)
}

// Pauses before trying again, variables so tests do not wait: the model
// after the gateway's own tries (then 4× and 9×), and the forja once.
var (
	aiPause    = 20 * time.Second
	forjaPause = 3 * time.Second
)

// aiTries is how many more times a turn is asked when the model's API
// failed for a reason that passes (ia.Passing), before giving up.
const aiTries = 3

// maxWork is how long the coding agent may work on an encargo, not
// counting the time it waits for runs (which spends nothing).
const maxWork = 3 * time.Hour

func (c *Coder) asks() (AskStore, bool) {
	a, ok := c.Store.(AskStore)
	return a, ok
}

// say is the job's c.say.
func (j *job) say(es, en string, args ...any) string { return j.c.say(es, en, args...) }

// complete asks the model one turn, and again (up to aiTries) when its API
// failed for a reason that passes: a reset connection, a 5xx, a 429. What
// each try cost is counted; only answered turns guide the brake.
func (j *job) complete(ctx context.Context, req ia.ToolRequest) (ia.ToolResponse, error) {
	c := j.c
	for try := 0; ; try++ {
		res, err := c.Model.CompleteTools(ctx, j.e.OrgID, CoderSource, req)
		j.spent += res.CostMicros
		if err == nil {
			j.costs = append(j.costs, res.CostMicros)
		}
		if res.CostMicros != 0 {
			_ = c.Store.AddEncargoSpent(context.WithoutCancel(ctx), j.e.ID, res.CostMicros)
		}
		if err == nil || !ia.Passing(err) || try >= aiTries || ctx.Err() != nil {
			return res, err
		}
		c.Log.Warn("coder: the model's API failed; trying again", "encargo", j.e.ID, "try", try+1, "err", err)
		j.doing(ctx, "ai_retry", fmt.Sprint(try+1))
		select {
		case <-ctx.Done():
			return res, err
		case <-time.After(time.Duration((try+1)*(try+1)) * aiPause):
		}
	}
}

// ---- asking the person ----

var askTool = ia.Tool{Name: "ask", Description: "Ask the person who asked for the encargo a question, when you cannot go on well without their word (a decision, a design to choose, something the encargo does not say). You stop at once without spending: what you changed is committed on your branch, and when they answer you resume on the same branch with their answer and your notes. Offer 2 to 4 short options when the choice is between known ones. If nobody answers in 3 days, the encargo goes to their own agent with your question. Do not ask what you can find out with your tools.",
	InputSchema: map[string]any{"type": "object", "required": []string{"question"}, "properties": map[string]any{
		"question": str("the question, short and clear, in the person's language"),
		"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "2 to 4 options to choose from, or none"},
		"notes":    str("for yourself when you resume: what you did, what you found, and what you will do with each answer"),
	}}}

// ask stops the work to ask the person: what changed is committed on the
// branch (tested by CI meanwhile), and the encargo asks until answered.
func (j *job) ask(ctx context.Context, input json.RawMessage) (string, bool) {
	c := j.c
	st, ok := c.asks()
	if !ok {
		return "asking is not possible here; give_up with your question instead", true
	}
	var in struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
		Notes    string   `json:"notes"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "the input is not valid JSON: " + err.Error(), true
	}
	in.Question = strings.TrimSpace(in.Question)
	var opts []string
	for _, o := range in.Options {
		if o = strings.TrimSpace(o); o != "" {
			opts = append(opts, truncate(o, 300))
		}
	}
	switch {
	case in.Question == "":
		return "the question is empty", true
	case len(opts) == 1 || len(opts) > 4:
		return fmt.Sprintf("offer 2 to 4 options, or none; you gave %d", len(opts)), true
	case len(j.marked()) > 0:
		return "these files still have conflict marks; resolve them before you ask: " + strings.Join(j.marked(), ", "), true
	}
	sha, msg, ok := j.commitBeforeAsking(ctx)
	if !ok {
		return msg, true
	}
	notes := strings.TrimSpace(in.Notes)
	if sha != "" {
		notes += "\n\n" + fmt.Sprintf("(Your changes were committed as %s before asking.)", sha[:min(7, len(sha))])
	}
	q := store.EncargoPregunta{Question: truncate(in.Question, 2000), Options: opts, Notes: truncate(strings.TrimSpace(notes), 8000), SpentMicros: j.spent}
	if _, err := st.AskEncargo(context.WithoutCancel(ctx), j.e.ID, j.key.ID, CoderName, q); err != nil {
		return "could not ask: " + err.Error(), true
	}
	j.finished, j.outcome = true, j.say("le preguntó a la persona y espera su respuesta", "asked the person and waits for their answer")
	return "asked; you stop here until the person answers", false
}

// commitBeforeAsking commits what changed before the job stops to ask (a
// question or a permission): the commit ("" when nothing changed), or why
// it may not ask yet.
func (j *job) commitBeforeAsking(ctx context.Context) (string, string, bool) {
	if left := j.marked(); len(left) > 0 {
		return "", "these files still have conflict marks; resolve them before you ask: " + strings.Join(left, ", "), false
	}
	pending := len(j.changed) > 0 || len(j.removed) > 0 || j.merging != ""
	if pending && j.same && store.EncargoOfBranch(j.branch) == 0 {
		return "", "you have changes not committed on the person's own branch: commit_and_test them first, then ask", false
	}
	if !pending {
		return "", "", true
	}
	sha, err := j.commit(context.WithoutCancel(ctx), j.say("Encargo %d: lo que llevaba al preguntar, sin probar todavía\n\nEl agente de código le preguntó algo a la persona y sigue al recibir su respuesta.",
		"Encargo %d: what it had when it asked, not tested yet\n\nThe coding agent asked the person something and goes on with their answer.", j.e.ID))
	if err != nil {
		return "", "could not commit your changes before asking: " + err.Error(), false
	}
	return sha, "", true
}

// resumedHead is the branch's head when the job resumes after a question
// (false when it does not, or the branch is gone).
func (j *job) resumedHead(ctx context.Context) (string, bool) {
	if j.resumed == nil {
		return "", false
	}
	head, err := j.c.Repos.BranchSHA(ctx, j.app.InstallationID, j.app.Repo, j.branch)
	return head, err == nil && head != ""
}

// resumeBrief is what the first message adds when the encargo resumes
// after a question: the question, the answer and the notes.
func (j *job) resumeBrief() string {
	p := j.resumed
	if p == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nYou worked on this encargo before and stopped to ask the person:\n%s\n", p.Question)
	for i, o := range p.Options {
		fmt.Fprintf(&b, "  %c. %s\n", 'A'+i, o)
	}
	fmt.Fprintf(&b, "\n%s answered:\n%s\n", orElse(p.AnsweredBy, "The person"), p.Answer)
	if p.Notes != "" {
		fmt.Fprintf(&b, "\nYour notes from before:\n%s\n", p.Notes)
	}
	fmt.Fprintf(&b, "\nYou go on on the same branch, %s, with what you committed; you had spent %s. If nothing changes after the answer, commit_and_test with no changes waits for the run of the branch's last commit.\n", j.branch, ia.Dollars(p.SpentMicros))
	return b.String()
}

func orElse(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// PassNote is what an encargo that asked says when it goes to the
// person's own agent: the question and its options, in lang.
func PassNote(lang string, q store.EncargoPregunta, expired bool) string {
	es := lang == "es"
	var b strings.Builder
	switch {
	case expired && es:
		b.WriteString("Nadie contestó en 3 días lo que preguntó el agente de código; pasa a su agente con la pregunta.")
	case expired:
		b.WriteString("Nobody answered the coding agent's question in 3 days; it goes to your agent with the question.")
	case es:
		b.WriteString("La persona lo pasó a su agente en lugar de contestar.")
	default:
		b.WriteString("The person passed it to their agent instead of answering.")
	}
	if es {
		b.WriteString("\n\nEl agente de código preguntó: ")
	} else {
		b.WriteString("\n\nThe coding agent asked: ")
	}
	b.WriteString(q.Question)
	for i, o := range q.Options {
		fmt.Fprintf(&b, "\n%c. %s", 'A'+i, o)
	}
	if q.Notes != "" {
		if es {
			b.WriteString("\n\nSus notas: ")
		} else {
			b.WriteString("\n\nIts notes: ")
		}
		b.WriteString(q.Notes)
	}
	return b.String()
}

// passUnanswered gives the person's own agent the encargos whose question
// nobody answered in store.AskWait, with the question.
func (c *Coder) passUnanswered(ctx context.Context, now time.Time) {
	st, ok := c.asks()
	if !ok {
		return
	}
	list, err := st.UnansweredSince(ctx, now.Add(-store.AskWait))
	if err != nil {
		if ctx.Err() == nil {
			c.Log.Warn("coder: questions nobody answered", "err", err)
		}
		return
	}
	for _, q := range list {
		if _, err := st.PassAskedEncargo(ctx, q.EncargoID, q.OrgID, CoderName, PassNote(c.Lang, q, true)); err != nil && !errors.Is(err, store.ErrEncargoState) {
			c.Log.Warn("coder: passing an unanswered encargo", "encargo", q.EncargoID, "err", err)
		}
	}
}

// ---- seeing the app ----

var seeTools = []ia.Tool{
	{Name: "runs", Description: "The app's recent CI runs, newest first, with each step's status and duration (and how many tries); with step, only that step, with its median duration over the runs shown. Only this encargo's app.",
		InputSchema: schema(map[string]any{"branch": str("only runs of this branch"), "step": str("only this step, like platform:test"), "limit": num("how many runs, up to 30 (10 if omitted)")})},
	{Name: "step_log", Description: "The end of one step's log in one of the app's runs (200 lines if omitted, up to 1000). Only this encargo's app.",
		InputSchema: schema(map[string]any{"run": num("the run's number"), "step": str("the step, like web:test"), "lines": num("how many lines from the end")}, "run", "step")},
	{Name: "slow_requests", Description: "The app's slow requests: those the platform's log (the Registro) noted as slower than a second, with route, milliseconds, status and time, and the slow requests the traces kept for its services. Only this encargo's app.",
		InputSchema: schema(map[string]any{"days": num("how many days back, up to 30 (7 if omitted)")})},
	{Name: "app_status", Description: "The app's status: its latest releases (with their verification), its open problems and the lookout's (vigía) findings about it. Only this encargo's app.",
		InputSchema: schema(map[string]any{})},
}

// moreTools are the tools added to coderTools: ask when questions can be
// kept, and the app's read-only views when the platform is there.
func (j *job) moreTools() []ia.Tool {
	var out []ia.Tool
	if _, ok := j.c.asks(); ok {
		out = append(out, askTool)
	}
	for _, t := range seeTools {
		if j.sees(t.Name) {
			out = append(out, t)
		}
	}
	for _, t := range infraTools {
		if j.infraOffered(t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// sees says whether a read-only tool can be offered.
func (j *job) sees(name string) bool {
	switch name {
	case "runs":
		return true
	case "step_log":
		return j.c.Logs != nil
	}
	return j.c.Plataforma != nil
}

// moreTool runs one of moreTools; ok is false for any other name.
func (j *job) moreTool(ctx context.Context, call ia.ToolCall) (out string, isErr, ok bool) {
	if call.Name == "ask" {
		if _, can := j.c.asks(); can {
			out, isErr = j.ask(ctx, call.Input)
			return out, isErr, true
		}
		return "", false, false
	}
	if out, isErr, ok := j.infraTool(ctx, call); ok {
		return out, isErr, true
	}
	known := false
	for _, t := range seeTools {
		known = known || t.Name == call.Name
	}
	if !known || !j.sees(call.Name) {
		return "", false, false
	}
	var in struct {
		Branch, Step      string
		Limit, Run, Lines int
		Days              int
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return "the input is not valid JSON: " + err.Error(), true, true
	}
	switch call.Name {
	case "runs":
		out, isErr = j.runs(ctx, strings.TrimSpace(in.Branch), strings.TrimSpace(in.Step), in.Limit)
	case "step_log":
		out, isErr = j.stepLog(ctx, int64(in.Run), strings.TrimSpace(in.Step), in.Lines)
	case "slow_requests":
		out, isErr = j.slowRequests(ctx, in.Days)
	case "app_status":
		out, isErr = j.appStatus(ctx)
	}
	return redact(out), isErr, true
}

// redact leaves out the lines that look like a credential.
func redact(s string) string {
	if !ruta.LooksSecret(s) {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if ruta.LooksSecret(l) {
			lines[i] = "(a line that looks like a secret was left out)"
		}
	}
	return strings.Join(lines, "\n")
}

func spanOf(from, to *time.Time) time.Duration {
	if from == nil || to == nil {
		return 0
	}
	return to.Sub(*from).Round(time.Second)
}

// runs lists the app's recent runs with their steps.
func (j *job) runs(ctx context.Context, branch, step string, limit int) (string, bool) {
	c := j.c
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 30)
	list, err := c.Store.ListRuns(ctx, j.app.ID, 100)
	if err != nil {
		return "could not list the runs: " + err.Error(), true
	}
	var b strings.Builder
	var durs []time.Duration
	shown := 0
	for _, r := range list {
		if shown == limit {
			break
		}
		if branch != "" && r.Branch != branch {
			continue
		}
		full, err := c.Store.GetRun(ctx, r.ID)
		if err != nil || full.AppID != j.app.ID {
			continue
		}
		shown++
		fmt.Fprintf(&b, "#%d %s %s %s, %s", full.ID, full.Branch, full.SHA[:min(7, len(full.SHA))], full.Status, full.CreatedAt.UTC().Format("2006-01-02 15:04"))
		if d := spanOf(full.StartedAt, full.FinishedAt); d > 0 {
			fmt.Fprintf(&b, ", took %s", d)
		}
		if full.Message != "" {
			fmt.Fprintf(&b, " (%s)", truncate(full.Message, 200))
		}
		b.WriteString("\n")
		for _, s := range full.Steps {
			if step != "" && s.ID != step {
				continue
			}
			d := spanOf(s.StartedAt, s.FinishedAt)
			fmt.Fprintf(&b, "  %s %s", s.ID, s.Status)
			if d > 0 {
				fmt.Fprintf(&b, " %s", d)
				if step != "" && s.Status == store.RunSucceeded {
					durs = append(durs, d)
				}
			}
			if s.Tries > 1 {
				fmt.Fprintf(&b, " (%d tries)", s.Tries)
			}
			if s.Message != "" && s.Status == store.RunFailed {
				fmt.Fprintf(&b, ": %s", truncate(s.Message, 200))
			}
			b.WriteString("\n")
		}
	}
	if shown == 0 {
		return "no runs of " + j.app.Name + " match", false
	}
	if len(durs) > 0 {
		sort.Slice(durs, func(a, b int) bool { return durs[a] < durs[b] })
		fmt.Fprintf(&b, "Step %s passed in %d of these runs: median %s, fastest %s, slowest %s.\n", step, len(durs), durs[len(durs)/2], durs[0], durs[len(durs)-1])
	}
	return truncate(b.String(), 20000), false
}

// stepLog is the end of a step's log, of a run of the encargo's app only.
func (j *job) stepLog(ctx context.Context, runID int64, step string, n int) (string, bool) {
	c := j.c
	if runID <= 0 || step == "" {
		return "say the run's number and the step", true
	}
	run, err := c.Store.GetRun(ctx, runID)
	if err != nil || run.AppID != j.app.ID {
		return fmt.Sprintf("run #%d is not a run of %s", runID, j.app.Name), true
	}
	if n <= 0 {
		n = 200
	}
	n = min(n, 1000)
	text, err := c.Logs(ctx, runID, step)
	if err != nil {
		return fmt.Sprintf("run #%d has no step %q, or its log could not be read: %v", runID, step, err), true
	}
	lines := strings.Split(strings.TrimRight(ansi.ReplaceAllString(text, ""), "\n"), "\n")
	head := fmt.Sprintf("run #%d, step %s: the last %d of %d lines\n", runID, step, min(n, len(lines)), len(lines))
	out := strings.Join(lines[max(0, len(lines)-n):], "\n")
	if len(out) > 30000 {
		out = "…" + out[len(out)-30000:]
	}
	return head + out, false
}

// slowRequests are the app's slow requests in the Registro and the traces.
func (j *job) slowRequests(ctx context.Context, days int) (string, bool) {
	p := j.c.Plataforma
	if days <= 0 {
		days = 7
	}
	days = min(days, 30)
	since := time.Now().AddDate(0, 0, -days)
	list, err := p.SlowRequests(ctx, j.app.Name, j.c.PlatformApp != "" && j.c.PlatformApp == j.app.Name, since, 100)
	if err != nil {
		return "could not read the Registro: " + err.Error(), true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Slow requests of %s in the Registro, the last %d days (%d):\n", j.app.Name, days, len(list))
	byRoute := map[string][]int64{}
	for _, q := range list {
		fmt.Fprintf(&b, "%s %s %d ms, status %d\n", q.At.UTC().Format("2006-01-02 15:04:05"), q.Route, q.Ms, q.Status)
		byRoute[q.Route] = append(byRoute[q.Route], q.Ms)
	}
	if len(byRoute) > 1 {
		routes := make([]string, 0, len(byRoute))
		for r := range byRoute {
			routes = append(routes, r)
		}
		sort.Slice(routes, func(a, b int) bool { return len(byRoute[routes[a]]) > len(byRoute[routes[b]]) })
		b.WriteString("By route:\n")
		for _, r := range routes {
			ms := byRoute[r]
			sort.Slice(ms, func(a, b int) bool { return ms[a] < ms[b] })
			fmt.Fprintf(&b, "  %s: %d times, median %d ms\n", r, len(ms), ms[len(ms)/2])
		}
	}
	var traces []store.Trace
	for _, svc := range j.app.Spec.Services {
		ts, err := p.ListTraces(ctx, store.TraceQuery{Service: j.app.Name + "/" + svc.Name, Limit: 100})
		if err != nil {
			continue
		}
		for _, t := range ts {
			if t.Reason == "slow" && t.StartedAt.After(since) && traceOf(j.app.Name, t) {
				traces = append(traces, t)
			}
		}
	}
	if len(traces) > 0 {
		fmt.Fprintf(&b, "Slow requests the traces kept (%d):\n", len(traces))
		for i, t := range traces {
			if i == 40 {
				break
			}
			fmt.Fprintf(&b, "%s %s %s %d ms (trace %s)\n", t.StartedAt.UTC().Format("2006-01-02 15:04:05"), t.Service, t.Name, t.DurationUS/1000, t.ID)
		}
	}
	return truncate(b.String(), 20000), false
}

// traceOf says whether a trace went through the app's services only as
// far as its first service: a request kept for another app is not shown.
func traceOf(app string, t store.Trace) bool {
	first, _, _ := strings.Cut(t.Service, "/")
	return first == app
}

// appStatus is the app's releases, open problems and lookout findings.
func (j *job) appStatus(ctx context.Context) (string, bool) {
	p := j.c.Plataforma
	var b strings.Builder
	fmt.Fprintf(&b, "App %s (repository %s, default branch %s)\n", j.app.Name, j.app.Repo, j.app.DefaultBranch)
	if rels, err := p.ListReleases(ctx, j.app.ID, 5); err == nil {
		b.WriteString("\nLatest releases:\n")
		for _, r := range rels {
			if r.AppID != j.app.ID {
				continue
			}
			fmt.Fprintf(&b, "#%d %s, %s", r.Number, r.SHA[:min(7, len(r.SHA))], r.CreatedAt.UTC().Format("2006-01-02 15:04"))
			if r.RollbackOf != nil {
				b.WriteString(", a rollback")
			}
			if r.VerifyStatus != "" {
				fmt.Fprintf(&b, ", verification %s", r.VerifyStatus)
				if r.VerifyMessage != "" {
					fmt.Fprintf(&b, ": %s", truncate(r.VerifyMessage, 200))
				}
			}
			b.WriteString("\n")
		}
	}
	if probs, err := p.ListProblems(ctx, j.app.Name, time.Now().AddDate(0, 0, -7), false, 20); err == nil {
		b.WriteString("\nOpen problems (the last 7 days):\n")
		n := 0
		for _, pr := range probs {
			if pr.ResolvedAt != nil || pr.App != j.app.Name {
				continue
			}
			n++
			fmt.Fprintf(&b, "- %s %s: %s", pr.Level, pr.Component, truncate(pr.Message, 300))
			if pr.Detail != "" {
				fmt.Fprintf(&b, " (%s)", truncate(pr.Detail, 300))
			}
			fmt.Fprintf(&b, ", %d times, last %s\n", pr.Count, pr.LastAt.UTC().Format("2006-01-02 15:04"))
		}
		if n == 0 {
			b.WriteString("none\n")
		}
	}
	if hs, err := p.HallazgosDeApp(ctx, j.app.Name, 10); err == nil && len(hs) > 0 {
		b.WriteString("\nThe lookout's findings about it:\n")
		for _, h := range hs {
			if h.App != j.app.Name {
				continue
			}
			fmt.Fprintf(&b, "- #%d (%s) %s: %s\n", h.ID, h.Estado, h.Titulo, truncate(h.Evidencia, 400))
		}
	}
	return truncate(b.String(), 20000), false
}

// seeRule tells the model of its read-only tools, and that a lookout's
// encargo starts by checking the numbers it is about.
func (j *job) seeRule() string {
	var names []string
	for _, t := range j.moreTools() {
		if t.Name != "ask" && j.modo(toolPerm[t.Name]) != store.ModoOff {
			names = append(names, t.Name)
		}
	}
	var b strings.Builder
	if len(names) > 0 {
		fmt.Fprintf(&b, "\n- You can see the app (only this encargo's app): %s. When the encargo names the platform's MCP tools or pages, they are yours: ejecucion_leer is runs, paso_registro is step_log, app_estado is app_status, the Registro (Log) page is slow_requests.", strings.Join(names, ", "))
	}
	if _, ok := j.c.asks(); ok {
		b.WriteString("\n- When you need the person's word (a decision, a design to choose first, something the encargo leaves open), call ask with the question and 2 to 4 options: you stop without spending and go on with their answer. Do not ask what your tools can tell you, and do not give_up only because something needs a decision.")
	}
	if vigiaBorn(j.e) && len(names) > 0 {
		b.WriteString("\n- This encargo comes from the lookout (vigía), which read numbers from the platform. Verify them first with your tools (runs, step_log, slow_requests, app_status) before changing anything; if they do not hold, deliver nothing changed: give_up saying what you found, with the numbers.")
	}
	return b.String()
}

var vigiaMark = regexp.MustCompile(`(?:The lookout saw it|Lo vio el vigía) \((?:finding|hallazgo) #\d+`)

// vigiaBorn says whether an encargo was made from a lookout's finding.
func vigiaBorn(e store.Encargo) bool { return vigiaMark.MatchString(e.Body) }

// ---- what passes by itself ----

// waitEnd waits for a run to end, marking the encargo as waiting on it: as
// long as the run takes (it spends nothing), up to runWaitMax.
func (j *job) waitEnd(ctx context.Context, run *store.Run) (*store.Run, error) {
	c := j.c
	_, _ = c.Store.WaitEncargo(ctx, j.e.ID, j.key.ID, run.ID, CoderName)
	start := time.Now()
	defer func() { j.waited += time.Since(start) }()
	long := false
	for {
		if run.Status != store.RunQueued && run.Status != store.RunRunning {
			if full, err := c.Store.GetRun(ctx, run.ID); err == nil && len(run.Steps) == 0 {
				run = full // a listed run has no steps; how it failed is in them
			}
			_, _ = c.Store.ResumeEncargo(ctx, j.e.ID, j.key.ID, CoderName, fmt.Sprintf("#%d: %s", run.ID, run.Status))
			return run, nil
		}
		if !long && time.Since(start) > runWait {
			long = true // longer than runs take: it keeps waiting, without spending
			c.Log.Info("coder: a run takes long; still waiting", "encargo", j.e.ID, "run", run.ID)
		}
		if time.Since(start) > runWaitMax {
			_, _ = c.Store.ResumeEncargo(ctx, j.e.ID, j.key.ID, CoderName, "")
			return nil, fmt.Errorf("run #%d did not end in %s", run.ID, runWaitMax)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.Poll):
		}
		r, err := c.Store.GetRun(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		run = r
	}
}

// clusterSigns are what a run says when the cluster failed it, not the
// code: the API server or etcd timed out, a pod was evicted or could not
// be scheduled, a node ran short, an image could not be pulled in time.
var clusterSigns = regexp.MustCompile(`(?i)etcdserver: (request timed out|leader changed|too many requests)|` +
	`\bevicted\b|the node was low on resource|node\(s\) had .{0,40}(disk|memory|pid)[- ]pressure|\b(disk|memory|pid)pressure\b|` +
	`imagepullbackoff|errimagepull|(pull(ing)?|pulled) image.{0,200}(timeout|timed out|deadline exceeded|i/o timeout|connection reset)|` +
	`the server was unable to return a response in the time allotted|tls handshake timeout|` +
	`insufficient (cpu|memory)|pod .{0,80}(was deleted|disappeared)|database is locked`)

// clusterSign is what in a failed run says the cluster failed it ("" when
// nothing does): its message, its failed steps' messages and logs' ends.
func (j *job) clusterSign(ctx context.Context, run *store.Run) string {
	if m := clusterSigns.FindString(run.Message); m != "" {
		return m
	}
	for _, s := range run.Steps {
		if s.Status != store.RunFailed {
			continue
		}
		if m := clusterSigns.FindString(s.Message); m != "" {
			return m
		}
		if j.c.Logs == nil {
			continue
		}
		if text, err := j.c.Logs(ctx, run.ID, s.ID); err == nil {
			lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
			if m := clusterSigns.FindString(strings.Join(lines[max(0, len(lines)-40):], "\n")); m != "" {
				return m
			}
		}
	}
	return ""
}

// rerun runs a run the cluster failed once more: with the platform's retry
// (what failed runs again, in the same run), or without one, an empty
// commit.
func (j *job) rerun(ctx context.Context, sha string, run *store.Run) (string, *store.Run, error) {
	c := j.c
	if c.RetryRun != nil {
		r, err := c.RetryRun(ctx, run.ID)
		if err != nil {
			return sha, nil, err
		}
		r, err = j.waitEnd(ctx, r)
		return sha, r, err
	}
	nsha, err := j.commit(ctx, j.say("Encargo %d: correr otra vez, el clúster falló la ejecución #%d", "Encargo %d: run again, the cluster failed run #%d", j.e.ID, run.ID))
	if err != nil {
		return sha, nil, err
	}
	r, err := j.waitRun(ctx, nsha)
	return nsha, r, err
}

// retryRepos is a forja that tries a call once more when it failed for a
// reason that passes (a dropped connection, a 5xx, a 429).
type retryRepos struct{ r Repos }

var forjaBusy = regexp.MustCompile(`(github|gitlab|bitbucket) [A-Z]+ \S+: (429|5\d\d)\b`)

// forjaPassing says whether a forja's error passes by itself.
func forjaPassing(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	s := err.Error()
	return forjaBusy.MatchString(s) || strings.Contains(s, "connection reset") || strings.Contains(s, "TLS handshake timeout")
}

func once[T any](ctx context.Context, f func() (T, error)) (T, error) {
	v, err := f()
	if forjaPassing(err) && ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return v, err
		case <-time.After(forjaPause):
		}
		v, err = f()
	}
	return v, err
}

func (r retryRepos) Files(ctx context.Context, installation int64, repo, ref string, maxFile int) (map[string][]byte, error) {
	return once(ctx, func() (map[string][]byte, error) { return r.r.Files(ctx, installation, repo, ref, maxFile) })
}

func (r retryRepos) BranchSHA(ctx context.Context, installation int64, repo, branch string) (string, error) {
	return once(ctx, func() (string, error) { return r.r.BranchSHA(ctx, installation, repo, branch) })
}

func (r retryRepos) CreateBranch(ctx context.Context, installation int64, repo, branch, sha string) error {
	_, err := once(ctx, func() (struct{}, error) { return struct{}{}, r.r.CreateBranch(ctx, installation, repo, branch, sha) })
	return err
}

func (r retryRepos) CommitFiles(ctx context.Context, installation int64, repo, branch, message string, files map[string][]byte, remove []string) (string, error) {
	return once(ctx, func() (string, error) {
		return r.r.CommitFiles(ctx, installation, repo, branch, message, files, remove)
	})
}

func (r retryRepos) MergeBase(ctx context.Context, installation int64, repo, base, head string) (string, time.Time, error) {
	var at time.Time
	sha, err := once(ctx, func() (string, error) {
		s, t, err := r.r.MergeBase(ctx, installation, repo, base, head)
		at = t
		return s, err
	})
	return sha, at, err
}

func (r retryRepos) ChangedFiles(ctx context.Context, installation int64, repo, base, head string) ([]string, bool, error) {
	var all bool
	files, err := once(ctx, func() ([]string, error) {
		f, ok, err := r.r.ChangedFiles(ctx, installation, repo, base, head)
		all = ok
		return f, err
	})
	return files, all, err
}

func (r retryRepos) FileAt(ctx context.Context, installation int64, repo, p, ref string) ([]byte, error) {
	return once(ctx, func() ([]byte, error) { return r.r.FileAt(ctx, installation, repo, p, ref) })
}

func (r retryRepos) CommitMerge(ctx context.Context, installation int64, repo, branch, head, other, message string, files map[string][]byte, remove []string) (string, error) {
	return once(ctx, func() (string, error) {
		return r.r.CommitMerge(ctx, installation, repo, branch, head, other, message, files, remove)
	})
}
