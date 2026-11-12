package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Two organizations can both have an app called "tienda": the platform's
// keeps the name as given, any other carries its address in front (which is
// also its namespace). Neither is told what the other has, and no address or
// name may begin with another organization's.
func TestAppNamesByOrganization(t *testing.T) {
	p, _, _, _ := setup(t)
	ctx := context.Background()
	almacen, err := p.Store.CreateOrg(ctx, "almacen", "El almacén", "ana")
	if err != nil {
		t.Fatal(err)
	}
	registrar := func(org int64, name, domain string) (*store.App, error) {
		sp, err := spec.Parse([]byte("services:\n  - name: web\n    domain: " + domain + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		return p.Register(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/" + name, DefaultBranch: "main", Name: name, Spec: *sp, OrgID: org})
	}

	suya, err := registrar(store.DefaultOrg, "nombres-tienda", "nombres-tienda.example.com")
	if err != nil || suya.Name != "nombres-tienda" {
		t.Fatalf("the platform's app = %+v, %v; want its name as given", suya, err)
	}
	otra, err := registrar(almacen.ID, "Nombres Tienda", "almacen-tienda.example.com")
	if err != nil || otra.Name != "almacen-nombres-tienda" || otra.OrgID != almacen.ID {
		t.Fatalf("the other organization's app = %+v, %v; want almacen-nombres-tienda", otra, err)
	}
	// Typing the address in front does not repeat it.
	if a, err := registrar(almacen.ID, "almacen-nombres-api", "almacen-api.example.com"); err != nil || a.Name != "almacen-nombres-api" {
		t.Fatalf("a name with the address already in front = %+v, %v", a, err)
	}

	// A second one of the same name is refused in its own organization, in
	// words that only speak of its own app.
	if _, err := registrar(almacen.ID, "nombres-tienda", "almacen-otra.example.com"); err == nil || !strings.Contains(err.Error(), `"almacen-nombres-tienda"`) {
		t.Fatalf("a repeated name in the organization: %v", err)
	}
	// The platform's organization may not take a name that begins like
	// another organization's apps.
	if _, err := registrar(store.DefaultOrg, "almacen-nombres-caja", "nombres-caja.example.com"); err == nil || !strings.Contains(err.Error(), `"almacen-"`) {
		t.Fatalf("the platform took another organization's prefix: %v", err)
	}
	// A name too long for a namespace is refused, not cut.
	larga, err := p.Store.CreateOrg(ctx, "bodega-de-direccion-muy-larga", "La bodega", "ana")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registrar(larga.ID, strings.Repeat("n", 40), "bodega-larga.example.com"); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("a name longer than a namespace: %v", err)
	}

	// Taking over what already runs in the cluster is the platform's: the
	// name another organization gives is not the namespace that was found.
	sp, _ := spec.Parse([]byte("services:\n  - name: web\n    domain: almacen-vieja.example.com\n"))
	if _, err := p.Register(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/vieja", DefaultBranch: "main", Name: "nombres-vieja", Spec: *sp, OrgID: almacen.ID, Adopt: true}); err == nil {
		t.Fatal("another organization adopted a namespace")
	}

	// No organization's address may begin another's, nor an app's name.
	for _, slug := range []string{"almacen-norte", "alma-cen", "nombres"} {
		if slug == "alma-cen" {
			if _, err := p.Store.CreateOrg(ctx, "alma", "Alma", "ana"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := p.Store.CreateOrg(ctx, slug, slug, "ana"); !errors.Is(err, store.ErrSlugPrefix) {
			t.Errorf("CreateOrg(%q) = %v; want ErrSlugPrefix", slug, err)
		}
	}
	if _, err := p.Store.CreateOrg(ctx, "almacenes", "Almacenes", "ana"); err != nil {
		t.Errorf("an address that only shares letters with another was refused: %v", err)
	}
}
