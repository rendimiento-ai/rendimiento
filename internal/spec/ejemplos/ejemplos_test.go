package ejemplos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// Every example, and the complete example in the book, is valid.
func TestValidos(t *testing.T) {
	todos, err := Todos()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range todos {
		if err := e.Spec.Validate(); err != nil {
			t.Errorf("%s: %v", e.Nombre, err)
		}
	}
	book, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "content", "guide", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(book), "```yaml\n")
	block, _, ok2 := strings.Cut(rest, "```")
	if !ok || !ok2 {
		t.Fatal("no yaml example in the book's spec page")
	}
	s, err := spec.Parse([]byte(block))
	if err != nil {
		t.Fatalf("the book's complete example: %v", err)
	}
	s.Default()
	if err := s.Validate(); err != nil {
		t.Fatalf("the book's complete example: %v", err)
	}
}

// Every bad example is refused, with the error its first line says.
func TestMalos(t *testing.T) {
	malos, err := Malos()
	if err != nil {
		t.Fatal(err)
	}
	if len(malos) == 0 {
		t.Fatal("no bad examples")
	}
	for _, m := range malos {
		_, err := spec.Parse(m.Crudo)
		switch {
		case m.Error == "":
			t.Errorf("%s: its first line must be # error: <the message>", m.Nombre)
		case err == nil:
			t.Errorf("%s: accepted, want %q", m.Nombre, m.Error)
		case !strings.Contains(err.Error(), m.Error):
			t.Errorf("%s: %v, want %q", m.Nombre, err, m.Error)
		}
	}
}

// The secrets an app reads, whatever reads them (a service whole, by key or
// as files, its own tests by key), are the ones its copies get from their
// preview variants: the copy's spec reads the same names.
func TestCopySecretNames(t *testing.T) {
	todos, err := Todos()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range todos {
		if e.Nombre != "vistas-previas-con-secretos" {
			continue
		}
		want := "certificados,correo,pagos,usuario-prueba"
		if got := strings.Join(e.Spec.SecretNames(), ","); got != want {
			t.Fatalf("SecretNames = %s, want %s", got, want)
		}
		if got := strings.Join(spec.PreviewOf(*e.Spec, 3).SecretNames(), ","); got != want {
			t.Fatalf("the copy's SecretNames = %s, want %s", got, want)
		}
		if got := strings.Join(spec.ShortCopyOf(*e.Spec, 41).SecretNames(), ","); got != want {
			t.Fatalf("the short copy's SecretNames = %s, want %s", got, want)
		}
		return
	}
	t.Fatal("no example vistas-previas-con-secretos")
}
