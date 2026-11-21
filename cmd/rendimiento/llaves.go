package main

import (
	"context"
	"log/slog"
	"net/url"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/dns"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/llaves"
	"github.com/rendimiento-ai/rendimiento/internal/notify"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// llavesQueCambian wires the keys that change on the Environment page
// (Llaves y ajustes): what tests and saves a new one, and what makes this
// process follow a change made in any of them. A key is followed here only
// when this process uses its service: the Cloudflare token when DNS
// automation is on, the email key when email is, the AI key always.
func llavesQueCambian(kube client.Client, st *store.Store, ns, baseURL, issuer string, dnsProvider dns.Provider,
	notifier *notify.Notifier, gateway *ia.Gateway, log *slog.Logger) (*llaves.Cambiador, *llaves.Recargador) {
	aplicar := map[string]func(string){}
	if cf, ok := dnsProvider.(*dns.Cloudflare); ok {
		aplicar[llaves.Cloudflare] = cf.SetToken
	}
	if r, ok := notifier.Sender.(*notify.Resend); ok {
		aplicar[llaves.Correo] = r.SetKey
	}
	if gateway != nil {
		aplicar[llaves.IA] = gateway.SetPlatformKey
	}
	rec := &llaves.Recargador{Store: st, Aplicar: aplicar, Log: log,
		Leer: func(ctx context.Context, id string) (string, error) { return llaves.Leer(ctx, kube, ns, id) }}
	zona := os.Getenv("DNS_ZONE")
	cam := &llaves.Cambiador{Kube: kube, Namespace: ns, ClusterIssuer: issuer, CertManagerNamespace: env("CERT_MANAGER_NAMESPACE", "cert-manager"),
		Store: st, Recargador: rec, ZonaPrueba: zona, ResendFrom: env("NOTIFY_EMAIL_FROM", "rendimiento <alerts@example.com>"),
		ResendTo: notifier.To, Lang: notifier.Lang, Log: log,
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
