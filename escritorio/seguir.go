package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// The window follows the rendimiento inside it (decision 138): the inside is
// updated from the Version card, by the platform's administrators; when it
// runs a newer version than the window, the window downloads its own program
// of that same version, checks it as rendimiento checks its releases, and
// puts it in its own place. The new window opens next time.

// llavePublica checks releases; it is internal/firma.Publica (a test of
// that package keeps the two the same).
const llavePublica = "c09LwLCIFNQQwM2eVm7c+Cj0loc+Of3ElzKTOT/g5vo="

// seguidor keeps the window's program at the inside's version.
type seguidor struct {
	version   string // the window's own
	direccion string // where rendimiento answers: http://localhost:8080
	descargas string // a release's files: …/releases/download (+ /v<version>)
	exe       string // the window's program
	http      *http.Client
	publica   string // "": llavePublica
}

// adentro is the version rendimiento runs inside ("" when it does not say).
func (s *seguidor) adentro(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.direccion, "/")+"/healthz", nil)
	if err != nil {
		return ""
	}
	res, err := s.cliente().Do(req)
	if err != nil {
		return ""
	}
	res.Body.Close()
	return res.Header.Get("X-Rendimiento-Version")
}

func (s *seguidor) cliente() *http.Client {
	if s.http != nil {
		return s.http
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

// revisar puts the window's program of the inside's version in place, when
// the inside is newer; it says which version it put ("" when none).
func (s *seguidor) revisar(ctx context.Context) (string, error) {
	nueva := strings.TrimPrefix(s.adentro(ctx), "v")
	if nueva == "" || !masNueva(s.version, nueva) {
		return "", nil
	}
	base := strings.TrimRight(s.descargas, "/") + "/v" + nueva
	const nombre = "rendimiento-windows-amd64.exe"
	sums, err := s.bajar(ctx, base+"/SHA256SUMS")
	if err != nil {
		return "", err
	}
	sig, err := s.bajar(ctx, base+"/SHA256SUMS.sig")
	if err != nil {
		return "", fmt.Errorf("the release is not signed, so the window stays as it is: %w", err)
	}
	exe, err := s.bajar(ctx, base+"/"+nombre)
	if err != nil {
		return "", err
	}
	if err := comprobar(s.publica, sums, string(sig), nombre, exe); err != nil {
		return "", err
	}
	nuevo := s.exe + ".nuevo"
	if err := os.WriteFile(nuevo, exe, 0o755); err != nil {
		return "", err
	}
	// Windows lets a running program be renamed, not overwritten.
	_ = os.Remove(s.exe + ".anterior")
	if err := os.Rename(s.exe, s.exe+".anterior"); err != nil {
		return "", err
	}
	if err := os.Rename(nuevo, s.exe); err != nil {
		_ = os.Rename(s.exe+".anterior", s.exe)
		return "", err
	}
	return nueva, nil
}

func (s *seguidor) bajar(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.cliente().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 512<<20))
}

// comprobar checks a download as internal/firma.Comprobar does: SHA256SUMS
// signed by rendimiento's key, and the file's sum the one it lists.
func comprobar(publica string, sums []byte, firma, nombre string, archivo []byte) error {
	if publica == "" {
		publica = llavePublica
	}
	pub, err := base64.StdEncoding.DecodeString(publica)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("the public key is not an Ed25519 key in base64")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(firma))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), sums, sig) {
		return errors.New("the release's signature does not check out")
	}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		campos := strings.Fields(sc.Text())
		if len(campos) == 2 && strings.TrimPrefix(campos[1], "*") == nombre {
			got := sha256.Sum256(archivo)
			if hex.EncodeToString(got[:]) != strings.ToLower(campos[0]) {
				return fmt.Errorf("%s does not match its sum in SHA256SUMS", nombre)
			}
			return nil
		}
	}
	return fmt.Errorf("SHA256SUMS does not list %s", nombre)
}

// masNueva says whether nueva is a newer version than actual (three
// numbers each; a build that is not, "dev", follows nothing).
func masNueva(actual, nueva string) bool {
	a, okA := tresNumeros(actual)
	b, okB := tresNumeros(nueva)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if b[i] != a[i] {
			return b[i] > a[i]
		}
	}
	return false
}

func tresNumeros(v string) (n [3]int, ok bool) {
	v, _, _ = strings.Cut(strings.TrimPrefix(strings.TrimSpace(v), "v"), "-")
	partes := strings.Split(v, ".")
	if len(partes) != 3 {
		return n, false
	}
	for i, p := range partes {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return n, false
		}
		n[i] = x
	}
	return n, true
}
