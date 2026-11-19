package github

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
)

// A Holder with an App whose webhook secret is secret, on the accounts given.
func forjaHolder(t *testing.T, secret string, accounts ...string) forja.Forja {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	a, err := New(Credentials{AppID: 7, PrivateKey: string(pemKey), WebhookSecret: secret, ClientID: "cliente"})
	if err != nil {
		t.Fatal(err)
	}
	h := &Holder{Accounts: accounts}
	h.Set(a)
	return h
}

func signed(secret, kind, body string) http.Header {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return http.Header{"X-Github-Event": {kind}, "X-Hub-Signature-256": {"sha256=" + hex.EncodeToString(mac.Sum(nil))}}
}

// Before setup the forja is not ready, signs nobody in and reads no
// webhook.
func TestForjaBeforeSetup(t *testing.T) {
	var f forja.Forja = &Holder{}
	if f.Name() != "github" || !errors.Is(f.Ready(), ErrNotConfigured) || f.Allows("ana") {
		t.Fatalf("name %q, ready %v, allows %v", f.Name(), f.Ready(), f.Allows("ana"))
	}
	if _, err := f.AuthorizeURL("s", "https://ci.example.com/cb"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("AuthorizeURL before setup: %v", err)
	}
	if _, err := f.ParseWebhook(signed("x", "push", "{}"), []byte("{}")); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("ParseWebhook before setup: %v", err)
	}
}

// A push comes through the forja as a neutral Push, only when it is signed.
func TestForjaWebhookPush(t *testing.T) {
	f := forjaHolder(t, "whsec", "forja-uno")
	body := `{"ref":"refs/heads/forja-rama","after":"abc123","installation":{"id":42},"repository":{"full_name":"forja-uno/app"},
		"head_commit":{"message":"arregla [skip ci]"},"commits":[{"message":"uno"},{"message":"Ruta: 102"}]}`
	if _, err := f.ParseWebhook(signed("otro", "push", body), []byte(body)); !errors.Is(err, forja.ErrBadSignature) {
		t.Fatalf("a wrong signature: %v", err)
	}
	if _, err := f.ParseWebhook(signed("whsec", "push", "{"), []byte("{")); !errors.Is(err, forja.ErrBadPayload) {
		t.Fatalf("bad JSON: %v", err)
	}
	ev, err := f.ParseWebhook(signed("whsec", "push", body), []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	p := ev.Push
	if ev.Kind != "push" || p == nil || ev.Change != nil {
		t.Fatalf("event %+v", ev)
	}
	if p.Installation != 42 || p.Repo != "forja-uno/app" || p.Branch != "forja-rama" || p.SHA != "abc123" || !p.SkipCI || fmt.Sprint(p.Messages) != "[uno Ruta: 102]" {
		t.Fatalf("push %+v", p)
	}
	if !f.Allows("Forja-Uno") || f.Allows("forja-dos") {
		t.Fatal("Allows should follow the App's accounts")
	}
	// A tag is not a branch: nothing to run.
	tag := `{"ref":"refs/tags/v1","after":"abc"}`
	if ev, err := f.ParseWebhook(signed("whsec", "push", tag), []byte(tag)); err != nil || ev.Push != nil {
		t.Fatalf("a tag: %+v %v", ev, err)
	}
}

// A pull request closed by a merge comes as a ChangeEvent.
func TestForjaWebhookChange(t *testing.T) {
	f := forjaHolder(t, "whsec")
	body := `{"action":"closed","number":9,"repository":{"full_name":"forja-uno/app"},
		"pull_request":{"merged":true,"merged_at":"2026-11-19T10:00:00Z","head":{"ref":"forja-rama","sha":"def456","repo":{"full_name":"forja-uno/app"}}}}`
	ev, err := f.ParseWebhook(signed("whsec", "pull_request", body), []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	c := ev.Change
	if ev.Push != nil || c == nil || c.Action != "closed" || c.Number != 9 || !c.Merged || c.MergedAt == nil || c.HeadRef != "forja-rama" || c.HeadSHA != "def456" || c.HeadRepo != "forja-uno/app" {
		t.Fatalf("change %+v", c)
	}
	// From a fork that is gone: no head repository.
	gone := `{"action":"opened","number":3,"repository":{"full_name":"forja-uno/app"},"pull_request":{"head":{"ref":"x","sha":"1","repo":null}}}`
	if ev, err := f.ParseWebhook(signed("whsec", "pull_request", gone), []byte(gone)); err != nil || ev.Change == nil || ev.Change.HeadRepo != "" {
		t.Fatalf("a gone fork: %+v %v", ev, err)
	}
}

// GitHub's errors answer the forja's questions.
func TestForjaErrors(t *testing.T) {
	wrapped := fmt.Errorf("read: %w", &APIError{Status: 404, Body: `{"message":"Not Found."}`})
	if !forja.IsNotFound(wrapped) || errors.Is(wrapped, forja.ErrConflict) {
		t.Fatal("a 404 is not found, not a conflict")
	}
	if !errors.Is(&APIError{Status: 409}, forja.ErrConflict) || forja.IsNotFound(&APIError{Status: 409}) {
		t.Fatal("a 409 is a conflict")
	}
	// A "Validation Failed" from GitHub says what it was about.
	err := &APIError{Status: 422, Body: `{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"No commits between main and vistazo"}]}`}
	if got := forja.Says(err); got != "Validation Failed: No commits between main and vistazo" {
		t.Fatalf("Says = %q", got)
	}
	field := &APIError{Status: 422, Body: `{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"invalid","field":"head"}]}`}
	if got := forja.Says(field); got != "Validation Failed: head invalid" {
		t.Fatalf("Says = %q", got)
	}
	if got := forja.Says(wrapped); got != "Not Found" {
		t.Fatalf("Says = %q", got)
	}
	if got := forja.Says(&APIError{Status: 500, Method: "GET", Path: "/x", Body: "oops"}); got != "github GET /x: 500 oops" {
		t.Fatalf("Says without a message = %q", got)
	}
}
