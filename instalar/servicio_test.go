package instalar

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakes stand in for what the installer calls on a real computer: the
// container engine, systemd, the download and the health check. curl hands
// out a program and its sum, so the checksum the installer checks holds.
var fakes = map[string]string{
	"systemctl":                        "#!/bin/sh\nexit 0\n",
	"loginctl":                         "#!/bin/sh\necho Linger=yes\n",
	"nerdctl":                          "#!/bin/sh\nexit 0\n",
	"containerd-rootless-setuptool.sh": "#!/bin/sh\nexit 0\n",
	// The browser: it notes the address it was asked to open.
	"xdg-open": "#!/bin/sh\nprintf '%s\\n' \"$1\" >>\"$HOME/abierta\"\n",
	"id": `#!/bin/sh
case "$1" in -u) echo 1000 ;; *) echo example ;; esac
`,
	"curl": `#!/bin/sh
out=
url=
while [ $# -gt 0 ]; do
	case "$1" in
	-o) out=$2; shift ;;
	-*) ;;
	*) url=$1 ;;
	esac
	shift
done
if [ -z "$out" ]; then
	# rendimiento's answer about its setup: installed only when the test says.
	case "$url" in */api/setup/status) [ -z "${CONFIGURADO:-}" ] || echo '{"activity":false,"configured":true}' ;; esac
	exit 0
fi
case "$url" in
*/SHA256SUMS)
	sum=$(printf 'program\n' | sha256sum | cut -d' ' -f1)
	printf '%s  rendimiento-linux-amd64\n%s  rendimiento-linux-arm64\n' "$sum" "$sum" >"$out" ;;
*) printf 'program\n' >"$out" ;;
esac
`,
}

// instalar runs the installer for one computer in home, with the fakes
// first in PATH (the installer puts ~/.local/bin first), and returns what
// it said. It names an administrator ahead, by the old name, as scripts
// did.
func instalar(t *testing.T, home string) string {
	t.Helper()
	out, err := correr(t, home, []string{"RENDIMIENTO_GITHUB=example-user"})
	if err != nil {
		t.Fatalf("installer: %v\n%s", err, out)
	}
	return out
}

// correr runs the installer in home with these settings and arguments,
// and returns what it said and how it ended.
func correr(t *testing.T, home string, env []string, args ...string) (string, error) {
	t.Helper()
	puedeInstalar(t)
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range fakes {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	run := filepath.Join(home, "run")
	if err := os.MkdirAll(run, 0o700); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("instalar.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Env = append([]string{"HOME=" + home, "PATH=" + bin + ":/usr/local/bin:/usr/bin:/bin", "XDG_RUNTIME_DIR=" + run,
		"RENDIMIENTO_SI=1", "LANG=C"}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// puedeInstalar skips a test where the installer cannot run.
func puedeInstalar(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("the installer is for Linux on amd64 or arm64")
	}
	for _, tool := range []string{"sh", "sha256sum", "install", "mktemp", "grep", "cut", "tr"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is needed to run the installer")
		}
	}
}

// unidad is the service the installer left in home.
func unidad(t *testing.T, home string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "rendimiento.service"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The service reads the person's settings file, which wins over the unit's
// own values; the first install makes the file, saying how to use it, and
// no install after that writes it again.
func TestSettingsFile(t *testing.T) {
	puedeInstalar(t)
	home := t.TempDir()
	out := instalar(t, home)
	settings := filepath.Join(home, ".config", "rendimiento", "env")
	if !strings.Contains(out, settings) {
		t.Errorf("the installer does not say where the settings go:\n%s", out)
	}

	unit, err := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "rendimiento.service"))
	if err != nil {
		t.Fatal(err)
	}
	u := string(unit)
	read := strings.Index(u, "\nEnvironmentFile=-"+settings+"\n")
	if read < 0 || !strings.Contains(u, "\nEnvironment=SETTINGS_FILE="+settings+"\n") {
		t.Fatalf("the unit does not read the settings file:\n%s", u)
	}
	// systemd applies the file after Environment= in any order; after them
	// it also reads that way.
	if strings.LastIndex(u, "\nEnvironment=") > read {
		t.Errorf("the settings file comes before one of the unit's own values:\n%s", u)
	}

	b, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME=value", "NOMBRE=valor", "/reference/config/", "systemctl --user restart rendimiento"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the settings file's header lacks %q:\n%s", want, b)
		}
	}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l != "#" && !strings.HasPrefix(l, "# ") {
			t.Errorf("the new settings file sets something: %q", l)
		}
	}
	for f, want := range map[string]os.FileMode{settings: 0o600, filepath.Dir(settings): 0o700} {
		if fi, err := os.Stat(f); err != nil {
			t.Error(err)
		} else if fi.Mode().Perm() != want {
			t.Errorf("%s: mode %v, want %v", f, fi.Mode().Perm(), want)
		}
	}

	// The person's settings stay through the next install (an update),
	// while the unit is written again.
	mine := "NOTIFY_LANG=es\n"
	if err := os.WriteFile(settings, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "systemd", "user", "rendimiento.service"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	instalar(t, home)
	if b, _ := os.ReadFile(settings); string(b) != mine {
		t.Fatalf("a second install rewrote the settings file: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "rendimiento.service")); !strings.Contains(string(b), "EnvironmentFile=-"+settings) {
		t.Fatalf("a second install did not write the unit: %q", b)
	}
}

// Nobody has to be named to install: without a terminal (curl | sh in a
// script) it does not ask, the service has no ALLOWED_USERS, and the end
// says to finish on the setup page with the setup token.
func TestSinAdministrador(t *testing.T) {
	home := t.TempDir()
	out, err := correr(t, home, nil)
	if err != nil {
		t.Fatalf("installer: %v\n%s", err, out)
	}
	if u := unidad(t, home); strings.Contains(u, "ALLOWED_USERS") || !strings.Contains(u, "\nEnvironment=MODE=local\n") {
		t.Fatalf("the unit:\n%s", u)
	}
	for _, want := range []string{"setup page", "first administrator", "ficha-configuracion"} {
		if !strings.Contains(out, want) {
			t.Errorf("the installer does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "GitHub login") {
		t.Errorf("it still asks for a GitHub login:\n%s", out)
	}
}

// Scripts may still name the first administrators ahead, in any of the
// names Members takes; RENDIMIENTO_GITHUB is the old name of the setting.
func TestAdministradoresDeAntemano(t *testing.T) {
	for _, c := range []struct{ env, quiere string }{
		{"RENDIMIENTO_GITHUB=octocat", "octocat"},
		{"RENDIMIENTO_GITHUB=ana@gitlab.com", "ana@gitlab.com"},
		{"RENDIMIENTO_ADMINS=ana@git.example.com:8443, luis@bitbucket.org,bea", "ana@git.example.com:8443,luis@bitbucket.org,bea"},
	} {
		home := t.TempDir()
		out, err := correr(t, home, []string{c.env})
		if err != nil {
			t.Fatalf("%s: %v\n%s", c.env, err, out)
		}
		if u := unidad(t, home); !strings.Contains(u, "\nEnvironment=ALLOWED_USERS="+c.quiere+"\n") {
			t.Errorf("%s: the unit:\n%s", c.env, u)
		}
	}
	for _, malo := range []string{"-octocat", "octo_cat", "ana@", "ana@Git.Example.com", "a$(id)", "*"} {
		home := t.TempDir()
		out, err := correr(t, home, []string{"RENDIMIENTO_ADMINS=" + malo})
		if err == nil || !strings.Contains(out, "is not a username") {
			t.Errorf("%q: it installed, or said: %s", malo, out)
		}
		if _, err := os.Stat(filepath.Join(home, ".config", "systemd", "user", "rendimiento.service")); err == nil {
			t.Errorf("%q: a unit was written", malo)
		}
	}
}

// The forja chosen ahead opens the setup page on it, with the setup token
// in the address; GitLab and Bitbucket get the callback to set, which is
// this computer's plain http address.
func TestForjaDeAntemano(t *testing.T) {
	const ficha = "0123456789abcdef"
	for _, c := range []struct {
		nombre string
		env    []string
		args   []string
		pagina string
		dice   string
	}{
		{"chosen on the page", nil, nil, "http://localhost:8080/?token=" + ficha, ""},
		{"github", nil, []string{"--forja", "GitHub"}, "http://localhost:8080/?token=" + ficha + "&forja=github", ""},
		{"gitlab.com", nil, []string{"--forja=gitlab"}, "http://localhost:8080/?token=" + ficha + "&forja=gitlab&gitlab=https://gitlab.com",
			"http://localhost:8080/api/forjas/gitlab/callback"},
		{"a GitLab of its own", []string{"RENDIMIENTO_GITLAB=git.example.com/"}, nil, "http://localhost:8080/?token=" + ficha + "&forja=gitlab&gitlab=https://git.example.com",
			"On https://git.example.com, create an OAuth application"},
		{"bitbucket", []string{"RENDIMIENTO_FORJA=bitbucket"}, nil, "http://localhost:8080/?token=" + ficha + "&forja=bitbucket",
			"http://localhost:8080/api/forjas/bitbucket/callback"},
	} {
		t.Run(c.nombre, func(t *testing.T) {
			home := t.TempDir()
			datos := filepath.Join(home, ".config", "rendimiento")
			if err := os.MkdirAll(datos, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(datos, "ficha-configuracion"), []byte(ficha+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := correr(t, home, append([]string{"DISPLAY=:0"}, c.env...), c.args...)
			if err != nil {
				t.Fatalf("installer: %v\n%s", err, out)
			}
			if b, _ := os.ReadFile(filepath.Join(home, "abierta")); string(b) != c.pagina+"\n" {
				t.Errorf("the browser opened %q, want %q", b, c.pagina)
			}
			if !strings.Contains(out, c.dice) {
				t.Errorf("the installer does not say %q:\n%s", c.dice, out)
			}
			// Not a terminal: the token stays out of what it printed.
			if strings.Contains(out, ficha) {
				t.Errorf("the setup token is in the output:\n%s", out)
			}
		})
	}
	t.Run("not a forja", func(t *testing.T) {
		out, err := correr(t, t.TempDir(), nil, "--forja", "sourceforge")
		if err == nil || !strings.Contains(out, "not a forja") {
			t.Fatalf("%v: %s", err, out)
		}
	})
	t.Run("installed already: no token", func(t *testing.T) {
		home := t.TempDir()
		out, err := correr(t, home, []string{"DISPLAY=:0", "CONFIGURADO=1"}, "--forja", "gitlab")
		if err != nil {
			t.Fatalf("installer: %v\n%s", err, out)
		}
		if b, _ := os.ReadFile(filepath.Join(home, "abierta")); string(b) != "http://localhost:8080\n" {
			t.Errorf("the browser opened %q", b)
		}
		if !strings.Contains(out, "already set up") || strings.Contains(out, "callback") {
			t.Errorf("it said:\n%s", out)
		}
	})
}
