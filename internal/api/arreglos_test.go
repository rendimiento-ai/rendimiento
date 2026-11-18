package api

import (
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// A recipe fix is added to rendimiento.yaml as text: at the end of its
// fixes: list (keeping comments and the keys after it), or as a new list.
func TestAddFixYAML(t *testing.T) {
	f := spec.Fix{Name: "npm-web", Recipe: "npm", Path: "web"}
	for name, c := range map[string]struct{ in, want string }{
		"no list": {"# The shop.\nservices:\n  - name: web\n    path: web\n",
			"# The shop.\nservices:\n  - name: web\n    path: web\n\n# Commands that regenerate files when they no longer match (rendimiento's declared fixes).\nfixes:\n  - name: npm-web\n    recipe: npm\n    path: web\n"},
		"a list, then a key": {"services:\n  - name: web\nfixes:\n  - name: modulos # go.sum\n    recipe: go\n\nobserve: true\n",
			"services:\n  - name: web\nfixes:\n  - name: modulos # go.sum\n    recipe: go\n  - name: npm-web\n    recipe: npm\n    path: web\n\nobserve: true\n"},
		"a list without indent": {"services:\n- name: web\nfixes:\n- name: modulos\n  recipe: go\n",
			"services:\n- name: web\nfixes:\n- name: modulos\n  recipe: go\n- name: npm-web\n  recipe: npm\n  path: web\n"},
	} {
		got, err := addFixYAML([]byte(c.in), f)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s:\n%s\nwant\n%s", name, got, c.want)
		}
	}
	if _, err := addFixYAML([]byte("services: [{name: web}]\nfixes: [{name: a, recipe: go}]\n"), f); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Fatalf("a flow list: %v", err)
	}
}

// A fix's name is its recipe and folder, not taken by anything else.
func TestFixName(t *testing.T) {
	sp := spec.Spec{Services: []spec.Service{{Name: "npm-web"}}}
	if n := fixName("npm", "web", nil, sp); n != "npm-web-2" {
		t.Fatalf("got %s", n)
	}
	if n := fixName("go", ".", nil, spec.Spec{}); n != "go" {
		t.Fatalf("got %s", n)
	}
	if n := fixName("pnpm", "apps/Mobile_App", nil, spec.Spec{}); n != "pnpm-apps-mobile-app" {
		t.Fatalf("got %s", n)
	}
}
