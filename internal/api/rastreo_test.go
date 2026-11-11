package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The tag is written only from an address and a website id that are what
// they should be.
func TestTrackerTag(t *testing.T) {
	id := "00000000-1111-2222-3333-444444444444"
	want := `<script defer src="https://umami.example.com/script.js" data-website-id="` + id + `"></script>`
	if got := TrackerTag("https://umami.example.com/", id); got != want {
		t.Fatalf("tag: %s", got)
	}
	for _, c := range [][2]string{{"", id}, {"https://umami.example.com", ""}, {"umami.example.com", id}, {"https://umami.example.com", `x" onload="alert(1)`}} {
		if got := TrackerTag(c[0], c[1]); got != "" {
			t.Fatalf("%q %q: %s", c[0], c[1], got)
		}
	}
}

// With a tracker, every page of the UI carries its tag; the assets do not
// change, and without one the page is the built one.
func TestUITracker(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("<html><head><title>r</title></head><body></body></html>")}, "assets/app.js": {Data: []byte("js")}}
	tag := TrackerTag("https://umami.example.com", "00000000-1111-2222-3333-444444444444")
	get := func(s *Server, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.ui().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	s := &Server{UI: ui, Tracker: tag}
	for _, p := range []string{"/", "/apps/shop", "/index.html"} {
		w := get(s, p)
		if !strings.Contains(w.Body.String(), tag+"</head>") || w.Header().Get("Cache-Control") != "no-cache" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: %v %s", p, w.Header(), w.Body.String())
		}
	}
	if w := get(s, "/assets/app.js"); w.Body.String() != "js" {
		t.Fatalf("asset: %s", w.Body.String())
	}
	if w := get(&Server{UI: ui}, "/"); strings.Contains(w.Body.String(), "script") {
		t.Fatalf("without a tracker: %s", w.Body.String())
	}
}
