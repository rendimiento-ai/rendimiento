package instalar

import (
	"os"
	"regexp"
	"testing"
)

// The install commands people copy never pin a release: the installer and
// Helm take the latest, so a number written there only goes stale (the
// README still said 0.1.3 when 0.1.5 was out). A release's own number
// belongs in CHANGELOG.md, which the binarios workflow updates with each
// tag. The files are the ones that show install commands.
func TestNoPinnedReleaseInInstallCommands(t *testing.T) {
	fijada := regexp.MustCompile(`(?m)^(?:curl|helm install|  -n|  --set)[^\n]*(?:--version [0-9]|RENDIMIENTO_VERSION=v?[0-9])`)
	for _, f := range []string{"../README.md", "../docs/content/environment/deploy.md", "../docs/content/environment/deploy.es.md"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := fijada.Find(b); m != nil {
			t.Errorf("%s pins a release in an install command: %q (leave it out: the latest is taken)", f, m)
		}
	}
}
