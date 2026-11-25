package destino

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"github.com/rendimiento-ai/rendimiento/internal/render"
)

// selectorDe picks the app's objects (and one service's, when given).
func selectorDe(app, servicio string) string {
	set := labels.Set{render.LabelApp: app}
	if servicio != "" {
		set[render.LabelService] = servicio
	}
	return labels.SelectorFromSet(set).String()
}

// Mirar reads the app's namespace (named after it): only the pods,
// workloads and events of objects labeled with its name, so another app
// sharing the namespace is never seen. It never reads a Secret.
func (k Kubernetes) Mirar(ctx context.Context, app string, q Mirar) (Mirada, error) {
	if k.Clientset == nil {
		return Mirada{}, ErrSinMirar
	}
	q = q.Normal()
	ns, sel := app, selectorDe(app, q.Servicio)
	m := Mirada{Donde: "cluster", Espacio: ns}
	desde := time.Now().Add(-q.Ventana)
	pods, err := k.Clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if apierrors.IsNotFound(err) || (err == nil && len(pods.Items) == 0 && q.Servicio == "") {
		m.Notas = append(m.Notas, "no pods of the app in its namespace now")
	} else if err != nil {
		return m, fmt.Errorf("reading the pods: %w", err)
	}
	var items []corev1.Pod
	if pods != nil {
		items = pods.Items
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreationTimestamp.After(items[j].CreationTimestamp.Time) })
	nombres := map[string]bool{}
	for _, p := range items {
		nombres[p.Name] = true
		if q.Pide(MirarPods) {
			m.Pods = append(m.Pods, podVisto(p))
		}
	}
	deps, _ := k.Clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{LabelSelector: sel})
	sets, _ := k.Clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{LabelSelector: sel})
	rss, _ := k.Clientset.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if deps != nil {
		for _, d := range deps.Items {
			nombres[d.Name] = true
			if q.Pide(MirarRollout) {
				m.Rollouts = append(m.Rollouts, rolloutDeployment(d))
			}
		}
	}
	if sets != nil {
		for _, s := range sets.Items {
			nombres[s.Name] = true
			if q.Pide(MirarRollout) {
				m.Rollouts = append(m.Rollouts, rolloutStatefulSet(s))
			}
		}
	}
	if rss != nil {
		for _, r := range rss.Items {
			nombres[r.Name] = true
		}
	}
	if q.Pide(MirarEventos) {
		evs, err := k.Clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			m.Notas = append(m.Notas, "the events could not be read: "+err.Error())
		} else {
			m.Eventos = eventosVistos(evs.Items, nombres, desde)
		}
	}
	if q.Pide(MirarLogs) {
		since := int64(q.Ventana.Seconds())
		tail := int64(q.Lineas)
		for i, p := range items {
			if i == maxPodsLogs {
				m.Notas = append(m.Notas, fmt.Sprintf("logs of %d more pods left out; ask for one service", len(items)-maxPodsLogs))
				break
			}
			for _, c := range p.Spec.Containers {
				opts := &corev1.PodLogOptions{Container: c.Name, TailLines: &tail, Previous: q.Anterior}
				if !q.Anterior {
					opts.SinceSeconds = &since
				}
				texto, err := leerLog(ctx, k, ns, p.Name, opts)
				if err != nil {
					m.Notas = append(m.Notas, fmt.Sprintf("the log of %s/%s could not be read: %v", p.Name, c.Name, err))
					continue
				}
				m.Logs = append(m.Logs, LogVisto{Pod: p.Name, Contenedor: c.Name, Anterior: q.Anterior, Texto: ultimas(texto, q.Lineas)})
			}
		}
	}
	return m, nil
}

func leerLog(ctx context.Context, k Kubernetes, ns, pod string, opts *corev1.PodLogOptions) (string, error) {
	rc, err := k.Clientset.CoreV1().Pods(ns).GetLogs(pod, opts).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 256<<10))
	return string(b), err
}

func podVisto(p corev1.Pod) PodVisto {
	v := PodVisto{Nombre: p.Name, Servicio: p.Labels[render.LabelService], Fase: string(p.Status.Phase), Creado: p.CreationTimestamp.Time}
	if p.Status.Reason != "" {
		v.Fase += " (" + p.Status.Reason + ")"
	}
	for _, c := range p.Status.Conditions {
		s := string(c.Type) + "=" + string(c.Status)
		if c.Reason != "" {
			s += " " + c.Reason
		}
		if c.Status != corev1.ConditionTrue && c.Message != "" {
			s += ": " + c.Message
		}
		v.Condiciones = append(v.Condiciones, s)
	}
	for _, c := range append(append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...) {
		v.Contenedor = append(v.Contenedor, ContenedorVisto{Nombre: c.Name, Listo: c.Ready, Reinicios: c.RestartCount,
			Estado: estadoDe(c.State), Antes: estadoAntes(c.LastTerminationState)})
	}
	return v
}

func estadoDe(s corev1.ContainerState) string {
	switch {
	case s.Running != nil:
		return "running since " + s.Running.StartedAt.UTC().Format("15:04:05")
	case s.Waiting != nil:
		w := "waiting: " + s.Waiting.Reason
		if s.Waiting.Message != "" {
			w += " (" + recortar(s.Waiting.Message, 300) + ")"
		}
		return w
	case s.Terminated != nil:
		return terminado(s.Terminated)
	}
	return "unknown"
}

func estadoAntes(s corev1.ContainerState) string {
	if s.Terminated == nil {
		return ""
	}
	return terminado(s.Terminated)
}

func terminado(t *corev1.ContainerStateTerminated) string {
	s := fmt.Sprintf("terminated: %s, exit code %d", t.Reason, t.ExitCode)
	if !t.FinishedAt.IsZero() {
		s += ", at " + t.FinishedAt.UTC().Format("2006-01-02 15:04:05")
	}
	if t.Message != "" {
		s += " (" + recortar(t.Message, 300) + ")"
	}
	return s
}

func recortar(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func rolloutDeployment(d appsv1.Deployment) RolloutVisto {
	r := RolloutVisto{Tipo: "Deployment", Nombre: d.Name, Actuales: d.Status.UpdatedReplicas, Listas: d.Status.ReadyReplicas,
		Disponibles: d.Status.AvailableReplicas, AlDia: d.Status.ObservedGeneration >= d.Generation}
	if d.Spec.Replicas != nil {
		r.Deseadas = *d.Spec.Replicas
	}
	for _, c := range d.Status.Conditions {
		r.Condiciones = append(r.Condiciones, condicion(string(c.Type), string(c.Status), c.Reason, c.Message))
	}
	return r
}

func rolloutStatefulSet(s appsv1.StatefulSet) RolloutVisto {
	r := RolloutVisto{Tipo: "StatefulSet", Nombre: s.Name, Actuales: s.Status.UpdatedReplicas, Listas: s.Status.ReadyReplicas,
		Disponibles: s.Status.AvailableReplicas, AlDia: s.Status.ObservedGeneration >= s.Generation}
	if s.Spec.Replicas != nil {
		r.Deseadas = *s.Spec.Replicas
	}
	for _, c := range s.Status.Conditions {
		r.Condiciones = append(r.Condiciones, condicion(string(c.Type), string(c.Status), c.Reason, c.Message))
	}
	return r
}

func condicion(tipo, status, reason, msg string) string {
	s := tipo + "=" + status
	if reason != "" {
		s += " " + reason
	}
	if msg != "" {
		s += ": " + recortar(msg, 300)
	}
	return s
}

// eventosVistos are the events about the app's own objects (by name) since a
// time, oldest first, the newest maxEventos.
func eventosVistos(evs []corev1.Event, nombres map[string]bool, desde time.Time) []EventoVisto {
	var out []EventoVisto
	for _, e := range evs {
		if !nombres[e.InvolvedObject.Name] {
			continue
		}
		at := e.LastTimestamp.Time
		if at.IsZero() {
			at = e.EventTime.Time
		}
		if at.IsZero() {
			at = e.CreationTimestamp.Time
		}
		if at.Before(desde) {
			continue
		}
		out = append(out, EventoVisto{Cuando: at, Tipo: e.Type, Objeto: e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
			Motivo: e.Reason, Mensaje: recortar(e.Message, 400), Veces: e.Count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cuando.Before(out[j].Cuando) })
	if len(out) > maxEventos {
		out = out[len(out)-maxEventos:]
	}
	return out
}

// ReiniciarServicio restarts one service's Deployment of the app, as
// kubectl rollout restart does: only one labeled with the app's name, in
// its namespace.
func (k Kubernetes) ReiniciarServicio(ctx context.Context, app, servicio string) error {
	if k.Clientset == nil {
		return ErrSinMirar
	}
	if strings.TrimSpace(servicio) == "" {
		return fmt.Errorf("say which service")
	}
	deps, err := k.Clientset.AppsV1().Deployments(app).List(ctx, metav1.ListOptions{LabelSelector: selectorDe(app, servicio)})
	if err != nil {
		return err
	}
	if len(deps.Items) == 0 {
		return fmt.Errorf("the app %s has no deployment of service %s", app, servicio)
	}
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`, time.Now().UTC().Format(time.RFC3339))
	for _, d := range deps.Items {
		if d.Labels[render.LabelApp] != app {
			continue
		}
		if _, err := k.Clientset.AppsV1().Deployments(app).Patch(ctx, d.Name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{}); err != nil {
			return err
		}
	}
	return nil
}
