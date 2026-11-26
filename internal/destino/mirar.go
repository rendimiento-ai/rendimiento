package destino

// What an agent may see of an app's infrastructure, read-only (Ruta 184,
// the infra_read permission): its pods, recent events, logs (the previous
// container's too) and rollout in the cluster; its containers and their
// logs on a computer. Only the app's own: its namespace and the objects
// labeled with its name, never Secrets, never another app. The callers
// leave out lines that look like a credential (ruta.LooksSecret).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Mirador is a destination that can show an app's infrastructure to an
// agent, and restart one of its services (the effects permission).
type Mirador interface {
	Mirar(ctx context.Context, app string, q Mirar) (Mirada, error)
	ReiniciarServicio(ctx context.Context, app, servicio string) error
}

// ErrSinMirar: the platform has nowhere it can read an app's
// infrastructure (no cluster, no container engine).
var ErrSinMirar = errors.New("this platform cannot show the app's infrastructure")

// What a look can ask for.
const (
	MirarPods     = "pods"
	MirarEventos  = "events"
	MirarLogs     = "logs"
	MirarRollout  = "rollout"
	maxLineas     = 500
	maxPodsLogs   = 4
	maxEventos    = 60
	maxTextoMirar = 30000
	maxVentana    = 24 * time.Hour
)

// Mirar is what an agent asks to see: some of pods, events, logs and
// rollout (none: pods, events and rollout), maybe of one service only; for
// logs the last Lineas lines (100; up to 500), of the previous container
// when Anterior; and the window of time back from now (1 hour; up to 24).
type Mirar struct {
	Que      []string      `json:"que,omitempty"`
	Servicio string        `json:"servicio,omitempty"`
	Lineas   int           `json:"lineas,omitempty"`
	Anterior bool          `json:"anterior,omitempty"`
	Ventana  time.Duration `json:"ventana,omitempty"`
}

// Normal is the look with its defaults and limits.
func (q Mirar) Normal() Mirar {
	if len(q.Que) == 0 {
		q.Que = []string{MirarPods, MirarEventos, MirarRollout}
	}
	var que []string
	for _, w := range q.Que {
		switch w = strings.ToLower(strings.TrimSpace(w)); w {
		case MirarPods, MirarEventos, MirarLogs, MirarRollout:
			que = append(que, w)
		}
	}
	q.Que = que
	if q.Lineas <= 0 {
		q.Lineas = 100
	}
	q.Lineas = min(q.Lineas, maxLineas)
	if q.Ventana <= 0 {
		q.Ventana = time.Hour
	}
	q.Ventana = min(q.Ventana, maxVentana)
	q.Servicio = strings.TrimSpace(q.Servicio)
	return q
}

// Pide says whether the look asks for w.
func (q Mirar) Pide(w string) bool {
	for _, x := range q.Que {
		if x == w {
			return true
		}
	}
	return false
}

// Mirada is what was seen: where (cluster or computer), and each part.
type Mirada struct {
	Donde    string         `json:"donde"` // "cluster" or "computer"
	Espacio  string         `json:"espacio,omitempty"`
	Pods     []PodVisto     `json:"pods,omitempty"`
	Eventos  []EventoVisto  `json:"eventos,omitempty"`
	Logs     []LogVisto     `json:"logs,omitempty"`
	Rollouts []RolloutVisto `json:"rollouts,omitempty"`
	// Notas say what could not be read, or does not exist here.
	Notas []string `json:"notas,omitempty"`
}

// PodVisto is a pod (or, on a computer, a container) and its containers.
type PodVisto struct {
	Nombre      string            `json:"nombre"`
	Servicio    string            `json:"servicio,omitempty"`
	Fase        string            `json:"fase"`
	Condiciones []string          `json:"condiciones,omitempty"`
	Contenedor  []ContenedorVisto `json:"contenedores,omitempty"`
	Creado      time.Time         `json:"creado,omitempty"`
}

// ContenedorVisto is a container's state: ready, how many restarts, and
// why it waits or why it ended (now, and the time before).
type ContenedorVisto struct {
	Nombre    string `json:"nombre"`
	Listo     bool   `json:"listo"`
	Reinicios int32  `json:"reinicios"`
	Estado    string `json:"estado"`
	Antes     string `json:"antes,omitempty"`
}

// EventoVisto is one of the app's recent events.
type EventoVisto struct {
	Cuando  time.Time `json:"cuando"`
	Tipo    string    `json:"tipo"`
	Objeto  string    `json:"objeto"`
	Motivo  string    `json:"motivo"`
	Mensaje string    `json:"mensaje"`
	Veces   int32     `json:"veces"`
}

// LogVisto is the end of a container's log.
type LogVisto struct {
	Pod        string `json:"pod"`
	Contenedor string `json:"contenedor"`
	Anterior   bool   `json:"anterior,omitempty"`
	Texto      string `json:"texto"`
}

// RolloutVisto is how a deployment or a statefulset rolls out.
type RolloutVisto struct {
	Tipo        string   `json:"tipo"`
	Nombre      string   `json:"nombre"`
	Deseadas    int32    `json:"deseadas"`
	Actuales    int32    `json:"actualizadas"`
	Listas      int32    `json:"listas"`
	Disponibles int32    `json:"disponibles"`
	AlDia       bool     `json:"alDia"` // the controller saw its last change
	Condiciones []string `json:"condiciones,omitempty"`
}

// Texto is the look as text for a model, cut to its size.
func (m Mirada) Texto() string {
	var b strings.Builder
	where := "the cluster, namespace " + m.Espacio
	if m.Donde == "computer" {
		where = "this computer's containers"
	}
	fmt.Fprintf(&b, "Seen in %s:\n", where)
	if len(m.Rollouts) > 0 {
		b.WriteString("\nRollout:\n")
		for _, r := range m.Rollouts {
			fmt.Fprintf(&b, "- %s %s: %d wanted, %d updated, %d ready, %d available", r.Tipo, r.Nombre, r.Deseadas, r.Actuales, r.Listas, r.Disponibles)
			if !r.AlDia {
				b.WriteString(" (the controller has not seen its last change yet)")
			}
			if len(r.Condiciones) > 0 {
				fmt.Fprintf(&b, "; %s", strings.Join(r.Condiciones, "; "))
			}
			b.WriteString("\n")
		}
	}
	if len(m.Pods) > 0 {
		b.WriteString("\nPods:\n")
		for _, p := range m.Pods {
			fmt.Fprintf(&b, "- %s", p.Nombre)
			if p.Servicio != "" {
				fmt.Fprintf(&b, " (service %s)", p.Servicio)
			}
			fmt.Fprintf(&b, ": %s", p.Fase)
			if !p.Creado.IsZero() {
				fmt.Fprintf(&b, ", since %s", p.Creado.UTC().Format("2006-01-02 15:04"))
			}
			if len(p.Condiciones) > 0 {
				fmt.Fprintf(&b, "; %s", strings.Join(p.Condiciones, ", "))
			}
			b.WriteString("\n")
			for _, c := range p.Contenedor {
				fmt.Fprintf(&b, "    container %s: %s, ready %v, %d restarts", c.Nombre, c.Estado, c.Listo, c.Reinicios)
				if c.Antes != "" {
					fmt.Fprintf(&b, "; before: %s", c.Antes)
				}
				b.WriteString("\n")
			}
		}
	}
	if len(m.Eventos) > 0 {
		b.WriteString("\nRecent events (newest last):\n")
		for _, e := range m.Eventos {
			fmt.Fprintf(&b, "- %s %s %s %s: %s", e.Cuando.UTC().Format("2006-01-02 15:04:05"), e.Tipo, e.Objeto, e.Motivo, e.Mensaje)
			if e.Veces > 1 {
				fmt.Fprintf(&b, " (%d times)", e.Veces)
			}
			b.WriteString("\n")
		}
	}
	for _, l := range m.Logs {
		prev := ""
		if l.Anterior {
			prev = ", the previous container"
		}
		fmt.Fprintf(&b, "\nLog of %s, container %s%s:\n%s\n", l.Pod, l.Contenedor, prev, strings.TrimRight(l.Texto, "\n"))
	}
	for _, n := range m.Notas {
		fmt.Fprintf(&b, "\n(%s)", n)
	}
	s := b.String()
	if len(s) > maxTextoMirar {
		s = s[:maxTextoMirar] + "\n… (cut: ask for less, one service or fewer lines)"
	}
	return s
}

// ultimas is the last n lines of a text, cut to a size.
func ultimas(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	out := strings.Join(lines[max(0, len(lines)-n):], "\n")
	if len(out) > 12000 {
		out = "…" + out[len(out)-12000:]
	}
	return out
}
