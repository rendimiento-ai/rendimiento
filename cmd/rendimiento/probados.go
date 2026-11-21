package main

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/ajustes"
	"github.com/rendimiento-ai/rendimiento/internal/dns"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/probados"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ajustesProbados wires the settings tested before they change (Keys and
// settings): what tests a new registry, zone, issuer, storage class,
// ingress class or address against this cluster, and what restarts the
// platform's Deployments once one is saved, so they read it.
func ajustesProbados(kube client.Client, st *store.Store, ns string, executor *pipeline.KubeExecutor, dnsProvider dns.Provider, settings *ajustes.Holder,
	entorno map[string]string, services []string, dsn string, log *slog.Logger) *probados.Probador {
	pr := &probados.Probador{
		Kube: kube, Namespace: ns, BuildNamespace: executor.Namespace, Store: st, Entorno: entorno, Actual: os.Getenv,
		RegistryInsecure: executor.InsecureRegistry, Empujar: executor.PushTestImage, ImagenPrueba: executor.BuildImage,
		IngressNamespace: func(class string) string { return ajustes.IngressNamespaceOf(settings.Effective(), class) },
		Identidad:        probados.LlaveDeIdentidad(dsn),
		Reiniciar:        reiniciarPlataforma(kube, ns, services, env("SELF_DEPLOYMENT", "rendimiento")),
		Log:              log,
	}
	if cf, ok := dnsProvider.(*dns.Cloudflare); ok {
		pr.DNS = cf
	}
	return pr
}

// reiniciarPlataforma restarts the platform's Deployments (SERVICES), the
// one answering the request last, as an update does: each rolls over, the
// new pods reading the environment again.
func reiniciarPlataforma(kube client.Client, ns string, services []string, self string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		names := slices.DeleteFunc(slices.Clone(services), func(d string) bool { return d == self })
		names = append(names, self)
		var done []string
		at := time.Now().UTC().Format(time.RFC3339)
		for _, name := range names {
			var d appsv1.Deployment
			if err := kube.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, &d); apierrors.IsNotFound(err) {
				continue
			} else if err != nil {
				return "", err
			}
			before := d.DeepCopy()
			if d.Spec.Template.Annotations == nil {
				d.Spec.Template.Annotations = map[string]string{}
			}
			d.Spec.Template.Annotations["rendimiento.ai/ajustes-probados"] = at
			if err := kube.Patch(ctx, &d, client.StrategicMergeFrom(before)); err != nil {
				return "", err
			}
			done = append(done, name)
		}
		return i18n.M("the platform restarts now to use it (%s): a minute or two; a run that is going may have to run again", strings.Join(done, ", ")), nil
	}
}
