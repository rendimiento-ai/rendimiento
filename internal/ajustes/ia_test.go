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

// What the platform's key may spend in all is dollars a month, none or
// more, and $5 when unset; it is written back without trailing zeros.
func TestAIPlatformCap(t *testing.T) {
	for in, want := range map[string]string{
		"5":     "",
		"15.5":  "",
		"0":     "",
		"-1":    "must be dollars a month",
		"cinco": "must be dollars a month",
		"Inf":   "must be dollars a month",
	} {
		_, err := Read(Values{AIPlatformCap: in}, "rendimiento-system", "")
		if (want == "" && err != nil) || (want != "" && (err == nil || !strings.Contains(err.Error(), want))) {
			t.Errorf("%q: %v, want %q", in, err, want)
		}
	}
	for in, want := range map[string]int64{"": 5_000_000, "16": 16_000_000, "2.5": 2_500_000, "0": 0} {
		if got := AIPlatformCapMicros(Values{AIPlatformCap: in}); got != want {
			t.Errorf("AIPlatformCapMicros(%q) = %d, want %d", in, got, want)
		}
	}
	for micros, want := range map[int64]string{16_000_000: "16", 2_500_000: "2.5", 0: "0"} {
		if got := Dollars(micros); got != want {
			t.Errorf("Dollars(%d) = %q, want %q", micros, got, want)
		}
	}
}
