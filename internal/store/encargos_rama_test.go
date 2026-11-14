package store

import "testing"

func TestRamaDeEncargo(t *testing.T) {
	run := int64(9)
	for _, c := range []struct {
		e    Encargo
		want string
	}{
		{Encargo{ID: 25, Title: "Cambiar la configuración de la plataforma desde rendimiento"}, "feature/encargo-25-cambiar-la-configuracion-de-la"},
		{Encargo{ID: 25, Title: "YC 9: multiusuario (org_id en cada ruta)"}, "feature/encargo-25-yc-9-multiusuario-org-id-en-cada-ruta"},
		{Encargo{ID: 25, Title: "Añadir «Quién entra»"}, "feature/encargo-25-anadir-quien-entra"},
		{Encargo{ID: 25, Title: "¡¿?!"}, "feature/encargo-25"},
		{Encargo{ID: 31, Title: "Arreglar la ejecución #1276", RunID: &run}, "fix/encargo-31-arreglar-la-ejecucion-1276"},
	} {
		if got := RamaDeEncargo(c.e); got != c.want {
			t.Errorf("RamaDeEncargo(%q) = %q, want %q", c.e.Title, got, c.want)
		}
	}
}

func TestEncargoOfBranch(t *testing.T) {
	for branch, want := range map[string]int64{
		"fix/encargo-31-arreglar": 31, "feature/encargo-25": 25, "encargo-12-saludar": 12, "encargo-7": 7,
		"propina-redonda": 0, "fix/otra-cosa": 0, "encargos-viejos": 0,
	} {
		if got := EncargoOfBranch(branch); got != want {
			t.Errorf("EncargoOfBranch(%q) = %d, want %d", branch, got, want)
		}
	}
}
