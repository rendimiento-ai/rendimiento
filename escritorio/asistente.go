package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/escritorio/wsl"
)

// distro is what the assistant asks of rendimiento's distribution
// (*wsl.Distro; a fake in tests).
type distro interface {
	Estado(ctx context.Context) (wsl.Estado, error)
	InstalarWSL(ctx context.Context) error
	Importar(ctx context.Context, avance func(hecho, total int64)) error
	Configurar(ctx context.Context, usuario string) error
	Despertar(ctx context.Context) error
	Dormir(ctx context.Context) error
	Soltar()
	Ficha(ctx context.Context) (string, error)
	Configurado(ctx context.Context) (bool, error)
}

// ventana is the app's window (Wails on Windows; a fake in tests).
type ventana interface {
	Emitir(v Vista)       // gives the assistant's page what to show
	Ir(direccion string)  // takes the window to that site, unless it is there
	Volver()              // brings the assistant's page back
	Mostrar()             // shows the window, in front
	Navegador(dir string) // opens an address in the person's browser
	Salir()               // ends the app
}

// Asistente takes a computer from nothing to rendimiento awake, one step
// at a time, and then lends the window to rendimiento's own pages. Its
// exported methods are what the assistant's page may call, and nothing
// else: Wails binds them all.
type Asistente struct {
	d         distro
	direccion string
	idioma    string
	version   string
	ctx       context.Context
	reloj     func(context.Context) time.Duration
	cada      time.Duration // how often the progress of a download is shown

	mu    sync.Mutex
	s     situacion
	v     ventana    // nil until the window exists
	fuera bool       // the window is not on the assistant's page
	menu  func(Menu) // the icon, when there is one
}

func nuevoAsistente(ctx context.Context, d distro, direccion, idioma, version string, reloj func(context.Context) time.Duration) *Asistente {
	return &Asistente{
		d: d, direccion: direccion, idioma: idioma, version: version, ctx: ctx, reloj: reloj,
		cada: 250 * time.Millisecond,
		// Busy from the start, so the page cannot ask for anything before
		// the first look.
		s: situacion{tarea: "mirar"},
	}
}

// Vista is what the page shows now. The page asks when it loads, so the
// question also says the window is back on the assistant.
func (a *Asistente) Vista() Vista {
	a.mu.Lock()
	a.fuera = false
	s := a.s
	a.mu.Unlock()
	if s.entra() {
		go a.cambiar(nil)
	}
	return ver(s, a.idioma, a.version)
}

// Instalar turns WSL on.
func (a *Asistente) Instalar() { a.hacer("instalar", a.d.InstalarWSL) }

// Importar downloads and imports the distribution.
func (a *Asistente) Importar() {
	a.hacer("importar", func(ctx context.Context) error {
		var ultimo time.Time
		return a.d.Importar(ctx, func(hecho, total int64) {
			// A download reports every few kilobytes; the page needs a
			// few times a second, and the end.
			if hecho < total && time.Since(ultimo) < a.cada {
				return
			}
			ultimo = time.Now()
			a.cambiar(func(s *situacion) { s.hecho, s.total = hecho, total })
		})
	})
}

// Configurar writes who signs in and wakes rendimiento up.
func (a *Asistente) Configurar(usuario string) {
	a.hacer("configurar", func(ctx context.Context) error {
		if err := a.d.Configurar(ctx, usuario); err != nil {
			return err
		}
		a.cambiar(func(s *situacion) { s.tarea = "despertar" })
		return a.despertar(ctx)
	})
}

// Despertar starts rendimiento.
func (a *Asistente) Despertar() { a.hacer("despertar", a.despertar) }

// despertar wakes the distribution once; a wake that fails must not leave
// behind what would keep it awake.
func (a *Asistente) despertar(ctx context.Context) error {
	err := a.d.Despertar(ctx)
	if err != nil {
		a.d.Soltar()
	}
	return err
}

// Conectar takes the window to rendimiento's page that creates its GitHub
// App. The setup token goes in the address and nowhere else.
func (a *Asistente) Conectar() {
	a.hacer("conectar", func(ctx context.Context) error {
		ficha, err := a.d.Ficha(ctx)
		if err != nil {
			return err
		}
		a.salirA(a.direccion + "/api/setup/github?token=" + url.QueryEscape(ficha))
		return nil
	})
}

// Entrar takes the window to rendimiento's pages.
func (a *Asistente) Entrar() {
	if a.ahora().afuera() {
		a.salirA(a.direccion)
	}
}

// Revisar looks at the computer again, and at its clock.
func (a *Asistente) Revisar() {
	a.hacer("mirar", func(ctx context.Context) error {
		a.medir()
		return nil
	})
}

// empezar is called once the window exists: it looks at the computer and,
// when rendimiento is ready to run, wakes it without asking (waking is
// also what keeps it awake while the app is open).
func (a *Asistente) empezar(v ventana) {
	a.mu.Lock()
	a.v = v
	a.mu.Unlock()
	go a.medir()
	e, err := a.d.Estado(a.ctx)
	if err == nil && (e == wsl.Dormida || e == wsl.Despierta) {
		a.cambiar(func(s *situacion) { s.estado, s.tarea = e, "despertar" })
		a.mirar(a.despertar(a.ctx))
	} else {
		a.poner(e, err, nil)
	}
	go a.vigilar(30 * time.Second)
}

// abrir shows the window. When it was left on GitHub's pages and GitHub
// is already connected, it goes to rendimiento's: GitHub does not send
// the person back after choosing repositories.
func (a *Asistente) abrir() {
	a.mu.Lock()
	v, s, fuera := a.v, a.s, a.fuera
	a.mu.Unlock()
	if v == nil {
		return
	}
	v.Mostrar()
	if !fuera || !s.afuera() {
		return
	}
	if !s.configurado {
		if hecho, _ := a.d.Configurado(a.ctx); hecho {
			s.configurado = true
			a.cambiar(func(s *situacion) { s.configurado = true })
		}
	}
	if s.configurado {
		v.Ir(a.direccion)
	}
}

// alternar is the icon's "Sleep" or "Wake up".
func (a *Asistente) alternar() {
	switch a.ahora().estado {
	case wsl.Despierta:
		a.hacer("dormir", a.d.Dormir)
	case wsl.Dormida:
		a.Despertar()
	}
}

// navegador opens rendimiento in the person's browser.
func (a *Asistente) navegador() {
	if v := a.laVentana(); v != nil && a.ahora().estado == wsl.Despierta {
		v.Navegador(a.direccion)
	}
}

// salir ends the app; main lets go of the distribution after the window.
func (a *Asistente) salir() {
	if v := a.laVentana(); v != nil {
		v.Salir()
	}
}

// escuchar gives the icon each change of its menu, starting now.
func (a *Asistente) escuchar(menu func(Menu)) {
	a.mu.Lock()
	a.menu = menu
	s := a.s
	a.mu.Unlock()
	menu(menuDe(s, a.idioma))
}

func (a *Asistente) ahora() situacion {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.s
}

func (a *Asistente) laVentana() ventana {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}

// salirA takes the window away from the assistant's page.
func (a *Asistente) salirA(direccion string) {
	a.mu.Lock()
	a.fuera = true
	v := a.v
	a.mu.Unlock()
	if v != nil {
		v.Ir(direccion)
	}
}

// hacer runs one task at a time, to its end, and looks at the computer
// again: what the page shows next is what the computer says, not what the
// task meant to do.
func (a *Asistente) hacer(tarea string, f func(context.Context) error) {
	a.mu.Lock()
	libre := a.s.tarea == ""
	a.mu.Unlock()
	if !libre {
		return
	}
	a.cambiar(func(s *situacion) { s.tarea, s.problema = tarea, nil })
	err := f(a.ctx)
	if err != nil {
		log.Printf("%s: %v", tarea, err)
	}
	a.mirar(err)
}

// mirar asks the computer where it is and ends the task, with what went
// wrong in it, if anything.
func (a *Asistente) mirar(err error) {
	e, errEstado := a.d.Estado(a.ctx)
	a.poner(e, errEstado, err)
}

// poner ends the task with what a look at the computer said. A look that
// failed says nothing about where the computer is: the page keeps its
// step and offers to check again.
func (a *Asistente) poner(e wsl.Estado, errEstado, err error) {
	hecho := false
	if errEstado != nil {
		log.Printf("estado: %v", errEstado)
		if err == nil {
			err = errEstado
		}
	} else if e == wsl.Despierta {
		hecho, _ = a.d.Configurado(a.ctx)
	}
	a.cambiar(func(s *situacion) {
		if errEstado == nil {
			s.estado, s.configurado = e, hecho
		}
		s.tarea, s.hecho, s.total, s.problema = "", 0, 0, problemaDe(err)
	})
}

// medir compares the clock with the real time. Without an answer there is
// nothing to warn about.
func (a *Asistente) medir() {
	ctx, cancelar := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancelar()
	d := a.reloj(ctx)
	a.cambiar(func(s *situacion) { s.reloj = d })
}

// vigilar notices what happens without the app: rendimiento stopped from
// outside, or GitHub connected in the window while the assistant waits.
func (a *Asistente) vigilar(cada time.Duration) {
	reloj := time.NewTicker(cada)
	defer reloj.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-reloj.C:
			a.vistazo()
		}
	}
}

func (a *Asistente) vistazo() {
	a.mu.Lock()
	s, fuera := a.s, a.fuera
	a.mu.Unlock()
	if s.tarea != "" {
		return
	}
	if s.estado != wsl.Despierta {
		if fuera {
			a.cambiar(nil) // the assistant's page did not come back: again
		}
		return
	}
	// rendimiento's own answer is the cheap question; wsl.exe is asked
	// only when it stops answering.
	e := wsl.Despierta
	hecho, err := a.d.Configurado(a.ctx)
	if err != nil {
		if e, err = a.d.Estado(a.ctx); err != nil {
			return
		}
		hecho = s.configurado && e == wsl.Despierta
	}
	if e == s.estado && hecho == s.configurado {
		return
	}
	a.cambiar(func(s *situacion) {
		if s.tarea == "" {
			s.estado, s.configurado = e, hecho
		}
	})
}

// cambiar applies a change and puts the window and the icon where the new
// situation says.
func (a *Asistente) cambiar(f func(*situacion)) {
	a.mu.Lock()
	if f != nil {
		f(&a.s)
	}
	s, v, fuera, menu := a.s, a.v, a.fuera, a.menu
	entra := v != nil && !fuera && s.entra()
	if entra {
		a.fuera = true
	}
	a.mu.Unlock()
	if menu != nil {
		menu(menuDe(s, a.idioma))
	}
	switch {
	case v == nil:
	case entra:
		v.Ir(a.direccion)
	case fuera && s.afuera():
		// The person is in rendimiento's pages, or GitHub's: leave them.
	case fuera:
		// Stays "fuera" until the page asks for its Vista.
		v.Volver()
	default:
		v.Emitir(ver(s, a.idioma, a.version))
	}
}

// problemaDe is an error as the page explains it.
func problemaDe(err error) *wsl.Problema {
	if err == nil {
		return nil
	}
	var p *wsl.Problema
	if errors.As(err, &p) {
		return p
	}
	return &wsl.Problema{Clave: wsl.Otro, Detalle: err.Error()}
}

// guionIr is the script that takes the window to an address unless it is
// already on that site: it must not reload rendimiento's pages under the
// person each time the window is shown.
func guionIr(direccion string) string {
	origen := direccion
	if u, err := url.Parse(direccion); err == nil {
		origen = u.Scheme + "://" + u.Host
	}
	o, _ := json.Marshal(origen)
	d, _ := json.Marshal(direccion)
	return fmt.Sprintf("if(location.origin!==%s)location.replace(%s)", o, d)
}
