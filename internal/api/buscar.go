package api

// The top bar's search finds more than apps, runs and pages: every place and
// setting of rendimiento (by its name and by words people type for it, in
// English and Spanish) and every section of the book, each with where to go
// (the place, and the book's page about it). It is the helper's map and the
// book's headings, so a new page or section is found without more work. No
// AI: the browser filters this index as one types.

import (
	"net/http"
	"strings"

	"github.com/rendimiento-ai/rendimiento/docs"
	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// LugarBuscable is a place of the app the search can open, in the reader's
// language: its address, its name, what is there, the words it is found by,
// and the book's page about it (an address under Libro, "" when none).
type LugarBuscable struct {
	Ruta   string `json:"ruta"`
	Nombre string `json:"nombre"`
	Que    string `json:"que"`
	Claves string `json:"claves,omitempty"`
	Libro  string `json:"libro,omitempty"`
}

// IndiceBuscable is what the search finds besides apps and runs: the places,
// the book's sections, and where the book is served in this language.
type IndiceBuscable struct {
	Lugares   []LugarBuscable `json:"lugares"`
	Secciones []docs.Seccion  `json:"secciones"`
	Libro     string          `json:"libro"`
}

func (s *Server) buscarRoutes(auth func(role, pattern string, h func(http.ResponseWriter, *http.Request, string))) {
	auth(store.MemberGuest, "GET /api/buscar", s.getBuscar)
}

func (s *Server) getBuscar(w http.ResponseWriter, r *http.Request, _ string) {
	writeJSON(w, indiceBuscable(idiomaDe(r)))
}

// indiceBuscable is the index in a language. A place that needs an app's
// name or a run's number ({app}, {id}) is left out: the apps and runs the
// search finds open those.
func indiceBuscable(lang string) IndiceBuscable {
	ix := IndiceBuscable{Secciones: docs.Secciones(lang), Libro: agentes.LibroURL(lang)}
	for _, l := range agentes.Lugares {
		if strings.Contains(l.Ruta, "{") {
			continue
		}
		b := LugarBuscable{Ruta: l.Ruta, Nombre: l.Nombre, Que: l.Que, Claves: l.Claves, Libro: l.Libro}
		if lang == "es" && l.NombreEs != "" {
			b.Nombre, b.Que = l.NombreEs, l.QueEs
		}
		ix.Lugares = append(ix.Lugares, b)
	}
	return ix
}
