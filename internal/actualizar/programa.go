package actualizar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/firma"
)

// Programa changes rendimiento's own program on a computer (Linux, and the
// WSL distribution under Windows): it downloads a release, checks its
// signature and sum, keeps the program that runs as the previous one, puts
// the new one in its place, and exits so the service starts it again. If
// the new one keeps failing to start, it goes back by itself; a person can
// also go back to the previous version, only ever down (decision 138).
type Programa struct {
	Ruta      string // the running program (os.Executable)
	Datos     string // DATA_DIR: where the record of an update is kept
	Descargas string // a release's files: …/releases/download (+ /v<version>)
	HTTP      *http.Client
	Publica   string // the key to check with ("": firma.Publica)
	Arch      string // amd64 or arm64 ("": this one's)
	// Salir ends the process with a code the service restarts on.
	Salir func(code int)
	// Sana is how long the new version must run before it is kept (2 min).
	Sana time.Duration
}

// CodigoReinicio is the exit code after a change of program: not 0, since
// the Linux service restarts only on failure.
const CodigoReinicio = 75

// MaxArranques is how often a new version may start without staying up
// Sana before rendimiento goes back to the previous one.
const MaxArranques = 3

// Marca is the record of an update in DATA_DIR, read at the next start.
type Marca struct {
	De        string    `json:"de"` // the version before
	A         string    `json:"a"`  // the version installed
	Cuando    time.Time `json:"cuando"`
	Arranques int       `json:"arranques"`
	Regreso   bool      `json:"regreso,omitempty"` // it went back by itself
	Quien     string    `json:"quien,omitempty"`
}

func (p *Programa) marca() string    { return filepath.Join(p.Datos, "actualizacion.json") }
func (p *Programa) anterior() string { return p.Ruta + ".anterior" }

// Anterior is the version kept as the previous one ("" when none).
func (p *Programa) Anterior() string {
	b, err := os.ReadFile(p.anterior() + ".version")
	if err != nil {
		return ""
	}
	if _, err := os.Stat(p.anterior()); err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Fallida is the last version that did not stay up here ("" when none):
// updating by itself never installs it again.
func (p *Programa) Fallida() string {
	b, err := os.ReadFile(filepath.Join(p.Datos, "version-fallida"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (p *Programa) arch() string {
	if p.Arch != "" {
		return p.Arch
	}
	return runtime.GOARCH
}

func (p *Programa) bajar(ctx context.Context, url string) ([]byte, error) {
	cl := p.HTTP
	if cl == nil {
		cl = &http.Client{Timeout: 10 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 512<<20))
}

// Instalar puts version in place of the running program (actual): only a
// newer version, signed by rendimiento's key. The caller then exits with
// CodigoReinicio, once it answered.
func (p *Programa) Instalar(ctx context.Context, actual, version, quien string) error {
	if nueva, _ := Comparar(actual, version); !nueva {
		return fmt.Errorf("%s is not newer than %s", version, actual)
	}
	base := strings.TrimRight(p.Descargas, "/") + "/v" + strings.TrimPrefix(version, "v")
	nombre := "rendimiento-linux-" + p.arch()
	sums, err := p.bajar(ctx, base+"/SHA256SUMS")
	if err != nil {
		return err
	}
	sig, err := p.bajar(ctx, base+"/SHA256SUMS.sig")
	if err != nil {
		return fmt.Errorf("the release is not signed, so it is not installed: %w", err)
	}
	bin, err := p.bajar(ctx, base+"/"+nombre)
	if err != nil {
		return err
	}
	if err := firma.Comprobar(p.Publica, sums, string(sig), nombre, bin); err != nil {
		return err
	}
	nuevo := p.Ruta + ".nuevo"
	if err := os.WriteFile(nuevo, bin, 0o755); err != nil {
		return err
	}
	// The running program becomes the previous one; Linux keeps running it
	// from its open file until the process ends.
	if err := os.Rename(p.Ruta, p.anterior()); err != nil {
		return err
	}
	if err := os.WriteFile(p.anterior()+".version", []byte(actual+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(nuevo, p.Ruta); err != nil {
		_ = os.Rename(p.anterior(), p.Ruta) // put it back as it was
		return err
	}
	return p.guardar(Marca{De: actual, A: strings.TrimPrefix(version, "v"), Cuando: time.Now().UTC(), Quien: quien})
}

// Regresar puts the previous version back, only when it is lower than the
// one running. The caller then exits with CodigoReinicio.
func (p *Programa) Regresar(actual, quien string) error {
	ant := p.Anterior()
	if ant == "" {
		return errors.New("there is no previous version kept on this computer")
	}
	if menor, _ := Comparar(ant, actual); !menor {
		return fmt.Errorf("the previous version (%s) is not lower than this one (%s)", ant, actual)
	}
	return p.cambiarAAnterior(Marca{De: actual, A: ant, Cuando: time.Now().UTC(), Quien: quien, Regreso: true})
}

func (p *Programa) cambiarAAnterior(m Marca) error {
	if err := os.Rename(p.Ruta, p.Ruta+".dejada"); err != nil {
		return err
	}
	if err := os.Rename(p.anterior(), p.Ruta); err != nil {
		_ = os.Rename(p.Ruta+".dejada", p.Ruta)
		return err
	}
	_ = os.Remove(p.anterior() + ".version")
	return p.guardar(m)
}

func (p *Programa) guardar(m Marca) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(p.marca(), b, 0o644)
}

// AlArrancar reads the record of an update when rendimiento starts. A new
// version counts its starts; past MaxArranques without staying up Sana, it
// puts the previous version back and exits (the service starts that one).
// One that stays up Sana is kept. It returns the last update, for a person
// to know of it (nil when nothing happened).
func (p *Programa) AlArrancar(version string) *Marca {
	b, err := os.ReadFile(p.marca())
	if err != nil {
		return nil
	}
	var m Marca
	if json.Unmarshal(b, &m) != nil {
		_ = os.Remove(p.marca())
		return nil
	}
	version = strings.TrimPrefix(version, "v")
	switch {
	case m.Regreso:
		_ = os.Remove(p.marca())
		return &m
	case m.A != version:
		_ = os.Remove(p.marca())
		return nil
	}
	m.Arranques++
	if m.Arranques > MaxArranques {
		// It is not installed by itself again: updating by itself skips it.
		_ = os.WriteFile(filepath.Join(p.Datos, "version-fallida"), []byte(m.A+"\n"), 0o644)
		m2 := Marca{De: m.A, A: m.De, Cuando: time.Now().UTC(), Regreso: true, Quien: "automatic"}
		if err := p.cambiarAAnterior(m2); err == nil && p.Salir != nil {
			p.Salir(CodigoReinicio)
		}
		return nil
	}
	_ = p.guardar(m)
	sana := p.Sana
	if sana == 0 {
		sana = 2 * time.Minute
	}
	go func() {
		time.Sleep(sana)
		_ = os.Remove(p.marca()) // it stayed up: kept
	}()
	return &m
}
