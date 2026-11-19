package platform

import (
	"reflect"
	"testing"
)

// A commit names Ruta entries in «Ruta: N» lines: with or without #,
// several comma-separated, in any case; anything else on the line, or a
// line that only mentions the Ruta, names none.
func TestRutaRefs(t *testing.T) {
	for msg, want := range map[string][]int64{
		"Arreglo\n\nRuta: 150\nCo-Authored-By: x":      {150},
		"Arreglo\n\nRuta: #150":                        {150},
		"Arreglo\n\nruta: 150, #151,152\nRuta: 150":    {150, 151, 152},
		"Ruta: la memoria compartida":                  nil,
		"Ruta: 150 es la nota":                         nil,
		"Ver la Ruta: 150":                             nil,
		"Arreglo\n\n  Ruta:   7  \r\nSigned-off-by: y": {7},
		"Ruta: 0": nil,
	} {
		if got := RutaRefs(msg); !reflect.DeepEqual(got, want) {
			t.Errorf("RutaRefs(%q) = %v, want %v", msg, got, want)
		}
	}
}
