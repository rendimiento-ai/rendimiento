package destino

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kfake "k8s.io/client-go/kubernetes/fake"

	"github.com/rendimiento-ai/rendimiento/internal/motor"
	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// rolloutFallido is a namespace where release 2 of the app's web service
// timed out progressing: its Deployment at revision 2, the new ReplicaSet
// with a pod in a crash loop, the old one (revision 1) still ready, and the
// events about them and about something else.
func rolloutFallido(app string) []runtime.Object {
	labels := func(hash string) map[string]string {
		return map[string]string{render.LabelApp: app, render.LabelService: "web", appsv1.DefaultDeploymentUniqueLabelKey: hash}
	}
	owner := []metav1.OwnerReference{{Kind: "Deployment", Name: "web", APIVersion: "apps/v1"}}
	rs := func(hash, rev string, age time.Duration) *appsv1.ReplicaSet {
		return &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "web-" + hash, Namespace: app, Labels: labels(hash), OwnerReferences: owner,
			Annotations: map[string]string{revisionAnnotation: rev}, CreationTimestamp: metav1.NewTime(time.Now().Add(-age))}}
	}
	at := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	event := func(name, kind, obj, reason, msg string, ago time.Duration) *corev1.Event {
		return &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: app},
			InvolvedObject: corev1.ObjectReference{Kind: kind, Name: obj, Namespace: app}, Type: corev1.EventTypeWarning, Reason: reason, Message: msg,
			LastTimestamp: metav1.NewTime(time.Now().Add(-ago)), Count: 3}
	}
	return []runtime.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: app, Annotations: map[string]string{revisionAnnotation: "2"}},
			Status: appsv1.DeploymentStatus{Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue, Reason: "MinimumReplicasAvailable"},
				{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded", Message: `ReplicaSet "web-7c9d" has timed out progressing.`},
			}}},
		rs("5b8c", "1", time.Hour), rs("7c9d", "2", 10*time.Minute),
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-7c9d-abcde", Namespace: app, Labels: labels("7c9d")},
			Spec: corev1.PodSpec{NodeName: "worker-1"},
			Status: corev1.PodStatus{Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse, Reason: "ContainersNotReady", Message: "containers with unready status: [web]"},
					{Type: corev1.PodScheduled, Status: corev1.ConditionTrue}},
				ContainerStatuses: []corev1.ContainerStatus{{Name: "web", RestartCount: 5,
					State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off 5m0s restarting failed container"}},
					LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error", FinishedAt: at}}}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-5b8c-zzzzz", Namespace: app, Labels: labels("5b8c")},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				ContainerStatuses: []corev1.ContainerStatus{{Name: "web", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}},
		event("e1", "Pod", "web-7c9d-abcde", "BackOff", "Back-off restarting failed container web", time.Minute),
		event("e2", "ReplicaSet", "web-7c9d", "SuccessfulCreate", "Created pod: web-7c9d-abcde", 9*time.Minute),
		event("e3", "Pod", "otra-1234", "Failed", "something else's", 30*time.Second),
		event("e4", "Pod", "web-5b8c-zzzzz", "Pulled", "the old release's", 50*time.Minute),
	}
}

// A release that timed out progressing: its new ReplicaSet's pod with its
// state and both logs, the Deployment's reason, and only the events about
// the release's own objects, oldest first. The old ReplicaSet's pod is not
// the release's.
func TestKubernetesEvidencia(t *testing.T) {
	ctx := context.Background()
	cs := kfake.NewSimpleClientset(rolloutFallido("evidencia")...)
	k := Kubernetes{Clientset: cs}
	var _ Testigo = k
	sp := spec.Spec{Services: []spec.Service{{Name: "web"}, {Name: "worker"}}}
	ev, err := k.Evidencia(ctx, "evidencia", &store.Release{Number: 2, Spec: sp})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Where != "kubernetes" || len(ev.Services) != 2 {
		t.Fatalf("evidence: %+v", ev)
	}
	web := ev.Services[0]
	if web.ReplicaSet != "web-7c9d" || !strings.Contains(web.Message, `ProgressDeadlineExceeded: ReplicaSet "web-7c9d" has timed out progressing.`) {
		t.Fatalf("web: %+v", web)
	}
	if len(web.Pods) != 1 || web.Pods[0].Name != "web-7c9d-abcde" || web.Pods[0].Node != "worker-1" ||
		len(web.Pods[0].Conditions) != 1 || web.Pods[0].Conditions[0] != "Ready: ContainersNotReady (containers with unready status: [web])" {
		t.Fatalf("web's pods: %+v", web.Pods)
	}
	c := web.Pods[0].Containers[0]
	if c.State != "waiting" || c.Reason != "CrashLoopBackOff" || c.Restarts != 5 || c.LastExitCode == nil || *c.LastExitCode != 1 || c.LastReason != "Error" ||
		c.LastFinishedAt == nil || c.Log == "" || c.PreviousLog == "" {
		t.Fatalf("web's container: %+v", c)
	}
	if got := c.Describe(); got != "waiting CrashLoopBackOff: back-off 5m0s restarting failed container, 5 restarts, last exited 1 (Error)" {
		t.Fatalf("described: %q", got)
	}
	if w := ev.Services[1]; w.Name != "worker" || w.Message != "no Deployment" || len(w.Pods) != 0 {
		t.Fatalf("worker: %+v", w)
	}
	var objs []string
	for _, e := range ev.Events {
		objs = append(objs, e.Object+" "+e.Reason)
	}
	if strings.Join(objs, ", ") != "ReplicaSet/web-7c9d SuccessfulCreate, Pod/web-7c9d-abcde BackOff" {
		t.Fatalf("events: %v", objs)
	}
	sum := ev.Summary(10)
	for _, want := range []string{"service web, ReplicaSet web-7c9d", "web-7c9d-abcde Running (Ready: ContainersNotReady", "last lines:", "Pod/web-7c9d-abcde BackOff: Back-off restarting failed container web (3 times)"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary without %q:\n%s", want, sum)
		}
	}

	// Without a clientset it cannot say.
	if _, err := (Kubernetes{}).Evidencia(ctx, "evidencia", &store.Release{Spec: sp}); !errors.Is(err, ErrSinTestigo) {
		t.Fatalf("without a clientset: %v", err)
	}
}

// Of many events, the latest EvidenciaEventos are kept, oldest first.
func TestEventosDe(t *testing.T) {
	var items []corev1.Event
	now := time.Now()
	for i := range 80 {
		items = append(items, corev1.Event{InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web-1"}, Reason: fmt.Sprint(i),
			LastTimestamp: metav1.NewTime(now.Add(time.Duration(i) * time.Second))})
	}
	got := eventosDe(items, map[string]bool{"Pod/web-1": true})
	if len(got) != EvidenciaEventos || got[0].Reason != "30" || got[len(got)-1].Reason != "79" {
		t.Fatalf("%d events, from %s to %s", len(got), got[0].Reason, got[len(got)-1].Reason)
	}
}

// On a computer: a release whose container keeps stopping is removed, but
// what it showed (its state, exit code and last lines) is kept for the
// platform to ask; a release that still runs is read as it is.
func TestLocalEvidencia(t *testing.T) {
	ctx := context.Background()
	e := &engine{containers: map[string]*fakeContainer{}, crash: "Error: listen EADDRINUSE :::3000"}
	l := &Local{Motor: &motor.Motor{Bin: "nerdctl", Exec: e.exec}, Caja: memCaja{}, Esperar: 300 * time.Millisecond,
		Responde: func(context.Context, string, *spec.Health) bool { return false }}
	var _ Testigo = l
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 3000}}}
	sp.Default()
	if err := l.Crear(ctx, &store.App{Name: "evidencia"}, false); err != nil {
		t.Fatal(err)
	}
	rel := &store.Release{Number: 4, Spec: sp, Images: map[string]string{"web": "localhost:5000/evidencia-web@sha256:4"}}
	if err := l.Aplicar(ctx, "evidencia", rel); err != nil {
		t.Fatal(err)
	}
	waitEstado(t, l, "evidencia", 4, Fallida)
	if e.containers["evidencia-web-r4"] != nil {
		t.Fatal("the failed release's container is still there")
	}
	ev, err := l.Evidencia(ctx, "evidencia", rel)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Where != "local" || len(ev.Services) != 1 || len(ev.Services[0].Pods) != 1 {
		t.Fatalf("evidence: %+v", ev)
	}
	c := ev.Services[0].Pods[0].Containers[0]
	if c.Name != "web" || c.State != "terminated" || c.Reason != "exited" || c.ExitCode == nil || *c.ExitCode != 1 || c.Restarts != 2 ||
		!strings.Contains(c.Log, "EADDRINUSE") {
		t.Fatalf("container: %+v", c)
	}

	// A release that answers, read while it runs.
	e.crash = ""
	l.Responde = func(context.Context, string, *spec.Health) bool { return true }
	rel5 := &store.Release{Number: 5, Spec: sp, Images: map[string]string{"web": "localhost:5000/evidencia-web@sha256:5"}}
	_ = l.Aplicar(ctx, "evidencia", rel5)
	waitEstado(t, l, "evidencia", 5, Sana)
	ev, _ = l.Evidencia(ctx, "evidencia", rel5)
	if len(ev.Services[0].Pods) != 1 || ev.Services[0].Pods[0].Name != "evidencia-web-r5" || !ev.Services[0].Pods[0].Containers[0].Ready ||
		ev.Services[0].Pods[0].Containers[0].State != "running" {
		t.Fatalf("a running release: %+v", ev.Services[0].Pods)
	}
}
