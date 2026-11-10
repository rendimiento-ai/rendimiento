// Package wsl keeps rendimiento's WSL2 distribution for the Windows app
// (instalar/wsl, published with each release): it says what is missing,
// turns WSL on, imports the distribution, writes who signs in first, wakes
// it and puts it to sleep. Everything goes through wsl.exe by an Ejecutor,
// so its tests run on any system with a fake one.
package wsl

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

	mu      sync.Mutex
	detener func() // ends the wsl.exe that keeps the distribution awake
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

const programa = "wsl.exe"

// How long one thing may take; tests shorten them.
var (
	esperaWSL    = 20 * time.Second // wsl.exe about itself: the list, its version, terminate
	esperaDentro = 2 * time.Minute  // a command inside the distribution, which starts it first
	esperaHTTP   = 15 * time.Second // a small answer from the network
	esperaSalud  = 3 * time.Second  // /healthz, on this computer
)

// sinInstalar is in what Windows' own wsl.exe prints, in any language,
// when WSL is not installed or its optional component is off.
const sinInstalar = "aka.ms/wslinstall"

// sinMaquina are wsl.exe's error codes for a WSL2 that cannot start its
// virtual machine: Windows was not restarted after installing WSL, or
// virtualization (SVM Mode on AMD, Intel VT) is off in the BIOS/UEFI.
var sinMaquina = []string{"HCS_E_HYPERV_NOT_INSTALLED", "ERROR_NOT_SUPPORTED", "HCS_E_SERVICE_NOT_AVAILABLE", "0x80370102"}

// wsl runs wsl.exe to its end, within espera (0: as long as ctx lets it),
// and returns what it printed, read as text. Its error is ctx's when ctx
// ended, one that says wsl.exe did not answer when espera did, or the
// Ejecutor's own.
func (d *Distro) wsl(ctx context.Context, espera time.Duration, entrada []byte, args ...string) (string, error) {
	dentro := ctx
	if espera > 0 {
		var cancel context.CancelFunc
		dentro, cancel = context.WithTimeout(ctx, espera)
		defer cancel()
	}
	out, err := d.Ejecutor.Ejecutar(dentro, entrada, programa, args...)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		err = ctx.Err()
	case dentro.Err() != nil:
		err = fmt.Errorf("wsl.exe %s did not answer in %s", args[0], espera)
	}
	return texto(out), err
}

// pedir is wsl for what should work: a refusal comes back as a Problema.
func (d *Distro) pedir(ctx context.Context, espera time.Duration, entrada []byte, args ...string) (string, error) {
	out, err := d.wsl(ctx, espera, entrada, args...)
	if err == nil {
		return out, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return "", problema(out, err)
}

// dentro runs a command inside the distribution, as root and without a
// shell of WSL's in between. It starts a stopped distribution.
func (d *Distro) dentro(ctx context.Context, entrada []byte, comando ...string) (string, error) {
	return d.pedir(ctx, esperaDentro, entrada, append([]string{"-d", d.Nombre, "-u", "root", "--exec"}, comando...)...)
}

// problema is wsl.exe's refusal for the app: what it printed is the
// Detalle, and its error code picks the Clave.
func problema(salida string, err error) *Problema {
	detalle := cola(salida, 600)
	if detalle == "" {
		detalle = err.Error()
	}
	return &Problema{Clave: clave(salida), Detalle: detalle}
}

func clave(salida string) string {
	for _, codigo := range sinMaquina {
		if strings.Contains(salida, codigo) {
			return Virtualizacion
		}
	}
	return Otro
}

// falta: the program is not on this computer.
func falta(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist)
}

// salio: the program ran and ended with an error of its own.
func salio(err error) bool {
	var e *exec.ExitError
	return errors.As(err, &e)
}

// hayWSL reports whether this computer has a WSL that can run the
// distribution. wsl.exe --version is the question: the WSL that Windows
// brings (before wsl --install or --update) does not know it, and has no
// systemd either.
func (d *Distro) hayWSL(ctx context.Context) (bool, error) {
	out, err := d.wsl(ctx, esperaWSL, nil, "--version")
	switch {
	case err == nil:
		return true, nil
	case falta(err), salio(err), strings.Contains(out, sinInstalar):
		return false, nil
	case ctx.Err() != nil:
		return false, err
	}
	return false, problema(out, err)
}

// mirar asks wsl.exe, without starting anything, how far this computer
// is: SinWSL, SinDistro, or "" when the distribution is imported.
func (d *Distro) mirar(ctx context.Context) (Estado, error) {
	out, err := d.wsl(ctx, esperaWSL, nil, "-l", "-q")
	switch {
	case falta(err), strings.Contains(out, sinInstalar):
		return SinWSL, nil
	case err == nil && d.enLista(out):
		return "", nil
	case ctx.Err() != nil:
		return "", ctx.Err()
	case err != nil && !salio(err):
		// It did not answer, or could not run: that says nothing about
		// what is installed.
		return "", problema(out, err)
	}
	// The distribution is not in the list, or there was no list: wsl.exe
	// ends with an error when there are none, and when WSL is missing.
	hay, errWSL := d.hayWSL(ctx)
	switch {
	case errWSL != nil:
		return "", errWSL
	case !hay:
		return SinWSL, nil
	case err == nil || strings.Contains(out, "WSL_E_DEFAULT_DISTRO_NOT_FOUND"):
		return SinDistro, nil
	}
	// WSL is there and its list failed for a reason of its own. With the
	// distribution's disk in place that is a failure to tell, not a
	// computer to import into again.
	if _, e := os.Stat(filepath.Join(d.disco(), "ext4.vhdx")); e == nil {
		return "", problema(out, err)
	}
	return SinDistro, nil
}

func (d *Distro) enLista(lista string) bool {
	for _, nombre := range lineas(lista) {
		if strings.EqualFold(nombre, d.Nombre) {
			return true
		}
	}
	return false
}

// disco is the folder wsl --import keeps the distribution's disk in.
func (d *Distro) disco() string { return filepath.Join(d.Carpeta, "distro") }

func (d *Distro) cliente() *http.Client {
	if d.HTTP != nil {
		return d.HTTP
	}
	return http.DefaultClient
}

// Estado says where this computer is. It never changes anything: at most,
// the first time it meets a distribution this app did not configure, it
// starts it to read who may sign in, and remembers (see marca).
func (d *Distro) Estado(ctx context.Context) (Estado, error) {
	estado, err := d.mirar(ctx)
	if err != nil || estado != "" {
		return estado, err
	}
	if d.responde(ctx) {
		return Despierta, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if _, err := os.Stat(d.marca()); err == nil {
		return Dormida, nil
	}
	env, err := d.dentro(ctx, nil, "sh", "-c", leerEnv)
	if err != nil {
		return "", err
	}
	usuario := usuarioDe(env)
	if usuario == "" {
		return SinUsuario, nil
	}
	d.marcar(usuario)
	return Dormida, nil
}

// InstalarWSL turns WSL2 on (wsl --install --no-distribution, as
// administrator). When it ends well Windows still has to restart: it
// returns a Problema with Clave Reinicio unless WSL already works.
func (d *Distro) InstalarWSL(ctx context.Context) error {
	hay, err := d.hayWSL(ctx)
	if err != nil {
		return err
	}
	if hay {
		return nil
	}
	err = d.Ejecutor.Elevar(ctx, programa, "--install", "--no-distribution")
	var p *Problema
	switch {
	case err == nil:
	case errors.As(err, &p): // Cancelado
		return p
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		// The installer's window is not ours to read. If WSL is there
		// now, it did its work, whatever it ended with.
		if hay, _ := d.hayWSL(ctx); !hay {
			return &Problema{Clave: Otro, Detalle: err.Error()}
		}
	}
	return &Problema{Clave: Reinicio, Detalle: "WSL is installed; Windows has to restart before it works"}
}
