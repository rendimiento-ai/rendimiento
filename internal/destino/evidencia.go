package destino

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Testigo is a destination that can say what it sees of a release: each
// service's new pods (or containers) with their state, the events about
// them and the last lines they wrote. The platform keeps it when a release
// fails verification, before the rollback removes them.
type Testigo interface {
	Evidencia(ctx context.Context, app string, rel *store.Release) (*store.ReleaseEvidence, error)
}

// ErrSinTestigo: this destination cannot say what it saw (in Kubernetes,
// without a clientset).
var ErrSinTestigo = errors.New("this destination cannot read its pods")

// What is read of a failed release.
const (
	// EvidenciaEventos is how many of the latest events are kept.
	EvidenciaEventos = 50
	// EvidenciaLineas is how many of each container's last lines are read.
	EvidenciaLineas = 200
	// evidenciaPods is how many pods of each service are looked at (the
	// ones not ready first).
	evidenciaPods = 3
	// evidenciaBytesLog is the most read of one container's log.
	evidenciaBytesLog = 32 << 10
)

// revisionAnnotation is the Deployment's revision, which its newest
// ReplicaSet carries too.
const revisionAnnotation = "deployment.kubernetes.io/revision"

// Evidencia reads, for each service of the release, its Deployment's new
// ReplicaSet and that ReplicaSet's pods (the ones not ready first), with
// their containers' states and last lines (and the lines before the last
// restart), and the namespace's latest events about any of them. What it
// cannot read is said in Notes; it fails only without a clientset.
func (k Kubernetes) Evidencia(ctx context.Context, app string, rel *store.Release) (*store.ReleaseEvidence, error) {
	if k.Clientset == nil {
		return nil, ErrSinTestigo
	}
	ns := app
	ev := &store.ReleaseEvidence{CapturedAt: time.Now().UTC(), Where: k.Nombre(), Services: []store.ServiceEvidence{}}
	objetos := map[string]bool{} // kind/name of what the events may be about
	note := func(format string, a ...any) { ev.Notes = append(ev.Notes, fmt.Sprintf(format, a...)) }
	for _, svc := range rel.Spec.Services {
		se := store.ServiceEvidence{Name: svc.Name, Pods: []store.PodEvidence{}}
		sel := render.LabelApp + "=" + app + "," + render.LabelService + "=" + svc.Name
		objetos["Deployment/"+svc.Name] = true
		hash := ""
		dep, err := k.Clientset.AppsV1().Deployments(ns).Get(ctx, svc.Name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			se.Message = "no Deployment"
		case err != nil:
			note("%s: the Deployment: %v", svc.Name, err)
		default:
			se.Message = deploymentMessage(dep)
			rss, err := k.Clientset.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{LabelSelector: sel})
			if err != nil {
				note("%s: its ReplicaSets: %v", svc.Name, err)
			} else if rs := newReplicaSet(dep, rss.Items); rs != nil {
				se.ReplicaSet = rs.Name
				objetos["ReplicaSet/"+rs.Name] = true
				hash = rs.Labels[appsv1.DefaultDeploymentUniqueLabelKey]
			}
		}
		podSel := sel
		if hash != "" {
			podSel += "," + appsv1.DefaultDeploymentUniqueLabelKey + "=" + hash
		}
		pods, err := k.Clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: podSel})
		if err != nil {
			note("%s: its pods: %v", svc.Name, err)
		} else {
			items := pods.Items
			sort.SliceStable(items, func(i, j int) bool {
				ri, rj := podReady(&items[i]), podReady(&items[j])
				if ri != rj {
					return !ri
				}
				return items[i].Name < items[j].Name
			})
			for i := range items {
				objetos["Pod/"+items[i].Name] = true
				if i < evidenciaPods {
					se.Pods = append(se.Pods, k.podEvidencia(ctx, &items[i]))
				}
			}
		}
		ev.Services = append(ev.Services, se)
	}
	events, err := k.Clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		note("the events: %v", err)
	} else {
		ev.Events = eventosDe(events.Items, objetos)
	}
	return ev, nil
}

// deploymentMessage is why a Deployment is not progressing or available
// ("" when it is both).
func deploymentMessage(d *appsv1.Deployment) string {
	var out []string
	for _, c := range d.Status.Conditions {
		bad := (c.Type == appsv1.DeploymentReplicaFailure && c.Status == corev1.ConditionTrue) ||
			(c.Type != appsv1.DeploymentReplicaFailure && c.Status != corev1.ConditionTrue) ||
			(c.Type == appsv1.DeploymentProgressing && c.Reason == "ProgressDeadlineExceeded")
		if bad {
			out = append(out, fmt.Sprintf("%s: %s", c.Reason, c.Message))
		}
	}
	return strings.Join(out, "; ")
}

// newReplicaSet is the Deployment's ReplicaSet of its current revision (the
// release's), or else the newest one it owns.
func newReplicaSet(d *appsv1.Deployment, rss []appsv1.ReplicaSet) *appsv1.ReplicaSet {
	var newest *appsv1.ReplicaSet
	for i := range rss {
		rs := &rss[i]
		owned := false
		for _, o := range rs.OwnerReferences {
			if o.Kind == "Deployment" && o.Name == d.Name {
				owned = true
			}
		}
		if !owned {
			continue
		}
		if rev := d.Annotations[revisionAnnotation]; rev != "" && rs.Annotations[revisionAnnotation] == rev {
			return rs
		}
		if newest == nil || rs.CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = rs
		}
	}
	return newest
}

func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// podEvidencia is a pod's phase, its conditions that are not true and its
// containers (init containers first), each with its last lines.
func (k Kubernetes) podEvidencia(ctx context.Context, p *corev1.Pod) store.PodEvidence {
	pe := store.PodEvidence{Name: p.Name, Phase: string(p.Status.Phase), Node: p.Spec.NodeName, Containers: []store.ContainerEvidence{}}
	if p.Status.Reason != "" {
		pe.Phase += " (" + p.Status.Reason + ")"
	}
	for _, c := range p.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			line := string(c.Type)
			if c.Reason != "" {
				line += ": " + c.Reason
			}
			if c.Message != "" {
				line += " (" + c.Message + ")"
			}
			pe.Conditions = append(pe.Conditions, line)
		}
	}
	statuses := append(append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...)
	for _, cs := range statuses {
		c := containerEvidencia(cs)
		started := cs.State.Running != nil || cs.State.Terminated != nil || cs.LastTerminationState.Terminated != nil
		if started {
			c.Log = k.registro(ctx, p, cs.Name, false)
		}
		if cs.RestartCount > 0 {
			c.PreviousLog = k.registro(ctx, p, cs.Name, true)
		}
		pe.Containers = append(pe.Containers, c)
	}
	return pe
}

// containerEvidencia is a container's state, restarts and last end.
func containerEvidencia(cs corev1.ContainerStatus) store.ContainerEvidence {
	c := store.ContainerEvidence{Name: cs.Name, Ready: cs.Ready, Restarts: cs.RestartCount}
	switch s := cs.State; {
	case s.Waiting != nil:
		c.State, c.Reason, c.Message = "waiting", s.Waiting.Reason, s.Waiting.Message
	case s.Terminated != nil:
		code := s.Terminated.ExitCode
		c.State, c.Reason, c.Message, c.ExitCode = "terminated", s.Terminated.Reason, s.Terminated.Message, &code
	case s.Running != nil:
		c.State = "running"
	default:
		c.State = "unknown"
	}
	if t := cs.LastTerminationState.Terminated; t != nil {
		code := t.ExitCode
		c.LastReason, c.LastExitCode = t.Reason, &code
		if !t.FinishedAt.IsZero() {
			at := t.FinishedAt.UTC()
			c.LastFinishedAt = &at
		}
	}
	return c
}

// registro is the end of a container's log (previous: of its run before the
// last restart); "" when it cannot be read.
func (k Kubernetes) registro(ctx context.Context, p *corev1.Pod, container string, previous bool) string {
	tail, limit := int64(EvidenciaLineas), int64(evidenciaBytesLog)
	rc, err := k.Clientset.CoreV1().Pods(p.Namespace).GetLogs(p.Name, &corev1.PodLogOptions{
		Container: container, Previous: previous, TailLines: &tail, LimitBytes: &limit,
	}).Stream(ctx)
	if err != nil {
		return ""
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, limit))
	return strings.TrimRight(string(b), "\n")
}

// eventosDe are the latest events about the objects (kind/name), oldest
// first, at most EvidenciaEventos.
func eventosDe(items []corev1.Event, objetos map[string]bool) []store.EvidenceEvent {
	var out []store.EvidenceEvent
	for _, e := range items {
		obj := e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name
		if !objetos[obj] {
			continue
		}
		out = append(out, store.EvidenceEvent{At: eventTime(&e), Type: e.Type, Reason: e.Reason, Object: obj, Message: e.Message, Count: e.Count})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	if len(out) > EvidenciaEventos {
		out = out[len(out)-EvidenciaEventos:]
	}
	return out
}

// eventTime is when an event last happened.
func eventTime(e *corev1.Event) time.Time {
	switch {
	case e.Series != nil && !e.Series.LastObservedTime.IsZero():
		return e.Series.LastObservedTime.UTC()
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.UTC()
	case !e.EventTime.IsZero():
		return e.EventTime.UTC()
	}
	return e.CreationTimestamp.UTC()
}
