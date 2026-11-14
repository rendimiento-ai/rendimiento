package platform

import (
	"context"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A delivered encargo's branch gets its pull request: its link goes before
// the branch's, which stays; another organization's repository, the main
// branch and other links are left alone.
func TestPullsForDelivery(t *testing.T) {
	p, fake, _, _ := setup(t)
	ctx := context.Background()
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 3000}}}
	sp.Default()
	app := &store.App{Name: "entregas", Repo: "o/entregas", DefaultBranch: "main", Spec: sp}
	if err := p.Store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	fake.heads = map[string]string{"fix/encargo-7-saludo": strings.Repeat("d", 40)}
	links := []string{
		"https://github.com/o/entregas/pull/new/fix/encargo-7-saludo",
		"https://github.com/o/entregas/tree/main",
		"https://rendimiento.example.com/apps/entregas/runs/9",
		"https://github.com/ajena/otra/pull/new/rama",
	}
	got := p.PullsForDelivery(ctx, app.OrgID, 7, "Saludo", "Listo.", links)
	want := []string{
		"https://github.com/o/entregas/pull/100",
		"https://github.com/o/entregas/pull/new/fix/encargo-7-saludo",
		"https://github.com/o/entregas/tree/main",
		"https://rendimiento.example.com/apps/entregas/runs/9",
		"https://github.com/ajena/otra/pull/new/rama",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("links\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(fake.opened) != 1 || fake.opened[0] != "fix/encargo-7-saludo: Saludo" {
		t.Fatalf("opened %v", fake.opened)
	}
	if got := p.PullsForDelivery(ctx, app.OrgID+1, 7, "Saludo", "Listo.", links[:1]); len(got) != 1 {
		t.Fatalf("another organization's encargo opened a pull request: %v", got)
	}
}
