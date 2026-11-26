package platform

import (
	"context"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A finished run of a dependency update's open pull request asks for its
// report, with the pull request's notes; a person's branch, the default
// branch and a deploy do not; with «only when it fails» a run that passed
// does not either, and with it off nothing does.
func TestPedirAlDia(t *testing.T) {
	p, fake, _, _ := setup(t)
	ctx := context.Background()
	sp, err := spec.Parse([]byte("services:\n  - name: web\n    port: 3000\n"))
	if err != nil {
		t.Fatal(err)
	}
	app := &store.App{Name: "pedir-al-dia", Repo: "o/pedir-al-dia", DefaultBranch: "main", Spec: *sp}
	if err := p.Store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	pull := func(n int, branch, body string) forja.Change {
		var pr forja.Change
		pr.Number, pr.State, pr.Title, pr.Body = n, "open", "Update "+branch, body
		pr.Head.Ref, pr.Base.Ref = branch, "main"
		return pr
	}
	fake.recent = []forja.Change{pull(71, "renovate/vite-8.x", "## Breaking changes\n- Node 18 dropped"), pull(72, "propina", "")}
	sha := strings.Repeat("e", 40)
	pedidos := func() []store.AlDia {
		got, err := p.Store.AlDiaPendientes(ctx, 50)
		if err != nil {
			t.Fatal(err)
		}
		var mine []store.AlDia
		for _, a := range got {
			if a.AppID == app.ID {
				mine = append(mine, a)
			}
		}
		return mine
	}

	p.pedirAlDia(ctx, app, &store.Run{ID: 9101, AppID: app.ID, SHA: sha, Branch: "renovate/vite-8.x"}, false)
	p.pedirAlDia(ctx, app, &store.Run{ID: 9102, AppID: app.ID, SHA: sha, Branch: "propina"}, true)
	p.pedirAlDia(ctx, app, &store.Run{ID: 9103, AppID: app.ID, SHA: sha, Branch: "main"}, true)
	p.pedirAlDia(ctx, app, &store.Run{ID: 9104, AppID: app.ID, SHA: sha, Branch: "renovate/vite-8.x", Deploy: true}, true)
	got := pedidos()
	if len(got) != 1 || got[0].RunID != 9101 || got[0].Pull != 71 || !strings.Contains(got[0].Notas, "Node 18 dropped") || got[0].RunFailed {
		t.Fatalf("asked for: %+v", got)
	}

	set := store.AlDiaAjustes{Encendido: true, Quien: store.AlDiaQuienPlataforma, Cuando: store.AlDiaCuandoFalla, TopeMicros: store.AlDiaTopeDefault}
	if err := p.Store.GuardarAlDiaAjustes(ctx, app.OrgID, set, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	p.pedirAlDia(ctx, app, &store.Run{ID: 9105, AppID: app.ID, SHA: sha, Branch: "renovate/vite-8.x"}, false)
	p.pedirAlDia(ctx, app, &store.Run{ID: 9106, AppID: app.ID, SHA: sha, Branch: "renovate/vite-8.x"}, true)
	set.Encendido = false
	if err := p.Store.GuardarAlDiaAjustes(ctx, app.OrgID, set, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	p.pedirAlDia(ctx, app, &store.Run{ID: 9107, AppID: app.ID, SHA: sha, Branch: "renovate/vite-8.x"}, true)
	if got := pedidos(); len(got) != 2 || got[1].RunID != 9106 || !got[1].RunFailed {
		t.Fatalf("with «only when it fails», then off: %+v", got)
	}
}
