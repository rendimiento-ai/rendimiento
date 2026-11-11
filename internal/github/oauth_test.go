package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// On a computer GitHub cannot reach, the App gets no webhook (the platform
// asks for new commits instead); elsewhere it keeps it.
func TestManifestLocal(t *testing.T) {
	for _, base := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://rendimiento.localhost"} {
		m := Manifest("rendimiento", base)
		if _, ok := m["hook_attributes"]; ok || len(m["default_events"].([]string)) != 0 {
			t.Errorf("%s: %v", base, m)
		}
	}
	m := Manifest("rendimiento", "https://rendimiento.example.com")
	hook, ok := m["hook_attributes"].(map[string]any)
	if !ok || hook["url"] != "https://rendimiento.example.com/api/webhooks/github" {
		t.Errorf("a public address keeps its webhook: %v", m)
	}
}

// GitHub sends the person back to the platform after they choose the
// repositories, on this computer too.
func TestManifestComesBack(t *testing.T) {
	for _, base := range []string{"http://localhost:8080", "https://rendimiento.example.com"} {
		if m := Manifest("rendimiento", base); m["setup_url"] != base {
			t.Errorf("%s: setup_url is %v", base, m["setup_url"])
		}
	}
}

// With GITHUB_API_URL (APIBase), the App is created at that address and not
// at GitHub's: how an installed rendimiento is tested against a stand-in.
func TestAPIBase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app-manifests/abc/conversions" {
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"id": 7, "slug": "de-prueba"}`)
	}))
	defer srv.Close()
	old := APIBase
	APIBase = srv.URL
	defer func() { APIBase = old }()
	c, err := ConvertManifest(context.Background(), "", "abc")
	if err != nil || c.AppID != 7 || c.Slug != "de-prueba" {
		t.Fatalf("credentials %+v, err %v", c, err)
	}
	if got := (&App{}).base(); got != srv.URL {
		t.Errorf("the App asks %s, want %s", got, srv.URL)
	}
}
