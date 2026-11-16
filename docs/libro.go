// Package docs carries the book's table of contents into the program, so
// the platform's AI can point people to the page that answers them (the
// comments' classifier, decision 145). The book itself is served apart.
package docs

import (
	_ "embed"
	"regexp"
	"strings"
)

//go:embed mkdocs.yml
var mkdocs string

// Pagina is one page of the book: its title in the table of contents and
// its address under the book's URL ("guide/ai/").
type Pagina struct {
	Titulo, Ruta string
}

var entrada = regexp.MustCompile(`^\s*- ([^:]+): ([a-z0-9_./-]+)\.md\s*$`)

// Paginas are the book's titled pages, in the order of its contents.
func Paginas() []Pagina {
	var out []Pagina
	nav := false
	for _, line := range strings.Split(mkdocs, "\n") {
		switch {
		case strings.HasPrefix(line, "nav:"):
			nav = true
			continue
		case nav && line != "" && !strings.HasPrefix(line, " "):
			return out // the next top-level key
		}
		if m := entrada.FindStringSubmatch(line); nav && m != nil {
			out = append(out, Pagina{Titulo: strings.TrimSpace(m[1]), Ruta: m[2] + "/"})
		}
	}
	return out
}
