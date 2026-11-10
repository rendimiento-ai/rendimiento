package main

import (
	"time"

	"github.com/rendimiento-ai/rendimiento/escritorio/wsl"
)

// situacion is what the app knows: where the computer is, what the app is
// doing about it and what went wrong the last time.
type situacion struct {
	estado      wsl.Estado // "" until the first look
	configurado bool       // rendimiento has its GitHub App
	tarea       string     // "", mirar, instalar, importar, configurar, despertar, dormir, conectar
	problema    *wsl.Problema
	hecho       int64 // bytes of the download
	total       int64
	reloj       time.Duration // how far the clock is; 0 is fine
}

// entra says the window belongs to rendimiento's own pages: nothing is
// left for the assistant to ask or to say.
func (s situacion) entra() bool {
	return s.afuera() && s.configurado && s.problema == nil && s.reloj == 0
}

// afuera says a window that already left the assistant may stay where it
// is (rendimiento's pages, or GitHub's while it is being connected).
func (s situacion) afuera() bool { return s.estado == wsl.Despierta && s.tarea == "" }

// Paso is one step of the assistant's list.
type Paso struct {
	Nombre string `json:"nombre"`
	Estado string `json:"estado"` // "hecho", "actual" or ""
}

// Vista is what the assistant's page shows, already in the person's
// language: the page only paints it.
type Vista struct {
	Idioma   string `json:"idioma"`
	Pasos    []Paso `json:"pasos"`
	Titulo   string `json:"titulo"`
	Texto    string `json:"texto"`
	Boton    string `json:"boton"`    // "" when there is nothing to press
	Accion   string `json:"accion"`   // the Asistente method the button calls
	Campo    string `json:"campo"`    // the label of the text the action takes, or ""
	Ocupado  bool   `json:"ocupado"`  // the app is working
	Avance   int    `json:"avance"`   // percent of the download, or -1
	Problema string `json:"problema"` // what went wrong, explained
	Detalle  string `json:"detalle"`  // what the system said
	Reloj    string `json:"reloj"`    // the warning about the clock, or ""
	Revisar  string `json:"revisar"`  // the label of "check again", or ""
	Version  string `json:"version"`
}

// pasos are the assistant's steps, each with the state it resolves and
// the Asistente method its button calls.
var pasos = []struct {
	estado        wsl.Estado
	clave, accion string
}{
	{wsl.SinWSL, "wsl", "Instalar"},
	{wsl.SinDistro, "descarga", "Importar"},
	{wsl.SinUsuario, "usuario", "Configurar"},
	{wsl.Dormida, "arranque", "Despertar"},
	{wsl.Despierta, "github", "Conectar"},
}

const pasoArranque = 3

// pasoDe is the step a situation is on: -1 before the first look,
// len(pasos) when all are done.
func pasoDe(s situacion) int {
	switch {
	case s.tarea == "despertar" || s.tarea == "dormir":
		// Waking also follows "who signs in", before the state says so.
		return pasoArranque
	case s.estado == wsl.Despierta && s.configurado:
		return len(pasos)
	}
	for i, p := range pasos {
		if p.estado == s.estado {
			return i
		}
	}
	return -1
}

// ocupado is what the page says while each task runs.
var ocupado = map[string]string{
	"instalar":   "wsl.ocupado",
	"configurar": "usuario.ocupado",
	"despertar":  "arranque.ocupado",
	"dormir":     "dormir.ocupado",
	"conectar":   "github.ocupado",
}

// ver is the page for a situation.
func ver(s situacion, idioma, version string) Vista {
	t := func(clave string, valores ...any) string { return T(idioma, clave, valores...) }
	v := Vista{Idioma: idioma, Version: version, Avance: -1, Ocupado: s.tarea != ""}
	n := pasoDe(s)
	for i, p := range pasos {
		estado := ""
		if i < n {
			estado = "hecho"
		} else if i == n {
			estado = "actual"
		}
		v.Pasos = append(v.Pasos, Paso{t("paso." + p.clave), estado})
	}
	switch {
	case n < 0 && v.Ocupado:
		v.Titulo = t("mirar.titulo")
	case n < 0:
		v.Titulo = t("mirar.fallo")
	case n == len(pasos):
		v.Titulo, v.Texto, v.Boton, v.Accion = t("listo.titulo"), t("listo.texto"), t("listo.boton"), "Entrar"
	default:
		p := pasos[n]
		v.Titulo, v.Texto, v.Boton, v.Accion = t(p.clave+".titulo"), t(p.clave+".texto"), t(p.clave+".boton"), p.accion
		if p.accion == "Configurar" {
			v.Campo = t("usuario.campo")
		}
	}
	switch {
	case s.problema == nil:
	case s.problema.Clave == wsl.Reinicio:
		// Not a failure: it is what installing WSL ends with.
		v.Titulo, v.Texto, v.Boton, v.Accion = t("reinicio.titulo"), t("reinicio.texto"), "", ""
	default:
		clave := "problema." + s.problema.Clave
		if _, hay := textos[clave]; !hay {
			clave = "problema.otro"
		}
		v.Problema, v.Detalle = t(clave), s.problema.Detalle
	}
	if v.Ocupado {
		v.Boton, v.Accion, v.Campo = "", "", ""
		switch {
		case s.tarea == "dormir":
			v.Titulo, v.Texto = t("dormir.titulo"), t("dormir.ocupado")
		case s.tarea == "importar" && s.total <= 0:
			v.Texto = t("descarga.empieza")
		case s.tarea == "importar" && s.hecho < s.total:
			const mb = 1 << 20
			v.Texto = t("descarga.ocupado", s.hecho/mb, s.total/mb)
			v.Avance = int(s.hecho * 100 / s.total)
		case s.tarea == "importar":
			v.Texto = t("descarga.importa")
		case ocupado[s.tarea] != "":
			v.Texto = t(ocupado[s.tarea])
		}
	} else {
		v.Revisar = t("revisar")
	}
	if s.reloj != 0 {
		v.Reloj = textoReloj(s.reloj, idioma)
	}
	return v
}

// textoReloj is the warning for a clock that is off by d.
func textoReloj(d time.Duration, idioma string) string {
	clave := "reloj.adelantado"
	if d < 0 {
		clave, d = "reloj.atrasado", -d
	}
	cuanto := T(idioma, "reloj.min", int(d.Minutes()))
	if d >= 2*time.Hour {
		cuanto = T(idioma, "reloj.h", int(d.Hours()))
	}
	return T(idioma, clave, cuanto)
}

// Menu is what the icon next to the clock says.
type Menu struct {
	Estado    string // the icon's tooltip
	Alternar  string // "Sleep" or "Wake up"
	Activo    bool   // whether Alternar can be chosen now
	Navegador bool   // whether there is something to open in the browser
}

// menuDe is the icon's menu for a situation.
func menuDe(s situacion, idioma string) Menu {
	m := Menu{Estado: T(idioma, "estado.pendiente"), Alternar: T(idioma, "bandeja.despertar")}
	switch {
	case s.tarea == "despertar":
		m.Estado = T(idioma, "estado.despertando")
	case s.tarea == "dormir":
		m.Estado, m.Alternar = T(idioma, "estado.durmiendo"), T(idioma, "bandeja.dormir")
	case s.estado == wsl.Dormida:
		m.Estado, m.Activo = T(idioma, "estado.dormida"), s.tarea == ""
	case s.estado == wsl.Despierta:
		m.Estado, m.Alternar = T(idioma, "estado.despierta"), T(idioma, "bandeja.dormir")
		m.Activo, m.Navegador = s.tarea == "", true
	}
	return m
}
