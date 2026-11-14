// Package firma signs and checks rendimiento's releases (decision 138):
// each release's SHA256SUMS is signed with Ed25519 when it is published, and
// rendimiento installs a downloaded version only when that signature checks
// out against the public key it carries (Publica), and the file's sum is
// the one SHA256SUMS lists. The private key lives only in GitHub's secrets
// (FIRMA_VERSIONES); losing it means a new key and a release that carries
// it, never a way around the check.
package firma

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Publica is the public key releases are signed with, base64 (its private
// half is GitHub's secret FIRMA_VERSIONES). Empty means this build cannot
// check signatures, so it installs nothing.
var Publica = "c09LwLCIFNQQwM2eVm7c+Cj0loc+Of3ElzKTOT/g5vo="

// ErrSinLlave: this build has no public key to check with.
var ErrSinLlave = errors.New("this build of rendimiento has no key to check releases with")

// Firmar signs data with a private key (base64, as Nueva writes it) and
// returns the signature, base64.
func Firmar(privada string, data []byte) (string, error) {
	k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privada))
	if err != nil || len(k) != ed25519.PrivateKeySize {
		return "", errors.New("the private key is not an Ed25519 key in base64")
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(k), data)), nil
}

// Revisar checks a signature (base64) of data against a public key
// (base64; "" means Publica).
func Revisar(publica string, data []byte, firma string) error {
	if publica == "" {
		publica = Publica
	}
	if publica == "" {
		return ErrSinLlave
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publica))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("the public key is not an Ed25519 key in base64")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(firma))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
		return errors.New("the release's signature does not check out: it was not signed by rendimiento's key")
	}
	return nil
}

// Nueva makes a key pair: the private key (for GitHub's secrets) and the
// public one (for Publica), both base64.
func Nueva() (privada, publica string, err error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(priv), base64.StdEncoding.EncodeToString(pub), nil
}

// Suma is the sum SHA256SUMS lists for a file name.
func Suma(sums []byte, nombre string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		campos := strings.Fields(sc.Text())
		if len(campos) == 2 && strings.TrimPrefix(campos[1], "*") == nombre {
			return strings.ToLower(campos[0]), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS does not list %s", nombre)
}

// Comprobar checks a downloaded file: SHA256SUMS signed by rendimiento's
// key, and the file's sum the one it lists for nombre.
func Comprobar(publica string, sums []byte, firma, nombre string, archivo []byte) error {
	if err := Revisar(publica, sums, firma); err != nil {
		return err
	}
	want, err := Suma(sums, nombre)
	if err != nil {
		return err
	}
	got := sha256.Sum256(archivo)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("%s does not match its sum in SHA256SUMS", nombre)
	}
	return nil
}
