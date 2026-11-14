package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/actualizar"
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
}
