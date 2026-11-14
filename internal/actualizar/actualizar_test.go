package actualizar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestComparar(t *testing.T) {
	for _, c := range []struct {
		actual, nueva   string
		masNueva, fixes bool
	}{
		{"0.2.0", "v0.2.1", true, true},
		{"0.2.0", "0.3.0", true, false},
		{"0.2.9", "0.10.0", true, false},
		{"0.2.1", "0.2.1", false, false},
		{"0.3.0", "0.2.5", false, false},
		{"dev", "0.2.1", false, false},
		{"0.2.0", "0.2.1-rc1", true, true},
		{"1.0.0", "2.0.0", true, false},
	} {
		m, f := Comparar(c.actual, c.nueva)
		if m != c.masNueva || f != c.fixes {
			t.Errorf("Comparar(%q, %q) = %v, %v; want %v, %v", c.actual, c.nueva, m, f, c.masNueva, c.fixes)
		}
	}
}

func TestConsultor(t *testing.T) {
	var veces atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		veces.Add(1)
		if r.URL.Path != "/repos/rendimiento-ai/rendimiento/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.1","body":"- **Fix:** algo","html_url":"https://github.com/x","published_at":"2026-11-14T00:42:20Z",
			"assets":[{"name":"SHA256SUMS"},{"name":"SHA256SUMS.sig"}]}`))
	}))
	defer srv.Close()
	c := &Consultor{API: srv.URL}
	p, err := c.Ultima(context.Background(), false)
	if err != nil || p.Version != "0.2.1" || !p.Firmada || p.Notas == "" {
		t.Fatalf("Ultima = %+v, %v", p, err)
	}
	if _, _ = c.Ultima(context.Background(), false); veces.Load() != 1 {
		t.Fatalf("asked GitHub %d times; the answer is kept a day", veces.Load())
	}
	if _, _ = c.Ultima(context.Background(), true); veces.Load() != 2 {
		t.Fatal("refrescar did not ask again")
	}
}
