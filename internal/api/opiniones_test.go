package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The comments endpoint answers only where señales are received, keeps a
// comment from the form (with its email) and one from an installation
// (with only the data its person saw), turns away the malformed, drops
// what robots send as if kept, and holds a sender to three an hour.
func TestRecibirOpinion(t *testing.T) {
	e := newEnv(t, false)
	mandar := func(body string, hdr map[string]string) (int, string) {
		t.Helper()
		r, b := e.do(t, "POST", "/api/senales/comentarios", body, nil, hdr)
		return r.StatusCode, b
	}
	forma := `{"texto":"¿Se puede con GitLab?","idioma":"es","nombre":"Ana","correo":"ana@example.com"}`
	if code, _ := mandar(forma, nil); code != 404 {
		t.Fatalf("not receiving: %d", code)
	}
	e.s.SenalesRecibir = true
	desde := map[string]string{"CF-Connecting-IP": "203.0.113.7"}
	if code, b := mandar(forma, desde); code != 201 || !strings.Contains(b, `"id":`) {
		t.Fatalf("from the form: %d %s", code, b)
	}
	ficha := strings.Repeat("ab", 32)
	app := `{"texto":"Me encanta el pie de página","datos":{"version":"0.2.2","modo":"local","motor":"docker","so":"linux","apps":["secreta"]},"ficha":"` + ficha + `"}`
	if code, b := mandar(app, map[string]string{"CF-Connecting-IP": "198.51.100.2"}); code != 201 {
		t.Fatalf("from an installation: %d %s", code, b)
	}
	for _, malo := range []string{
		`{"texto":"  "}`,
		`{"texto":"sin a quién responder"}`,
		`{"texto":"hola","correo":"no es un correo"}`,
		`{"texto":"hola","ficha":"corta"}`,
		`{"texto":"` + strings.Repeat("a", MaxOpinion+1) + `","correo":"ana@example.com"}`,
		`no es json`,
	} {
		if code, _ := mandar(malo, map[string]string{"CF-Connecting-IP": "192.0.2.9"}); code != 400 {
			t.Errorf("%.40s: %d, want 400", malo, code)
		}
	}
	if code, _ := mandar(`{"texto":"compre ya","correo":"bot@example.com","sitio":"https://spam.example"}`, desde); code != 204 {
		t.Fatalf("a robot: %d", code)
	}
	mandar(forma, desde)
	mandar(forma, desde)
	if code, _ := mandar(forma, desde); code != 429 {
		t.Fatalf("a fourth in the hour from one sender: %d", code)
	}

	got, err := e.store.Opiniones(context.Background(), 10)
	if err != nil || len(got) != 4 {
		t.Fatalf("kept: %d %v", len(got), err)
	}
	var deApp bool
	for _, c := range got {
		if c.Origen == "app" {
			deApp = true
			var d map[string]any
			if err := json.Unmarshal(c.Datos, &d); err != nil || d["version"] != "0.2.2" || d["so"] != "linux" || len(d) != 4 {
				t.Fatalf("an installation's data, only the known fields: %s", c.Datos)
			}
		}
		if strings.Contains(c.Texto, "compre") {
			t.Fatal("a robot's comment was kept")
		}
	}
	if !deApp {
		t.Fatal("the installation's comment is missing")
	}
}
