//go:build windows

package main

import (
	"runtime"
	"sync"

	"fyne.io/systray"
)

// bandeja puts rendimiento's icon next to the clock. A click on it shows
// the window; the other button, its menu.
func bandeja(a *Asistente, idioma string) {
	go func() {
		// The icon's hidden window and its messages belong to one thread
		// of their own: the main one is Wails'.
		runtime.LockOSThread()
		systray.Run(func() { atender(a, idioma) }, nil)
	}()
}

// atender fills the menu and serves its clicks, each in its own goroutine:
// waking takes minutes, and the menu must keep answering.
func atender(a *Asistente, idioma string) {
	systray.SetIcon(icono)
	systray.SetTooltip("rendimiento")
	systray.SetOnTapped(func() { go a.abrir() })
	abrir := systray.AddMenuItem(T(idioma, "bandeja.abrir"), "")
	inicio := systray.AddMenuItem(T(idioma, "bandeja.inicio"), "")
	navegador := systray.AddMenuItem(T(idioma, "bandeja.navegador"), "")
	alternar := systray.AddMenuItem(T(idioma, "bandeja.despertar"), "")
	systray.AddSeparator()
	salir := systray.AddMenuItem(T(idioma, "bandeja.salir"), "")

	var mu sync.Mutex // changes come from several goroutines
	a.escuchar(func(m Menu) {
		mu.Lock()
		defer mu.Unlock()
		systray.SetTooltip(m.Estado)
		alternar.SetTitle(m.Alternar)
		activar(alternar, m.Activo)
		activar(navegador, m.Navegador)
	})
	for {
		select {
		case <-abrir.ClickedCh:
			go a.abrir()
		case <-inicio.ClickedCh:
			go a.inicio()
		case <-navegador.ClickedCh:
			go a.navegador()
		case <-alternar.ClickedCh:
			go a.alternar()
		case <-salir.ClickedCh:
			go a.salir()
		}
	}
}

func activar(m *systray.MenuItem, si bool) {
	if si {
		m.Enable()
	} else {
		m.Disable()
	}
}
