package platform

import (
	"context"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ---- Poner al día (decision 191) ----
//
// When a run of a dependency update's pull request (Renovate's,
// Dependabot's) ends, its report is asked for: the agents (agentes.AlDia)
// write it, with what would fail, what would change and how much. The
// organization chooses on the AI page whether it is on, and whether every
// update gets one or only those whose run failed.

// alDiaNotas is how much of the pull request's text (its release notes)
// is kept for the report.
const alDiaNotas = 20_000

// pedirAlDia asks for the report on a finished run, when its branch's open
// pull request is a dependency update and the organization wants one.
func (p *Platform) pedirAlDia(ctx context.Context, app *store.App, run *store.Run, failed bool) {
	if p.Store == nil || run.Deploy || run.Branch == app.DefaultBranch {
		return
	}
	pr, ok := p.openPull(ctx, app, run.Branch)
	if !ok || !depsPull(pr, app) {
		return
	}
	set, err := p.Store.AlDiaAjustesDe(ctx, app.OrgID)
	if err != nil || !set.Encendido || (set.Cuando == store.AlDiaCuandoFalla && !failed) {
		return
	}
	notas := pr.Body
	if r := []rune(notas); len(r) > alDiaNotas {
		notas = string(r[:alDiaNotas]) + "…"
	}
	asked, err := p.Store.PedirAlDia(ctx, store.AlDia{OrgID: app.OrgID, AppID: app.ID, Pull: pr.Number, Titulo: pr.Title, Notas: notas,
		Branch: run.Branch, SHA: run.SHA, RunID: run.ID, RunFailed: failed})
	if err != nil {
		p.Log.Warn("al día: asking for the report", "app", app.Name, "run", run.ID, "err", err)
	} else if asked {
		p.Log.Info("al día: report asked for", "app", app.Name, "run", run.ID, "pull", pr.Number)
	}
}
