package platform

// What agents may see and do of an app beyond its runs (Ruta 184): its
// infrastructure (infra_read), its preview copies (copies_read), and the
// actions with effect a person allows one by one (effects): restart a
// service, run a branch again (never the default one), make a copy again.
// The permission is checked by the caller (agentes, ruta); these only ever
// touch the app they are given.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/destino"
	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// AgentInfra is the platform as agents' infra, copies and effects tools see it.
type AgentInfra struct{ P *Platform }

// MirarApp reads the app's infrastructure where it runs.
func (a AgentInfra) MirarApp(ctx context.Context, app *store.App, q destino.Mirar) (destino.Mirada, error) {
	m, ok := a.P.Target().(destino.Mirador)
	if !ok {
		return destino.Mirada{}, destino.ErrSinMirar
	}
	return m.Mirar(ctx, app.Name, q)
}

// ReiniciarServicio restarts one of the app's services.
func (a AgentInfra) ReiniciarServicio(ctx context.Context, app *store.App, servicio string) error {
	m, ok := a.P.Target().(destino.Mirador)
	if !ok {
		return destino.ErrSinMirar
	}
	if _, found := serviceOf(app, servicio); !found {
		return errors.New(M("%s has no service %s", app.Name, servicio))
	}
	return m.ReiniciarServicio(ctx, app.Name, servicio)
}

func serviceOf(app *store.App, name string) (int, bool) {
	for i, s := range app.Spec.Services {
		if s.Name == name {
			return i, true
		}
	}
	return 0, false
}

// LanzarRama runs a branch of the app again, never its default branch:
// that one deploys, and a person does it.
func (a AgentInfra) LanzarRama(ctx context.Context, app *store.App, branch string) (*store.Run, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" || branch == app.DefaultBranch {
		return nil, errors.New(M("not the default branch %s: it deploys, and a person runs it", app.DefaultBranch))
	}
	return a.P.QueueRun(ctx, app, branch, "", "agent")
}

// RehacerCopia makes pull request number's copy again from its branch's
// last run that passed.
func (a AgentInfra) RehacerCopia(ctx context.Context, app *store.App, number int, who string) (string, error) {
	r, err := a.P.RemakeCopy(ctx, app, number, who)
	if err != nil {
		return "", err
	}
	if r.Restarted {
		return fmt.Sprintf("the copy of pull request #%d already runs run #%d: its services were restarted", number, r.Run), nil
	}
	return fmt.Sprintf("the copy of pull request #%d is being made again from run #%d", number, r.Run), nil
}

// CopiasTexto is the app's live copies, as text for a model: each one's
// pull request, branch, health, its tests and checks, and when it goes;
// the Secrets a copy lacks by name only.
func (a AgentInfra) CopiasTexto(ctx context.Context, app *store.App) (string, error) {
	v, err := a.P.CopiasOf(ctx, app)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if v.Previews == nil {
		fmt.Fprintf(&b, "%s asks for no previews (rendimiento.yaml has no previews:).\n", app.Name)
	} else {
		fmt.Fprintf(&b, "%s's previews on the %s: up to %d copies, kept %d days, data %s.\n", app.Name, v.Where, v.Previews.Max, v.Previews.Keep, v.Previews.Data)
	}
	if len(v.Live) == 0 {
		b.WriteString("No live copies now.\n")
	}
	for _, c := range v.Live {
		if c.Short {
			fmt.Fprintf(&b, "- a short copy of run #%d", c.Run)
		} else {
			fmt.Fprintf(&b, "- pull request #%d", c.Pull)
			if c.Title != "" {
				fmt.Fprintf(&b, " (%s)", c.Title)
			}
		}
		if c.Branch != "" {
			fmt.Fprintf(&b, ", branch %s", c.Branch)
		}
		fmt.Fprintf(&b, ": %s", c.Health)
		if c.Message != "" {
			fmt.Fprintf(&b, " (%s)", c.Message)
		}
		if c.URL != "" {
			fmt.Fprintf(&b, ", at %s", c.URL)
		}
		if c.Run > 0 && !c.Short {
			fmt.Fprintf(&b, ", runs run #%d", c.Run)
		}
		if c.Tests != nil {
			fmt.Fprintf(&b, "; its tests: %d of %d passed", c.Tests.Passed, c.Tests.Total)
			if len(c.Tests.Failed) > 0 {
				fmt.Fprintf(&b, ", failed: %s", strings.Join(c.Tests.Failed, ", "))
			}
		}
		if c.Dry != nil {
			fmt.Fprintf(&b, "; checks en seco: %d of %d", c.Dry.Passed, c.Dry.Total)
			if len(c.Dry.Names) > 0 {
				fmt.Fprintf(&b, " (not passing: %s)", strings.Join(c.Dry.Names, ", "))
			}
		}
		if c.DeletesOn != nil {
			fmt.Fprintf(&b, "; goes on %s", c.DeletesOn.UTC().Format("2006-01-02"))
		}
		if len(c.MissingSecrets) > 0 {
			fmt.Fprintf(&b, "; lacks the Secrets %s (names only)", strings.Join(c.MissingSecrets, ", "))
		}
		b.WriteString("\n")
	}
	for i, g := range v.Gone {
		if i == 5 {
			break
		}
		fmt.Fprintf(&b, "- gone: pull request #%d, branch %s, %s\n", g.Pull, g.Branch, g.GoneWhy)
	}
	return b.String(), nil
}
