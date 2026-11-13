package spec

import (
	"strings"
	"testing"
)

func TestPreviewsValidate(t *testing.T) {
	base := "services:\n  - name: web\n    domain: propinas.example.com\n"
	for in, want := range map[string]string{
		base + "previews: {data: copy}":             "never gets production's data",
		base + "previews: {data: lots}":             "must be empty or seed",
		base + "previews: {data: seed}":             "must name one of the app's tasks",
		base + "previews: {data: seed, seed: nada}": "must name one of the app's tasks",
		base + "previews: {seed: datos}":            "only used with data: seed",
		base + "previews: {keep: 31}":               "1 to 30 days",
		base + "previews: {max: 6}":                 "1 to 5 copies",
		"services:\n  - name: web\npreviews: {}":    "need a service with a domain",
	} {
		if _, err := Parse([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %v, want %q", in, err, want)
		}
	}
	ok := base + "tasks:\n  - name: datos\n    image: node:22\n    command: npm run seed\npreviews: {data: seed, seed: datos, keep: 3, max: 2}"
	s, err := Parse([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if s.Previews.KeepDays() != 3 || s.Previews.Copies() != 2 || (*Previews)(nil).KeepDays() != 7 || (*Previews)(nil).Copies() != 3 {
		t.Fatalf("previews %+v", s.Previews)
	}
}

// The copy of a pull request answers at pr-<n>.<domain>, once, with a
// quarter of the requests, and drops what belongs to production only.
func TestPreviewOf(t *testing.T) {
	s, err := Parse([]byte(`services:
  - name: web
    domain: propinas.example.com
    aliases: [www.propinas.example.com]
    routes: [propinas.example.com/api]
    replicas: 3
    size: medium
    tlsSecret: propinas-tls
    volume: {existingClaim: datos-viejos, mount: /data}
    ingress: {name: viejo}
jobs:
  - name: limpiar
    schedule: "0 3 * * *"
    service: web
    command: [limpiar]
previews: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	p := PreviewOf(*s, 42)
	w := p.Services[0]
	if w.Domain != "pr-42.propinas.example.com" || len(w.Aliases) != 0 || w.Routes[0] != "pr-42.propinas.example.com/api" {
		t.Fatalf("hosts %q %q %q", w.Domain, w.Aliases, w.Routes)
	}
	if w.Replicas != 1 || w.TLSSecret != "" || w.Ingress.Name != "" || w.Volume.ExistingClaim != "" || w.Volume.Size != "1Gi" {
		t.Fatalf("service %+v", w)
	}
	r := ResourcesFor(w.Size, w.Resources)
	if r.CPURequest != "25m" || r.MemRequest != "64Mi" || r.CPULimit != "1" {
		t.Fatalf("resources %+v, want a quarter of medium's requests and its limits", r)
	}
	if len(p.Jobs) != 0 || p.Previews != nil || p.Verify == nil || !p.Verify.Disabled {
		t.Fatalf("copy spec %+v", p)
	}
	if s.Services[0].Domain != "propinas.example.com" || s.Services[0].Replicas != 3 || len(s.Jobs) != 1 {
		t.Fatal("PreviewOf changed the app's own spec")
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("the copy's spec is not valid: %v", err)
	}
}
