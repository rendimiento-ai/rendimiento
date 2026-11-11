package main

import (
	"fmt"
	"strings"
)

// texto is one thing the app says, in each language it speaks.
type texto struct{ en, es string }

// textos is everything the assistant's page and the icon say. The Spanish
// is Mexico's, with usted; textos_test.go fails on a missing translation.
var textos = map[string]texto{
	"paso.wsl":      {"WSL", "WSL"},
	"paso.descarga": {"Download", "Descarga"},
	"paso.usuario":  {"Who signs in", "Quién entra"},
	"paso.arranque": {"Start", "Arranque"},
	"paso.github":   {"GitHub", "GitHub"},

	"mirar.titulo": {"Looking at this computer…", "Revisando esta computadora…"},
	"mirar.fallo":  {"This computer could not be checked", "No se pudo revisar esta computadora"},
	"revisar":      {"Check again", "Revisar de nuevo"},

	"wsl.titulo":  {"Turn on WSL", "Active WSL"},
	"wsl.texto":   {"rendimiento runs inside a small Linux system that Windows keeps (WSL 2). Windows will ask for permission to install it.", "rendimiento funciona dentro de un pequeño sistema Linux que Windows mantiene (WSL 2). Windows le pedirá permiso para instalarlo."},
	"wsl.boton":   {"Install WSL", "Instalar WSL"},
	"wsl.ocupado": {"Installing WSL. Windows asks for permission: answer Yes. It takes a few minutes.", "Instalando WSL. Windows pide permiso: responda que sí. Tarda unos minutos."},

	"reinicio.titulo": {"Restart Windows", "Reinicie Windows"},
	"reinicio.texto":  {"WSL is installed, and Windows has to restart before it works. Restart Windows and open rendimiento again: it continues from here.", "WSL ya está instalado, y Windows tiene que reiniciarse para que funcione. Reinicie Windows y abra rendimiento otra vez: continúa desde aquí."},

	"descarga.titulo":  {"Download rendimiento", "Descargue rendimiento"},
	"descarga.texto":   {"It is about 174 MB, and it stays on this computer.", "Son unos 174 MB, y se quedan en esta computadora."},
	"descarga.boton":   {"Download", "Descargar"},
	"descarga.empieza": {"Starting the download…", "Empezando la descarga…"},
	"descarga.ocupado": {"Downloading: %d of %d MB.", "Descargando: %d de %d MB."},
	"descarga.importa": {"Checking the download and importing it. It takes a minute.", "Comprobando la descarga e importándola. Tarda un minuto."},

	"usuario.titulo":  {"Who signs in", "Quién entra"},
	"usuario.texto":   {"Write your GitHub user name. Only that account will be able to sign in to rendimiento on this computer.", "Escriba su nombre de usuario de GitHub. Solo esa cuenta podrá iniciar sesión en rendimiento en esta computadora."},
	"usuario.campo":   {"GitHub user name", "Nombre de usuario de GitHub"},
	"usuario.boton":   {"Continue", "Continuar"},
	"usuario.ocupado": {"Writing who signs in…", "Anotando quién entra…"},

	"arranque.titulo":  {"Start rendimiento", "Inicie rendimiento"},
	"arranque.texto":   {"rendimiento is asleep: it uses no memory until you wake it up.", "rendimiento está dormido: no ocupa memoria hasta que usted lo despierte."},
	"arranque.boton":   {"Wake up", "Despertar"},
	"arranque.ocupado": {"Starting. The first time takes a few minutes.", "Iniciando. La primera vez tarda unos minutos."},

	"dormir.titulo":  {"Going to sleep…", "Durmiendo…"},
	"dormir.ocupado": {"Stopping rendimiento: its apps, its builds and the memory it held. Its data stays.", "Deteniendo rendimiento: sus aplicaciones, sus compilaciones y la memoria que ocupaba. Sus datos se quedan."},

	"github.titulo":  {"Connect GitHub", "Conecte GitHub"},
	"github.texto":   {"rendimiento builds what you push to GitHub. GitHub will ask you to create rendimiento's app in your account, and then to choose the repositories it may see.\n\nWhen GitHub is done, it brings you back. To come back before that, choose Back or Back to the start in the Navigate menu, at the top of the window.", "rendimiento compila lo que usted sube a GitHub. GitHub le pedirá crear la aplicación de rendimiento en su cuenta, y después elegir los repositorios que puede ver.\n\nCuando GitHub termine, lo trae de regreso. Para volver antes, elija Atrás o Volver al inicio en el menú Navegar, arriba en la ventana."},
	"github.boton":   {"Connect GitHub", "Conectar GitHub"},
	"github.ocupado": {"Opening GitHub…", "Abriendo GitHub…"},

	"listo.titulo": {"rendimiento is ready", "rendimiento está listo"},
	"listo.texto":  {"It is awake on this computer.", "Está despierto en esta computadora."},
	"listo.boton":  {"Open", "Abrir"},

	"problema.reinicio":       {"Windows has to restart before WSL works.", "Windows tiene que reiniciarse para que WSL funcione."},
	"problema.virtualizacion": {"Windows could not start the virtual machine WSL needs. One of two things fixes it:\n\n1. If WSL was just installed, restart Windows.\n\n2. If it still fails, turn on virtualization in this computer's BIOS/UEFI: it is called SVM Mode on AMD and Intel Virtualization Technology on Intel. When it is on, Task Manager → Performance → CPU says “Virtualization: Enabled”.", "Windows no pudo iniciar la máquina virtual que WSL necesita. Una de dos cosas lo arregla:\n\n1. Si WSL se acaba de instalar, reinicie Windows.\n\n2. Si sigue fallando, active la virtualización en el BIOS/UEFI de esta computadora: se llama SVM Mode en AMD e Intel Virtualization Technology en Intel. Cuando está activa, Administrador de tareas → Rendimiento → CPU dice «Virtualización: Habilitado»."},
	"problema.descarga":       {"rendimiento could not be downloaded. Check the internet connection and try again.", "No se pudo descargar rendimiento. Revise la conexión a internet e intente de nuevo."},
	"problema.suma":           {"The download arrived damaged: it does not match what the release published. Try again.", "La descarga llegó dañada: no coincide con lo que la versión publicó. Intente de nuevo."},
	"problema.usuario":        {"That is not a GitHub user name: letters, numbers and hyphens, without spaces.", "Ese no es un nombre de usuario de GitHub: letras, números y guiones, sin espacios."},
	"problema.arranque":       {"rendimiento did not answer in time. Try again; the first start is the slowest.", "rendimiento no respondió a tiempo. Intente de nuevo; el primer arranque es el más lento."},
	"problema.cancelado":      {"You answered no to Windows' question, so nothing changed. Try again when you want.", "Usted respondió que no a la pregunta de Windows, así que nada cambió. Intente de nuevo cuando quiera."},
	"problema.otro":           {"Something did not work. This is what the system said:", "Algo no funcionó. Esto dijo el sistema:"},

	"reloj.adelantado": {"This computer's clock is %s ahead, and GitHub refuses rendimiento's signature when the clock is off. In Windows: Settings → Time & language → Date & time → Sync now.", "El reloj de esta computadora está adelantado %s, y GitHub rechaza la firma de rendimiento cuando el reloj está mal. En Windows: Configuración → Hora e idioma → Fecha y hora → Sincronizar ahora."},
	"reloj.atrasado":   {"This computer's clock is %s behind, and GitHub refuses rendimiento's signature when the clock is off. In Windows: Settings → Time & language → Date & time → Sync now.", "El reloj de esta computadora está atrasado %s, y GitHub rechaza la firma de rendimiento cuando el reloj está mal. En Windows: Configuración → Hora e idioma → Fecha y hora → Sincronizar ahora."},
	"reloj.min":        {"%d min", "%d min"},
	"reloj.h":          {"%d h", "%d h"},

	"menu.navegar": {"Navigate", "Navegar"},
	"menu.atras":   {"Back", "Atrás"},

	"bandeja.abrir":     {"Open", "Abrir"},
	"bandeja.inicio":    {"Back to the start", "Volver al inicio"},
	"bandeja.navegador": {"Open in the browser", "Abrir en el navegador"},
	"bandeja.dormir":    {"Sleep", "Dormir"},
	"bandeja.despertar": {"Wake up", "Despertar"},
	"bandeja.salir":     {"Quit", "Salir"},

	"estado.pendiente":   {"rendimiento: not ready yet", "rendimiento: aún no está listo"},
	"estado.dormida":     {"rendimiento: asleep", "rendimiento: dormido"},
	"estado.despertando": {"rendimiento: starting…", "rendimiento: iniciando…"},
	"estado.durmiendo":   {"rendimiento: going to sleep…", "rendimiento: durmiendo…"},
	"estado.despierta":   {"rendimiento: awake", "rendimiento: despierto"},

	"webview.falta":     {"rendimiento needs Microsoft's WebView2 Runtime to show its window.", "rendimiento necesita WebView2 Runtime, de Microsoft, para mostrar su ventana."},
	"webview.viejo":     {"Microsoft's WebView2 Runtime on this computer is too old for rendimiento.", "El WebView2 Runtime de Microsoft en esta computadora es demasiado viejo para rendimiento."},
	"webview.instalar":  {"Press OK to install it.", "Presione Aceptar para instalarlo."},
	"webview.fallo":     {"WebView2 Runtime could not be installed.", "No se pudo instalar WebView2 Runtime."},
	"webview.requisito": {"Something rendimiento needs is missing", "Falta algo que rendimiento necesita"},
	"webview.error":     {"Error", "Error"},
}

// T is a text in the person's language, with its values put in.
func T(idioma, clave string, valores ...any) string {
	t, hay := textos[clave]
	if !hay {
		return "¿" + clave + "?"
	}
	s := t.en
	if idioma == "es" {
		s = t.es
	}
	if len(valores) > 0 {
		return fmt.Sprintf(s, valores...)
	}
	return s
}

// idiomaDe picks the app's language from the ones Windows lists for the
// person, the preferred one first ("es-MX", "en-US").
func idiomaDe(preferidos []string) string {
	for _, p := range preferidos {
		p = strings.ToLower(p)
		if p == "es" || strings.HasPrefix(p, "es-") {
			return "es"
		}
		if p == "en" || strings.HasPrefix(p, "en-") {
			return "en"
		}
	}
	return "en"
}
