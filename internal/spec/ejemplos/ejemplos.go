// Package ejemplos holds rendimiento.yaml examples that every layer must
// accept: the spec's validation, the App object's schema in Kubernetes,
// render, and the local destination. Each layer's tests run them all, so a
// rule changed in one layer cannot quietly stay different in another. Add
// one here for every kind of app rendimiento supports.
package ejemplos

import (
	"embed"
	"io/fs"
	"sort"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

//go:embed *.yaml
var files embed.FS

// Ejemplo is one example, by its file name (without .yaml).
type Ejemplo struct {
	Nombre string
	Spec   *spec.Spec
	// Crudo is the file as written, for a repository that carries it.
	Crudo []byte
}

// Todos parses every example, defaulted, in name order.
func Todos() ([]Ejemplo, error) {
	names, err := fs.Glob(files, "*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var out []Ejemplo
	for _, n := range names {
		raw, err := files.ReadFile(n)
		if err != nil {
			return nil, err
		}
		s, err := spec.Parse(raw)
		if err != nil {
			return nil, err
		}
		s.Default()
		out = append(out, Ejemplo{Nombre: strings.TrimSuffix(n, ".yaml"), Spec: s, Crudo: raw})
	}
	return out, nil
}

//go:embed malos/*.yaml
var malos embed.FS

// Malo is an example every layer must refuse: its first line says the
// error, as "# error: <part of the message>".
type Malo struct {
	Nombre string
	Crudo  []byte
	Error  string
}

// Malos are the examples that must be refused, in name order.
func Malos() ([]Malo, error) {
	names, err := fs.Glob(malos, "malos/*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var out []Malo
	for _, n := range names {
		raw, err := malos.ReadFile(n)
		if err != nil {
			return nil, err
		}
		first, _, _ := strings.Cut(string(raw), "\n")
		out = append(out, Malo{Nombre: strings.TrimSuffix(strings.TrimPrefix(n, "malos/"), ".yaml"), Crudo: raw,
			Error: strings.TrimSpace(strings.TrimPrefix(first, "# error:"))})
	}
	return out, nil
}
