package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/ajustes"
	"github.com/rendimiento-ai/rendimiento/internal/platform"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// liveSettings reads the platform's live settings (the boundary between
// organizations and its quota, see internal/ajustes): the environment's
// values at boot, checked as always, then whatever is saved on the
// Environment page over them.
func liveSettings(st *store.Store, platformNS, ingressClass string, log *slog.Logger) (*ajustes.Holder, error) {
	h := &ajustes.Holder{Store: st, Boot: ajustes.FromEnv(os.Getenv), PlatformNS: platformNS, IngressClass: ingressClass, Log: log}
	return h, h.Start()
}

// liveQuota is the organizations' quota in force now.
func liveQuota(h *ajustes.Holder) func() platform.OrgQuota {
	return func() platform.OrgQuota {
		b := h.Boundary()
		return platform.OrgQuota{CPU: b.QuotaCPU, Memory: b.QuotaMemory}
	}
}

// AnnotationSettings marks an App when the boundary changed, so the
// controller reconciles it at once instead of at its next resync.
const AnnotationSettings = "rendimiento.ai/ajustes"

// nudgeApps has every app reconciled again: the boundary changed.
func nudgeApps(ctx context.Context, c client.Client, log *slog.Logger) {
	var list v1alpha1.AppList
	if err := c.List(ctx, &list); err != nil {
		log.Warn("the boundary changed, but the apps could not be listed; they follow at their next resync", "err", err)
		return
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	for i := range list.Items {
		app := &list.Items[i]
		patch := client.MergeFrom(app.DeepCopy())
		if app.Annotations == nil {
			app.Annotations = map[string]string{}
		}
		app.Annotations[AnnotationSettings] = stamp
		if err := c.Patch(ctx, app, patch); err != nil {
			log.Warn("could not mark an app for the new boundary", "app", app.Name, "err", err)
		}
	}
	log.Info("the boundary changed: every app is reconciled again", "apps", len(list.Items))
}
