package destino

import (
	"context"
	"fmt"
	"strings"
)

// Mirar on a computer reads the app's containers (by the label with its
// name): their state, exit code and restarts, and the end of their logs
// within the window. A computer has no events, and a container keeps one
// log: there is no previous one apart.
func (l *Local) Mirar(ctx context.Context, app string, q Mirar) (Mirada, error) {
	if l.Motor == nil {
		return Mirada{}, ErrSinMirar
	}
	q = q.Normal()
	m := Mirada{Donde: "computer"}
	var vistos []string
	for _, info := range l.servicios(ctx, app) {
		if info.Etiquetas[etApp] != app {
			continue
		}
		svc := info.Etiquetas[etServicio]
		if q.Servicio != "" && svc != q.Servicio {
			continue
		}
		vistos = append(vistos, info.Nombre)
		if q.Pide(MirarPods) {
			estado := info.Estado
			if !info.Corriendo && info.Estado != "" {
				estado = fmt.Sprintf("%s, exit code %d", info.Estado, info.Salida)
			}
			if info.Desde != "" && info.Corriendo {
				estado += " since " + info.Desde
			}
			parte := info.Etiquetas[etParte]
			fase := "Running"
			if !info.Corriendo {
				fase = "Stopped"
			}
			if r := info.Etiquetas[etVersion]; r != "" {
				fase += ", release #" + r
			}
			if parte != "" && parte != "service" {
				fase += " (" + parte + ")"
			}
			m.Pods = append(m.Pods, PodVisto{Nombre: info.Nombre, Servicio: svc, Fase: fase,
				Contenedor: []ContenedorVisto{{Nombre: info.Nombre, Listo: info.Corriendo, Reinicios: int32(info.Reinicios), Estado: estado}}})
		}
	}
	if q.Pide(MirarRollout) {
		l.mu.Lock()
		e, ok := l.estados[app]
		l.mu.Unlock()
		if ok {
			r := RolloutVisto{Tipo: "Release", Nombre: fmt.Sprintf("#%d", e.version), AlDia: true}
			r.Condiciones = append(r.Condiciones, [...]string{"rolling out", "healthy", "failed", "replaced"}[min(int(e.estado), 3)])
			if e.mensaje != "" {
				r.Condiciones = append(r.Condiciones, recortar(e.mensaje, 400))
			}
			m.Rollouts = append(m.Rollouts, r)
		}
	}
	if q.Pide(MirarEventos) {
		m.Notas = append(m.Notas, "a computer keeps no events: the containers' state and logs say what happened")
	}
	if q.Pide(MirarLogs) {
		if q.Anterior {
			m.Notas = append(m.Notas, "on a computer a container keeps one log, the previous runs included")
		}
		for i, n := range vistos {
			if i == maxPodsLogs {
				m.Notas = append(m.Notas, fmt.Sprintf("logs of %d more containers left out; ask for one service", len(vistos)-maxPodsLogs))
				break
			}
			texto, err := l.Motor.RegistrosDesde(ctx, n, q.Lineas, q.Ventana)
			if err != nil {
				m.Notas = append(m.Notas, "the log of "+n+" could not be read: "+err.Error())
				continue
			}
			m.Logs = append(m.Logs, LogVisto{Pod: n, Contenedor: n, Texto: ultimas(texto, q.Lineas)})
		}
	}
	if len(vistos) == 0 {
		m.Notas = append(m.Notas, "no containers of the app on this computer now")
	}
	return m, nil
}

// ReiniciarServicio restarts one service's containers of the app.
func (l *Local) ReiniciarServicio(ctx context.Context, app, servicio string) error {
	if l.Motor == nil {
		return ErrSinMirar
	}
	servicio = strings.TrimSpace(servicio)
	n := 0
	for _, info := range l.servicios(ctx, app) {
		if info.Etiquetas[etApp] == app && info.Etiquetas[etParte] == "service" && info.Etiquetas[etServicio] == servicio {
			if err := l.Motor.Reiniciar(ctx, info.Nombre); err != nil {
				return err
			}
			n++
		}
	}
	if n == 0 {
		return fmt.Errorf("the app %s has no container of service %s", app, servicio)
	}
	return nil
}
