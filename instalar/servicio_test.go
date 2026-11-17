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
[ -n "$out" ] || exit 0
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
// it said.
func instalar(t *testing.T, home string) string {
	t.Helper()
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
	cmd := exec.Command("sh", script)
	cmd.Env = []string{"HOME=" + home, "PATH=" + bin + ":/usr/local/bin:/usr/bin:/bin", "XDG_RUNTIME_DIR=" + run,
		"RENDIMIENTO_GITHUB=example-user", "RENDIMIENTO_SI=1", "LANG=C"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("installer: %v\n%s", err, out)
	}
	return string(out)
}

// The service reads the person's settings file, which wins over the unit's
// own values; the first install makes the file, saying how to use it, and
// no install after that writes it again.
func TestSettingsFile(t *testing.T) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("the installer is for Linux on amd64 or arm64")
	}
	for _, tool := range []string{"sh", "sha256sum", "install", "mktemp", "grep", "cut"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is needed to run the installer")
		}
	}
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
