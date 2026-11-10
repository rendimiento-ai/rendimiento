package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/escritorio/wsl"
)

// Every text exists in both languages, with the same values to fill.
func TestTextosCompletos(t *testing.T) {
	for clave, x := range textos {
		if x.en == "" || x.es == "" {
			t.Errorf("%s: missing a translation", clave)
		}
		if strings.Count(x.en, "%") != strings.Count(x.es, "%") {
			t.Errorf("%s: the two languages take different values", clave)
		}
	}
}

// Every key the code names is in the catalog, and every key of the
// catalog is named by the code (whole, or as "<step>." + its part).
func TestTextosUsados(t *testing.T) {
	archivos, _ := filepath.Glob("*.go")
	var codigo strings.Builder
	for _, a := range archivos {
		if a == "textos.go" || strings.HasSuffix(a, "_test.go") {
			continue
		}
		b, err := os.ReadFile(a)
		if err != nil {
			t.Fatal(err)
		}
		codigo.Write(b)
	}
	for _, m := range regexp.MustCompile(`\b[Tt]\((?:idioma, )?"([a-z.]+[a-z])"[,)]`).FindAllStringSubmatch(codigo.String(), -1) {
		if _, hay := textos[m[1]]; !hay {
			t.Errorf("%q is used and is not in the catalog", m[1])
		}
	}
	for clave := range textos {
		grupo, parte, _ := strings.Cut(clave, ".")
		entera := strings.Contains(codigo.String(), `"`+clave+`"`)
		armada := strings.Contains(codigo.String(), `"`+grupo+`."`) || strings.Contains(codigo.String(), `".`+parte+`"`)
		if !entera && !armada {
			t.Errorf("%q is in the catalog and nothing uses it", clave)
		}
	}
}

// Every problem the wsl package can report has its explanation.
func TestTextosDeProblemas(t *testing.T) {
	for _, clave := range []string{wsl.Reinicio, wsl.Virtualizacion, wsl.Descarga, wsl.Suma, wsl.Usuario, wsl.Arranque, wsl.Cancelado, wsl.Otro} {
		if _, hay := textos["problema."+clave]; !hay {
			t.Errorf("problema.%s has no text", clave)
		}
	}
}

func TestIdioma(t *testing.T) {
	for _, c := range []struct {
		preferidos []string
		idioma     string
	}{
		{[]string{"es-MX", "en-US"}, "es"},
		{[]string{"en-US", "es-MX"}, "en"},
		{[]string{"fr-FR", "es-ES"}, "es"},
		{[]string{"de-DE"}, "en"},
		{nil, "en"},
	} {
		if g := idiomaDe(c.preferidos); g != c.idioma {
			t.Errorf("%v: %s, want %s", c.preferidos, g, c.idioma)
		}
	}
}
