package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// When the inside runs a newer version, the window puts its own program of
// that version in place, only signed; an older or same inside changes
// nothing.
func TestSeguir(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	exe := []byte("ventana 0.2.1")
	sum := sha256.Sum256(exe)
	sums := []byte(hex.EncodeToString(sum[:]) + "  rendimiento-windows-amd64.exe\n")
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums))
	adentro := "0.2.1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.Header().Set("X-Rendimiento-Version", adentro)
		case strings.HasSuffix(r.URL.Path, "/v0.2.1/SHA256SUMS"):
			_, _ = w.Write(sums)
		case strings.HasSuffix(r.URL.Path, "/v0.2.1/SHA256SUMS.sig"):
			_, _ = w.Write([]byte(sig))
		case strings.HasSuffix(r.URL.Path, "/v0.2.1/rendimiento-windows-amd64.exe"):
			_, _ = w.Write(exe)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ruta := filepath.Join(t.TempDir(), "rendimiento.exe")
	if err := os.WriteFile(ruta, []byte("ventana 0.2.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &seguidor{version: "0.2.0", direccion: srv.URL, descargas: srv.URL, exe: ruta, publica: base64.StdEncoding.EncodeToString(pub)}
	ctx := context.Background()

	adentro = "0.2.0"
	if v, err := s.revisar(ctx); v != "" || err != nil {
		t.Fatalf("the same version inside: %q %v", v, err)
	}
	adentro = "0.2.1"
	if v, err := s.revisar(ctx); v != "0.2.1" || err != nil {
		t.Fatalf("a newer inside: %q %v", v, err)
	}
	if got, _ := os.ReadFile(ruta); string(got) != "ventana 0.2.1" {
		t.Fatalf("window program %q", got)
	}
	if got, _ := os.ReadFile(ruta + ".anterior"); string(got) != "ventana 0.2.0" {
		t.Fatalf("the window before %q", got)
	}

	// Another key: nothing changes.
	otra, _, _ := ed25519.GenerateKey(nil)
	s.publica, s.version = base64.StdEncoding.EncodeToString(otra), "0.2.0"
	if _, err := s.revisar(ctx); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("a release signed with another key: %v", err)
	}
	if masNueva("dev", "0.2.1") || !masNueva("0.2.0", "0.10.0") || masNueva("0.3.0", "0.2.9") {
		t.Fatal("masNueva")
	}
}
