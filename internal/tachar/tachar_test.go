package tachar

import (
	"strings"
	"testing"
)

// A credential is found, and only its line is struck out.
func TestLineas(t *testing.T) {
	for _, s := range []string{
		"GITHUB_TOKEN=ghp_" + strings.Repeat("a", 30),
		"-----BEGIN RSA PRIVATE KEY-----",
		"password: Xk29fjQ02mzP77aLq",
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijklmno",
		PrefijoLlave + strings.Repeat("x", 44),
	} {
		if !Parece(s) {
			t.Errorf("not seen as a secret: %q", s)
		}
	}
	for _, s := range []string{"listening on :8080", "token: the one in the rendimiento-github Secret", "GET /health 200 3ms", "http://shop-api:8080/items"} {
		if Parece(s) {
			t.Errorf("seen as a secret: %q", s)
		}
	}
	if got := Lineas("connecting to postgresql://app:s3cretvalue@shop-postgres:5432/shop"); got != Omitida {
		t.Errorf("a URL with a password: %q", got)
	}
	got := Lineas("starting\npassword=Xk29fjQ02mzP77aLq\nready")
	if got != "starting\n"+Omitida+"\nready" {
		t.Fatalf("Lineas: %q", got)
	}
	if Lineas("nothing here") != "nothing here" {
		t.Fatal("changed a clean text")
	}
}
