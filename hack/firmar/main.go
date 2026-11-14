// Command firmar signs a release's SHA256SUMS (decision 138).
//
//	go run ./hack/firmar -nueva <file>   makes a key pair: the private key goes
//	                                     to <file> (mode 0600), never to the
//	                                     screen; the public one is printed,
//	                                     for internal/firma.Publica
//	go run ./hack/firmar <file>          signs <file> with FIRMA_VERSIONES
//	                                     (the private key) into <file>.sig
//
// The binarios workflow runs the second with the FIRMA_VERSIONES secret.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rendimiento-ai/rendimiento/internal/firma"
)

func main() {
	nueva := flag.Bool("nueva", false, "make a key pair: the private key to the file, the public one printed")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: firmar [-nueva] <file>")
		os.Exit(2)
	}
	archivo := flag.Arg(0)
	if *nueva {
		priv, pub, err := firma.Nueva()
		if err != nil {
			fail(err)
		}
		f, err := os.OpenFile(archivo, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			fail(err)
		}
		if _, err := fmt.Fprintln(f, priv); err != nil {
			fail(err)
		}
		if err := f.Close(); err != nil {
			fail(err)
		}
		fmt.Printf("private key: %s (put it in GitHub's secret FIRMA_VERSIONES, then delete the file)\npublic key:  %s\n", archivo, pub)
		return
	}
	priv := os.Getenv("FIRMA_VERSIONES")
	if priv == "" {
		fail(fmt.Errorf("FIRMA_VERSIONES (the private key) is not set"))
	}
	data, err := os.ReadFile(archivo)
	if err != nil {
		fail(err)
	}
	sig, err := firma.Firmar(priv, data)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(archivo+".sig", []byte(sig+"\n"), 0o644); err != nil {
		fail(err)
	}
	fmt.Println("signed:", archivo+".sig")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "firmar:", err)
	os.Exit(1)
}
