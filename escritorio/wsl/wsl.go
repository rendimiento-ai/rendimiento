// Package wsl keeps rendimiento's WSL2 distribution for the Windows app
// (instalar/wsl, published with each release): it says what is missing,
// turns WSL on, imports the distribution, writes who signs in first, wakes
// it and puts it to sleep. Everything goes through wsl.exe by an Ejecutor,
// so its tests run on any system with a fake one.
package wsl

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Ejecutor runs Windows programs for a Distro: the app gives the real one
// (Sistema, which never opens a console window); tests, a fake.
type Ejecutor interface {
	// Ejecutar runs a program to its end, with entrada on its standard
	// input, and returns what it printed (stdout and stderr together, as
	// the program wrote it: wsl.exe's own messages are UTF-16).
	Ejecutar(ctx context.Context, entrada []byte, nombre string, args ...string) ([]byte, error)
	// Iniciar starts a program that stays (the one that keeps the
	// distribution awake) and returns how to end it.
	Iniciar(ctx context.Context, nombre string, args ...string) (detener func(), err error)
	// Elevar runs a program as administrator (Windows asks the person
	// first) and waits for its end.
	Elevar(ctx context.Context, nombre string, args ...string) error
}

// Estado is how far this computer is from running rendimiento: each one
// is a step of the app's first-time assistant, or a state of its icon.
type Estado string

const (
	SinWSL     Estado = "sin-wsl"     // WSL2 is not installed (or wsl.exe is missing)
	SinDistro  Estado = "sin-distro"  // WSL2 works; the distribution is not imported
	SinUsuario Estado = "sin-usuario" // imported; nobody may sign in yet (no ALLOWED_USERS)
	Dormida    Estado = "dormida"     // ready, and not running
	Despierta  Estado = "despierta"   // rendimiento answers on Direccion
)

// Problema is an error the app explains in the person's language: Clave
// picks the text, Detalle is what the system said (never a secret).
type Problema struct {
	Clave   string
	Detalle string
}

func (p *Problema) Error() string { return p.Clave + ": " + p.Detalle }

// The Clave of each Problema.
const (
	Reinicio       = "reinicio"       // Windows must restart before WSL2 works
	Virtualizacion = "virtualizacion" // virtualization is off in the BIOS/UEFI (HCS_E_HYPERV_NOT_INSTALLED)
	Descarga       = "descarga"       // the distribution could not be downloaded
	Suma           = "suma"           // the download does not match the release's SHA256SUMS
	Usuario        = "usuario"        // that is not a GitHub login
	Arranque       = "arranque"       // rendimiento did not answer in time
	Cancelado      = "cancelado"      // the person said no to Windows' administrator question
	Otro           = "otro"           // anything else wsl.exe refused
)

// Distro is rendimiento's WSL2 distribution on this computer.
type Distro struct {
	Nombre    string // the distribution's name in WSL: "rendimiento"
	Carpeta   string // where its disk and downloads live: %LOCALAPPDATA%\rendimiento
	Descargas string // where a release's files are: …/releases/latest/download
	Direccion string // where rendimiento answers: http://localhost:8080
	Ejecutor  Ejecutor
	HTTP      *http.Client
}

// Nueva is the distribution with its usual names, kept in carpeta.
func Nueva(carpeta string, e Ejecutor) *Distro {
	return &Distro{
		Nombre:    "rendimiento",
		Carpeta:   carpeta,
		Descargas: "https://github.com/rendimiento-ai/rendimiento/releases/latest/download",
		Direccion: "http://localhost:8080",
		Ejecutor:  e,
		HTTP:      &http.Client{},
	}
}

var errPendiente = errors.New("pendiente")

// Estado says where this computer is. It never changes anything.
func (d *Distro) Estado(ctx context.Context) (Estado, error) { return "", errPendiente }

// InstalarWSL turns WSL2 on (wsl --install --no-distribution, as
// administrator). When it ends well Windows still has to restart: it
// returns a Problema with Clave Reinicio unless WSL already works.
func (d *Distro) InstalarWSL(ctx context.Context) error { return errPendiente }

// Importar downloads the latest release's distribution into Carpeta,
// checks it against the release's SHA256SUMS and imports it. avance (may
// be nil) hears how many bytes of the download are done, and of how many.
func (d *Distro) Importar(ctx context.Context, avance func(hecho, total int64)) error {
	return errPendiente
}

// Configurar writes who signs in first (ALLOWED_USERS in
// /etc/rendimiento/env) and restarts rendimiento inside the distribution.
func (d *Distro) Configurar(ctx context.Context, usuario string) error { return errPendiente }

// Despertar starts the distribution, keeps it awake (WSL stops one with
// no wsl.exe attached) and waits until rendimiento answers.
func (d *Distro) Despertar(ctx context.Context) error { return errPendiente }

// Dormir stops the distribution: its apps, its builds and the memory it
// held. Its data stays.
func (d *Distro) Dormir(ctx context.Context) error { return errPendiente }

// Soltar ends what keeps the distribution awake, without stopping it at
// once: for when the app exits.
func (d *Distro) Soltar() {}

// Ficha is the setup token rendimiento made on its first start, for the
// page that connects GitHub. It is a secret: never log it.
func (d *Distro) Ficha(ctx context.Context) (string, error) { return "", errPendiente }

// Configurado says whether rendimiento already has its GitHub App (GET
// /api/setup/status): until then the app opens the setup page.
func (d *Distro) Configurado(ctx context.Context) (bool, error) { return false, errPendiente }

// Reloj is how far this computer's clock is from the real time (positive:
// ahead), by the Date of an HTTPS answer; under a minute counts as zero.
// GitHub refuses rendimiento's signature when the clock is off.
func Reloj(ctx context.Context, c *http.Client) (time.Duration, error) { return 0, errPendiente }
