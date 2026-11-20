package forja

import (
	"testing"
	"testing/fstest"
)

func TestOf(t *testing.T) {
	for repo, want := range map[string]string{
		"o/app":                        GitHub,
		"bitbucket.org/tienda/a":       Bitbucket,
		"gitlab.com/grupo/app":         GitLab,
		"git.example.com:8443/g/sub/p": GitLab,
	} {
		if got := Of(repo); got != want {
			t.Errorf("Of(%q) = %q, want %q", repo, got, want)
		}
	}
}

func TestBranchLinks(t *testing.T) {
	for _, tc := range []struct{ name, repo, branch string }{
		{GitHub, "o/app", "fix/encargo-7"},
		{GitLab, "git.example.com/grupo/app", "fix/encargo-7"},
		{Bitbucket, "bitbucket.org/tienda/a", "fix/encargo-7"},
	} {
		link := NewPullURL(tc.name, tc.repo, "", tc.branch)
		repo, branch, ok := BranchOfLink(link)
		if !ok || repo != tc.repo || branch != tc.branch {
			t.Errorf("%s: %s read as %q %q %v", tc.name, link, repo, branch, ok)
		}
		// A change into another branch is not one into the default one.
		if _, _, ok := BranchOfLink(NewPullURL(tc.name, tc.repo, "previas", tc.branch)); ok {
			t.Errorf("%s: a link into another branch was read", tc.name)
		}
	}
	for link, want := range map[string]string{
		"https://github.com/o/app/tree/rama":                 "o/app rama",
		"https://git.example.com/grupo/app/-/tree/rama":      "git.example.com/grupo/app rama",
		"https://bitbucket.org/tienda/a/src/rama":            "bitbucket.org/tienda/a rama",
		"https://example.com/otra/cosa":                      "",
		"http://github.com/o/app/pull/new/rama":              "",
		"https://rendimiento.example.com/apps/a/runs/3":      "",
		"https://bitbucket.org/tienda/a/pull-requests/new":   "",
		"https://git.example.com/g/a/-/merge_requests/new?x": "",
	} {
		repo, branch, ok := BranchOfLink(link)
		got := ""
		if ok {
			got = repo + " " + branch
		}
		if got != want {
			t.Errorf("%s: %q, want %q", link, got, want)
		}
	}
	if u := PullURL(GitLab, "gitlab.com/g/a", 7); u != "https://gitlab.com/g/a/-/merge_requests/7" {
		t.Error(u)
	}
	if u := PullURL(Bitbucket, "bitbucket.org/t/a", 7); u != "https://bitbucket.org/t/a/pull-requests/7" {
		t.Error(u)
	}
	if u := PullURL("", "o/a", 7); u != "https://github.com/o/a/pull/7" {
		t.Error(u)
	}
	if u := NewPullURL(GitHub, "o/a", "previas", "fix"); u != "https://github.com/o/a/compare/previas...fix?expand=1" {
		t.Error(u)
	}
}

func TestFilesOf(t *testing.T) {
	tree := fstest.MapFS{
		"a.txt":      {Data: []byte("hola")},
		"web/b.json": {Data: []byte("{}")},
		"grande.txt": {Data: []byte("0123456789")},
		"imagen.png": {Data: []byte{0x89, 'P', 0}},
	}
	got, err := FilesOf(tree, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || string(got["a.txt"]) != "hola" || string(got["web/b.json"]) != "{}" {
		t.Fatalf("files: %v", got)
	}
}
