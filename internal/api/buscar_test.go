package api

import (
	"strings"
	"testing"
)

// The search's index: the places in the reader's language with the words
// they are found by and their book page, none that needs an app's name, and
// the book's sections.
func TestIndiceBuscable(t *testing.T) {
	es := indiceBuscable("es")
	var llaves *LugarBuscable
	for i, l := range es.Lugares {
		if strings.Contains(l.Ruta, "{") {
			t.Fatalf("a place that needs an app: %+v", l)
		}
		if l.Ruta == "/environment/llaves" {
			llaves = &es.Lugares[i]
		}
	}
	if llaves == nil || llaves.Nombre != "Llaves" || !strings.Contains(llaves.Claves, "keys") || llaves.Libro == "" {
		t.Fatalf("the keys, in Spanish: %+v", llaves)
	}
	if !strings.HasPrefix(es.Libro, "https://") || len(es.Secciones) == 0 {
		t.Fatalf("the book: %q, %d sections", es.Libro, len(es.Secciones))
	}
	if en := indiceBuscable("en"); en.Lugares[0].Nombre != "Home" {
		t.Fatalf("in English: %+v", en.Lugares[0])
	}
}
