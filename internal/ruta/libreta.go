package ruta

import (
	"fmt"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/libreta"
)

// The libreta of an app as agents read it in app_estado (Ruta 150): what
// was asked for the app and whether it is done. The states are computed
// by rendimiento; an agent changes them by doing the work (a branch, a
// run, a merge), never by setting them.

// PedidoOut is one line of an app's libreta.
type PedidoOut struct {
	// Fuente: ruta (una entrada), encargo, renovate (su solicitud) o
	// pruebas (las pruebas propias de la aplicación).
	Fuente string `json:"fuente"`
	ID     int64  `json:"id,omitempty"`
	Titulo string `json:"titulo"`
	// Estado: pedido, en_curso, listo (para integrar), integrado,
	// publicado, esperando, descartado o hecho.
	Estado    string     `json:"estado"`
	Porque    string     `json:"porque,omitempty"`
	Motivo    string     `json:"motivo,omitempty"` // por qué se descartó
	Agente    string     `json:"agente,omitempty"`
	Rama      string     `json:"rama,omitempty"`
	Solicitud int        `json:"solicitud,omitempty"`
	Ejecucion int64      `json:"ejecucion,omitempty"`
	Version   int64      `json:"version,omitempty"`
	Pedido    *time.Time `json:"pedido,omitempty"`
	Terminado *time.Time `json:"terminado,omitempty"`
	Progreso  string     `json:"progreso,omitempty"` // p. ej. "1 de 5 versiones limpias"
}

// LibretaOut is an app's libreta: what is open, and what was finished or
// set aside in the last week.
type LibretaOut struct {
	Abiertos     []PedidoOut `json:"abiertos"`
	HechosSemana []PedidoOut `json:"hechos_semana"`
	Descartados  []PedidoOut `json:"descartados_semana,omitempty"`
	// GitHub es falso si no se pudo preguntar: las integraciones salen solo
	// de las ejecuciones y faltan las solicitudes de Renovate.
	GitHub bool `json:"github"`
}

// libretaMax caps each list of the libreta in app_estado.
const libretaMax = 30

// libretaOut is the libreta in the agents' words.
func libretaOut(l libreta.Libreta, now time.Time) *LibretaOut {
	out := &LibretaOut{Abiertos: []PedidoOut{}, HechosSemana: []PedidoOut{}, GitHub: l.GitHub}
	week := now.Add(-7 * 24 * time.Hour)
	for _, it := range l.Items {
		p := PedidoOut{Fuente: it.Source, ID: it.ID, Titulo: it.Title, Estado: it.Status, Porque: it.Why, Motivo: it.Reason, Agente: it.Agent,
			Rama: it.Branch, Solicitud: it.Pull, Ejecucion: it.Run, Version: it.Release, Terminado: it.DoneAt}
		if !it.At.IsZero() {
			at := it.At
			p.Pedido = &at
		}
		if it.Source == libreta.FromPruebas {
			p.Titulo = "Pruebas propias que solo avisan: " + strings.Join(it.Tests, ", ")
		}
		if it.Progress != nil {
			p.Progreso = fmt.Sprintf("%d de %d versiones limpias", it.Progress.Done, it.Progress.Of)
		}
		recent := it.DoneAt != nil && it.DoneAt.After(week)
		switch {
		case libreta.Open(it.Status):
			if len(out.Abiertos) < libretaMax {
				out.Abiertos = append(out.Abiertos, p)
			}
		case libreta.Done(it.Status) && recent:
			if len(out.HechosSemana) < libretaMax {
				out.HechosSemana = append(out.HechosSemana, p)
			}
		case it.Status == libreta.Descartado && recent:
			if len(out.Descartados) < libretaMax {
				out.Descartados = append(out.Descartados, p)
			}
		}
	}
	return out
}
