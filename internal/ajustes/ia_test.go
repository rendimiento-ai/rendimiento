package ajustes

import (
	"strings"
	"testing"
)

// The local model's address must be an http(s) address, and its model a
// name without spaces.
func TestLocalModelSettings(t *testing.T) {
	for in, want := range map[string]string{
		"http://192.168.1.20:11434":  "",
		"https://ollama.example.com": "",
		"192.168.1.20:11434":         "must be an Ollama's address",
		"ftp://x":                    "must be an Ollama's address",
	} {
		_, err := Read(Values{AILocalURL: in}, "rendimiento-system", "")
		if (want == "" && err != nil) || (want != "" && (err == nil || !strings.Contains(err.Error(), want))) {
			t.Errorf("%q: %v, want %q", in, err, want)
		}
	}
	if _, err := Read(Values{AILocalModel: "qwen 3"}, "rendimiento-system", ""); err == nil {
		t.Error("a model's name with a space was accepted")
	}
}
