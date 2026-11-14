package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/actualizar"
	"github.com/rendimiento-ai/rendimiento/internal/firma"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The platform's administrators see the published version, newer or not,
// and how this install takes it; a collaborator does not.
func TestPlatformPublished(t *testing.T) {
	e := newEnv(t, false)
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.1","body":"- **Fix:** algo","html_url":"https://example.com/r","assets":[{"name":"SHA256SUMS.sig"}]}`))
	}))
	defer gh.Close()
	e.s.Publicadas = &actualizar.Consultor{API: gh.URL}
	e.s.Version, e.s.Modo = "0.2.0", "local"
	if err := e.store.SetMember(context.Background(), store.DefaultOrg, "colabora", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}

	r, body := e.do(t, "GET", "/api/platform/published", "", e.session(t, "p0dxD"), nil)
	var got Published
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &got) != nil {
		t.Fatalf("published: %d %s", r.StatusCode, body)
	}
	if got.Version != "0.2.0" || got.How != "computer" || !got.Newer || !got.OnlyFix || got.Latest == nil || !got.Latest.Firmada {
		t.Fatalf("published = %s", body)
	}
	if r, _ := e.do(t, "GET", "/api/platform/published", "", e.session(t, "colabora"), nil); r.StatusCode != http.StatusForbidden {
		t.Fatalf("a collaborator: %d, want 403", r.StatusCode)
	}
	if got.CanInstall {
		t.Fatal("a cluster cannot install a program")
	}
}

// On a computer, the latest signed version is installed from the card and
// rendimiento restarts in it; a collaborator cannot, nor can another one.
func TestPlatformInstall(t *testing.T) {
	e := newEnv(t, false)
	priv, pub, _ := firma.Nueva()
	bin := []byte("rendimiento 0.2.1")
	sum := sha256.Sum256(bin)
	sums := []byte(hex.EncodeToString(sum[:]) + "  rendimiento-linux-" + runtime.GOARCH + "\n")
	sig, _ := firma.Firmar(priv, sums)
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			_, _ = w.Write([]byte(`{"tag_name":"v0.2.1","assets":[{"name":"SHA256SUMS.sig"}]}`))
		case strings.HasSuffix(r.URL.Path, "/v0.2.1/SHA256SUMS"):
			_, _ = w.Write(sums)
		case strings.HasSuffix(r.URL.Path, "/v0.2.1/SHA256SUMS.sig"):
			_, _ = w.Write([]byte(sig))
		case strings.HasSuffix(r.URL.Path, "/v0.2.1/rendimiento-linux-"+runtime.GOARCH):
			_, _ = w.Write(bin)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()
	dir := t.TempDir()
	ruta := filepath.Join(dir, "rendimiento")
	if err := os.WriteFile(ruta, []byte("rendimiento 0.2.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	salio := make(chan int, 1)
	e.s.Publicadas = &actualizar.Consultor{API: gh.URL}
	e.s.Version, e.s.Modo = "0.2.0", "local"
	e.s.Programa = &actualizar.Programa{Ruta: ruta, Datos: dir, Descargas: gh.URL, Publica: pub, Salir: func(c int) { salio <- c }}

	r, body := e.do(t, "GET", "/api/platform/published", "", e.session(t, "p0dxD"), nil)
	if r.StatusCode != 200 || !strings.Contains(body, `"canInstall":true`) {
		t.Fatalf("published: %d %s", r.StatusCode, body)
	}
	if r, _ := e.do(t, "POST", "/api/platform/install", `{"version":"0.2.0"}`, e.session(t, "p0dxD"), nil); r.StatusCode != http.StatusConflict {
		t.Fatalf("installing a version that is not the latest: %d", r.StatusCode)
	}
	if r, body := e.do(t, "POST", "/api/platform/install", `{"version":"0.2.1"}`, e.session(t, "p0dxD"), nil); r.StatusCode != http.StatusAccepted {
		t.Fatalf("install: %d %s", r.StatusCode, body)
	}
	select {
	case c := <-salio:
		if c != actualizar.CodigoReinicio {
			t.Fatalf("exit code %d", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rendimiento did not restart in the new version")
	}
	if got, _ := os.ReadFile(ruta); string(got) != "rendimiento 0.2.1" {
		t.Fatalf("program %q", got)
	}
}
