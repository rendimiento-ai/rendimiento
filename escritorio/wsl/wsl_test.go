package wsl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

// u16 is s as wsl.exe prints its own messages: UTF-16LE.
func u16(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

var errSalida = &exec.ExitError{} // the program ran and ended with an error

type respuesta struct {
	salida []byte
	err    error
}

// falso is an Ejecutor with scripted answers: the first rule whose text is
// in the command line answers; a command with no rule fails the test.
type falso struct {
	t  *testing.T
	mu sync.Mutex

	reglas   []string
	guion    map[string]respuesta
	llamadas []string
	entradas []string // the standard input of each call
	vivos    int      // programs started with Iniciar and not ended
	elevar   func() error
}

func nuevoFalso(t *testing.T) *falso {
	return &falso{t: t, guion: map[string]respuesta{}}
}

func (f *falso) cuando(texto string, salida []byte, err error) *falso {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.guion[texto]; !ok {
		f.reglas = append(f.reglas, texto)
	}
	f.guion[texto] = respuesta{salida, err}
	return f
}

func (f *falso) Ejecutar(ctx context.Context, entrada []byte, nombre string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	linea := nombre + " " + strings.Join(args, " ")
	f.llamadas = append(f.llamadas, linea)
	f.entradas = append(f.entradas, string(entrada))
	for _, r := range f.reglas {
		if strings.Contains(linea, r) {
			return f.guion[r].salida, f.guion[r].err
		}
	}
	f.t.Errorf("unexpected command: %s", linea)
	return nil, errors.New("unexpected")
}

func (f *falso) Iniciar(_ context.Context, nombre string, args ...string) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.llamadas = append(f.llamadas, "iniciar "+nombre+" "+strings.Join(args, " "))
	f.vivos++
	var una sync.Once
	return func() {
		una.Do(func() {
			f.mu.Lock()
			f.vivos--
			f.mu.Unlock()
		})
	}, nil
}

func (f *falso) Elevar(_ context.Context, nombre string, args ...string) error {
	f.mu.Lock()
	f.llamadas = append(f.llamadas, "elevar "+nombre+" "+strings.Join(args, " "))
	f.mu.Unlock()
	if f.elevar != nil {
		return f.elevar()
	}
	return nil
}

func (f *falso) hizo(texto string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, l := range f.llamadas {
		if strings.Contains(l, texto) {
			n++
		}
	}
	return n
}

// distro is a Distro over f in a folder of the test's, with rendimiento at
// servidor (nil: nothing answers there).
func distro(t *testing.T, f *falso, servidor http.Handler) *Distro {
	t.Helper()
	d := Nueva(t.TempDir(), f)
	d.Direccion = "http://127.0.0.1:1" // refused at once
	if servidor != nil {
		s := httptest.NewServer(servidor)
		t.Cleanup(s.Close)
		d.Direccion = s.URL
	}
	return d
}

// sana is a rendimiento that answers.
func sana(configurado bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
	mux.HandleFunc("GET /api/setup/status", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"activity":false,"configured":%v,"tokenCommand":"cat x"}`, configurado)
	})
	return mux
}

func clavede(t *testing.T, err error) string {
	t.Helper()
	var p *Problema
	if !errors.As(err, &p) {
		t.Fatalf("not a Problema: %v", err)
	}
	return p.Clave
}

func corto(t *testing.T, v *time.Duration, d time.Duration) {
	antes := *v
	*v = d
	t.Cleanup(func() { *v = antes })
}

const (
	noInstalado = "Windows Subsystem for Linux is not installed. You can install by running 'wsl.exe --install'.\r\nFor more information please visit https://aka.ms/wslinstall\r\n"
	sinNinguna  = "El Subsistema de Windows para Linux no tiene distribuciones instaladas.\r\nError code: Wsl/WSL_E_DEFAULT_DISTRO_NOT_FOUND\r\n"
	sinHyperV   = "No se pudo iniciar la máquina virtual.\r\nError code: Wsl/Service/RegisterDistro/CreateVm/HCS/HCS_E_HYPERV_NOT_INSTALLED\r\n"
)

// Each Estado, by what wsl.exe and rendimiento answer.
func TestEstado(t *testing.T) {
	lista := u16("Ubuntu\r\nrendimiento\r\n")
	for _, c := range []struct {
		nombre   string
		guion    func(f *falso)
		servidor http.Handler
		marca    bool
		quiere   Estado
		falla    bool
		nunca    string // a command that must not run
	}{
		{nombre: "no wsl.exe", guion: func(f *falso) { f.cuando("-l -q", nil, &exec.Error{Name: "wsl.exe", Err: exec.ErrNotFound}) }, quiere: SinWSL},
		{nombre: "Windows' own wsl.exe", guion: func(f *falso) { f.cuando("-l -q", u16(noInstalado), errSalida) }, quiere: SinWSL},
		{nombre: "that one, waiting for a key until it is ended", guion: func(f *falso) { f.cuando("-l -q", u16(noInstalado), context.DeadlineExceeded) }, quiere: SinWSL},
		{nombre: "a WSL without --version", guion: func(f *falso) {
			f.cuando("-l -q", u16("Ubuntu\r\n"), nil).cuando("--version", u16("Invalid command line option: --version\r\n"), errSalida)
		}, quiere: SinWSL},
		{nombre: "no distributions", guion: func(f *falso) {
			f.cuando("-l -q", u16(sinNinguna), errSalida).cuando("--version", []byte("WSL version: 2.3.26.0\n"), nil)
		}, quiere: SinDistro},
		{nombre: "others, not ours, in UTF-8", guion: func(f *falso) {
			f.cuando("-l -q", []byte("Ubuntu\ndocker-desktop\n"), nil).cuando("--version", nil, nil)
		}, quiere: SinDistro, nunca: "-d rendimiento"},
		{nombre: "the list fails with our disk in place", guion: func(f *falso) {
			f.cuando("-l -q", u16("Error code: Wsl/Service/E_UNEXPECTED\r\n"), errSalida).cuando("--version", nil, nil)
		}, marca: true, falla: true},
		{nombre: "wsl.exe cannot run", guion: func(f *falso) { f.cuando("-l -q", nil, errors.New("access denied")) }, falla: true},
		{nombre: "imported by hand, no user", guion: func(f *falso) {
			f.cuando("-l -q", lista, nil).cuando(leerEnv, nil, nil)
		}, quiere: SinUsuario},
		{nombre: "imported and configured by hand", guion: func(f *falso) {
			f.cuando("-l -q", lista, nil).cuando(leerEnv, []byte("ALLOWED_USERS=octocat\n"), nil)
		}, quiere: Dormida},
		{nombre: "configured by the app: it is not woken to look", guion: func(f *falso) { f.cuando("-l -q", lista, nil) }, marca: true, quiere: Dormida, nunca: "-d rendimiento"},
		{nombre: "answers", guion: func(f *falso) { f.cuando("-l -q", lista, nil) }, servidor: sana(false), quiere: Despierta, nunca: "-d rendimiento"},
		{nombre: "cannot start to be read", guion: func(f *falso) {
			f.cuando("-l -q", lista, nil).cuando(leerEnv, u16(sinHyperV), errSalida)
		}, falla: true},
	} {
		t.Run(c.nombre, func(t *testing.T) {
			f := nuevoFalso(t)
			c.guion(f)
			d := distro(t, f, c.servidor)
			if c.marca {
				d.marcar("octocat")
				os.MkdirAll(d.disco(), 0o755)
				os.WriteFile(filepath.Join(d.disco(), "ext4.vhdx"), nil, 0o644)
			}
			got, err := d.Estado(context.Background())
			if (err != nil) != c.falla || got != c.quiere {
				t.Fatalf("Estado = %q, %v; want %q (fails: %v)", got, err, c.quiere, c.falla)
			}
			if c.nunca != "" && f.hizo(c.nunca) > 0 {
				t.Fatalf("it ran %q: %v", c.nunca, f.llamadas)
			}
		})
	}
}

// A distribution configured by hand is started once to be read; after
// that the mark on the Windows side answers.
func TestEstadoRecuerda(t *testing.T) {
	f := nuevoFalso(t).cuando("-l -q", u16("rendimiento\r\n"), nil).cuando(leerEnv, []byte("# mine\nALLOWED_USERS=\"octocat\"\n"), nil)
	d := distro(t, f, nil)
	for range 3 {
		if e, err := d.Estado(context.Background()); e != Dormida || err != nil {
			t.Fatalf("Estado = %q, %v", e, err)
		}
	}
	if n := f.hizo(leerEnv); n != 1 {
		t.Fatalf("the distribution was started %d times to be read", n)
	}
	if b, _ := os.ReadFile(d.marca()); string(b) != "octocat\n" {
		t.Fatalf("mark: %q", b)
	}
}

// A cancelled look is not a state.
func TestEstadoCancelado(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := nuevoFalso(t).cuando("-l -q", nil, errSalida)
	if e, err := distro(t, f, nil).Estado(ctx); e != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("Estado = %q, %v", e, err)
	}
}

func TestInstalarWSL(t *testing.T) {
	t.Run("already works: nobody is asked", func(t *testing.T) {
		f := nuevoFalso(t).cuando("--version", nil, nil)
		if err := distro(t, f, nil).InstalarWSL(context.Background()); err != nil {
			t.Fatal(err)
		}
		if f.hizo("elevar") != 0 {
			t.Fatal("it asked for administrator")
		}
	})
	t.Run("installed: restart", func(t *testing.T) {
		f := nuevoFalso(t).cuando("--version", u16(noInstalado), errSalida)
		if c := clavede(t, distro(t, f, nil).InstalarWSL(context.Background())); c != Reinicio {
			t.Fatalf("Clave = %s", c)
		}
		if f.hizo("elevar wsl.exe --install --no-distribution") != 1 {
			t.Fatalf("calls: %v", f.llamadas)
		}
	})
	t.Run("the person says no", func(t *testing.T) {
		f := nuevoFalso(t).cuando("--version", nil, errSalida)
		f.elevar = func() error { return &Problema{Clave: Cancelado} }
		if c := clavede(t, distro(t, f, nil).InstalarWSL(context.Background())); c != Cancelado {
			t.Fatalf("Clave = %s", c)
		}
	})
	t.Run("the installer fails", func(t *testing.T) {
		f := nuevoFalso(t).cuando("--version", nil, errSalida)
		f.elevar = func() error { return errors.New("wsl.exe as administrator: exit status 1") }
		err := distro(t, f, nil).InstalarWSL(context.Background())
		if c := clavede(t, err); c != Otro || !strings.Contains(err.Error(), "exit status 1") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("the installer ends with an error and WSL is there", func(t *testing.T) {
		f := nuevoFalso(t).cuando("--version", nil, errSalida)
		f.elevar = func() error {
			f.cuando("--version", nil, nil)
			return errors.New("exit status 3010")
		}
		if c := clavede(t, distro(t, f, nil).InstalarWSL(context.Background())); c != Reinicio {
			t.Fatalf("Clave = %s", c)
		}
	})
}

// publicacion serves a release: the distribution and its sums.
func publicacion(t *testing.T, contenido, sumado []byte) (*httptest.Server, *int) {
	t.Helper()
	suma := sha256.Sum256(sumado)
	bajadas := new(int)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  rendimiento-linux-amd64\n%s  %s\n", strings.Repeat("0", 64), hex.EncodeToString(suma[:]), archivo)
	})
	mux.HandleFunc("GET /"+archivo, func(w http.ResponseWriter, _ *http.Request) {
		*bajadas++
		w.Write(contenido)
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s, bajadas
}

func TestImportar(t *testing.T) {
	contenido := []byte(strings.Repeat("rendimiento", 100_000))
	antes := arquitectura
	arquitectura = "amd64" // the tests run on any machine
	defer func() { arquitectura = antes }()
	listo := func(t *testing.T) *falso {
		return nuevoFalso(t).cuando("-l -q", u16("Ubuntu\r\n"), nil).cuando("wsl.exe --version", nil, nil)
	}

	t.Run("downloads, checks, imports and cleans", func(t *testing.T) {
		s, _ := publicacion(t, contenido, contenido)
		f := listo(t).cuando("--import", u16("The operation completed successfully.\r\n"), nil)
		d := distro(t, f, nil)
		d.Descargas = s.URL
		d.marcar("an-earlier-one")
		var hecho, total int64
		if err := d.Importar(context.Background(), func(h, tt int64) { hecho, total = h, tt }); err != nil {
			t.Fatal(err)
		}
		destino := filepath.Join(d.Carpeta, archivo)
		want := "wsl.exe --import rendimiento " + d.disco() + " " + destino + " --version 2"
		if f.hizo(want) != 1 {
			t.Fatalf("calls: %v\nwant: %s", f.llamadas, want)
		}
		if hecho != int64(len(contenido)) || total != hecho {
			t.Fatalf("avance: %d of %d", hecho, total)
		}
		for _, resto := range []string{destino, destino + ".part", d.marca()} {
			if _, err := os.Stat(resto); err == nil {
				t.Errorf("%s was left", resto)
			}
		}
	})
	t.Run("already imported: untouched", func(t *testing.T) {
		f := nuevoFalso(t).cuando("-l -q", u16("rendimiento\r\n"), nil)
		d := distro(t, f, nil)
		d.Descargas = "http://127.0.0.1:1"
		if err := d.Importar(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		if f.hizo("--import") != 0 {
			t.Fatal("it imported over it")
		}
	})
	t.Run("imported meanwhile", func(t *testing.T) {
		s, _ := publicacion(t, contenido, contenido)
		f := listo(t).cuando("--import", u16("Error code: Wsl/Service/RegisterDistro/ERROR_ALREADY_EXISTS\r\n"), errSalida)
		d := distro(t, f, nil)
		d.Descargas = s.URL
		if err := d.Importar(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("another architecture", func(t *testing.T) {
		arquitectura = "arm64"
		defer func() { arquitectura = "amd64" }()
		f := nuevoFalso(t)
		if c := clavede(t, distro(t, f, nil).Importar(context.Background(), nil)); c != Otro || len(f.llamadas) != 0 {
			t.Fatalf("Clave = %s, calls %v", c, f.llamadas)
		}
	})
	t.Run("no WSL", func(t *testing.T) {
		f := nuevoFalso(t).cuando("-l -q", u16(noInstalado), errSalida)
		if c := clavede(t, distro(t, f, nil).Importar(context.Background(), nil)); c != Otro {
			t.Fatalf("Clave = %s", c)
		}
	})
	t.Run("the sum does not match", func(t *testing.T) {
		s, _ := publicacion(t, contenido, []byte("another file"))
		f := listo(t)
		d := distro(t, f, nil)
		d.Descargas = s.URL
		if c := clavede(t, d.Importar(context.Background(), nil)); c != Suma {
			t.Fatalf("Clave = %s", c)
		}
		if resto, _ := filepath.Glob(filepath.Join(d.Carpeta, archivo+"*")); len(resto) != 0 || f.hizo("--import") != 0 {
			t.Fatalf("left %v, calls %v", resto, f.llamadas)
		}
	})
	t.Run("no network", func(t *testing.T) {
		d := distro(t, listo(t), nil)
		d.Descargas = "http://127.0.0.1:1"
		if c := clavede(t, d.Importar(context.Background(), nil)); c != Descarga {
			t.Fatalf("Clave = %s", c)
		}
	})
	t.Run("the release has no such file", func(t *testing.T) {
		s := httptest.NewServer(http.NotFoundHandler())
		defer s.Close()
		d := distro(t, listo(t), nil)
		d.Descargas = s.URL
		if c := clavede(t, d.Importar(context.Background(), nil)); c != Descarga {
			t.Fatalf("Clave = %s", c)
		}
	})
	t.Run("cancelled while it downloads", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		mux := http.NewServeMux()
		mux.HandleFunc("GET /SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), archivo)
		})
		mux.HandleFunc("GET /"+archivo, func(w http.ResponseWriter, r *http.Request) {
			w.Write(contenido[:1000])
			w.(http.Flusher).Flush()
			cancel()
			<-r.Context().Done()
		})
		s := httptest.NewServer(mux)
		defer s.Close()
		d := distro(t, listo(t), nil)
		d.Descargas = s.URL
		if err := d.Importar(ctx, nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		if resto, _ := filepath.Glob(filepath.Join(d.Carpeta, archivo+"*")); len(resto) != 0 {
			t.Fatalf("left %v", resto)
		}
	})
	t.Run("WSL2 cannot start its machine: the download stays for the next try", func(t *testing.T) {
		s, bajadas := publicacion(t, contenido, contenido)
		for _, salida := range []string{sinHyperV, "Error code: Wsl/Service/RegisterDistro/CreateVm/ERROR_NOT_SUPPORTED\r\n"} {
			f := listo(t).cuando("--import", u16(salida), errSalida)
			d := distro(t, f, nil)
			d.Descargas = s.URL
			*bajadas = 0
			err := d.Importar(context.Background(), nil)
			if c := clavede(t, err); c != Virtualizacion || !strings.Contains(err.Error(), "Wsl/Service/RegisterDistro") {
				t.Fatalf("%v", err)
			}
			f.cuando("--import", nil, nil)
			if err := d.Importar(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			if *bajadas != 1 {
				t.Fatalf("downloaded %d times", *bajadas)
			}
		}
	})
	t.Run("wsl.exe refuses for another reason", func(t *testing.T) {
		s, _ := publicacion(t, contenido, contenido)
		f := listo(t).cuando("--import", u16("Error code: Wsl/Service/RegisterDistro/E_ACCESSDENIED\r\n"), errSalida)
		d := distro(t, f, nil)
		d.Descargas = s.URL
		if c := clavede(t, d.Importar(context.Background(), nil)); c != Otro {
			t.Fatalf("Clave = %s", c)
		}
	})
}

func TestConfigurar(t *testing.T) {
	t.Run("only a GitHub login", func(t *testing.T) {
		for _, malo := range []string{"", "-octocat", "octo cat", "octo_cat", "octocat\nMODE=x", "x; rm -rf /", "$(reboot)", "`id`", "a'b", strings.Repeat("a", 40), "josé"} {
			f := nuevoFalso(t)
			if c := clavede(t, distro(t, f, nil).Configurar(context.Background(), malo)); c != Usuario {
				t.Errorf("%q: Clave = %s", malo, c)
			}
			if len(f.llamadas) != 0 {
				t.Errorf("%q reached wsl.exe: %v", malo, f.llamadas)
			}
		}
	})
	t.Run("writes on standard input, restarts and remembers", func(t *testing.T) {
		f := nuevoFalso(t).
			cuando(leerEnv, []byte("wsl: a warning of its own\n# kept\nLOG_LEVEL=debug\nALLOWED_USERS=someone-else\n"), nil).
			cuando(escribirEnv, nil, nil).
			cuando("systemctl restart rendimiento", nil, nil)
		d := distro(t, f, nil)
		if err := d.Configurar(context.Background(), " Octo-Cat1 "); err != nil {
			t.Fatal(err)
		}
		want := []string{
			"wsl.exe -d rendimiento -u root --exec sh -c " + leerEnv,
			"wsl.exe -d rendimiento -u root --exec sh -c " + escribirEnv,
			"wsl.exe -d rendimiento -u root --exec systemctl restart rendimiento",
		}
		if strings.Join(f.llamadas, "\n") != strings.Join(want, "\n") {
			t.Fatalf("calls:\n%s", strings.Join(f.llamadas, "\n"))
		}
		if strings.Contains(strings.Join(f.llamadas, " "), "Octo-Cat1") {
			t.Fatal("the login is in a command line")
		}
		if got := f.entradas[1]; got != "# kept\nLOG_LEVEL=debug\nALLOWED_USERS=Octo-Cat1\n" {
			t.Fatalf("env written: %q", got)
		}
		if b, _ := os.ReadFile(d.marca()); string(b) != "Octo-Cat1\n" {
			t.Fatalf("mark: %q", b)
		}
	})
	t.Run("the distribution cannot start", func(t *testing.T) {
		f := nuevoFalso(t).cuando(leerEnv, u16(sinHyperV), errSalida)
		d := distro(t, f, nil)
		if c := clavede(t, d.Configurar(context.Background(), "octocat")); c != Virtualizacion {
			t.Fatalf("Clave = %s", c)
		}
		if _, err := os.Stat(d.marca()); err == nil {
			t.Fatal("it remembered what it did not write")
		}
	})
	t.Run("systemd refuses", func(t *testing.T) {
		f := nuevoFalso(t).cuando(leerEnv, nil, nil).cuando(escribirEnv, nil, nil).
			cuando("systemctl", []byte("System has not been booted with systemd as init system (PID 1). Can't operate.\n"), errSalida)
		err := distro(t, f, nil).Configurar(context.Background(), "octocat")
		if c := clavede(t, err); c != Otro || !strings.Contains(err.Error(), "systemd") {
			t.Fatalf("%v", err)
		}
	})
}

func TestConUsuario(t *testing.T) {
	for _, c := range []struct{ env, quiere string }{
		{"", "ALLOWED_USERS=octocat\n"},
		{"ALLOWED_USERS=a,b\n", "ALLOWED_USERS=octocat\n"},
		{"A=1\n\nALLOWED_USERS=a\nB=2\nALLOWED_USERS=b", "A=1\nALLOWED_USERS=octocat\nB=2\n"},
		{"#ALLOWED_USERS=a\nGITHUB_URL=https://github.example.com", "#ALLOWED_USERS=a\nGITHUB_URL=https://github.example.com\nALLOWED_USERS=octocat\n"},
	} {
		if got := conUsuario(c.env, "octocat"); got != c.quiere {
			t.Errorf("conUsuario(%q) = %q, want %q", c.env, got, c.quiere)
		}
	}
}

func TestDespertar(t *testing.T) {
	conUsuario := []byte("ALLOWED_USERS=octocat\n")

	t.Run("keeps it awake and waits for the answer", func(t *testing.T) {
		corto(t, &pausa, time.Millisecond)
		var mu sync.Mutex
		intentos := 0
		servidor := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			if intentos++; intentos < 4 {
				http.Error(w, "starting", http.StatusServiceUnavailable)
			}
		})
		f := nuevoFalso(t).cuando(leerEnv, conUsuario, nil)
		d := distro(t, f, servidor)
		if err := d.Despertar(context.Background()); err != nil {
			t.Fatal(err)
		}
		if f.hizo("iniciar wsl.exe -d rendimiento --exec sleep infinity") != 1 || f.vivos != 1 {
			t.Fatalf("calls %v, alive %d", f.llamadas, f.vivos)
		}
		if _, err := os.Stat(d.marca()); err != nil {
			t.Fatal("it did not remember the user it read")
		}
	})
	t.Run("twice and from two goroutines: one keep-alive is left", func(t *testing.T) {
		f := nuevoFalso(t).cuando(leerEnv, conUsuario, nil).cuando("--terminate rendimiento", nil, nil)
		d := distro(t, f, sana(true))
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := d.Despertar(context.Background()); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if f.vivos != 1 {
			t.Fatalf("alive: %d", f.vivos)
		}
		d.Soltar()
		d.Soltar()
		if f.vivos != 0 || f.hizo("--terminate") != 0 {
			t.Fatalf("after Soltar: alive %d, calls %v", f.vivos, f.llamadas)
		}
		if err := d.Despertar(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := d.Dormir(context.Background()); err != nil {
			t.Fatal(err)
		}
		if f.vivos != 0 || f.hizo("wsl.exe --terminate rendimiento") != 1 {
			t.Fatalf("after Dormir: alive %d, calls %v", f.vivos, f.llamadas)
		}
	})
	t.Run("no answer in time: what the journal says", func(t *testing.T) {
		corto(t, &pausa, time.Millisecond)
		corto(t, &paciencia, 30*time.Millisecond)
		f := nuevoFalso(t).cuando(leerEnv, conUsuario, nil).
			cuando("journalctl -u rendimiento -n 5 --no-pager", []byte("level=ERROR msg=\"postgres: connection refused\"\n"), nil)
		err := distro(t, f, nil).Despertar(context.Background())
		if c := clavede(t, err); c != Arranque || !strings.Contains(err.Error(), "connection refused") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("nobody may sign in: it does not wait, and Estado asks again", func(t *testing.T) {
		f := nuevoFalso(t).cuando(leerEnv, nil, nil)
		d := distro(t, f, nil)
		d.marcar("octocat")
		if c := clavede(t, d.Despertar(context.Background())); c != Arranque {
			t.Fatalf("Clave = %s", c)
		}
		if _, err := os.Stat(d.marca()); err == nil || f.vivos != 0 {
			t.Fatalf("mark kept, or alive: %d", f.vivos)
		}
	})
	t.Run("WSL2 cannot start its machine", func(t *testing.T) {
		f := nuevoFalso(t).cuando(leerEnv, u16(sinHyperV), errSalida)
		if c := clavede(t, distro(t, f, nil).Despertar(context.Background())); c != Virtualizacion || f.vivos != 0 {
			t.Fatalf("Clave = %s, alive %d", c, f.vivos)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		corto(t, &pausa, time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		f := nuevoFalso(t).cuando(leerEnv, conUsuario, nil)
		if err := distro(t, f, nil).Despertar(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDormirFalla(t *testing.T) {
	f := nuevoFalso(t).cuando("--terminate", u16("Error code: Wsl/Service/WSL_E_DISTRO_NOT_FOUND\r\n"), errSalida)
	err := distro(t, f, nil).Dormir(context.Background())
	if c := clavede(t, err); c != Otro || !strings.Contains(err.Error(), "WSL_E_DISTRO_NOT_FOUND") {
		t.Fatalf("%v", err)
	}
}

// The setup token comes back trimmed, and never in an error.
func TestFicha(t *testing.T) {
	const ficha = "3f9a1c0de4b5a69788f6e5d4c3b2a190"
	for _, c := range []struct {
		nombre string
		salida []byte
		err    error
		quiere string
		clave  string
	}{
		{nombre: "alone", salida: []byte(ficha + "\n"), quiere: ficha},
		{nombre: "after a UTF-8 warning of wsl.exe", salida: []byte("wsl: a localhost proxy was detected\r\n" + ficha + "\n"), quiere: ficha},
		{nombre: "after a UTF-16 warning of wsl.exe", salida: append(u16("wsl: se detectó una configuración de proxy\r\n"), ficha+"\n"...), quiere: ficha},
		{nombre: "cat fails with a part of it", salida: []byte(ficha + "cat: read error\n"), err: errSalida, clave: Otro},
		{nombre: "the machine cannot start", salida: append(u16(sinHyperV), ficha...), err: errSalida, clave: Virtualizacion},
		{nombre: "empty", salida: []byte("\n"), clave: Otro},
		{nombre: "only noise", salida: []byte("one line\nanother line\n"), clave: Otro},
	} {
		t.Run(c.nombre, func(t *testing.T) {
			f := nuevoFalso(t).cuando("--exec cat "+rutaFicha, c.salida, c.err)
			got, err := distro(t, f, nil).Ficha(context.Background())
			if c.clave == "" {
				if err != nil || got != c.quiere {
					t.Fatalf("Ficha = %q, %v", got, err)
				}
				return
			}
			if k := clavede(t, err); k != c.clave || got != "" {
				t.Fatalf("Ficha = %q, %v", got, err)
			}
			if strings.Contains(err.Error(), ficha[:8]) {
				t.Fatalf("the token is in the error: %v", err)
			}
		})
	}
}

func TestConfigurado(t *testing.T) {
	for _, quiere := range []bool{true, false} {
		got, err := distro(t, nuevoFalso(t), sana(quiere)).Configurado(context.Background())
		if err != nil || got != quiere {
			t.Fatalf("Configurado = %v, %v; want %v", got, err, quiere)
		}
	}
	if _, err := distro(t, nuevoFalso(t), nil).Configurado(context.Background()); err == nil {
		t.Fatal("no error with nothing answering")
	}
	if _, err := distro(t, nuevoFalso(t), http.NotFoundHandler()).Configurado(context.Background()); err == nil {
		t.Fatal("no error on a 404")
	}
}

func TestReloj(t *testing.T) {
	real := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method %s", r.Method)
		}
		w.Header().Set("Date", real.Format(http.TimeFormat))
		w.WriteHeader(http.StatusForbidden) // any answer has the time
	}))
	defer s.Close()
	antesURL, antesAhora := relojURL, ahora
	defer func() { relojURL, ahora = antesURL, antesAhora }()
	relojURL = s.URL
	for _, c := range []struct{ aqui, quiere time.Duration }{
		{0, 0},
		{59 * time.Second, 0},
		{-59 * time.Second, 0},
		{10 * time.Hour, 10 * time.Hour},
		{-90 * time.Second, -90 * time.Second},
	} {
		ahora = func() time.Time { return real.Add(c.aqui) }
		got, err := Reloj(context.Background(), nil)
		if err != nil || got != c.quiere {
			t.Errorf("clock off by %s: Reloj = %s, %v", c.aqui, got, err)
		}
	}
	relojURL = "http://127.0.0.1:1"
	if _, err := Reloj(context.Background(), s.Client()); err == nil {
		t.Fatal("no error with nothing answering")
	}
}

func TestTexto(t *testing.T) {
	for _, c := range []struct {
		nombre string
		b      []byte
		quiere string
	}{
		{"UTF-8", []byte("máquina\r\nlista\n"), "máquina\nlista\n"},
		{"UTF-8 with its mark", append([]byte{0xEF, 0xBB, 0xBF}, "hola"...), "hola"},
		{"UTF-16", u16("Versión de WSL: 2\r\nrendimiento\r\n"), "Versión de WSL: 2\nrendimiento\n"},
		{"UTF-16 with its mark", append([]byte{0xFF, 0xFE}, u16("día ’ 日本")...), "día ’ 日本"},
		{"UTF-16 beyond Latin", u16("Ошибка: 0x80370102\r\n"), "Ошибка: 0x80370102\n"},
		{"UTF-16, then UTF-8", append(u16("wsl: configuración\r\n"), "ALLOWED_USERS=octocat\n"...), "wsl: configuración\nALLOWED_USERS=octocat\n"},
		{"UTF-8, then UTF-16", append([]byte("uno\n"), u16("Error code: Wsl/X\r\n")...), "uno\nError code: Wsl/X\n"},
		{"an odd byte at the end", append(u16("ok"), 'x'), "ok"},
		{"nothing", nil, ""},
	} {
		if got := texto(c.b); got != c.quiere {
			t.Errorf("%s: %q, want %q", c.nombre, got, c.quiere)
		}
	}
}

func TestSistemaFueraDeWindows(t *testing.T) {
	d := Nueva(t.TempDir(), Sistema())
	if _, err := d.Estado(context.Background()); err == nil {
		t.Skip("this is Windows")
	}
}
