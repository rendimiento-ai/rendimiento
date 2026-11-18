package spec

import (
	"reflect"
	"testing"
)

func TestUntested(t *testing.T) {
	// Like stockpulse: a web and an API, a job, and a mobile app beside them
	// that no step builds.
	apart := Spec{
		Services: []Service{{Name: "web", Path: "web"}, {Name: "api", Path: "api/", Watch: []string{"shared"}}, {Name: "db", Image: "postgres:16"}},
		Jobs:     []Job{{Name: "scraper", Path: "congress"}, {Name: "nightly", Service: "api"}},
		Tasks: []Task{
			{Name: "lint", Path: "tools/lint", When: TaskOnAlways},
			{Name: "release", Path: "release"},                                               // when: deploy: not on branches
			{Name: "migrate", Path: "migrations", When: TaskOnAlways, Stage: StagePreDeploy}, // around the deploy
		},
	}
	root := Spec{Services: []Service{{Name: "app"}}}
	underRoot := Spec{Services: []Service{{Name: "app", Path: "./"}}}
	dockerfileOut := Spec{Services: []Service{{Name: "api", Path: "api", Build: Build{Dockerfile: "../docker/api.Dockerfile"}}}}
	nested := Spec{Services: []Service{{Name: "web", Path: "packages/web"}}}

	for _, c := range []struct {
		name  string
		spec  Spec
		files []string
		want  []string
	}{
		{"a mobile change no step builds", apart, []string{"mobile/package.json", "mobile/package-lock.json"}, []string{"mobile/"}},
		{"a web change", apart, []string{"web/package.json", "web/src/App.tsx"}, nil},
		{"a watched folder", apart, []string{"shared/util.py"}, nil},
		{"a built job", apart, []string{"congress/requirements.txt"}, nil},
		{"a task that runs on branches", apart, []string{"tools/lint/rules.yaml"}, nil},
		{"a task that runs only on deploy tests nothing here", apart, []string{"release/notes.sh"}, []string{"release/"}},
		{"a pre-deploy task tests nothing here", apart, []string{"migrations/0001.sql"}, []string{"migrations/"}},
		{"a root lockfile beside folders", apart, []string{"package-lock.json"}, []string{"package-lock.json"}},
		{"rendimiento.yaml is read by the run", apart, []string{"rendimiento.yaml"}, nil},
		{"documentation and Renovate's settings", apart, []string{"README.md", "docs/guide.md", "renovate.json", "LICENSE"}, nil},
		{"web and mobile", apart, []string{"web/a.ts", "mobile/b.ts", "mobile/ios/Podfile.lock", "scripts/x.sh"}, []string{"mobile/", "scripts/"}},
		{"a root build covers the root lockfile", root, []string{"package-lock.json", "Dockerfile", "mobile/x"}, nil},
		{"./ is the root", underRoot, []string{"anything/at/all"}, nil},
		{"a Dockerfile outside the folder", dockerfileOut, []string{"docker/api.Dockerfile", "docker/other"}, []string{"docker/other"}},
		{"a sibling of a nested folder", nested, []string{"packages/mobile/app.json", "packages/package.json", "packages/web/x"}, []string{"packages/mobile/", "packages/package.json"}},
		{"nothing changed", apart, nil, nil},
	} {
		if got := c.spec.Untested(c.files); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Untested(%v) = %v, want %v", c.name, c.files, got, c.want)
		}
	}
}
