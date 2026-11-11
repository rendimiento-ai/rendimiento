package platform

import "testing"

func TestKindOf(t *testing.T) {
	for _, c := range []struct {
		branch string
		labels []string
		want   string
	}{
		{"main", []string{"bug"}, KindMain}, // the default branch, whatever its labels
		{"pagina-lenta", []string{"bug"}, KindFix},
		{"pagina-lenta", []string{"Type: Bug"}, KindFix},
		{"pagina-lenta", []string{"kind/feature"}, KindFeature},
		{"pagina-lenta", []string{"good first issue", "enhancement"}, KindFeature},
		{"fix/login", []string{"enhancement"}, KindFeature}, // the label says more than the name
		{"fix/login", nil, KindFix},
		{"arreglo-cache", nil, KindFix},
		{"Feature/Mapa", nil, KindFeature},
		{"funcion-mapa", nil, KindFeature},
		{"release/1.2", nil, KindRelease},
		{"changelog-v0.1.9", nil, KindRelease},
		{"renovate/react-19.x", nil, KindDeps},
		{"pagina-lenta", []string{"documentation"}, KindOther},
		{"fixture-nueva", nil, KindOther}, // "fix" alone is not a prefix
	} {
		if got := kindOf(c.branch, "main", c.labels); got != c.want {
			t.Errorf("kindOf(%q, %v) = %q, want %q", c.branch, c.labels, got, c.want)
		}
	}
}
