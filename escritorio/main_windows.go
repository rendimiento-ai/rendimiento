//go:build windows

// rendimiento for Windows: the window, the icon next to the clock and the
// first-time assistant over rendimiento's WSL2 distribution (package wsl).
// It builds with plain go build (-tags desktop,production, CGO_ENABLED=0):
// the page in frontend/ needs no npm and no wails command.
package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"fyne.io/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailswindows "github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

	"github.com/rendimiento-ai/rendimiento/escritorio/wsl"
)

func main() {
	carpeta := carpetaDe()
	registrar(carpeta)
	log.Printf("rendimiento %s", version)

	idioma := "en"
	if preferidos, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); err == nil {
		idioma = idiomaDe(preferidos)
	}

	ctx, cancelar := context.WithCancel(context.Background())
	d := wsl.Nueva(carpeta, wsl.Sistema())
	a := nuevoAsistente(ctx, d, d.Direccion, idioma, version, func(ctx context.Context) time.Duration {
		// Without an answer (no network, or a clock so far off that TLS
		// fails) there is nothing sure to say.
		lejos, err := wsl.Reloj(ctx, &http.Client{})
		if err != nil {
			log.Printf("reloj: %v", err)
			return 0
		}
		return lejos
	})
	bandeja(a, idioma)

	mensajes := wailswindows.DefaultMessages()
	mensajes.Error = T(idioma, "webview.error")
	mensajes.MissingRequirements = T(idioma, "webview.requisito")
	mensajes.InstallationRequired = T(idioma, "webview.falta") + " "
	mensajes.Webview2NotInstalled = T(idioma, "webview.falta") + " "
	mensajes.UpdateRequired = T(idioma, "webview.viejo") + " "
	mensajes.PressOKToInstall = T(idioma, "webview.instalar")
	mensajes.FailedToInstall = T(idioma, "webview.fallo")

	err := wails.Run(&options.App{
		Title:             "rendimiento",
		Width:             1100,
		Height:            760,
		MinWidth:          720,
		MinHeight:         560,
		HideWindowOnClose: true, // the X leaves rendimiento running, under its icon
		BackgroundColour:  options.NewRGB(0xFB, 0xF5, 0xEA),
		AssetServer:       &assetserver.Options{Assets: fs.FS(pagina)},
		Logger:            registro{},
		Bind:              []interface{}{a},
		// Wails starts this in its own goroutine: looking at the
		// computer takes seconds and must not hold the window.
		OnStartup: func(wctx context.Context) { a.empezar(ventanaWails{wctx}) },
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "ai.rendimiento.escritorio",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { go a.abrir() },
		},
		Windows: &wailswindows.Options{
			WebviewUserDataPath: filepath.Join(carpeta, "webview"),
			Messages:            mensajes,
		},
	})
	if err != nil {
		log.Printf("ventana: %v", err)
	}
	cancelar()
	d.Soltar()
	systray.Quit()
}

// carpetaDe is where the app keeps the distribution, its downloads, the
// window's data and its log: %LOCALAPPDATA%\rendimiento.
func carpetaDe() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	carpeta := filepath.Join(base, "rendimiento")
	os.MkdirAll(carpeta, 0o755)
	return carpeta
}

// registrar sends the log to rendimiento.log: a windowed program has no
// console. One earlier log is kept, so the file cannot grow forever.
func registrar(carpeta string) {
	ruta := filepath.Join(carpeta, "rendimiento.log")
	if i, err := os.Stat(ruta); err == nil && i.Size() > 1<<20 {
		os.Rename(ruta, ruta+".1")
	}
	if f, err := os.OpenFile(ruta, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(f)
	}
}

// registro puts what Wails logs (errors only, in production) in the same
// file.
type registro struct{}

func (registro) Print(m string)   { log.Print(m) }
func (registro) Trace(m string)   { log.Print(m) }
func (registro) Debug(m string)   { log.Print(m) }
func (registro) Info(m string)    { log.Print(m) }
func (registro) Warning(m string) { log.Print(m) }
func (registro) Error(m string)   { log.Print(m) }
func (registro) Fatal(m string)   { log.Print(m) }

// ventanaWails is the window, for the assistant. Every call is safe from
// any goroutine: Wails passes them to the window's thread.
type ventanaWails struct{ ctx context.Context }

func (v ventanaWails) Emitir(vista Vista) { runtime.EventsEmit(v.ctx, "vista", vista) }

// Ir runs in whatever page the window has: WebView2 lets it go anywhere.
func (v ventanaWails) Ir(direccion string) { runtime.WindowExecJS(v.ctx, guionIr(direccion)) }

// Volver loads the app's own page again, from any site (Wails does it by
// setting location.href in the page that is loaded).
func (v ventanaWails) Volver() { runtime.WindowReloadApp(v.ctx) }

func (v ventanaWails) Mostrar() { runtime.WindowShow(v.ctx) }

func (v ventanaWails) Navegador(direccion string) { runtime.BrowserOpenURL(v.ctx, direccion) }

func (v ventanaWails) Salir() { runtime.Quit(v.ctx) }
