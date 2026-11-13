package platform

import "testing"

func TestSaysSkipCI(t *testing.T) {
	for msg, want := range map[string]bool{
		"Encargo 25: trabajo sin terminar [skip ci]": true,
		"Fix the build [CI SKIP]":                    true,
		"wip [no ci]\n\nmore":                        true,
		"skip ci, but not in brackets":               false,
		"An app can have its own list":               false,
	} {
		if got := SaysSkipCI(msg); got != want {
			t.Errorf("SaysSkipCI(%q) = %v, want %v", msg, got, want)
		}
	}
}
