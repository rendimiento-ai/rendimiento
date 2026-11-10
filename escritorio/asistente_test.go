package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/escritorio/wsl"
)

const casa = "http://localhost:8080"

// falsa is a distribution that goes where each step takes a real one.
type falsa struct {
	mu          sync.Mutex
	estado      wsl.Estado
	configurado bool
	fallos      map[string]error // by method
	llamadas    []string
	espera      chan struct{} // when set, Despertar waits for it
}

func (f *falsa) paso(nombre string, sigue wsl.Estado) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.llamadas = append(f.llamadas, nombre)
	if err := f.fallos[nombre]; err != nil {
		return err
	}
	if sigue != "" {
		f.estado = sigue
	}
	return nil
}

func (f *falsa) poner(e wsl.Estado, configurado bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.estado, f.configurado = e, configurado
}

func (f *falsa) hizo() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.llamadas, " ")
}

func (f *falsa) Estado(context.Context) (wsl.Estado, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fallos["estado"]; err != nil {
		return "", err
	}
	return f.estado, nil
}
func (f *falsa) InstalarWSL(context.Context) error {
	if err := f.paso("instalar", ""); err != nil {
		return err
	}
	return &wsl.Problema{Clave: wsl.Reinicio}
}
func (f *falsa) Importar(_ context.Context, avance func(int64, int64)) error {
	for i := int64(1); i <= 1000; i++ {
		avance(i<<20, 1000<<20)
	}
	return f.paso("importar", wsl.SinUsuario)
}
func (f *falsa) Configurar(_ context.Context, usuario string) error {
	return f.paso("configurar:"+usuario, wsl.Dormida)
}
func (f *falsa) Despertar(context.Context) error {
	if f.espera != nil {
		<-f.espera
	}
	return f.paso("despertar", wsl.Despierta)
}
func (f *falsa) Dormir(context.Context) error { return f.paso("dormir", wsl.Dormida) }
func (f *falsa) Soltar()                      { f.paso("soltar", "") }
func (f *falsa) Ficha(context.Context) (string, error) {
	return "ficha-secreta", f.paso("ficha", "")
}
func (f *falsa) Configurado(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configurado, f.fallos["configurado"]
}

// cristal is a window that remembers what it was told.
type cristal struct {
	mu       sync.Mutex
	vistas   []Vista
	idas     []string
	vueltas  int
	muestras int
	salidas  int
}

func (c *cristal) Emitir(v Vista) { c.mu.Lock(); c.vistas = append(c.vistas, v); c.mu.Unlock() }
func (c *cristal) Ir(d string)    { c.mu.Lock(); c.idas = append(c.idas, d); c.mu.Unlock() }
func (c *cristal) Volver()        { c.mu.Lock(); c.vueltas++; c.mu.Unlock() }
func (c *cristal) Mostrar()       { c.mu.Lock(); c.muestras++; c.mu.Unlock() }
func (c *cristal) Navegador(d string) {
	c.mu.Lock()
	c.idas = append(c.idas, "navegador:"+d)
	c.mu.Unlock()
}
func (c *cristal) Salir() { c.mu.Lock(); c.salidas++; c.mu.Unlock() }

func (c *cristal) ultima() Vista {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.vistas) == 0 {
		return Vista{}
	}
	return c.vistas[len(c.vistas)-1]
}

func (c *cristal) fue() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.idas, " ")
}

// armar is an assistant with its window, after its first look.
func armar(t *testing.T, f *falsa) (*Asistente, *cristal) {
	t.Helper()
	ctx, cancelar := context.WithCancel(context.Background())
	t.Cleanup(cancelar)
	a := nuevoAsistente(ctx, f, casa, "en", "v1", func(context.Context) time.Duration { return 0 })
	c := &cristal{}
	a.v = c
	a.mirar(nil)
	return a, c
}

func TestCaminoCompleto(t *testing.T) {
	var registro bytes.Buffer
	log.SetOutput(&registro)
	defer log.SetOutput(log.Writer())

	f := &falsa{estado: wsl.SinWSL}
	a, c := armar(t, f)
	if v := c.ultima(); v.Accion != "Instalar" {
		t.Fatalf("without WSL: %+v", v)
	}
	a.Instalar()
	if v := c.ultima(); v.Titulo != "Restart Windows" || v.Boton != "" || v.Problema != "" {
		t.Fatalf("after installing WSL: %+v", v)
	}

	f.poner(wsl.SinDistro, false) // Windows restarted
	a.Revisar()
	if v := c.ultima(); v.Accion != "Importar" || v.Titulo != "Download rendimiento" {
		t.Fatalf("after the restart: %+v", v)
	}
	a.Importar()
	if v := c.ultima(); v.Accion != "Configurar" || v.Campo == "" || v.Avance != -1 {
		t.Fatalf("after the import: %+v", v)
	}
	a.Configurar("octocat")
	if v := c.ultima(); v.Accion != "Conectar" {
		t.Fatalf("after who signs in: %+v", v)
	}
	if g := f.hizo(); g != "instalar importar configurar:octocat despertar" {
		t.Fatalf("the distribution was asked: %s", g)
	}
	if g := c.fue(); g != "" {
		t.Fatalf("the window left the assistant early: %s", g)
	}

	a.Conectar()
	if g, want := c.fue(), casa+"/api/setup/github?token=ficha-secreta"; g != want {
		t.Fatalf("to connect GitHub the window went to %q, want %q", g, want)
	}
	// GitHub connected in the window; it does not send the person back.
	f.poner(wsl.Despierta, true)
	a.abrir()
	if g := c.fue(); !strings.HasSuffix(g, " "+casa) || c.muestras != 1 || c.vueltas != 0 {
		t.Fatalf("opening after GitHub: went %q, shown %d, back %d", g, c.muestras, c.vueltas)
	}

	// The setup token went to the window's address and nowhere else.
	vistas, _ := json.Marshal(c.vistas)
	if strings.Contains(string(vistas), "ficha-secreta") || strings.Contains(registro.String(), "ficha-secreta") {
		t.Fatal("the setup token is in a view or in the log")
	}
}

func TestAvanceMedido(t *testing.T) {
	f := &falsa{estado: wsl.SinDistro}
	a, c := armar(t, f)
	a.cada = time.Hour
	a.Importar()
	descargando, importando := 0, 0
	for _, v := range c.vistas {
		if strings.Contains(v.Texto, " MB") && v.Ocupado {
			descargando++
		}
		if strings.Contains(v.Texto, "importing") {
			importando++
		}
	}
	// Of a thousand reports: the first one, and the end.
	if descargando != 1 || importando != 1 {
		t.Fatalf("%d views of the download and %d of the import, of %d", descargando, importando, len(c.vistas))
	}
}

func TestListaEntraYDuerme(t *testing.T) {
	f := &falsa{estado: wsl.Dormida, configurado: true}
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	a := nuevoAsistente(ctx, f, casa, "en", "v1", func(context.Context) time.Duration { return 0 })
	if v := a.Vista(); v.Titulo != "Looking at this computer…" || !v.Ocupado {
		t.Fatalf("before the first look: %+v", v)
	}
	a.Importar() // the page cannot ask for anything yet
	c := &cristal{}
	var menus []Menu
	var mu sync.Mutex
	a.escuchar(func(m Menu) { mu.Lock(); menus = append(menus, m); mu.Unlock() })
	a.empezar(c)
	if g := f.hizo(); g != "despertar" {
		t.Fatalf("a ready computer is woken at the start, once: %s", g)
	}
	if g := c.fue(); g != casa {
		t.Fatalf("ready: the window went to %q", g)
	}
	mu.Lock()
	if m := menus[len(menus)-1]; m.Alternar != "Sleep" || !m.Activo || !m.Navegador {
		t.Fatalf("the icon, awake: %+v", m)
	}
	mu.Unlock()

	a.navegador()
	a.alternar()
	if !strings.HasSuffix(c.fue(), "navegador:"+casa) || c.vueltas == 0 {
		t.Fatalf("sleeping from rendimiento's pages: went %q, back %d", c.fue(), c.vueltas)
	}
	// The assistant's page loads again and asks.
	if v := a.Vista(); v.Accion != "Despertar" {
		t.Fatalf("asleep: %+v", v)
	}
	a.alternar()
	if g := f.hizo(); g != "despertar dormir despertar" {
		t.Fatalf("the distribution was asked: %s", g)
	}
	if g := c.fue(); !strings.HasSuffix(g, " "+casa) {
		t.Fatalf("awake again: the window went %q", g)
	}
	a.salir()
	if c.salidas != 1 {
		t.Fatal("quit did not reach the window")
	}
}

// A window left on GitHub's pages (a sign-in that leads nowhere) has a way
// back from the icon, connected or not.
func TestInicio(t *testing.T) {
	f := &falsa{estado: wsl.Despierta}
	a, c := armar(t, f)
	a.inicio()
	if c.muestras != 1 || c.vueltas != 0 {
		t.Fatalf("on the assistant's page: shown %d, back %d", c.muestras, c.vueltas)
	}
	a.Conectar()
	a.inicio()
	if c.vueltas != 1 {
		t.Fatalf("away on GitHub: back %d", c.vueltas)
	}
	// The assistant's page loads again and asks.
	if v := a.Vista(); v.Accion != "Conectar" {
		t.Fatalf("back at the start: %+v", v)
	}
}

func TestProblemas(t *testing.T) {
	// A look that fails is not "WSL is missing".
	f := &falsa{fallos: map[string]error{"estado": errors.New("wsl.exe did not answer")}}
	a, c := armar(t, f)
	if v := c.ultima(); v.Titulo != "This computer could not be checked" || v.Detalle != "wsl.exe did not answer" || v.Accion != "" || v.Revisar == "" {
		t.Fatalf("a look that failed: %+v", v)
	}
	f.fallos = nil
	f.poner(wsl.Dormida, false)
	a.Revisar()
	if v := c.ultima(); v.Accion != "Despertar" || v.Problema != "" {
		t.Fatalf("checking again: %+v", v)
	}

	// A wake that fails lets go of the distribution and says why.
	f.fallos = map[string]error{"despertar": fmt.Errorf("wake: %w", &wsl.Problema{Clave: wsl.Arranque, Detalle: "no answer in 8m"})}
	a.Despertar()
	if v := c.ultima(); !strings.Contains(v.Problema, "did not answer in time") || v.Detalle != "no answer in 8m" || v.Accion != "Despertar" {
		t.Fatalf("a wake that failed: %+v", v)
	}
	if g := f.hizo(); g != "despertar soltar" {
		t.Fatalf("the distribution was asked: %s", g)
	}

	// One task at a time: what is pressed meanwhile is not done.
	f.fallos, f.espera = nil, make(chan struct{})
	hecho := make(chan struct{})
	go func() { a.Despertar(); close(hecho) }()
	for a.ahora().tarea == "" {
		time.Sleep(time.Millisecond)
	}
	a.alternar()
	a.Importar()
	close(f.espera)
	<-hecho
	if g := f.hizo(); g != "despertar soltar despertar" {
		t.Fatalf("while waking: %s", g)
	}
}

func TestVistazo(t *testing.T) {
	f := &falsa{estado: wsl.Despierta}
	a, c := armar(t, f)
	a.Conectar()
	// GitHub is connected in the window: the person stays where they are.
	f.poner(wsl.Despierta, true)
	a.vistazo()
	if !a.ahora().configurado || c.vueltas != 0 || len(c.idas) != 1 {
		t.Fatalf("connected meanwhile: %+v, back %d, went %v", a.ahora(), c.vueltas, c.idas)
	}
	// rendimiento was stopped from outside: the assistant comes back.
	f.poner(wsl.Dormida, true)
	f.fallos = map[string]error{"configurado": errors.New("connection refused")}
	a.vistazo()
	if a.ahora().estado != wsl.Dormida || c.vueltas != 1 {
		t.Fatalf("stopped from outside: %+v, back %d", a.ahora(), c.vueltas)
	}
	a.vistazo() // the page has not asked yet: again
	if c.vueltas != 2 {
		t.Fatalf("the page did not come back and nobody insisted: %d", c.vueltas)
	}
	a.Vista()
	a.vistazo()
	if c.vueltas != 2 {
		t.Fatalf("the page is back and is called again: %d", c.vueltas)
	}
}

func TestRelojAvisaYNoDejaEntrar(t *testing.T) {
	f := &falsa{estado: wsl.Despierta, configurado: true}
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	lejos := 10 * time.Minute
	a := nuevoAsistente(ctx, f, casa, "en", "v1", func(context.Context) time.Duration { return lejos })
	c := &cristal{}
	a.v = c
	a.medir()
	a.mirar(nil)
	if v := c.ultima(); !strings.Contains(v.Reloj, "10 min ahead") || v.Accion != "Entrar" || c.fue() != "" {
		t.Fatalf("with the clock off: %+v, went %q", v, c.fue())
	}
	a.Entrar() // the person may go in anyway
	if c.fue() != casa {
		t.Fatalf("going in anyway: %q", c.fue())
	}
	a.Vista()
	lejos = 0
	a.Revisar()
	if g := c.fue(); g != casa+" "+casa {
		t.Fatalf("with the clock right: %q", g)
	}
}

func TestGuionIr(t *testing.T) {
	want := `if(location.origin!=="http://localhost:8080")location.replace("http://localhost:8080/api/setup/github?token=a\u0026b")`
	if g := guionIr(casa + "/api/setup/github?token=a&b"); g != want {
		t.Fatalf("%s", g)
	}
}
