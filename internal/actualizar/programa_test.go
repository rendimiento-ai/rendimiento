package actualizar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/firma"
)

// A signed release takes the program's place and the old one is kept; a
// version that keeps failing to start goes back by itself; a person goes
// back only down; an unsigned or changed release is never installed.
func TestPrograma(t *testing.T) {
	priv, pub, _ := firma.Nueva()
	bin := []byte("rendimiento 0.2.1")
	s := sha256.Sum256(bin)
	sums := []byte(hex.EncodeToString(s[:]) + "  rendimiento-linux-arm64\n")
	sig, _ := firma.Firmar(priv, sums)
	firmada := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/v0.2.1/SHA256SUMS"):
			_, _ = w.Write(sums)
		case strings.HasSuffix(r.URL.Path, "/v0.2.1/SHA256SUMS.sig") && firmada:
			_, _ = w.Write([]byte(sig))
		case strings.HasSuffix(r.URL.Path, "/v0.2.1/rendimiento-linux-arm64"):
			_, _ = w.Write(bin)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	ruta := filepath.Join(dir, "rendimiento")
	if err := os.WriteFile(ruta, []byte("rendimiento 0.2.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	salidas := 0
	p := &Programa{Ruta: ruta, Datos: dir, Descargas: srv.URL, Publica: pub, Arch: "arm64", Salir: func(int) { salidas++ }, Sana: time.Hour}
	ctx := context.Background()

	firmada = false
	if err := p.Instalar(ctx, "0.2.0", "0.2.1", "ana"); err == nil || !strings.Contains(err.Error(), "not signed") {
		t.Fatalf("an unsigned release: %v", err)
	}
	firmada = true
	if err := p.Instalar(ctx, "0.2.1", "0.2.1", "ana"); err == nil {
		t.Fatal("installed a version that is not newer")
	}
	if err := p.Instalar(ctx, "0.2.0", "0.2.1", "ana"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(ruta); string(got) != "rendimiento 0.2.1" || p.Anterior() != "0.2.0" {
		t.Fatalf("program %q, previous %q", got, p.Anterior())
	}

	// The new version starts and fails, MaxArranques times: back to 0.2.0.
	for i := 0; i < MaxArranques; i++ {
		if m := p.AlArrancar("0.2.1"); m == nil || m.De != "0.2.0" || m.A != "0.2.1" || m.Regreso {
			t.Fatalf("start %d: %+v", i+1, m)
		}
	}
	if salidas != 0 {
		t.Fatal("went back too soon")
	}
	p.AlArrancar("0.2.1")
	if got, _ := os.ReadFile(ruta); string(got) != "rendimiento 0.2.0" || salidas != 1 {
		t.Fatalf("after %d failed starts: program %q, exits %d", MaxArranques+1, got, salidas)
	}
	if m := p.AlArrancar("0.2.0"); m == nil || !m.Regreso || m.De != "0.2.1" || m.A != "0.2.0" || m.Quien != "automatic" {
		t.Fatalf("the old one, started again: %+v", m)
	}

	// A person goes back only down, to the version kept.
	if err := p.Regresar("0.2.0", "ana"); err == nil {
		t.Fatal("went back with nothing kept")
	}
	if err := p.Instalar(ctx, "0.2.0", "0.2.1", "ana"); err != nil {
		t.Fatal(err)
	}
	if err := p.Regresar("0.2.1", "ana"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(ruta); string(got) != "rendimiento 0.2.0" {
		t.Fatalf("after going back: %q", got)
	}

	// A changed program does not match its signed sum.
	bin = []byte("something else")
	if err := p.Instalar(ctx, "0.2.0", "0.2.1", "ana"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("a changed program: %v", err)
	}
}
