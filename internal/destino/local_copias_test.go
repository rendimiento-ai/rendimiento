package destino

import (
	"context"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/motor"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A pull request's copy runs beside its app at <app>-pr-<n>.localhost, with
// the app's preview secrets (never production's) and a Postgres of its own,
// never at the app's domain;
// its labels say whose copy it is; rendimiento finds it again after a
// restart; removing it leaves the app running.
func TestLocalCopias(t *testing.T) {
	ctx := context.Background()
	e := &engine{containers: map[string]*fakeContainer{}}
	l := &Local{Motor: &motor.Motor{Bin: "nerdctl", Exec: e.exec}, Caja: memCaja{}, Esperar: 300 * time.Millisecond, Puerto: "8000",
		Responde: func(context.Context, string, *spec.Health) bool { return true }}
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 3000, Domain: "tienda.example.com", Secrets: []string{"llaves"},
		SecretEnv: map[string]string{"MAIL_KEY": "correo/key"}, Needs: []spec.Need{{Kind: spec.NeedPostgres}}}}, Previews: &spec.Previews{}}
	sp.Default()
	v, prueba, correo := "abc", "de-prueba", "correo-real"
	if err := l.GuardarSecreto(ctx, "tienda", "llaves", map[string]*string{"API_KEY": &v}); err != nil {
		t.Fatal(err)
	}
	if err := l.GuardarSecreto(ctx, "tienda", "llaves-preview", map[string]*string{"API_KEY": &prueba}); err != nil {
		t.Fatal(err)
	}
	// correo has no preview variant: the copy goes without it.
	if err := l.GuardarSecreto(ctx, "tienda", "correo", map[string]*string{"key": &correo}); err != nil {
		t.Fatal(err)
	}
	if err := l.Aplicar(ctx, "tienda", &store.Release{Number: 1, Spec: sp, Images: map[string]string{"web": "localhost:5000/tienda-web@sha256:1"}}); err != nil {
		t.Fatal(err)
	}
	waitEstado(t, l, "tienda", 1, Sana)
	app := l.Ruta("tienda.localhost")

	copia := spec.PreviewOf(sp, 7)
	if err := l.Copiar(ctx, "tienda", 7, &store.Release{Number: 41, Spec: copia, Images: map[string]string{"web": "localhost:5000/tienda-web@sha256:2"}}, 99); err != nil {
		t.Fatal(err)
	}
	waitEstado(t, l, "tienda-pr-7", 41, Sana)
	web := e.containers["tienda-pr-7-web-r41"]
	if web == nil || web.env["API_KEY"] != "de-prueba" || web.image != "localhost:5000/tienda-web@sha256:2" {
		t.Fatalf("the copy's container: %+v", web)
	}
	if _, has := web.env["MAIL_KEY"]; has {
		t.Fatalf("the copy got production's correo: %v", web.env)
	}
	if prod := e.containers["tienda-web-r1"]; prod == nil || prod.env["API_KEY"] != "abc" || prod.env["MAIL_KEY"] != "correo-real" {
		t.Fatalf("the app's own container: %+v", prod)
	}
	if e.containers["tienda-pr-7-postgres"] == nil {
		t.Fatal("the copy has no Postgres of its own")
	}
	ruta := l.Ruta("tienda-pr-7.localhost")
	if ruta == "" || ruta == app || l.Ruta("tienda.localhost") != app {
		t.Fatalf("routes: copy %q, app %q → %q", ruta, app, l.Ruta("tienda.localhost"))
	}
	if l.Ruta(copia.Services[0].Domain) != "" {
		t.Fatalf("the copy answers at the cluster's domain %s", copia.Services[0].Domain)
	}
	if got := l.DireccionCopia("tienda", 7); got != "http://tienda-pr-7.localhost:8000/" {
		t.Fatalf("address %q", got)
	}
	got, err := l.Copias(ctx, "")
	if err != nil || len(got) != 1 || got[0].App != "tienda" || got[0].Numero != 7 || got[0].Comentario != 99 || got[0].Empujada.IsZero() {
		t.Fatalf("copies %+v %v", got, err)
	}

	// After a restart the proxy finds the copy again.
	l.mu.Lock()
	l.rutas = nil
	l.mu.Unlock()
	l.Recargar(ctx, []*store.App{{Name: "tienda", Spec: sp}})
	if l.Ruta("tienda-pr-7.localhost") != ruta || l.Ruta("tienda.localhost") != app {
		t.Fatalf("after a restart: copy %q, app %q", l.Ruta("tienda-pr-7.localhost"), l.Ruta("tienda.localhost"))
	}

	if err := l.QuitarCopia(ctx, "tienda", 7); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.Copias(ctx, "tienda"); len(got) != 0 || e.containers["tienda-pr-7-web-r41"] != nil || l.Ruta("tienda-pr-7.localhost") != "" {
		t.Fatalf("after removing: %+v", got)
	}
	if e.containers["tienda-web-r1"] == nil || l.Ruta("tienda.localhost") != app {
		t.Fatal("removing the copy touched the app")
	}
}
