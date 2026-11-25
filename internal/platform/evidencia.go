package platform

import (
	"context"
	"errors"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/destino"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ErrSinEvidencia: the app's destination cannot say what it sees of a
// release (nowhere to run, or no clientset).
var ErrSinEvidencia = destino.ErrSinTestigo

// evidenceTimeout is the longest the rollback of a failed release waits
// for its evidence to be read.
const evidenceTimeout = 20 * time.Second

// Evidence is what the app's destination sees of a release now: each
// service's new pods (or containers) with their state, the events about
// them and the last lines they wrote, with the lines that look like
// secrets left out and at most store.MaxEvidence bytes. It reads; it
// keeps nothing (keepEvidence does, for a release that failed).
func (p *Platform) Evidence(ctx context.Context, app *store.App, rel *store.Release) (*store.ReleaseEvidence, error) {
	t, ok := p.Target().(destino.Testigo)
	if !ok || !rel.Spec.Runs() {
		return nil, ErrSinEvidencia
	}
	ev, err := t.Evidencia(ctx, app.Name, rel)
	if err != nil {
		return nil, err
	}
	ev.Redact()
	ev.Cap(store.MaxEvidence)
	return ev, nil
}

// keepEvidence keeps what is seen of a release that failed verification,
// before the rollback removes its pods: Kubernetes forgets events in about
// an hour, and the pods of a rolled-back release at once.
func (p *Platform) keepEvidence(ctx context.Context, app *store.App, rel *store.Release) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), evidenceTimeout)
	defer cancel()
	ev, err := p.Evidence(ctx, app, rel)
	if errors.Is(err, ErrSinEvidencia) {
		return
	}
	if err == nil {
		err = p.Store.SaveReleaseEvidence(ctx, rel.ID, ev)
	}
	if err != nil {
		p.Log.Warn("could not keep what was seen of a failed release", "app", app.Name, "release", rel.Number, "err", err)
	}
}
