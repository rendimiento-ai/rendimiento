package platform

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

func TestResolveAddOnly(t *testing.T) {
	const base, ours, main = "base", "ours", "999999999999999999999999999999999999999" + "9"
	menu := "nav:\n  - Roadmap: roadmap.md\n  - Open source: open-source.md\n"
	f := &fakeGitHub{base: base, files: map[string]fstest.MapFS{
		base: {"mkdocs.yml": {Data: []byte(menu)}, "main.go": {Data: []byte("package main\n")}, "es.ts": {Data: []byte("{\n};\n")}},
		ours: {"mkdocs.yml": {Data: []byte(strings.Replace(menu, "  - Open", "  - Previews: previews.md\n  - Open", 1))},
			"main.go": {Data: []byte("package main\n")}, "es.ts": {Data: []byte("{\n  \"a\": \"a\",\n};\n")}},
		main: {"mkdocs.yml": {Data: []byte(strings.Replace(menu, "  - Open", "  - AI tests: ai-tests.md\n  - Open", 1))},
			"main.go": {Data: []byte("package main\n\n// changed on main only\n")}, "es.ts": {Data: []byte("{\n  \"b\": \"b\",\n};\n")}},
	}, changed: map[string][]string{
		base + "..." + ours: {"es.ts", "mkdocs.yml"},
		base + "..." + main: {"es.ts", "main.go", "mkdocs.yml", "gone.md"},
	}}
	p := &Platform{GitHub: f, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	app := &store.App{Name: "choques", Repo: "o/choques", DefaultBranch: "main"}
	run := &store.Run{SHA: ours, Branch: "previews"}

	sha, joined, conflicts, err := p.resolveAddOnly(context.Background(), app, run, "previews")
	if err != nil || sha == "" || len(conflicts) != 0 || strings.Join(joined, ",") != "es.ts,mkdocs.yml" {
		t.Fatalf("resolve = %q %v %v %v", sha, joined, conflicts, err)
	}
	got := f.merges[0]
	if string(got["mkdocs.yml"]) != "nav:\n  - Roadmap: roadmap.md\n  - AI tests: ai-tests.md\n  - Previews: previews.md\n  - Open source: open-source.md\n" {
		t.Fatalf("menu = %q", got["mkdocs.yml"])
	}
	if string(got["es.ts"]) != "{\n  \"b\": \"b\",\n  \"a\": \"a\",\n};\n" {
		t.Fatalf("translations = %q", got["es.ts"])
	}
	// Changed on main only: main's version; deleted on main: deleted.
	if string(got["main.go"]) != "package main\n\n// changed on main only\n" || strings.Join(f.removed[0], ",") != "gone.md" {
		t.Fatalf("main's own changes = %q %v", got["main.go"], f.removed[0])
	}

	// Both changed the same line: nothing is committed, and the file is named.
	f.files[ours]["mkdocs.yml"] = &fstest.MapFile{Data: []byte(strings.Replace(menu, "Roadmap", "Roadmap (ours)", 1))}
	f.files[main]["mkdocs.yml"] = &fstest.MapFile{Data: []byte(strings.Replace(menu, "Roadmap", "Roadmap (main)", 1))}
	sha, _, conflicts, err = p.resolveAddOnly(context.Background(), app, run, "previews")
	if err != nil || sha != "" || strings.Join(conflicts, ",") != "mkdocs.yml" || len(f.merges) != 1 {
		t.Fatalf("a real conflict = %q %v %v, %d merges", sha, conflicts, err, len(f.merges))
	}
}
