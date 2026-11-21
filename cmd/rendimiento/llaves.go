package main

import (
	"context"
	"log/slog"
	"net/url"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/analytics"
	"github.com/rendimiento-ai/rendimiento/internal/dns"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/llaves"
	"github.com/rendimiento-ai/rendimiento/internal/logarchive"
	"github.com/rendimiento-ai/rendimiento/internal/notify"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// llavesQueCambian wires the keys that change on the Environment page
// (Llaves y ajustes): what tests and saves a new one, and what makes this
// process follow a change made in any of them. A key is followed here only
// when this process uses its service: the Cloudflare token when DNS
// automation is on, the email key when email is, the AI key always, the
// log archive's key when it is on (logBucket), the Umami user when visitor
// numbers are (umami).
func llavesQueCambian(kube client.Client, st *store.Store, ns, baseURL, issuer string, dnsProvider dns.Provider,
	notifier *notify.Notifier, gateway *ia.Gateway, logBucket *logarchive.MinIO, umami *analytics.Umami,
	log *slog.Logger) (*llaves.Cambiador, *llaves.Recargador) {
	aplicar := map[string]func(llaves.Valores){}
	if cf, ok := dnsProvider.(*dns.Cloudflare); ok {
		aplicar[llaves.Cloudflare] = func(v llaves.Valores) { cf.SetToken(v["CLOUDFLARE_API_TOKEN"]) }
	}
	if r, ok := notifier.Sender.(*notify.Resend); ok {
		aplicar[llaves.Correo] = func(v llaves.Valores) { r.SetKey(v["RESEND_API_KEY"]) }
	}
	if gateway != nil {
		aplicar[llaves.IA] = func(v llaves.Valores) { gateway.SetPlatformKey(v["AI_PLATFORM_KEY"]) }
	}
	if logBucket != nil {
		aplicar[llaves.Registros] = func(v llaves.Valores) { logBucket.SetKeys(v["LOG_ARCHIVE_ACCESS_KEY"], v["LOG_ARCHIVE_SECRET_KEY"]) }
	}
	if umami != nil {
		aplicar[llaves.Umami] = func(v llaves.Valores) { umami.SetLogin(v["UMAMI_USERNAME"], v["UMAMI_PASSWORD"]) }
	}
	rec := &llaves.Recargador{Store: st, Aplicar: aplicar, Log: log,
		Leer: func(ctx context.Context, id string) (llaves.Valores, error) { return llaves.Leer(ctx, kube, ns, id) }}
	zona := os.Getenv("DNS_ZONE")
	cam := &llaves.Cambiador{Kube: kube, Namespace: ns, ClusterIssuer: issuer, CertManagerNamespace: env("CERT_MANAGER_NAMESPACE", "cert-manager"),
		Store: st, Recargador: rec, ZonaPrueba: zona, ResendFrom: env("NOTIFY_EMAIL_FROM", "rendimiento <alerts@example.com>"),
		ResendTo: notifier.To, Lang: notifier.Lang, Log: log,
		ArchivoEndpoint: os.Getenv("LOG_ARCHIVE_ENDPOINT"), ArchivoBucket: env("LOG_ARCHIVE_BUCKET", "rendimiento-logs"),
		ArchivoSeguro: env("LOG_ARCHIVE_SECURE", "false") == "true", UmamiURL: os.Getenv("UMAMI_URL"),
		// The zones a new token must see: those the platform's and the
		// apps' hosts are in, of the zones the token in use knows, and
		// DNS_ZONE.
		Zonas: func(ctx context.Context) ([]string, error) {
			known, err := dnsProvider.Zones(ctx)
			var hosts []string
			if u, perr := url.Parse(baseURL); perr == nil && u.Hostname() != "" {
				hosts = append(hosts, u.Hostname())
			}
			if apps, aerr := st.ListApps(ctx); aerr == nil {
				for _, a := range apps {
					for _, s := range a.Spec.Services {
						hosts = append(hosts, s.Hosts()...)
					}
				}
			}
			return llaves.ZonasUsadas(hosts, known, zona), err
		}}
	if gateway != nil {
		cam.Modelo = gateway.PlatformModel
	}
	return cam, rec
}

// iaDelEntorno says on the Environment page how the platform's AI is set up.
func iaDelEntorno(g *ia.Gateway) func(context.Context) (bool, string) {
	return func(context.Context) (bool, string) {
		key := g.PlatformKeyNow() != ""
		local, model := g.LocalNow()
		switch {
		case key && local != "":
			return true, i18n.M("the platform's key (%s), and a local model (%s)", or(g.PlatformModel, ia.DefaultAnthropicModel), model)
		case key:
			return true, i18n.M("the platform's key, with %s", or(g.PlatformModel, ia.DefaultAnthropicModel))
		case local != "":
			return true, i18n.M("a local model, %s", model)
		}
		return false, ""
	}
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
