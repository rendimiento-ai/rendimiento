package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/escritorio/wsl"
)

func TestVer(t *testing.T) {
	const mb = 1 << 20
	for _, c := range []struct {
		nombre string
		s      situacion
		actual string // the step that is marked, in English
		titulo string
		accion string
		campo  bool
		avance int
		dice   string // somewhere in the text or the problem
	}{
		{"first look", situacion{tarea: "mirar"}, "", "Looking at this computer…", "", false, -1, ""},
		{"look failed", situacion{problema: &wsl.Problema{Clave: wsl.Otro, Detalle: "wsl.exe did not answer"}}, "", "This computer could not be checked", "", false, -1, "Something did not work"},
		{"no wsl", situacion{estado: wsl.SinWSL}, "WSL", "Turn on WSL", "Instalar", false, -1, "WSL 2"},
		{"installing", situacion{estado: wsl.SinWSL, tarea: "instalar"}, "WSL", "Turn on WSL", "", false, -1, "answer Yes"},
		{"restart", situacion{estado: wsl.SinWSL, problema: &wsl.Problema{Clave: wsl.Reinicio}}, "WSL", "Restart Windows", "", false, -1, "open rendimiento again"},
		{"said no", situacion{estado: wsl.SinWSL, problema: &wsl.Problema{Clave: wsl.Cancelado}}, "WSL", "Turn on WSL", "Instalar", false, -1, "answered no"},
		{"virtualization", situacion{estado: wsl.SinDistro, problema: &wsl.Problema{Clave: wsl.Virtualizacion, Detalle: "HCS_E_HYPERV_NOT_INSTALLED"}}, "Download", "Download rendimiento", "Importar", false, -1, "SVM Mode"},
		{"no distro", situacion{estado: wsl.SinDistro}, "Download", "Download rendimiento", "Importar", false, -1, "174 MB"},
		{"download starts", situacion{estado: wsl.SinDistro, tarea: "importar"}, "Download", "Download rendimiento", "", false, -1, "Starting the download"},
		{"downloading", situacion{estado: wsl.SinDistro, tarea: "importar", hecho: 87 * mb, total: 174 * mb}, "Download", "Download rendimiento", "", false, 50, "87 of 174 MB"},
		{"importing", situacion{estado: wsl.SinDistro, tarea: "importar", hecho: 174 * mb, total: 174 * mb}, "Download", "Download rendimiento", "", false, -1, "importing"},
		{"who signs in", situacion{estado: wsl.SinUsuario}, "Who signs in", "Who signs in", "Configurar", true, -1, "GitHub user name"},
		{"not a login", situacion{estado: wsl.SinUsuario, problema: &wsl.Problema{Clave: wsl.Usuario}}, "Who signs in", "Who signs in", "Configurar", true, -1, "not a GitHub user name"},
		{"waking after the login", situacion{estado: wsl.SinUsuario, tarea: "despertar"}, "Start", "Start rendimiento", "", false, -1, "first time takes a few minutes"},
		{"asleep", situacion{estado: wsl.Dormida}, "Start", "Start rendimiento", "Despertar", false, -1, "asleep"},
		{"going to sleep", situacion{estado: wsl.Despierta, configurado: true, tarea: "dormir"}, "Start", "Going to sleep…", "", false, -1, "data stays"},
		{"connect", situacion{estado: wsl.Despierta}, "GitHub", "Connect GitHub", "Conectar", false, -1, "top of the window"},
		{"ready", situacion{estado: wsl.Despierta, configurado: true}, "", "rendimiento is ready", "Entrar", false, -1, ""},
		{"unknown problem", situacion{estado: wsl.Dormida, problema: &wsl.Problema{Clave: "nueva", Detalle: "x"}}, "Start", "Start rendimiento", "Despertar", false, -1, "Something did not work"},
	} {
		v := ver(c.s, "en", "v1")
		actual := ""
		for _, p := range v.Pasos {
			if p.Estado == "actual" {
				actual = p.Nombre
			}
		}
		if actual != c.actual || v.Titulo != c.titulo || v.Accion != c.accion || (v.Campo != "") != c.campo || v.Avance != c.avance {
			t.Errorf("%s: step %q, title %q, action %q, field %q, progress %d", c.nombre, actual, v.Titulo, v.Accion, v.Campo, v.Avance)
		}
		if !strings.Contains(v.Texto+v.Problema+v.Campo, c.dice) {
			t.Errorf("%s: says %q %q, without %q", c.nombre, v.Texto, v.Problema, c.dice)
		}
		if (v.Boton == "") != (v.Accion == "") || v.Ocupado != (c.s.tarea != "") || (v.Revisar == "") != v.Ocupado {
			t.Errorf("%s: button %q for action %q, busy %v, check again %q", c.nombre, v.Boton, v.Accion, v.Ocupado, v.Revisar)
		}
	}
}

// No situation, in either language, asks for a text the catalog lacks.
func TestVerSinHuecos(t *testing.T) {
	estados := []wsl.Estado{"", wsl.SinWSL, wsl.SinDistro, wsl.SinUsuario, wsl.Dormida, wsl.Despierta}
	tareas := []string{"", "mirar", "instalar", "importar", "configurar", "despertar", "dormir", "conectar"}
	claves := []string{"", wsl.Reinicio, wsl.Virtualizacion, wsl.Descarga, wsl.Suma, wsl.Usuario, wsl.Arranque, wsl.Cancelado, wsl.Otro}
	for _, idioma := range []string{"en", "es"} {
		for _, e := range estados {
			for _, tarea := range tareas {
				for _, clave := range claves {
					for _, configurado := range []bool{false, true} {
						s := situacion{estado: e, tarea: tarea, configurado: configurado, hecho: 1 << 20, total: 3 << 20, reloj: -3 * time.Hour}
						if clave != "" {
							s.problema = &wsl.Problema{Clave: clave}
						}
						v := ver(s, idioma, "v1")
						m := menuDe(s, idioma)
						todo := []string{v.Titulo, v.Texto, v.Boton, v.Campo, v.Problema, v.Reloj, v.Revisar, m.Estado, m.Alternar}
						for _, p := range v.Pasos {
							todo = append(todo, p.Nombre)
						}
						if junto := strings.Join(todo, "|"); strings.Contains(junto, "¿") || strings.Contains(junto, "%!") {
							t.Fatalf("%s %q %q %q: %s", idioma, e, tarea, clave, junto)
						}
						if v.Titulo == "" || m.Estado == "" || m.Alternar == "" {
							t.Fatalf("%s %q %q %q: nothing to say", idioma, e, tarea, clave)
						}
					}
				}
			}
		}
	}
}

func TestReloj(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		dice string
	}{
		{7 * time.Minute, "7 min ahead"},
		{-90 * time.Minute, "90 min behind"},
		{26 * time.Hour, "26 h ahead"},
	} {
		if g := textoReloj(c.d, "en"); !strings.Contains(g, c.dice) || !strings.Contains(g, "Sync now") {
			t.Errorf("%v: %q", c.d, g)
		}
	}
	if g := textoReloj(-3*time.Hour, "es"); !strings.Contains(g, "atrasado 3 h") {
		t.Errorf("es: %q", g)
	}
	if v := ver(situacion{estado: wsl.Dormida}, "en", ""); v.Reloj != "" {
		t.Errorf("a clock that is right gets a warning: %q", v.Reloj)
	}
}

func TestMenu(t *testing.T) {
	for _, c := range []struct {
		s    situacion
		want Menu
	}{
		{situacion{tarea: "mirar"}, Menu{"rendimiento: not ready yet", "Wake up", false, false}},
		{situacion{estado: wsl.SinDistro}, Menu{"rendimiento: not ready yet", "Wake up", false, false}},
		{situacion{estado: wsl.Dormida}, Menu{"rendimiento: asleep", "Wake up", true, false}},
		{situacion{estado: wsl.Dormida, tarea: "despertar"}, Menu{"rendimiento: starting…", "Wake up", false, false}},
		{situacion{estado: wsl.Despierta}, Menu{"rendimiento: awake", "Sleep", true, true}},
		{situacion{estado: wsl.Despierta, tarea: "dormir"}, Menu{"rendimiento: going to sleep…", "Sleep", false, false}},
	} {
		if g := menuDe(c.s, "en"); g != c.want {
			t.Errorf("%+v: %+v, want %+v", c.s, g, c.want)
		}
	}
	if g := menuDe(situacion{estado: wsl.Despierta}, "es"); g.Alternar != "Dormir" || g.Estado != "rendimiento: despierto" {
		t.Errorf("es: %+v", g)
	}
}
