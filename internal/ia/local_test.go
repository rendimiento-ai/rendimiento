package ia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The local model saved on the AI page wins over the environment's, and an
// Ollama is asked which models it holds before it is saved.
func TestLocalModel(t *testing.T) {
	g := &Gateway{LocalURL: "http://env:11434"}
	if url, model := g.LocalNow(); url != "http://env:11434" || model != DefaultLocalModel {
		t.Fatalf("from the environment: %q %q", url, model)
	}
	g.Local = func() (string, string) { return "http://saved:11434", "llama3.2" }
	if url, model := g.LocalNow(); url != "http://saved:11434" || model != "llama3.2" {
		t.Fatalf("saved: %q %q", url, model)
	}
	g.Local = func() (string, string) { return "", "" }
	if url, _ := g.LocalNow(); url != "" {
		t.Fatalf("none saved or set: %q", url)
	}

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3.5:4b"},{"name":"llama3.2:latest"}]}`))
	}))
	defer ollama.Close()
	got, err := ProbeLocal(context.Background(), ollama.Client(), ollama.URL+"/")
	if err != nil || strings.Join(got, ",") != "qwen3.5:4b,llama3.2:latest" {
		t.Fatalf("models %v %v", got, err)
	}
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	if _, err := ProbeLocal(context.Background(), other.Client(), other.URL); err == nil || !strings.Contains(err.Error(), "not as an Ollama does") {
		t.Fatalf("not an Ollama: %v", err)
	}
}
