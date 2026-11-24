package destino

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Dormir stops every container of the apps and of their pull requests'
// copies (the platform's own, its database, registry and BuildKit, are
// not theirs and keep running), and makes the proxy answer «Volvemos
// pronto» meanwhile. It returns the containers it stopped, to start the
// same ones on waking; their volumes and releases stay as they are.
func (l *Local) Dormir(ctx context.Context) ([]string, error) {
	l.dormido.Store(true)
	names, err := l.Motor.Nombres(ctx, etApp)
	if err != nil {
		return nil, err
	}
	var stopped []string
	var errs []error
	for _, n := range names {
		info, err := l.Motor.Inspeccionar(ctx, n)
		if err != nil || !info.Corriendo {
			continue
		}
		if err := l.Motor.Detener(ctx, n); err != nil {
			errs = append(errs, err)
			continue
		}
		stopped = append(stopped, n)
	}
	sort.Strings(stopped)
	return stopped, errors.Join(errs...)
}

// Despertar starts the containers sleep stopped, finds their routes again
// (apps are what rendimiento knows of) and lets the proxy send people to
// them. It returns the apps that run again; a container that does not
// start is said in the error, and the others start anyway.
func (l *Local) Despertar(ctx context.Context, contenedores []string, apps []*store.App) ([]string, error) {
	seen := map[string]bool{}
	var errs []error
	for _, n := range contenedores {
		info, err := l.Motor.Inspeccionar(ctx, n)
		if err != nil {
			continue // removed meanwhile (a newer release, or the app)
		}
		// Started already when the computer started (they restart with it).
		if !info.Corriendo {
			if err := l.Motor.Arrancar(ctx, n); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if app := info.Etiquetas[etApp]; app != "" && info.Etiquetas[etCopia] == "" {
			seen[app] = true
		}
	}
	l.Recargar(ctx, apps)
	l.dormido.Store(false)
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Strings(out)
	return out, errors.Join(errs...)
}

// Desplegando reports whether a release is still starting here (a publish
// that sleep waits for).
func (l *Local) Desplegando() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.estados {
		if e.estado == Pendiente {
			return true
		}
	}
	return false
}

// Dormido reports whether the proxy answers «Volvemos pronto».
func (l *Local) Dormido() bool { return l.dormido.Load() }

// volvemosPronto is the page the proxy answers while rendimiento sleeps, to
// whoever reaches an app (on this computer, or through a tunnel when there
// is one): 503 with Retry-After, so search engines and monitors know it is
// for a while.
func volvemosPronto(w http.ResponseWriter, r *http.Request) {
	titulo, texto := "Back soon", "This app is resting for a while. Please come back later."
	lang := "en"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Accept-Language"))), "es") {
		titulo, texto, lang = "Volvemos pronto", "Esta aplicación está descansando un rato. Vuelva más tarde, por favor.", "es"
	}
	h := w.Header()
	h.Set("Retry-After", "3600")
	h.Set("Cache-Control", "no-store")
	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		h.Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, titulo+": "+texto)
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	fmt.Fprintf(w, `<!doctype html><html lang="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title><style>
:root{color-scheme:light dark}
body{margin:0;min-height:100vh;display:grid;place-items:center;font-family:system-ui,sans-serif;background:#FBF5EA;color:#2B1B12}
main{max-width:32rem;padding:2rem 1rem;text-align:center}
h1{font-family:Georgia,serif;font-size:2rem;margin:.5rem 0}
@media (prefers-color-scheme: dark){body{background:#120F2A;color:#F6EBDD}}
</style></head><body><main>
<svg width="72" height="72" viewBox="0 0 64 64" aria-hidden="true"><circle cx="32" cy="32" r="31" fill="#2E3E78"/><path d="M44 38A15 15 0 1 1 26 18a12 12 0 0 0 18 20z" fill="#F2C14E"/></svg>
<h1>%s</h1><p>%s</p></main></body></html>
`, lang, html.EscapeString(titulo), html.EscapeString(titulo), html.EscapeString(texto))
}
