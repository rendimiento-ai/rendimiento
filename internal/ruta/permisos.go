package ruta

// Each MCP key's permissions (Ruta 184), as the coding agent's: the Ruta
// and the list of apps (code_read) always; the platform's reading tools
// need platform_read; infra_leer infra_read; copias_leer copies_read; and
// running a branch, retrying a run, restarting a service and remaking a
// copy need effects, which a person allows one use at a time. Off, the
// tool answers that it is off; ask, the call asks a person (it shows in
// «Para usted») and answers so, and once given, the next call works.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rendimiento-ai/rendimiento/internal/destino"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Permisos is where the keys' permissions are kept (store.Store); nil:
// no permissions are checked, as before they existed (tests of other
// tools).
type Permisos interface {
	PermisoDecide(ctx context.Context, orgID int64, sujeto, permiso, app, accion string, encargoID int64) (store.Decision, error)
	UsarPermisoDado(ctx context.Context, id int64) (bool, error)
	PedirPermiso(ctx context.Context, p store.PermisoPedido) (store.PermisoPedido, error)
	PermisoPedidoDe(ctx context.Context, id int64) (store.PermisoPedido, error)
}

// Infra is what infra_leer, copias_leer and the effects tools use
// (platform.AgentInfra); nil: they are not offered.
type Infra interface {
	MirarApp(ctx context.Context, app *store.App, q destino.Mirar) (destino.Mirada, error)
	ReiniciarServicio(ctx context.Context, app *store.App, servicio string) error
	RehacerCopia(ctx context.Context, app *store.App, number int, who string) (string, error)
	CopiasTexto(ctx context.Context, app *store.App) (string, error)
}

// nombrePermiso is a permission in the agents' words.
var nombrePermiso = map[string]string{
	store.PermPlatformRead: "ver ejecuciones, registros de pasos y solicitudes",
	store.PermInfraRead:    "ver la infraestructura de su app",
	store.PermSandbox:      "correr comandos en la caja de arena",
	store.PermCopiesRead:   "ver sus copias de vista previa",
	store.PermEffects:      "una acción con efecto",
}

// ErrPermisoApagado is a tool whose permission is off for the key.
var ErrPermisoApagado = errors.New("permiso apagado")

// modoPara is the key's mode for a permission on an app, without asking:
// for lists that leave out what it may not see.
func (s *Server) modoPara(ctx context.Context, k store.AgentKey, permiso, app string) string {
	sujeto := store.SujetoLlave(k.ID)
	if s.Permisos == nil {
		return store.ModoAlways
	}
	d, err := s.Permisos.PermisoDecide(ctx, k.OrgID, sujeto, permiso, app, "", 0)
	if err != nil {
		return store.ModoOff
	}
	if d.Puede() {
		return store.ModoAlways
	}
	return d.Modo
}

// permitir says whether the key may use a permission on an app now: nil,
// or why not. In mode ask with nothing given, it asks a person (once:
// another call joins the same request) and says so; que is what the call
// reads or does, accion the exact action for effects.
func (s *Server) permitir(ctx context.Context, k store.AgentKey, permiso string, a *store.App, accion, que string) error {
	if permiso == store.PermCodeRead {
		return nil
	}
	sujeto := store.SujetoLlave(k.ID)
	if s.Permisos == nil {
		return nil
	}
	d, err := s.Permisos.PermisoDecide(ctx, k.OrgID, sujeto, permiso, a.Name, accion, 0)
	if err != nil {
		return err
	}
	switch {
	case d.Modo == store.ModoOff && d.Fuera:
		return fmt.Errorf("%w: «%s» de esta llave es solo para %s, no para %s (IA → Permisos)", ErrPermisoApagado, nombrePermiso[permiso], strings.Join(d.Apps, ", "), a.Name)
	case d.Modo == store.ModoOff:
		return fmt.Errorf("%w: «%s» está apagado para esta llave (IA → Permisos). Siga sin eso y dígalo en su entrega", ErrPermisoApagado, nombrePermiso[permiso])
	case d.Modo == store.ModoAlways:
		return nil
	case d.Dado != nil:
		if d.Dado.Alcance != store.AlcanceOnce {
			return nil
		}
		if ok, err := s.Permisos.UsarPermisoDado(ctx, d.Dado.ID); err != nil || ok {
			return err
		}
	}
	p := store.PermisoPedido{OrgID: k.OrgID, Sujeto: sujeto, Permiso: permiso, App: a.Name, Accion: accion, Que: que, AskedBy: k.Name}
	if c, err := s.Store.OpenCargo(ctx, k.ID); err == nil {
		p.CargoID = &c.ID
	}
	p, err = s.Permisos.PedirPermiso(context.WithoutCancel(ctx), p)
	if err != nil {
		return err
	}
	quien := k.CreatedBy
	if quien == "" || quien == "rendimiento" {
		quien = "una persona de su organización"
	}
	return fmt.Errorf("necesita permiso: «%s» para %s (%s). Se le pidió a %s en «Para usted» (pedido %d). Diga por qué con permiso_pedir (pedido %d y por_que): así decide mejor. Mientras, siga con lo demás; cuando le dé permiso, vuelva a llamar",
		nombrePermiso[permiso], a.Name, que, quien, p.ID, p.ID)
}

// toolPermiso is the permission each MCP tool beyond code_read needs, for
// the bitácora.
var toolPermiso = map[string]string{
	"app_estado": store.PermPlatformRead, "mesa_leer": store.PermPlatformRead, "ejecucion_leer": store.PermPlatformRead,
	"paso_registro": store.PermPlatformRead, "solicitudes_listar": store.PermPlatformRead, "solicitud_leer": store.PermPlatformRead,
	"infra_leer": store.PermInfraRead, "copias_leer": store.PermCopiesRead,
	"ejecucion_lanzar": store.PermEffects, "ejecucion_reintentar": store.PermEffects, "servicio_reiniciar": store.PermEffects, "copia_rehacer": store.PermEffects,
}

// ---- inputs and outputs ----

type permisoPedirIn struct {
	Pedido  int64  `json:"pedido,omitempty" jsonschema:"el número del pedido que dio la herramienta; vacío para uno nuevo"`
	Permiso string `json:"permiso,omitempty" jsonschema:"para uno nuevo: platform_read, infra_read, copies_read o effects"`
	App     string `json:"app,omitempty" jsonschema:"para uno nuevo: la aplicación"`
	Que     string `json:"que,omitempty" jsonschema:"qué quiere ver o hacer, corto"`
	PorQue  string `json:"por_que" jsonschema:"por qué: la persona lo lee para decidir"`
	Ventana string `json:"ventana,omitempty" jsonschema:"de cuándo a cuándo, si es de un rato (p. ej. de las 05:44 a las 05:54)"`
}

type pedidoOut struct {
	Pedido  int64  `json:"pedido"`
	Permiso string `json:"permiso"`
	App     string `json:"app"`
	Estado  string `json:"estado"`
}

type infraIn struct {
	App      string   `json:"app" jsonschema:"el nombre de la aplicación"`
	Que      []string `json:"que,omitempty" jsonschema:"pods, events, logs o rollout; vacío para pods, eventos y rollout"`
	Servicio string   `json:"servicio,omitempty" jsonschema:"solo este servicio de la aplicación"`
	Lineas   int      `json:"lineas,omitempty" jsonschema:"de los registros: cuántas líneas del final, hasta 500 (100 si se omite)"`
	Anterior bool     `json:"anterior,omitempty" jsonschema:"de los registros: los del contenedor anterior, el que terminó"`
	Minutos  int      `json:"minutos,omitempty" jsonschema:"cuánto hacia atrás, en minutos, hasta 1440 (60 si se omite)"`
}

type infraOut struct {
	App   string `json:"app"`
	Donde string `json:"donde"`
	Texto string `json:"texto"`
}

type copiasOut struct {
	App   string `json:"app"`
	Texto string `json:"texto"`
}

type reiniciarIn struct {
	App      string `json:"app" jsonschema:"el nombre de la aplicación"`
	Servicio string `json:"servicio" jsonschema:"el servicio a reiniciar"`
}

type rehacerIn struct {
	App       string `json:"app" jsonschema:"el nombre de la aplicación"`
	Solicitud int    `json:"solicitud" jsonschema:"el número de la solicitud de cambios de la copia"`
}

type hechoOut struct {
	App   string `json:"app"`
	Hecho string `json:"hecho"`
}

// ---- the tools ----

// addPermisoTools registers permiso_pedir, and the infrastructure, copies
// and effects tools when the platform gives them.
func (s *Server) addPermisoTools(srv herramientas, write bool) {
	if s.Permisos != nil {
		addTool(s, srv, &mcp.Tool{Name: "permiso_pedir", Description: "Pide permiso a una persona para algo que su llave hace solo con permiso (ver la infraestructura, una acción con efecto…), o dice por qué al pedido que abrió una herramienta. La persona lo ve en «Para usted» y da permiso solo esta vez, mientras dure su cargo, siempre para esa app, o no."}, s.permisoPedir)
	}
	if s.Infra == nil || s.Plataforma == nil {
		return
	}
	addTool(s, srv, &mcp.Tool{Name: "infra_leer", Description: "La infraestructura de una aplicación, solo lectura: en el clúster sus pods (estado, condiciones, contenedores con su razón, reinicios), sus eventos recientes, el final de sus registros (también del contenedor anterior) y el rollout; en una computadora sus contenedores (estado, código de salida, reinicios) y sus registros. Solo esa aplicación, nunca secretos. Necesita el permiso de ver la infraestructura."}, s.infraLeer)
	addTool(s, srv, &mcp.Tool{Name: "copias_leer", Description: "Las copias de vista previa de una aplicación: la de cada solicitud de cambios, su rama, su salud, su dirección, sus pruebas y cuándo se va. Necesita el permiso de ver las copias."}, s.copiasLeer)
	if write {
		addTool(s, srv, &mcp.Tool{Name: "servicio_reiniciar", Description: "Reinicia un servicio de una aplicación (como kubectl rollout restart). Una acción con efecto: una persona la permite cada vez. Necesita un cargo abierto."}, s.servicioReiniciar)
		addTool(s, srv, &mcp.Tool{Name: "copia_rehacer", Description: "Vuelve a hacer la copia de vista previa de una solicitud de cambios con la última ejecución de su rama que pasó. Una acción con efecto: una persona la permite cada vez. Necesita un cargo abierto."}, s.copiaRehacer)
	}
}

// permisoPedir (permiso_pedir) asks a person for a permission, or gives
// the reason of a request a tool opened.
func (s *Server) permisoPedir(ctx context.Context, k store.AgentKey, in permisoPedirIn) (pedidoOut, error) {
	por := strings.TrimSpace(in.PorQue)
	switch {
	case por == "":
		return pedidoOut{}, errors.New("diga por qué: la persona lo lee para decidir")
	case LooksSecret(por) || LooksSecret(in.Que):
		return pedidoOut{}, errSecret
	}
	sujeto := store.SujetoLlave(k.ID)
	p := store.PermisoPedido{OrgID: k.OrgID, Sujeto: sujeto, Permiso: strings.TrimSpace(in.Permiso), App: strings.TrimSpace(in.App),
		Que: recortar(in.Que, 500), PorQue: recortar(por, 2000), Ventana: recortar(in.Ventana, 200), AskedBy: k.Name}
	if in.Pedido > 0 {
		old, err := s.Permisos.PermisoPedidoDe(ctx, in.Pedido)
		if err != nil || old.Sujeto != sujeto || old.OrgID != k.OrgID {
			return pedidoOut{}, fmt.Errorf("no existe el pedido %d de esta llave", in.Pedido)
		}
		if old.AnsweredAt != nil {
			return pedidoOut{Pedido: old.ID, Permiso: old.Permiso, App: old.App, Estado: "respondido: " + old.Answer}, nil
		}
		p.Permiso, p.App, p.Accion, p.EncargoID = old.Permiso, old.App, old.Accion, old.EncargoID
	}
	if _, err := s.appFor(ctx, k, p.App); err != nil {
		return pedidoOut{}, err
	}
	if c, err := s.Store.OpenCargo(ctx, k.ID); err == nil {
		p.CargoID = &c.ID
	}
	got, err := s.Permisos.PedirPermiso(ctx, p)
	if errors.Is(err, store.ErrPermiso) {
		return pedidoOut{}, errors.New("el permiso es platform_read, infra_read, copies_read o effects")
	}
	if err != nil {
		return pedidoOut{}, err
	}
	return pedidoOut{Pedido: got.ID, Permiso: got.Permiso, App: got.App, Estado: "pedido: espera a una persona en «Para usted»"}, nil
}

func recortar(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// redactar leaves out the lines that look like a credential.
func redactar(s string) string {
	if !LooksSecret(s) {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if LooksSecret(l) {
			lines[i] = "(se omitió una línea que parece un secreto)"
		}
	}
	return strings.Join(lines, "\n")
}

// infraLeer (infra_leer) is an app's infrastructure, read-only.
func (s *Server) infraLeer(ctx context.Context, k store.AgentKey, in infraIn) (infraOut, error) {
	a, err := s.appFor(ctx, k, in.App)
	if err != nil {
		return infraOut{}, err
	}
	if in.Servicio != "" {
		found := false
		for _, svc := range a.Spec.Services {
			found = found || svc.Name == in.Servicio
		}
		if !found {
			return infraOut{}, fmt.Errorf("%s no tiene el servicio %s", a.Name, in.Servicio)
		}
	}
	que := "pods, eventos y rollout"
	if len(in.Que) > 0 {
		que = strings.Join(in.Que, ", ")
	}
	if in.Servicio != "" {
		que += ", servicio " + in.Servicio
	}
	if err := s.permitir(ctx, k, store.PermInfraRead, a, "", que); err != nil {
		return infraOut{}, err
	}
	m, err := s.Infra.MirarApp(ctx, a, destino.Mirar{Que: in.Que, Servicio: in.Servicio, Lineas: in.Lineas, Anterior: in.Anterior, Ventana: time.Duration(in.Minutos) * time.Minute})
	if err != nil {
		return infraOut{}, err
	}
	return infraOut{App: a.Name, Donde: m.Donde, Texto: redactar(m.Texto())}, nil
}

// copiasLeer (copias_leer) is an app's preview copies.
func (s *Server) copiasLeer(ctx context.Context, k store.AgentKey, in appIn) (copiasOut, error) {
	a, err := s.appFor(ctx, k, in.App)
	if err != nil {
		return copiasOut{}, err
	}
	if err := s.permitir(ctx, k, store.PermCopiesRead, a, "", "sus copias de vista previa"); err != nil {
		return copiasOut{}, err
	}
	t, err := s.Infra.CopiasTexto(ctx, a)
	if err != nil {
		return copiasOut{}, err
	}
	return copiasOut{App: a.Name, Texto: redactar(t)}, nil
}

// servicioReiniciar (servicio_reiniciar) restarts a service, one use a person allowed.
func (s *Server) servicioReiniciar(ctx context.Context, k store.AgentKey, in reiniciarIn) (hechoOut, error) {
	if _, err := s.cargoFor(ctx, k); err != nil {
		return hechoOut{}, err
	}
	a, err := s.appFor(ctx, k, in.App)
	if err != nil {
		return hechoOut{}, err
	}
	svc := strings.TrimSpace(in.Servicio)
	if err := s.permitir(ctx, k, store.PermEffects, a, "restart:"+svc, "reiniciar el servicio "+svc); err != nil {
		return hechoOut{}, err
	}
	if err := s.Infra.ReiniciarServicio(ctx, a, svc); err != nil {
		return hechoOut{}, err
	}
	return hechoOut{App: a.Name, Hecho: "se reinició el servicio " + svc}, nil
}

// copiaRehacer (copia_rehacer) makes a pull request's copy again, one use a person allowed.
func (s *Server) copiaRehacer(ctx context.Context, k store.AgentKey, in rehacerIn) (hechoOut, error) {
	if _, err := s.cargoFor(ctx, k); err != nil {
		return hechoOut{}, err
	}
	a, err := s.appFor(ctx, k, in.App)
	if err != nil {
		return hechoOut{}, err
	}
	if err := s.permitir(ctx, k, store.PermEffects, a, "remake:"+strconv.Itoa(in.Solicitud), fmt.Sprintf("rehacer la copia de la solicitud #%d", in.Solicitud)); err != nil {
		return hechoOut{}, err
	}
	hecho, err := s.Infra.RehacerCopia(ctx, a, in.Solicitud, k.Name)
	if err != nil {
		return hechoOut{}, err
	}
	return hechoOut{App: a.Name, Hecho: hecho}, nil
}
