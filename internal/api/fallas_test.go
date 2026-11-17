package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The UI's error pages reach the Problems page: grouped by kind and page,
// with where the person came from, every field checked and bounded, and
// nothing of who saw it, neither in the problem nor in the bitácora.
func TestFallasUI(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	if r, _ := e.do(t, "POST", "/api/fallas", `{"kind":"not_found","path":"/apps/nada"}`, nil, nil); r.StatusCode != 401 {
		t.Fatalf("without a session: %d", r.StatusCode)
	}
	me := e.session(t, "p0dxD")
	ok := `{"kind":"not_found","path":"/apps/nada/runs/88","from":"/universe","version":"0.2.3"}`
	for i := 0; i < 2; i++ {
		if r, body := e.do(t, "POST", "/api/fallas", ok, me, nil); r.StatusCode != 204 {
			t.Fatalf("a report: %d %s", r.StatusCode, body)
		}
	}
	// Another run of the same missing app counts as the same problem.
	if r, _ := e.do(t, "POST", "/api/fallas", `{"kind":"not_found","path":"/apps/nada/runs/89","from":"/universe"}`, me, nil); r.StatusCode != 204 {
		t.Fatalf("another run: %d", r.StatusCode)
	}

	for name, body := range map[string]string{
		"unknown kind":        `{"kind":"robado","path":"/x"}`,
		"no kind":             `{"path":"/x"}`,
		"no path":             `{"kind":"not_found"}`,
		"relative path":       `{"kind":"not_found","path":"apps/x"}`,
		"another site":        `{"kind":"not_found","path":"//example.com/x"}`,
		"a query":             `{"kind":"not_found","path":"/x?token=1"}`,
		"a space":             `{"kind":"not_found","path":"/x y"}`,
		"a control character": `{"kind":"not_found","path":"/x\u0007"}`,
		"too long":            `{"kind":"not_found","path":"/` + strings.Repeat("a", 300) + `"}`,
		"bad from":            `{"kind":"not_found","path":"/x","from":"https://example.com"}`,
		"bad version":         `{"kind":"not_found","path":"/x","version":"<script>"}`,
		"bad code":            `{"kind":"crashed","path":"/x","code":"zz top"}`,
		"not JSON":            `kind=not_found`,
	} {
		if r, b := e.do(t, "POST", "/api/fallas", body, me, nil); r.StatusCode != 400 {
			t.Errorf("%s: %d %s, want 400", name, r.StatusCode, b)
		}
	}

	// A page that failed drawing: its error and stack, cut and kept printable.
	crash, _ := json.Marshal(FallaUI{Kind: "crashed", Path: "/ruta", Version: "0.2.3", Code: "a81f",
		Message: "TypeError: x is undefined\x1b[31m" + strings.Repeat("é", 400), Stack: "at Ruta\nat Main\n" + strings.Repeat("s", 2000)})
	if r, body := e.do(t, "POST", "/api/fallas", string(crash), me, nil); r.StatusCode != 204 {
		t.Fatalf("a crash: %d %s", r.StatusCode, body)
	}

	list, err := e.store.ListProblems(ctx, "", time.Now().Add(-time.Hour), true, 100)
	if err != nil {
		t.Fatal(err)
	}
	var nf, cr *store.Problem
	for i, p := range list {
		if strings.Contains(p.Message+p.Detail+p.Attrs+p.Component+p.App, "p0dxD") {
			t.Errorf("who saw it is kept: %+v", p)
		}
		switch p.Message {
		case "page not found":
			nf = &list[i]
		case "a page failed to show":
			cr = &list[i]
		}
	}
	if nf == nil || nf.Count != 3 || nf.Level != "WARN" || nf.Component != "ui" || nf.App != "" || !strings.HasPrefix(nf.Detail, "/apps/nada/runs/") {
		t.Fatalf("not found: %+v", nf)
	}
	if cr == nil || cr.Level != "ERROR" || !strings.Contains(cr.Detail, "TypeError: x is undefined") || strings.Contains(cr.Detail, "\x1b") ||
		!strings.Contains(cr.Attrs, "code=a81f") || !strings.Contains(cr.Attrs, "version=0.2.3") || len(cr.Detail) > 2100 {
		t.Fatalf("crash: %+v", cr)
	}
	// A problem keeps the attributes of the last time it happened: where that one came from.
	if !strings.Contains(nf.Attrs, "from=/universe") {
		t.Fatalf("where from: %q", nf.Attrs)
	}

	// Nothing of it in the bitácora.
	rows, err := e.store.ListarBitacora(ctx, store.DefaultOrg, store.FiltroBitacora{Limite: 500})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range rows {
		if strings.Contains(b.Accion, "fallas") {
			t.Fatalf("in the bitácora: %+v", b)
		}
	}

	// A session reports a bounded number at a time.
	otra := e.session(t, "p0dxD")
	limited := false
	for i := 0; i < fallasPorVentana+1; i++ {
		r, _ := e.do(t, "POST", "/api/fallas", `{"kind":"forbidden","path":"/apps/otra"}`, otra, nil)
		if r.StatusCode == 429 {
			limited = i == fallasPorVentana
			break
		}
	}
	if !limited {
		t.Fatalf("no limit after %d reports", fallasPorVentana)
	}
}

// What the browser sends is checked as untrusted text (no database needed).
func TestFallaTexto(t *testing.T) {
	for p, want := range map[string]bool{"/": true, "/apps/x": true, "/apps/%20x": true, "": false, "x": false, "//x": false, "/a b": false, "/a\\b": false, "/a#b": false} {
		if rutaValida(p) != want {
			t.Errorf("rutaValida(%q) = %v", p, !want)
		}
	}
	if got := texto("a\x00b\nc  ", 10, false); got != "a b c" {
		t.Errorf("texto: %q", got)
	}
	if got := texto("a\nb", 10, true); got != "a\nb" {
		t.Errorf("texto with lines: %q", got)
	}
	if got := texto(strings.Repeat("é", 5), 3, false); got != "ééé" {
		t.Errorf("texto cut: %q", got)
	}
	var r ritmoFallas
	now := time.Now()
	for i := 0; i < fallasPorVentana; i++ {
		if !r.deja("s", now) {
			t.Fatalf("refused the %dth", i+1)
		}
	}
	if r.deja("s", now) || !r.deja("otra", now) || !r.deja("s", now.Add(fallasVentana+time.Second)) {
		t.Fatal("the window")
	}
}
