package pgprueba

import "testing"

func TestConBase(t *testing.T) {
	for _, c := range []struct{ dsn, want string }{
		{"postgres://u:p%40ss@db.example.com:5432/rendimiento?sslmode=disable&synchronous_commit=off",
			"postgres://u:p%40ss@db.example.com:5432/rendimiento_prueba_1?sslmode=disable&synchronous_commit=off"},
		{"postgresql://localhost", "postgresql://localhost/rendimiento_prueba_1"},
		{"host=localhost dbname=rendimiento", "host=localhost dbname=rendimiento dbname='rendimiento_prueba_1'"},
	} {
		got, err := conBase(c.dsn, "rendimiento_prueba_1")
		if err != nil || got != c.want {
			t.Errorf("conBase(%q) = %q, %v; want %q", c.dsn, got, err, c.want)
		}
	}
	if got := quoteIdent(`a"b`); got != `"a""b"` {
		t.Errorf("quoteIdent = %s", got)
	}
}
