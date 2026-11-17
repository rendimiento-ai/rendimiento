package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A person decides each finding of the lookout: an encargo with the
// evidence, a Ruta entry (a to-do, or a note for a fact), or set aside;
// once, and only an administrator of the platform.
func TestVigiaFindingsArePeoplesToDecide(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	admin := e.session(t, "p0dxD")
	if r, body := e.do(t, "GET", "/api/vigia", "", admin, nil); r.StatusCode != 200 || strings.Contains(body, `"revision"`) || !strings.Contains(body, `"encendido":true`) {
		t.Fatalf("before any review: %d %s", r.StatusCode, body)
	}
	if r, _ := e.do(t, "POST", "/api/vigia/revisar", "", admin, nil); r.StatusCode != 503 {
		t.Fatalf("review without a lookout: %d", r.StatusCode)
	}
	id, err := e.store.EmpezarRevision(ctx, time.Now().Add(-7*24*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.TerminarRevision(ctx, store.VigiaRevision{ID: id, Estado: store.RevisionHecha, Hallazgos: []store.VigiaHallazgo{
		{Clave: "paso-decide:tienda/web:test", Titulo: "web:test decides how long runs take", Evidencia: "42 runs · always the last", Accionable: true, App: "tienda"},
		{Clave: "ruta-lenta:GET /api/apps", Titulo: "GET /api/apps is slow", Evidencia: "12 times · 2 s", Accionable: true},
		{Clave: "pagina-sin-uso:/mercado", Titulo: "Nobody opened Mercado in 14 days", Evidencia: "usage fact · no action suggested"},
	}}); err != nil {
		t.Fatal(err)
	}
	var v VigiaVista
	_, body := e.do(t, "GET", "/api/vigia", "", admin, nil)
	if err := json.Unmarshal([]byte(body), &v); err != nil || v.Revision == nil || len(v.Revision.Hallazgos) != 3 {
		t.Fatalf("the review: %s", body)
	}
	if _, body := e.do(t, "GET", "/api/vigia/nuevos", "", admin, nil); !strings.Contains(body, `"nuevos":3`) {
		t.Fatalf("new: %s", body)
	}
	h := v.Revision.Hallazgos
	path := func(i int, what string) string { return fmt.Sprintf("/api/vigia/hallazgos/%d/%s", h[i].ID, what) }

	// Into an encargo, with its evidence and who accepted it.
	r, body := e.do(t, "POST", path(0, "encargo"), "", admin, nil)
	if r.StatusCode != 200 {
		t.Fatalf("encargo: %d %s", r.StatusCode, body)
	}
	var got store.VigiaHallazgo
	_ = json.Unmarshal([]byte(body), &got)
	if got.Estado != store.HallazgoEncargo || got.EncargoID == nil || got.DecididoPor != "p0dxD" {
		t.Fatalf("decided = %+v", got)
	}
	enc, err := e.store.Encargo(ctx, *got.EncargoID)
	if err != nil || enc.Title != h[0].Titulo || !strings.Contains(enc.Body, "42 runs") || !strings.Contains(enc.Body, "p0dxD") || enc.CreatedBy != "p0dxD" || enc.ByAgent {
		t.Fatalf("the encargo = %+v %v", enc, err)
	}
	// Once.
	if r, _ := e.do(t, "POST", path(0, "ruta"), "", admin, nil); r.StatusCode != 409 {
		t.Fatalf("decided twice: %d", r.StatusCode)
	}
	// A fact suggests no work: never an encargo, a note in the Ruta.
	if r, _ := e.do(t, "POST", path(2, "encargo"), "", admin, nil); r.StatusCode != 400 {
		t.Fatalf("a fact into an encargo: %d", r.StatusCode)
	}
	r, body = e.do(t, "POST", path(2, "ruta"), "", admin, nil)
	_ = json.Unmarshal([]byte(body), &got)
	if r.StatusCode != 200 || got.RutaID == nil {
		t.Fatalf("ruta: %d %s", r.StatusCode, body)
	}
	entries, _ := e.store.SearchRuta(ctx, store.DefaultOrg, "", "vigia", 10)
	if len(entries) != 1 || entries[0].Kind != store.KindNota || entries[0].Author != "p0dxD" || !strings.Contains(entries[0].Body, "usage fact") {
		t.Fatalf("the Ruta entry = %+v", entries)
	}
	// Set aside: remembered, and the bell has nothing left.
	if r, _ := e.do(t, "POST", path(1, "descartar"), "", admin, nil); r.StatusCode != 200 {
		t.Fatalf("set aside: %d", r.StatusCode)
	}
	if dec, _ := e.store.VigiaDecididos(ctx, time.Now()); len(dec) != 3 {
		t.Fatalf("remembered = %+v", dec)
	}
	if _, body := e.do(t, "GET", "/api/vigia/nuevos", "", admin, nil); !strings.Contains(body, `"nuevos":0`) {
		t.Fatalf("new after deciding: %s", body)
	}

	// Its settings.
	if r, _ := e.do(t, "PUT", "/api/vigia", `{"encendido": false, "topeDolares": 500}`, admin, nil); r.StatusCode != 400 {
		t.Fatalf("a cap too high: %d", r.StatusCode)
	}
	if r, body := e.do(t, "PUT", "/api/vigia", `{"encendido": false, "topeDolares": 0.25}`, admin, nil); r.StatusCode != 200 || !strings.Contains(body, `"topeMicros":250000`) || !strings.Contains(body, `"encendido":false`) {
		t.Fatalf("settings: %d %s", r.StatusCode, body)
	}
}
