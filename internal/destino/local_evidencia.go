package destino

import (
	"context"
	"strconv"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Evidencia is what was seen of the release's containers: kept as it
// failed to start (desplegar removes them then), or else read now, from
// the containers of the release that still run.
func (l *Local) Evidencia(ctx context.Context, app string, rel *store.Release) (*store.ReleaseEvidence, error) {
	l.mu.Lock()
	kept, ok := l.evidencias[app]
	l.mu.Unlock()
	if ok && kept.version == rel.Number {
		ev := *kept.ev
		return &ev, nil
	}
	return l.verEvidencia(ctx, app, rel), nil
}

// evidenciaLocal is what was seen of an app's release as it failed.
type evidenciaLocal struct {
	version int64
	ev      *store.ReleaseEvidence
}

// guardarEvidencia keeps what the release's containers show before they
// are removed, for Evidencia.
func (l *Local) guardarEvidencia(ctx context.Context, app string, rel *store.Release) {
	ev := l.verEvidencia(ctx, app, rel)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.evidencias == nil {
		l.evidencias = map[string]evidenciaLocal{}
	}
	l.evidencias[app] = evidenciaLocal{version: rel.Number, ev: ev}
}

// verEvidencia reads each service container of the release: its state,
// exit code, restarts and last lines. On a computer a service is one
// container, shown as a pod of one.
func (l *Local) verEvidencia(ctx context.Context, app string, rel *store.Release) *store.ReleaseEvidence {
	ev := &store.ReleaseEvidence{CapturedAt: time.Now().UTC(), Where: l.Nombre(), Services: []store.ServiceEvidence{}}
	version := strconv.FormatInt(rel.Number, 10)
	por := map[string][]store.PodEvidence{}
	names, err := l.Motor.Nombres(ctx, etApp+"="+app)
	if err != nil {
		ev.Notes = append(ev.Notes, err.Error())
	}
	for _, n := range names {
		info, err := l.Motor.Inspeccionar(ctx, n)
		if err != nil || info.Etiquetas[etParte] != "service" || info.Etiquetas[etVersion] != version {
			continue
		}
		c := store.ContainerEvidence{Name: info.Etiquetas[etServicio], Ready: info.Corriendo, Restarts: int32(info.Reinicios), Message: info.Error}
		switch {
		case info.Corriendo:
			c.State = "running"
		case info.Estado == "created":
			c.State, c.Reason = "waiting", "created"
		default:
			code := int32(info.Codigo)
			c.State, c.Reason, c.ExitCode = "terminated", info.Estado, &code
		}
		c.Log = l.Motor.Registros(ctx, n, EvidenciaLineas)
		svc := info.Etiquetas[etServicio]
		por[svc] = append(por[svc], store.PodEvidence{Name: n, Phase: info.Estado, Containers: []store.ContainerEvidence{c}})
	}
	for _, svc := range rel.Spec.Services {
		pods := por[svc.Name]
		if pods == nil {
			pods = []store.PodEvidence{}
		}
		ev.Services = append(ev.Services, store.ServiceEvidence{Name: svc.Name, Pods: pods})
	}
	return ev
}
