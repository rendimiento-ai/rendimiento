package api

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/libreta"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// «Anotar un pedido» writes a to-do in the Ruta about the app; the libreta
// shows it, with the state its branch's run gives it, beside the app's
// encargos; setting it aside on the Ruta page shows it as discarded.
func TestLibreta(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	me := e.session(t, "p0dxD")
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	app := &store.App{Name: "libreta-prueba", Repo: "p0dxD/libreta-prueba", DefaultBranch: "main", Spec: sp}
	if err := e.store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}

	if r, body := e.do(t, "POST", "/api/apps/libreta-prueba/libreta", `{"title":" "}`, me, nil); r.StatusCode != 400 {
		t.Fatalf("an empty request: %d %s", r.StatusCode, body)
	}
	if r, _ := e.do(t, "POST", "/api/apps/nadie/libreta", `{"title":"x"}`, me, nil); r.StatusCode != 404 {
		t.Fatalf("an unknown app: %d", r.StatusCode)
	}
	r, body := e.do(t, "POST", "/api/apps/libreta-prueba/libreta", `{"title":"Que el arreglo se dispare desde el botón","body":"en cualquier orden"}`, me, nil)
	var entry store.RutaEntry
	if r.StatusCode != 201 || json.Unmarshal([]byte(body), &entry) != nil || entry.App != "libreta-prueba" || entry.Kind != store.KindPendiente || entry.Author != "p0dxD" {
		t.Fatalf("anotar: %d %s", r.StatusCode, body)
	}
	otro, _ := e.store.AddRuta(ctx, store.RutaEntry{Kind: store.KindPendiente, Title: "Arreglo para Maven", Author: "p0dxD", App: "libreta-prueba"})
	if err := e.store.LinkRutaBranch(ctx, entry.ID, "arreglo-boton", "claude"); err != nil {
		t.Fatal(err)
	}
	run := &store.Run{AppID: app.ID, SHA: "abc", Branch: "arreglo-boton", Event: "push"}
	if err := e.store.CreateRun(ctx, run, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishRun(ctx, run.ID, store.RunSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	enc, _ := e.store.CreateEncargo(ctx, store.Encargo{Title: "Pruebas propias", App: "libreta-prueba", CreatedBy: "p0dxD"})

	// Set aside from the Ruta page, with its reason.
	if r, body := e.do(t, "PUT", "/api/ruta/"+strconv.FormatInt(otro.ID, 10), `{"title":"Arreglo para Maven","body":"","tags":[],"done":false,"discarded":"Maven no tiene lockfile"}`, me, nil); r.StatusCode != 200 {
		t.Fatalf("discard: %d %s", r.StatusCode, body)
	}

	r, body = e.do(t, "GET", "/api/apps/libreta-prueba/libreta", "", me, nil)
	var l libreta.Libreta
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &l) != nil || len(l.Items) != 3 {
		t.Fatalf("libreta: %d %s", r.StatusCode, body)
	}
	got := map[string]libreta.Item{}
	for _, it := range l.Items {
		got[it.Source+strconv.FormatInt(it.ID, 10)] = it
	}
	if it := got["ruta"+strconv.FormatInt(entry.ID, 10)]; it.Status != libreta.Listo || it.Branch != "arreglo-boton" || it.Run != run.ID {
		t.Errorf("the request on its branch: %+v", it)
	}
	if it := got["ruta"+strconv.FormatInt(otro.ID, 10)]; it.Status != libreta.Descartado || it.Reason != "Maven no tiene lockfile" {
		t.Errorf("the discarded one: %+v", it)
	}
	if it := got["encargo"+strconv.FormatInt(enc.ID, 10)]; it.Status != libreta.Pedido {
		t.Errorf("the encargo: %+v", it)
	}
	// The Ruta page keeps the app when it does not send it.
	r, body = e.do(t, "PUT", "/api/ruta/"+strconv.FormatInt(entry.ID, 10), `{"title":"Que el arreglo se dispare desde el botón","body":"","tags":[],"done":false}`, me, nil)
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &entry) != nil || entry.App != "libreta-prueba" {
		t.Fatalf("the app was lost on an edit: %d %s", r.StatusCode, body)
	}
}
