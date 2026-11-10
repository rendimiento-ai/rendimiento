package github

import "testing"

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
