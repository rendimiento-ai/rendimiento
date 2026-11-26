package agentes

// What each agent may do (Ruta 184). Every tool of the coding agent needs
// a permission: code_read (its files and branch) always; platform_read
// (runs, step_log, slow_requests, app_status); infra_read (infra_read);
// sandbox (run); copies_read (copies); effects (restart_service,
// rerun_branch, remake_copy). Off, the tool is not offered; always, it
// runs; ask, the call says why, the encargo asks a person (the ask
// machinery: it stops without spending) and, given, the next call runs.
// Effects never go always: each one is a person's yes to that very action.
// Every use goes to the encargo's history and to the bitácora.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/destino"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// PermisoStore is where agents' permissions are kept (store.Store): when an
// agent's store is one, its permissions count; without one, the defaults
// as they were before permissions existed.
type PermisoStore interface {
	PermisoDecide(ctx context.Context, orgID int64, sujeto, permiso, app, accion string, encargoID int64) (store.Decision, error)
	UsarPermisoDado(ctx context.Context, id int64) (bool, error)
	PedirPermiso(ctx context.Context, p store.PermisoPedido) (store.PermisoPedido, error)
	NoteEncargoPermiso(ctx context.Context, id int64, who, what, permiso, note string) error
}

// Bitacorista keeps actions in the bitácora (store.Store).
type Bitacorista interface {
	AnotarBitacora(ctx context.Context, b store.Bitacora) (store.Bitacora, error)
}

// Permite is the hook every agent of the platform checks before it reads
// or does what a permission covers: with st not a PermisoStore, the
// permission's default for the subject. accion narrows it (the
// diagnostician's infra_read on its run's steps is store.AccionPasos).
func Permite(ctx context.Context, st any, orgID int64, sujeto, permiso, app, accion string) bool {
	ps, ok := st.(PermisoStore)
	if !ok {
		m := store.PermisoDefault(sujeto, permiso)
		if a := store.AlcanceDe(sujeto, permiso); a != "" && a != accion && m == store.ModoAlways {
			return false
		}
		return m == store.ModoAlways
	}
	d, err := ps.PermisoDecide(ctx, orgID, sujeto, permiso, app, accion, 0)
	return err == nil && d.Puede()
}

// CoderInfra is what the coding agent's infra, copies and effects tools
// use (platform.AgentInfra): only ever about the encargo's own app.
type CoderInfra interface {
	MirarApp(ctx context.Context, app *store.App, q destino.Mirar) (destino.Mirada, error)
	ReiniciarServicio(ctx context.Context, app *store.App, servicio string) error
	LanzarRama(ctx context.Context, app *store.App, branch string) (*store.Run, error)
	RehacerCopia(ctx context.Context, app *store.App, number int, who string) (string, error)
	CopiasTexto(ctx context.Context, app *store.App) (string, error)
}

// toolPerm is the permission each tool beyond code_read needs.
var toolPerm = map[string]string{
	"runs": store.PermPlatformRead, "step_log": store.PermPlatformRead, "slow_requests": store.PermPlatformRead, "app_status": store.PermPlatformRead,
	"run": store.PermSandbox, "infra_read": store.PermInfraRead, "copies": store.PermCopiesRead,
	"restart_service": store.PermEffects, "rerun_branch": store.PermEffects, "remake_copy": store.PermEffects,
}

// permName is a permission in the model's words.
var permName = map[string]string{
	store.PermPlatformRead: "see the platform (runs, step logs, slow requests, the app's status)",
	store.PermInfraRead:    "see the app's infrastructure",
	store.PermSandbox:      "run commands in the sandbox",
	store.PermCopiesRead:   "see the app's preview copies",
	store.PermEffects:      "an action with effect",
}

var whyProp = map[string]any{"type": "string", "description": "when the tool needs a person's permission: why you need it, for the person, short, in their language; with it the person is asked and you stop until they answer"}

var infraTools = []ia.Tool{
	{Name: "infra_read", Description: "See your app's infrastructure, read-only. In the cluster: its pods (status, conditions, each container's state and reason, restarts), its recent events, the end of its containers' logs (the previous container's too, after a crash), the rollout of its deployments and statefulsets, and its releases' verification. On a computer: its containers (state, exit code, restarts) and their logs. Only this encargo's app, never Secrets or their values; lines that look like a credential are left out.",
		InputSchema: schema(map[string]any{
			"what":     map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"pods", "events", "logs", "rollout", "verification"}}, "description": "what to see; empty for pods, events, rollout and verification"},
			"service":  str("only this service of the app"),
			"lines":    num("for logs: how many lines from the end (100 if omitted, up to 500)"),
			"previous": map[string]any{"type": "boolean", "description": "for logs: the previous container's, the one that ended"},
			"minutes":  num("how far back, in minutes (60 if omitted, up to 1440)"),
		})},
	{Name: "copies", Description: "Your app's preview copies: each pull request's copy, its branch, health, address, its own tests and the checks en seco, when it goes, and the Secrets it lacks (names only). Only this encargo's app.",
		InputSchema: schema(map[string]any{})},
	{Name: "restart_service", Description: "Restart one service of your app (as kubectl rollout restart, or restarting its container on a computer). An action with effect: a person allows each one.",
		InputSchema: schema(map[string]any{"service": str("the service's name")}, "service")},
	{Name: "rerun_branch", Description: "Queue a CI run of a branch of your app again (never the default branch, which deploys). An action with effect: a person allows each one.",
		InputSchema: schema(map[string]any{"branch": str("the branch")}, "branch")},
	{Name: "remake_copy", Description: "Make a pull request's preview copy again from its branch's last run that passed. An action with effect: a person allows each one.",
		InputSchema: schema(map[string]any{"pull": num("the pull request's number")}, "pull")},
}

// permisos is the coder's PermisoStore, when its store is one.
func (c *Coder) permisos() (PermisoStore, bool) {
	p, ok := c.Store.(PermisoStore)
	return p, ok
}

// legacyModo is a permission's mode without a PermisoStore: what the
// coding agent could do before permissions (no infra, no effects).
func legacyModo(perm string) string {
	switch perm {
	case store.PermPlatformRead, store.PermSandbox, store.PermCopiesRead:
		return store.ModoAlways
	}
	return store.ModoOff
}

// modo is a permission's mode for the job now, read once a turn.
func (j *job) modo(perm string) string {
	if perm == "" || perm == store.PermCodeRead {
		return store.ModoAlways
	}
	if m, ok := j.modos[perm]; ok {
		return m
	}
	m := legacyModo(perm)
	if ps, ok := j.c.permisos(); ok && j.app != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		d, err := ps.PermisoDecide(ctx, j.e.OrgID, store.SujetoCoder, perm, j.app.Name, "", j.e.ID)
		cancel()
		m = store.ModoOff
		if err == nil {
			m = d.Modo
			if d.Modo == store.ModoAsk && d.Dado != nil && perm != store.PermEffects {
				m = store.ModoAlways // given for the encargo or always: no need to say why
			}
		}
	}
	if j.modos == nil {
		j.modos = map[string]string{}
	}
	j.modos[perm] = m
	return m
}

// gateTools leaves out the tools whose permission is off, and tells the
// model which ask (with why in their input).
func (j *job) gateTools(tools []ia.Tool) []ia.Tool {
	out := make([]ia.Tool, 0, len(tools))
	for _, t := range tools {
		perm, gated := toolPerm[t.Name]
		if !gated {
			out = append(out, t)
			continue
		}
		switch j.modo(perm) {
		case store.ModoOff:
			continue
		case store.ModoAsk:
			t = askVersion(t, perm)
		}
		out = append(out, t)
	}
	return out
}

// askVersion is a tool that asks: its description says so, and it takes why.
func askVersion(t ia.Tool, perm string) ia.Tool {
	t.Description += " NEEDS A PERSON'S PERMISSION (" + permName[perm] + "): call it with why; unless they gave it already, they are asked and you stop without spending until they answer. Ask only when it matters for the encargo."
	s := map[string]any{}
	for k, v := range t.InputSchema {
		s[k] = v
	}
	props := map[string]any{}
	if p, ok := s["properties"].(map[string]any); ok {
		for k, v := range p {
			props[k] = v
		}
	}
	props["why"] = whyProp
	s["properties"] = props
	t.InputSchema = s
	return t
}

// permRule tells the model how permissions work, when some tool asks or
// some is off.
func (j *job) permRule() string {
	var off, ask []string
	for _, perm := range []string{store.PermPlatformRead, store.PermInfraRead, store.PermSandbox, store.PermCopiesRead, store.PermEffects} {
		switch j.modo(perm) {
		case store.ModoOff:
			off = append(off, permName[perm])
		case store.ModoAsk:
			ask = append(ask, permName[perm])
		}
	}
	var b strings.Builder
	if len(ask) > 0 {
		fmt.Fprintf(&b, "\n- Some tools need a person's permission (%s): call the tool with why (your reason, for the person), and you stop without spending until they answer. Given, call it again the same way and it runs. If they say no, go on without it and say in your delivery what it would have needed.", strings.Join(ask, "; "))
	}
	if len(off) > 0 {
		fmt.Fprintf(&b, "\n- These are turned off for you (on the AI page, Permissions): %s. If the encargo needed one, say so in your delivery.", strings.Join(off, "; "))
	}
	return b.String()
}

// accionOf is the exact action an effects call is: what a person allows.
func accionOf(call ia.ToolCall) string {
	var in struct {
		Service, Branch string
		Pull            int
	}
	_ = json.Unmarshal(call.Input, &in)
	switch call.Name {
	case "restart_service":
		return "restart:" + strings.TrimSpace(in.Service)
	case "rerun_branch":
		return "rerun:" + strings.TrimSpace(in.Branch)
	case "remake_copy":
		return "remake:" + strconv.Itoa(in.Pull)
	}
	return ""
}

// queOf is what a call reads or does, in the person's language: a summary
// for the history and the request, never values.
func (j *job) queOf(call ia.ToolCall) (que, ventana string) {
	var in struct {
		What             []string
		Branch, Step     string
		Service, Command string
		Run, Lines, Days int
		Minutes, Pull    int
		Previous         bool
	}
	_ = json.Unmarshal(call.Input, &in)
	app := j.app.Name
	switch call.Name {
	case "runs":
		que = j.say("vio las ejecuciones de %s", "read the runs of %s", app)
		if in.Branch != "" {
			que += j.say(" (rama %s)", " (branch %s)", in.Branch)
		}
	case "step_log":
		que = j.say("leyó el registro del paso %s de la ejecución %d", "read the log of step %s of run #%d", in.Step, in.Run)
	case "slow_requests":
		que = j.say("vio las solicitudes lentas de %s", "read the slow requests of %s", app)
	case "app_status":
		que = j.say("vio el estado de %s", "read the status of %s", app)
	case "run":
		que = j.say("corrió %s", "ran %s", truncate(in.Command, 120))
	case "infra_read":
		what := strings.Join(in.What, ", ")
		if what == "" {
			what = j.say("los pods, eventos, rollout y verificación", "the pods, events, rollout and verification")
		}
		que = j.say("ver la infraestructura de %s: %s", "see the infrastructure of %s: %s", app, what)
		if in.Service != "" {
			que += j.say(", servicio %s", ", service %s", in.Service)
		}
		if in.Previous {
			que += j.say(", del contenedor anterior", ", the previous container's")
		}
		m := in.Minutes
		if m <= 0 {
			m = 60
		}
		ventana = j.say("los últimos %d min", "the last %d min", min(m, 1440))
	case "copies":
		que = j.say("ver las copias de vista previa de %s", "see the preview copies of %s", app)
	case "restart_service":
		que = j.say("reiniciar el servicio %s de %s", "restart service %s of %s", in.Service, app)
	case "rerun_branch":
		que = j.say("relanzar la ejecución de la rama %s", "run branch %s again", in.Branch)
	case "remake_copy":
		que = j.say("rehacer la copia de la solicitud #%d", "make the copy of pull request #%d again", in.Pull)
	default:
		que = call.Name
	}
	return que, ventana
}

// gate is whether a call may run now, by its permission: ok, or what the
// model is told instead (and whether that is an error).
func (j *job) gate(ctx context.Context, call ia.ToolCall, perm string) (string, bool, bool) {
	c := j.c
	ps, ok := c.permisos()
	if !ok {
		if legacyModo(perm) == store.ModoOff {
			return "this tool is off here", true, false
		}
		return "", false, true
	}
	accion := ""
	if perm == store.PermEffects {
		accion = accionOf(call)
	}
	d, err := ps.PermisoDecide(ctx, j.e.OrgID, store.SujetoCoder, perm, j.app.Name, accion, j.e.ID)
	if err != nil {
		return "could not read your permissions: " + err.Error(), true, false
	}
	switch {
	case d.Modo == store.ModoOff:
		return fmt.Sprintf("%s is turned off for you (%s, on the AI page, Permissions): go on without it, and say in your delivery what it would have needed.", call.Name, permName[perm]), true, false
	case d.Modo == store.ModoAlways:
		return "", false, true
	case d.Dado != nil:
		used := true
		if d.Dado.Alcance == store.AlcanceOnce {
			if used, err = ps.UsarPermisoDado(ctx, d.Dado.ID); err != nil {
				return "could not use the permission: " + err.Error(), true, false
			}
		}
		if used {
			return "", false, true
		}
	}
	var in struct{ Why string }
	_ = json.Unmarshal(call.Input, &in)
	why := strings.TrimSpace(in.Why)
	if why == "" {
		return fmt.Sprintf("%s needs a person's permission (%s). To ask, call %s again with why: your reason, short, for the person, in their language. You then stop without spending until they answer.", call.Name, permName[perm], call.Name), true, false
	}
	out, isErr := j.askPermission(ctx, ps, call, perm, accion, truncate(why, 2000))
	return out, isErr, false
}

// askPermission stops the work to ask a person for a permission: what
// changed is committed, as for ask, and the encargo asks with the request.
func (j *job) askPermission(ctx context.Context, ps PermisoStore, call ia.ToolCall, perm, accion, why string) (string, bool) {
	c := j.c
	st, ok := c.asks()
	if !ok {
		return "asking a person is not possible here: go on without it, and say so in your delivery", true
	}
	sha, msg, ok := j.commitBeforeAsking(ctx)
	if !ok {
		return msg, true
	}
	que, ventana := j.queOf(call)
	enc := j.e.ID
	p, err := ps.PedirPermiso(context.WithoutCancel(ctx), store.PermisoPedido{OrgID: j.e.OrgID, Sujeto: store.SujetoCoder, Permiso: perm, App: j.app.Name,
		Tool: call.Name, Accion: accion, Que: que, PorQue: why, Ventana: ventana, EncargoID: &enc, AskedBy: CoderName})
	if err != nil {
		return "could not ask for the permission: " + err.Error(), true
	}
	notes := fmt.Sprintf("You asked for permission to %s, to call %s with %s. If it was given, call it again the same way; if not, go on without it and say so in your delivery.", permName[perm], call.Name, truncate(string(call.Input), 1500))
	if sha != "" {
		notes += fmt.Sprintf("\n\n(Your changes were committed as %s before asking.)", sha[:min(7, len(sha))])
	}
	q := store.EncargoPregunta{Question: truncate(j.say("Permiso: %s", "Permission: %s", que), 2000), Options: []string{}, Notes: notes, SpentMicros: j.spent, PedidoID: &p.ID}
	if _, err := st.AskEncargo(context.WithoutCancel(ctx), j.e.ID, j.key.ID, CoderName, q); err != nil {
		return "could not ask: " + err.Error(), true
	}
	_ = ps.NoteEncargoPermiso(context.WithoutCancel(ctx), j.e.ID, CoderName, store.LogPermisoPedido, perm, que)
	j.bitacora(ctx, "permiso_pedir", perm, call.Name, que)
	j.finished, j.outcome = true, j.say("pidió un permiso y espera la respuesta", "asked for a permission and waits for the answer")
	return "asked for the permission; you stop here until the person answers", false
}

// noteUse keeps a use of a permission in the encargo's history (the
// sandbox's commands are there already, as 'ran') and in the bitácora.
func (j *job) noteUse(ctx context.Context, call ia.ToolCall, perm string, isErr bool) {
	ps, ok := j.c.permisos()
	if !ok {
		return
	}
	que, ventana := j.queOf(call)
	if ventana != "" {
		que += " · " + ventana
	}
	if isErr {
		que += j.say(" · no se pudo", " · it failed")
	}
	if perm != store.PermSandbox {
		_ = ps.NoteEncargoPermiso(context.WithoutCancel(ctx), j.e.ID, CoderName, store.LogPermisoUso, perm, truncate(que, 500))
	}
	j.bitacora(ctx, "permiso_usar", perm, call.Name, que)
}

// bitacora keeps a use or a request in the bitácora, as the coding agent.
func (j *job) bitacora(ctx context.Context, accion, perm, tool, que string) {
	b, ok := j.c.Store.(Bitacorista)
	if !ok {
		return
	}
	_, _ = b.AnotarBitacora(context.WithoutCancel(ctx), store.Bitacora{OrgID: j.e.OrgID, QuienTipo: store.QuienAgente, Quien: CoderName, Por: "agente",
		Accion: accion, Sobre: map[string]string{"app": j.app.Name, "encargo": strconv.FormatInt(j.e.ID, 10)},
		Resultado: "ok", Detalle: map[string]any{"permiso": perm, "herramienta": tool, "que": truncate(que, 500)}})
}

// ---- the tools ----

// infraOffered says whether a tool of infraTools can be offered at all.
func (j *job) infraOffered(name string) bool {
	if j.c.Infra == nil {
		return false
	}
	if name == "remake_copy" {
		return j.app == nil || j.app.Spec.Previews != nil
	}
	return true
}

// infraTool runs one of infraTools; ok is false for any other name.
func (j *job) infraTool(ctx context.Context, call ia.ToolCall) (string, bool, bool) {
	known := false
	for _, t := range infraTools {
		known = known || t.Name == call.Name
	}
	if !known || !j.infraOffered(call.Name) {
		return "", false, false
	}
	var in struct {
		What            []string
		Service, Branch string
		Lines, Minutes  int
		Previous        bool
		Pull            int
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return "the input is not valid JSON: " + err.Error(), true, true
	}
	inf := j.c.Infra
	switch call.Name {
	case "infra_read":
		out, isErr := j.infraRead(ctx, in.What, strings.TrimSpace(in.Service), in.Lines, in.Previous, in.Minutes)
		return redact(out), isErr, true
	case "copies":
		out, err := inf.CopiasTexto(ctx, j.app)
		if err != nil {
			return "could not read the copies: " + err.Error(), true, true
		}
		return redact(truncate(out, 20000)), false, true
	case "restart_service":
		if err := inf.ReiniciarServicio(ctx, j.app, strings.TrimSpace(in.Service)); err != nil {
			return "could not restart it: " + err.Error(), true, true
		}
		return fmt.Sprintf("restarted service %s of %s; look at it with infra_read in a minute", in.Service, j.app.Name), false, true
	case "rerun_branch":
		run, err := inf.LanzarRama(ctx, j.app, in.Branch)
		if err != nil {
			return "could not run it: " + err.Error(), true, true
		}
		return fmt.Sprintf("queued run #%d of branch %s", run.ID, in.Branch), false, true
	case "remake_copy":
		out, err := inf.RehacerCopia(ctx, j.app, in.Pull, CoderName)
		if err != nil {
			return "could not make it again: " + err.Error(), true, true
		}
		return out, false, true
	}
	return "", false, false
}

// infraRead is what infra_read answers: the look where the app runs, and
// its latest releases' verification.
func (j *job) infraRead(ctx context.Context, what []string, service string, lines int, previous bool, minutes int) (string, bool) {
	q := destino.Mirar{Servicio: service, Lineas: lines, Anterior: previous, Ventana: time.Duration(minutes) * time.Minute}
	verif := len(what) == 0
	for _, w := range what {
		if w == "verification" {
			verif = true
		} else {
			q.Que = append(q.Que, w)
		}
	}
	if len(what) > 0 && len(q.Que) == 0 {
		q.Que = []string{"none"} // only the verification
	}
	if service != "" {
		found := false
		for _, s := range j.app.Spec.Services {
			found = found || s.Name == service
		}
		if !found {
			return fmt.Sprintf("%s has no service %s", j.app.Name, service), true
		}
	}
	var b strings.Builder
	if len(q.Que) > 0 && q.Que[0] != "none" || len(what) == 0 {
		m, err := j.c.Infra.MirarApp(ctx, j.app, q)
		if err != nil {
			fmt.Fprintf(&b, "the infrastructure could not be read: %v\n", err)
		} else {
			b.WriteString(m.Texto())
			b.WriteString("\n")
		}
	}
	if verif && j.c.Plataforma != nil {
		if rels, err := j.c.Plataforma.ListReleases(ctx, j.app.ID, 5); err == nil {
			b.WriteString("\nThe latest releases' verification:\n")
			for _, r := range rels {
				if r.AppID != j.app.ID {
					continue
				}
				fmt.Fprintf(&b, "#%d %s, %s: %s", r.Number, r.SHA[:min(7, len(r.SHA))], r.CreatedAt.UTC().Format("2006-01-02 15:04"), orElse(r.VerifyStatus, "not verified"))
				if r.VerifyMessage != "" {
					fmt.Fprintf(&b, " (%s)", truncate(r.VerifyMessage, 300))
				}
				b.WriteString("\n")
			}
		}
	}
	return truncate(b.String(), 32000), false
}
