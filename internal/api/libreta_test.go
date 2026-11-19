package api

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
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
	// «Descartar» on the libreta: with its reason, for this app's entries
	// only; «Retomar» (no reason) takes it up again. The history keeps both.
	base := "/api/apps/libreta-prueba/libreta/" + strconv.FormatInt(entry.ID, 10) + "/discard"
	if r, body := e.do(t, "POST", base, `{"reason":"Lo resolvió otra rama"}`, me, nil); r.StatusCode != 200 || !strings.Contains(body, `"discarded":"Lo resolvió otra rama"`) {
		t.Fatalf("descartar: %d %s", r.StatusCode, body)
	}
	if r, _ := e.do(t, "POST", "/api/apps/libreta-prueba/libreta/999999/discard", `{"reason":"x"}`, me, nil); r.StatusCode != 404 {
		t.Fatalf("an unknown entry: %d", r.StatusCode)
	}
	nota, _ := e.store.AddRuta(ctx, store.RutaEntry{Kind: store.KindNota, Title: "una nota", Author: "p0dxD", App: "libreta-prueba"})
	if r, _ := e.do(t, "POST", "/api/apps/libreta-prueba/libreta/"+strconv.FormatInt(nota.ID, 10)+"/discard", `{"reason":"x"}`, me, nil); r.StatusCode != 400 {
		t.Fatalf("a note is not a request: %d", r.StatusCode)
	}
	ajena, _ := e.store.AddRuta(ctx, store.RutaEntry{Kind: store.KindPendiente, Title: "de otra", Author: "p0dxD", App: "otra-app"})
	if r, _ := e.do(t, "POST", "/api/apps/libreta-prueba/libreta/"+strconv.FormatInt(ajena.ID, 10)+"/discard", `{"reason":"x"}`, me, nil); r.StatusCode != 404 {
		t.Fatalf("another app's entry: %d", r.StatusCode)
	}
	if r, body := e.do(t, "POST", base, `{"reason":""}`, me, nil); r.StatusCode != 200 || strings.Contains(body, `"discarded"`) {
		t.Fatalf("retomar: %d %s", r.StatusCode, body)
	}

	// The Ruta page keeps the app when it does not send it.
	r, body = e.do(t, "PUT", "/api/ruta/"+strconv.FormatInt(entry.ID, 10), `{"title":"Que el arreglo se dispare desde el botón","body":"","tags":[],"done":false}`, me, nil)
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &entry) != nil || entry.App != "libreta-prueba" {
		t.Fatalf("the app was lost on an edit: %d %s", r.StatusCode, body)
	}
}

// A pushed commit's «Ruta: N» links its branch to those entries (the app's
// or no app's, never another app's), even when the push is not run; the
// pull request's merge, from GitHub's webhook, is kept on the link.
func TestLibretaLinksFromGitHub(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	app := &store.App{Name: "libreta-sola", Repo: "p0dxD/libreta-sola", DefaultBranch: "main", Spec: sp}
	if err := e.store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	mia, _ := e.store.AddRuta(ctx, store.RutaEntry{Kind: store.KindPendiente, Title: "que se ligue sola", Author: "p0dxD", App: "libreta-sola"})
	otra, _ := e.store.AddRuta(ctx, store.RutaEntry{Kind: store.KindPendiente, Title: "de otra", Author: "p0dxD", App: "otra-app"})
	id := func(e store.RutaEntry) string { return strconv.FormatInt(e.ID, 10) }
	hook := func(event, body string) {
		t.Helper()
		if r, out := e.do(t, "POST", "/api/webhooks/github", body, nil, map[string]string{"X-GitHub-Event": event, "X-Hub-Signature-256": sign("whsec", body)}); r.StatusCode >= 300 {
			t.Fatalf("%s: %d %s", event, r.StatusCode, out)
		}
	}
	msg := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	hook("push", `{"ref":"refs/heads/se-liga","after":"`+strings.Repeat("d", 40)+`","repository":{"full_name":"p0dxD/libreta-sola"},"installation":{"id":1},
		"commits":[{"message":`+msg("Primero\n\nRuta: #"+id(mia)+", "+id(otra))+`},{"message":`+msg("Lo demás [skip ci]\n\nRuta: 99999")+`}],
		"head_commit":{"message":`+msg("Lo demás [skip ci]\n\nRuta: 99999")+`}}`)
	ramas, err := e.store.RutaBranches(ctx, []int64{mia.ID, otra.ID})
	if err != nil || len(ramas[mia.ID]) != 1 || ramas[mia.ID][0] != "se-liga" || len(ramas[otra.ID]) != 0 {
		t.Fatalf("linked from the trailer: %v %v", ramas, err)
	}
	hook("pull_request", `{"action":"closed","number":12,"repository":{"full_name":"p0dxD/libreta-sola"},"installation":{"id":1},
		"pull_request":{"merged":true,"merged_at":"2026-11-18T10:00:00Z","head":{"ref":"se-liga","sha":"`+strings.Repeat("d", 40)+`","repo":{"full_name":"p0dxD/libreta-sola"}}}}`)
	kept, err := e.store.RutaBranchesKept(ctx, []int64{mia.ID})
	if k := kept[mia.ID]["se-liga"]; err != nil || k.MergedAt == nil || k.MergedSHA != strings.Repeat("d", 40) || k.MergedAt.UTC().Hour() != 10 {
		t.Fatalf("the merge is kept: %+v %v", k, err)
	}
}
