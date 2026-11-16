package docs

import "testing"

func TestPaginas(t *testing.T) {
	ps := Paginas()
	if len(ps) < 30 {
		t.Fatalf("%d pages", len(ps))
	}
	seen := map[string]string{}
	for _, p := range ps {
		seen[p.Ruta] = p.Titulo
	}
	if seen["start/quickstart/"] != "Quickstart" || seen["guide/spec/"] == "" {
		t.Fatalf("pages: %v", seen)
	}
}
