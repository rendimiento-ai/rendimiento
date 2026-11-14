package firma

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestFirma(t *testing.T) {
	priv, pub, err := Nueva()
	if err != nil {
		t.Fatal(err)
	}
	binario := []byte("rendimiento, the program")
	s := sha256.Sum256(binario)
	sums := []byte(hex.EncodeToString(s[:]) + "  rendimiento-linux-arm64\n0000  rendimiento-linux-amd64\n")
	sig, err := Firmar(priv, sums)
	if err != nil {
		t.Fatal(err)
	}
	if err := Comprobar(pub, sums, sig, "rendimiento-linux-arm64", binario); err != nil {
		t.Fatalf("a good release: %v", err)
	}
	if err := Comprobar(pub, sums, sig, "rendimiento-linux-amd64", binario); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("a file that is not the listed one: %v", err)
	}
	cambiadas := append([]byte(nil), sums...)
	cambiadas[0] ^= 1
	if err := Comprobar(pub, cambiadas, sig, "rendimiento-linux-arm64", binario); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("SHA256SUMS changed after signing: %v", err)
	}
	_, otra, _ := Nueva()
	if err := Revisar(otra, sums, sig); err == nil {
		t.Fatal("another key's check passed")
	}
	if err := Revisar("", sums, sig); !errors.Is(err, ErrSinLlave) && Publica == "" {
		t.Fatalf("no key in this build: %v", err)
	}
	if _, err := Suma(sums, "rendimiento-windows-amd64.exe"); err == nil {
		t.Fatal("a file SHA256SUMS does not list")
	}
}

// The window of rendimiento for Windows (escritorio/, a module of its own)
// carries a copy of the key: it must be this one.
func TestLlaveDeLaVentana(t *testing.T) {
	src, err := os.ReadFile("../../escritorio/seguir.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `const llavePublica = "`+Publica+`"`) {
		t.Fatal("escritorio/seguir.go's llavePublica is not internal/firma.Publica")
	}
}
