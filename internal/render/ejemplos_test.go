package render

import (
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/spec/ejemplos"
)

// Every example that runs something renders.
func TestEjemplos(t *testing.T) {
	todos, err := ejemplos.Todos()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range todos {
		if !e.Spec.Runs() {
			continue
		}
		images, urls := map[string]string{}, map[string]string{}
		for _, s := range e.Spec.Services {
			images[s.Name] = "registry.example.lan:5000/" + e.Nombre + "-" + s.Name + "@sha256:1"
		}
		// The controller resolves the services an app needs (serviceURLs).
		for _, ref := range NeedRefs(*e.Spec) {
			urls[ref] = "http://" + ref + ":8000"
		}
		if _, err := Render(Input{App: "ej-" + e.Nombre, Spec: *e.Spec, Images: images, ServiceURLs: urls}, DefaultOptions()); err != nil {
			t.Errorf("%s: %v", e.Nombre, err)
		}
		// An app with previews renders its copy too: its own namespace,
		// labeled with the pull request, at pr-<number>.<domain>.
		if e.Spec.Previews == nil {
			continue
		}
		copia := spec.PreviewOf(*e.Spec, 7)
		out, err := Render(Input{App: "ej-" + e.Nombre + "-pr-7", Preview: 7, Spec: copia, Images: images, ServiceURLs: urls}, DefaultOptions())
		if err != nil {
			t.Errorf("%s, preview: %v", e.Nombre, err)
			continue
		}
		if out.Namespace.Labels[LabelPreview] != "7" {
			t.Errorf("%s, preview: namespace labels %v", e.Nombre, out.Namespace.Labels)
		}
		for _, in := range out.Ingresses {
			for _, rule := range in.Spec.Rules {
				if !strings.HasPrefix(rule.Host, "pr-7.") {
					t.Errorf("%s, preview: host %s", e.Nombre, rule.Host)
				}
			}
		}
		for _, d := range out.Deployments {
			if *d.Spec.Replicas != 1 {
				t.Errorf("%s, preview: %s has %d replicas", e.Nombre, d.Name, *d.Spec.Replicas)
			}
		}
	}
}
