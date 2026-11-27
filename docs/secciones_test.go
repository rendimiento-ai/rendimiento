package docs

import (
	"strings"
	"testing"
)

// The book's headings, in each language, with the page they are on and the
// anchor the book gives them.
func TestSecciones(t *testing.T) {
	busca := func(lang, ruta string) (Seccion, bool) {
		for _, s := range Secciones(lang) {
			if s.Ruta == ruta {
				return s, true
			}
		}
		return Seccion{}, false
	}
	en, ok := busca("en", "environment/deploy/#capacity")
	if !ok || en.Titulo != "Capacity" || en.Pagina == "" {
		t.Fatalf("the capacity section in English: %+v %v", en, ok)
	}
	es, ok := busca("es", "environment/deploy/#capacity")
	if !ok || es.Titulo != "Capacidad" {
		t.Fatalf("the capacity section in Spanish: %+v %v", es, ok)
	}
	for _, s := range Secciones("en") {
		if strings.HasPrefix(s.Ruta, "develop/") || strings.HasPrefix(s.Ruta, "reference/code-map") || strings.Contains(s.Titulo, "`") {
			t.Fatalf("a section that should not be: %+v", s)
		}
	}
	if len(Secciones("es")) < 100 {
		t.Fatalf("only %d sections", len(Secciones("es")))
	}
}
