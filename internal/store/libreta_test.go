package store

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// What a libreta reads: an app's to-dos and decisions (not its notes, not
// another app's, not the archived ones), the branches each is worked on,
// and every encargo about the app with why it was cancelled.
func TestLibretaQueries(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	pedido, err := s.AddRuta(ctx, RutaEntry{Kind: KindPendiente, Title: "Que el arreglo se dispare desde el botón", Author: "p0dxD", App: "goconcalma"})
	if err != nil || pedido.App != "goconcalma" || pedido.Discarded != "" {
		t.Fatalf("add with app: %+v %v", pedido, err)
	}
	decision, _ := s.AddRuta(ctx, RutaEntry{Kind: KindDecision, Title: "Pruebas que solo avisan", Author: "claude", ByAgent: true, App: "goconcalma"})
	s.AddRuta(ctx, RutaEntry{Kind: KindNota, Title: "una nota", Author: "p0dxD", App: "goconcalma"})
	s.AddRuta(ctx, RutaEntry{Kind: KindPendiente, Title: "de otra", Author: "p0dxD", App: "stockpulse"})
	s.AddRuta(ctx, RutaEntry{Kind: KindPendiente, Title: "de ninguna", Author: "p0dxD"})
	viejo, _ := s.AddRuta(ctx, RutaEntry{Kind: KindPendiente, Title: "archivado", Author: "p0dxD", App: "goconcalma"})
	if err := s.ArchiveRuta(ctx, viejo.ID); err != nil {
		t.Fatal(err)
	}

	list, err := s.RutaOfApp(ctx, DefaultOrg, "goconcalma")
	if err != nil || len(list) != 2 || list[0].ID != decision.ID || list[1].ID != pedido.ID {
		t.Fatalf("entries of the app: %+v %v", list, err)
	}

	// Setting one aside, and taking it up again; the app can change or go.
	pedido.Discarded = "Maven no tiene lockfile"
	if pedido, err = s.UpdateRuta(ctx, pedido, "p0dxD"); err != nil || pedido.Discarded != "Maven no tiene lockfile" || pedido.App != "goconcalma" {
		t.Fatalf("discarded: %+v %v", pedido, err)
	}
	pedido.Discarded = ""
	if pedido, err = s.UpdateRuta(ctx, pedido, "p0dxD"); err != nil || pedido.Discarded != "" {
		t.Fatalf("taken up again: %+v %v", pedido, err)
	}
	var nulls int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ruta_entries WHERE discarded IS NULL AND id = $1`, pedido.ID).Scan(&nulls); err != nil || nulls != 1 {
		t.Fatalf("an empty reason is stored as NULL: %d %v", nulls, err)
	}

	for _, b := range []string{"oferta-y-pruebas-propias", "otra", "oferta-y-pruebas-propias", " "} {
		if err := s.LinkRutaBranch(ctx, pedido.ID, b, "claude"); err != nil {
			t.Fatal(err)
		}
	}
	ramas, err := s.RutaBranches(ctx, []int64{pedido.ID, decision.ID})
	if err != nil || len(ramas[pedido.ID]) != 2 || ramas[pedido.ID][0] != "oferta-y-pruebas-propias" || len(ramas[decision.ID]) != 0 {
		t.Fatalf("branches: %v %v", ramas, err)
	}

	e1, _ := s.CreateEncargo(ctx, Encargo{Title: "Pruebas propias", App: "goconcalma", CreatedBy: "p0dxD"})
	e2, _ := s.CreateEncargo(ctx, Encargo{Title: "Arreglo para Maven", App: "goconcalma", CreatedBy: "p0dxD"})
	s.CreateEncargo(ctx, Encargo{Title: "de otra", App: "stockpulse", CreatedBy: "p0dxD"})
	if _, err := s.CancelEncargo(ctx, e2.ID, "p0dxD", "no hace falta"); err != nil {
		t.Fatal(err)
	}
	encargos, err := s.EncargosOfApp(ctx, DefaultOrg, "goconcalma")
	if err != nil || len(encargos) != 2 || encargos[0].ID != e2.ID || encargos[0].CancelNote != "no hace falta" ||
		encargos[1].ID != e1.ID || encargos[1].CancelNote != "" {
		t.Fatalf("encargos of the app: %+v %v", encargos, err)
	}
}

// What links a branch to entries by itself (a commit's «Ruta: N», and an
// agent's run in its cargo), and what the libreta keeps of a branch: its
// merge and the release that carried it. The history keeps app and
// discarded too.
func TestLibretaLinksAndKeeps(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	add := func(kind, title, app string, cargo *int64) RutaEntry {
		t.Helper()
		e, err := s.AddRuta(ctx, RutaEntry{Kind: kind, Title: title, Author: "claude", ByAgent: cargo != nil, CargoID: cargo, App: app})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	ramas := func(id int64) []string {
		t.Helper()
		m, err := s.RutaBranches(ctx, []int64{id})
		if err != nil {
			t.Fatal(err)
		}
		return m[id]
	}

	// A commit's trailer: the entries of the app, or of none; never
	// another app's, an archived one or one that does not exist.
	mia, ninguna, otra, vieja := add(KindPendiente, "mía", "firme-uno", nil), add(KindNota, "de ninguna", "", nil),
		add(KindPendiente, "de otra", "firme-otra", nil), add(KindPendiente, "archivada", "firme-uno", nil)
	s.ArchiveRuta(ctx, vieja.ID)
	n, err := s.LinkRutaRefs(ctx, DefaultOrg, "firme-uno", "rama-firme", []int64{mia.ID, ninguna.ID, otra.ID, vieja.ID, 99999}, "github")
	if err != nil || n != 2 {
		t.Fatalf("trailer links: %d %v", n, err)
	}
	if n, _ := s.LinkRutaRefs(ctx, DefaultOrg, "firme-uno", "rama-firme", []int64{mia.ID}, "github"); n != 0 {
		t.Fatalf("linking twice adds nothing: %d", n)
	}
	if got := ramas(otra.ID); len(got) != 0 {
		t.Fatalf("another app's entry is not linked: %v", got)
	}
	if e, _ := s.Ruta(ctx, ninguna.ID); e.App != "" {
		t.Fatalf("the trailer never sets the app: %+v", e)
	}
	if n, _ := s.LinkRutaRefs(ctx, 4242, "firme-uno", "rama-firme", []int64{otra.ID}, "github"); n != 0 {
		t.Fatalf("another organization's entry is not linked: %d", n)
	}

	// An agent's cargo: what it wrote or changed, about the app it ran.
	k, _ := s.CreateAgentKey(ctx, DefaultOrg, "claude", "hfirme", RoleCollaborator, nil, "p0dxD", time.Now().Add(time.Hour))
	c, err := s.TakeCargo(ctx, k.ID, "probar")
	if err != nil {
		t.Fatal(err)
	}
	escrita := add(KindPendiente, "escrita en el cargo", "firme-uno", &c.ID)
	decidida := add(KindDecision, "decidida en el cargo", "firme-uno", &c.ID)
	nota := add(KindNota, "nota del cargo", "firme-uno", &c.ID)
	ajena := add(KindPendiente, "otra app, mismo cargo", "firme-otra", &c.ID)
	cambiada := add(KindPendiente, "cambiada en el cargo", "firme-uno", nil)
	intacta := add(KindPendiente, "no la tocó", "firme-uno", nil)
	descartada := add(KindPendiente, "descartada", "firme-uno", &c.ID)
	descartada.Discarded = "ya no"
	s.UpdateRuta(ctx, descartada, "claude")
	for _, a := range []AgentAction{
		{KeyID: k.ID, CargoID: &c.ID, Tool: "ruta_actualizar", Args: `{"id":` + itoa(cambiada.ID) + `,"hecho":true}`, OK: true},
		{KeyID: k.ID, CargoID: &c.ID, Tool: "ruta_actualizar", Args: `{"id":` + itoa(intacta.ID) + `}`, OK: false},
		{KeyID: k.ID, CargoID: &c.ID, Tool: "ejecucion_lanzar", Args: `{"app":"firme-uno","rama":"del-cargo"}`, OK: true},
		{KeyID: k.ID, CargoID: &c.ID, Tool: "ejecucion_lanzar", Args: `{"app":"firme-uno","rama":"main"}`, OK: false},
		{KeyID: k.ID, CargoID: &c.ID, Tool: "ejecucion_lanzar", Args: `{"app":"firme-uno","rama":"cortad…`, OK: true},
	} {
		if err := s.RecordAgentAction(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.LinkCargoEntries(ctx, c.ID, DefaultOrg, "firme-uno", "del-cargo", "claude"); err != nil || n != 3 {
		t.Fatalf("cargo entries: %d %v", n, err)
	}
	for _, e := range []RutaEntry{nota, ajena, intacta, descartada} {
		if got := ramas(e.ID); len(got) != 0 {
			t.Fatalf("%q is not linked: %v", e.Title, got)
		}
	}
	nueva := add(KindPendiente, "anotada después de lanzar", "firme-uno", &c.ID)
	if n, err := s.LinkCargoLaunches(ctx, c.ID, nueva.ID, "claude"); err != nil || n != 1 || ramas(nueva.ID)[0] != "del-cargo" {
		t.Fatalf("cargo launches: %d %v %v", n, err, ramas(nueva.ID))
	}
	if n, _ := s.LinkCargoLaunches(ctx, c.ID, ajena.ID, "claude"); n != 0 {
		t.Fatalf("a launch of another app is not linked: %d", n)
	}
	_ = escrita
	_ = decidida

	// Merged, then released by the first release built after it.
	merged := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	if n, err := s.MarkRutaMerged(ctx, DefaultOrg, "firme-uno", "rama-firme", "abc123", merged); err != nil || n != 2 {
		t.Fatalf("merged: %d %v", n, err)
	}
	if n, _ := s.MarkRutaMerged(ctx, DefaultOrg, "firme-uno", "rama-firme", "def456", time.Now()); n != 0 {
		t.Fatalf("the first merge seen is kept: %d", n)
	}
	if n, _ := s.MarkRutaReleased(ctx, DefaultOrg, "firme-uno", 6, merged.Add(-time.Minute), time.Now()); n != 0 {
		t.Fatalf("a release built before the merge does not carry it: %d", n)
	}
	if n, _ := s.MarkRutaReleased(ctx, DefaultOrg, "firme-uno", 7, merged.Add(time.Minute), time.Now()); n != 1 {
		t.Fatalf("released (the app's entry only): %d", n)
	}
	s.MarkRutaReleased(ctx, DefaultOrg, "firme-uno", 8, merged.Add(2*time.Minute), time.Now())
	kept, err := s.RutaBranchesKept(ctx, []int64{mia.ID, ninguna.ID, escrita.ID})
	if err != nil {
		t.Fatal(err)
	}
	if k := kept[mia.ID]["rama-firme"]; k.MergedSHA != "abc123" || k.MergedAt == nil || !k.MergedAt.Equal(merged) || k.Release != 7 || k.ReleasedAt == nil {
		t.Fatalf("kept: %+v", k)
	}
	if k := kept[ninguna.ID]["rama-firme"]; k.MergedAt == nil || k.Release != 0 {
		t.Fatalf("an entry about no app knows the merge, not the release: %+v", k)
	}
	if _, ok := kept[escrita.ID]; ok {
		t.Fatalf("nothing kept yet: %+v", kept[escrita.ID])
	}

	// What the libreta worked out fills only what is missing.
	if err := s.KeepRutaBranch(ctx, escrita.ID, "del-cargo", &merged, 0, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.KeepRutaBranch(ctx, escrita.ID, "del-cargo", nil, 9, &merged); err != nil {
		t.Fatal(err)
	}
	s.KeepRutaBranch(ctx, escrita.ID, "del-cargo", nil, 10, nil)
	kept, _ = s.RutaBranchesKept(ctx, []int64{escrita.ID})
	if k := kept[escrita.ID]["del-cargo"]; k.MergedAt == nil || k.Release != 9 || k.ReleasedAt == nil {
		t.Fatalf("kept from the libreta: %+v", k)
	}

	// The history keeps the app and why it was discarded.
	var app, discarded *string
	if err := s.pool.QueryRow(ctx, `SELECT app, discarded FROM ruta_history WHERE entry_id = $1 ORDER BY id DESC LIMIT 1`, descartada.ID).Scan(&app, &discarded); err != nil ||
		app == nil || *app != "firme-uno" || discarded != nil {
		t.Fatalf("history before discarding: %v %v %v", app, discarded, err)
	}
	descartada.Discarded, descartada.App = "", ""
	s.UpdateRuta(ctx, descartada, "p0dxD")
	if err := s.pool.QueryRow(ctx, `SELECT app, discarded FROM ruta_history WHERE entry_id = $1 ORDER BY id DESC LIMIT 1`, descartada.ID).Scan(&app, &discarded); err != nil ||
		discarded == nil || *discarded != "ya no" {
		t.Fatalf("history keeps the reason: %v %v", discarded, err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
