package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
	"github.com/rendimiento-ai/rendimiento/internal/notify"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// AprobacionVence is how long a request waits for a person.
const AprobacionVence = 24 * time.Hour

// PedirReversion records an agent's request to roll app back to release
// version, for a person to decide, and emails the administrators. The agent
// can never do it itself: only DecidirAprobacion does, in a person's name.
func (p *Platform) PedirReversion(ctx context.Context, app *store.App, version int64, motivo string, key store.AgentKey) (store.Aprobacion, error) {
	motivo = strings.TrimSpace(motivo)
	if motivo == "" {
		return store.Aprobacion{}, errors.New("say why: a person reads it to decide")
	}
	rel, err := p.Store.GetRelease(ctx, app.ID, version)
	if errors.Is(err, store.ErrNotFound) {
		return store.Aprobacion{}, fmt.Errorf("%s has no release #%d", app.Name, version)
	}
	if err != nil {
		return store.Aprobacion{}, err
	}
	if rel.VerifyStatus == store.VerifyBlocked {
		return store.Aprobacion{}, fmt.Errorf("release #%d was never deployed (a pre-deploy task failed), so it cannot be rolled back to", version)
	}
	if cur, err := p.Store.LatestRelease(ctx, app.ID); err == nil && cur.Number == version {
		return store.Aprobacion{}, fmt.Errorf("release #%d is the one running already", version)
	}
	a, err := p.Store.CrearAprobacion(ctx, store.Aprobacion{OrgID: app.OrgID, App: app.Name, Accion: store.AccionRevertir,
		Version: version, Motivo: motivo, PedidaPor: key.Name, KeyID: key.ID, Vence: time.Now().Add(AprobacionVence)})
	if err != nil {
		return a, err
	}
	p.Notify.Notify(notify.Message{
		Tone: notify.Warning, Key: fmt.Sprintf("aprobacion:%d", a.ID),
		Subject: M("⏳ %s: an agent asks to roll back to release #%d", app.Name, version),
		Title:   M("An agent asks for your approval"),
		Summary: M("%s asks to roll %s back to release #%d. Nothing happens until a person approves it.", key.Name, app.Name, version),
		Facts: []notify.Fact{
			{Label: M("App"), Value: app.Name},
			{Label: M("Asked by"), Value: key.Name},
			{Label: M("Why"), Value: motivo},
		},
		ActionURL: p.Config.BaseURL + "/bitacora", ActionLabel: M("Approve or reject"),
	})
	return a, nil
}

// DecidirAprobacion is a person's decision on a waiting request of the
// organization. Approved, the platform rolls the app back at once, in that
// person's name, and records it in the bitácora; the request then says
// what came of it (the new release, or why it failed).
func (p *Platform) DecidirAprobacion(ctx context.Context, orgID, id int64, aprobar bool, por, comentario string) (store.Aprobacion, error) {
	a, err := p.Store.DecidirAprobacion(ctx, orgID, id, aprobar, por, strings.TrimSpace(comentario))
	if err != nil || !aprobar {
		return a, err
	}
	fallo, resultado := false, ""
	app, err := p.Store.GetApp(ctx, a.App)
	if err == nil && app.OrgID != orgID {
		err = store.ErrNotFound
	}
	var rel *store.Release
	if err == nil {
		rel, err = p.Rollback(ctx, app, a.Version)
	}
	if err != nil {
		fallo, resultado = true, err.Error()
	} else {
		resultado = fmt.Sprintf("release #%d", rel.Number)
	}
	if terr := p.Store.TerminarAprobacion(context.WithoutCancel(ctx), a.ID, fallo, resultado); terr != nil {
		p.Log.Error("aprobación", "id", a.ID, "err", terr)
	}
	b := store.Bitacora{OrgID: orgID, QuienTipo: store.QuienPlataforma, Quien: "rendimiento", Por: "plataforma",
		Accion: store.AccionRevertir, Sobre: map[string]string{"app": a.App, "version": fmt.Sprint(a.Version)}, Resultado: "ok",
		Detalle: map[string]any{"aprobacion": a.ID, "pedida_por": a.PedidaPor, "aprobada_por": por, "resultado": resultado}}
	if fallo {
		b.Resultado = resultado
	}
	if _, berr := p.Store.AnotarBitacora(context.WithoutCancel(ctx), b); berr != nil {
		p.Log.Error("bitácora", "aprobacion", a.ID, "err", berr)
	}
	a, _ = p.Store.GetAprobacion(ctx, a.ID)
	return a, nil
}
