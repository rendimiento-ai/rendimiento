package docs

import (
	"embed"
	"io/fs"
	"regexp"
	"strings"
	"sync"
)

//go:embed content/*.md content/*/*.md
var contenido embed.FS

// Seccion is one heading of the book, in one language: its text, the title
// of its page, and its address under the book's URL with its anchor
// ("environment/deploy/#capacity"). The top bar's search finds them.
type Seccion struct {
	Titulo string `json:"titulo"`
	Pagina string `json:"pagina"`
	Ruta   string `json:"ruta"`
}

var (
	encabezado = regexp.MustCompile(`^(#{1,3})\s+(.+?)\s*(?:\{#([a-z0-9_-]+)\})?\s*$`)
	cerca      = regexp.MustCompile("`|\\*\\*|\\*|\\[|\\]\\([^)]*\\)")
	porIdioma  sync.Map // lang → []Seccion
)

// Secciones are the book's headings in a language ("es" reads the .es.md
// pages, and the English one where a page has none), in the order of its
// files. Pages about developing rendimiento and its code map are left out:
// the search is for people using it.
func Secciones(lang string) []Seccion {
	if v, ok := porIdioma.Load(lang); ok {
		return v.([]Seccion)
	}
	var out []Seccion
	_ = fs.WalkDir(contenido, "content", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".es.md") {
			return nil
		}
		ruta := strings.TrimSuffix(strings.TrimPrefix(path, "content/"), ".md")
		if strings.HasPrefix(ruta, "develop/") || ruta == "reference/code-map" {
			return nil
		}
		archivo := path
		if lang == "es" {
			if _, err := fs.Stat(contenido, strings.TrimSuffix(path, ".md")+".es.md"); err == nil {
				archivo = strings.TrimSuffix(path, ".md") + ".es.md"
			}
		}
		b, err := fs.ReadFile(contenido, archivo)
		if err != nil {
			return nil
		}
		dir := strings.TrimSuffix(ruta, "index") // index pages are their folder
		if dir != "" && !strings.HasSuffix(dir, "/") {
			dir += "/"
		}
		pagina := ""
		dentro := false // inside a fenced block, where a # is a comment
		for _, linea := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(linea), "```") {
				dentro = !dentro
				continue
			}
			m := encabezado.FindStringSubmatch(linea)
			if dentro || m == nil {
				continue
			}
			texto := strings.TrimSpace(cerca.ReplaceAllString(m[2], ""))
			if len(m[1]) == 1 {
				if pagina == "" {
					pagina = texto
				}
				out = append(out, Seccion{Titulo: texto, Pagina: texto, Ruta: dir})
				continue
			}
			ancla := m[3]
			if ancla == "" {
				ancla = slug(texto)
			}
			out = append(out, Seccion{Titulo: texto, Pagina: pagina, Ruta: dir + "#" + ancla})
		}
		return nil
	})
	porIdioma.Store(lang, out)
	return out
}

var noSlug = regexp.MustCompile(`[^\p{L}\p{N}\s_-]`)

// slug is the anchor the book's toc makes of a heading without its own.
func slug(s string) string {
	s = strings.ToLower(noSlug.ReplaceAllString(s, ""))
	return strings.Join(strings.Fields(s), "-")
}
