package destino

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/rendimiento-ai/rendimiento/internal/motor"
	"github.com/rendimiento-ai/rendimiento/internal/render"
)

// In the cluster an agent sees only its app's objects in its namespace:
// another app's pod and events beside it (a shared namespace) and every
// Secret never show; events older than the window are left out; a restart
// touches only the app's service's Deployment.
func TestMirarKubeOnlyItsApp(t *testing.T) {
	ns := "mirar-tienda"
	now := time.Now()
	lbl := func(app, svc string) map[string]string {
		return map[string]string{render.LabelApp: app, render.LabelService: svc}
	}
	two := int32(2)
	cs := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-7d9", Namespace: ns, Labels: lbl(ns, "api"), CreationTimestamp: metav1.NewTime(now)},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}}},
			Status: corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: "0/6 nodes are available: Insufficient memory"}},
				ContainerStatuses: []corev1.ContainerStatus{{Name: "api", RestartCount: 3, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
					LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}}}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "vecina-1", Namespace: ns, Labels: lbl("vecina", "web")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tienda-env", Namespace: ns, Labels: lbl(ns, "api")}, Data: map[string][]byte{"DB_PASSWORD": []byte("valor-que-nunca-se-ve")}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: ns, Labels: lbl(ns, "api"), Generation: 4},
			Spec: appsv1.DeploymentSpec{Replicas: &two}, Status: appsv1.DeploymentStatus{ObservedGeneration: 4, UpdatedReplicas: 1, ReadyReplicas: 0,
				Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded"}}}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: ns, Labels: lbl("vecina", "web")}},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e1", Namespace: ns}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-7d9"},
			Type: "Warning", Reason: "FailedScheduling", Message: "0/6 nodes are available: Insufficient memory", Count: 9, LastTimestamp: metav1.NewTime(now.Add(-5 * time.Minute))},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e2", Namespace: ns}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "vecina-1"},
			Type: "Warning", Reason: "BackOff", Message: "la vecina se cayó", LastTimestamp: metav1.NewTime(now.Add(-time.Minute))},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e3", Namespace: ns}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-7d9"},
			Type: "Normal", Reason: "Antiguo", Message: "hace mucho", LastTimestamp: metav1.NewTime(now.Add(-3 * time.Hour))},
	)
	k := Kubernetes{Clientset: cs}
	m, err := k.Mirar(context.Background(), ns, Mirar{Que: []string{MirarPods, MirarEventos, MirarRollout, MirarLogs}})
	if err != nil {
		t.Fatal(err)
	}
	text := m.Texto()
	for _, want := range []string{"api-7d9", "Insufficient memory", "CrashLoopBackOff", "OOMKilled, exit code 137", "3 restarts", "FailedScheduling", "(9 times)",
		"Deployment api: 2 wanted", "ProgressDeadlineExceeded", "Log of api-7d9"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	for _, never := range []string{"vecina", "la vecina se cayó", "hace mucho", "tienda-env", "valor-que-nunca-se-ve", "Deployment web"} {
		if strings.Contains(text, never) {
			t.Errorf("shows %q:\n%s", never, text)
		}
	}
	if err := k.ReiniciarServicio(context.Background(), ns, "web"); err == nil {
		t.Fatal("restarted another app's service")
	}
	if err := k.ReiniciarServicio(context.Background(), ns, "api"); err != nil {
		t.Fatal(err)
	}
	d, _ := cs.AppsV1().Deployments(ns).Get(context.Background(), "api", metav1.GetOptions{})
	if d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Fatalf("not restarted: %+v", d.Spec.Template.Annotations)
	}
	w, _ := cs.AppsV1().Deployments(ns).Get(context.Background(), "web", metav1.GetOptions{})
	if len(w.Spec.Template.Annotations) != 0 {
		t.Fatal("the other app's deployment changed")
	}
	if len(m.Texto()) > maxTextoMirar+100 {
		t.Fatal("not cut to its size")
	}
}

// motorFake is a container engine with two containers of the app and one
// of another; logs say which container and window they were asked for.
type motorFake struct{ calls []string }

func (f *motorFake) exec(_ context.Context, _ string, args []string, out io.Writer) error {
	f.calls = append(f.calls, strings.Join(args, " "))
	type info struct {
		name, app, svc, part string
		running              bool
		exit, restarts       int
	}
	all := []info{{"mirarc-api-71", "mirarc", "api", "service", false, 137, 4}, {"mirarc-postgres", "mirarc", "", "postgres", true, 0, 0}, {"otra-web-3", "otra", "web", "service", true, 0, 0}}
	switch args[0] {
	case "ps":
		filter := strings.TrimPrefix(args[3], "label=rendimiento.ai/app=")
		for _, c := range all {
			if c.app == filter {
				fmt.Fprintln(out, c.name)
			}
		}
	case "inspect":
		for _, c := range all {
			if c.name == args[len(args)-1] {
				status := "exited"
				if c.running {
					status = "running"
				}
				b, _ := json.Marshal(map[string]any{"State": map[string]any{"Running": c.running, "Status": status, "ExitCode": c.exit},
					"RestartCount": c.restarts, "Config": map[string]any{"Image": "img", "Labels": map[string]string{etApp: c.app, etServicio: c.svc, etParte: c.part, etVersion: "71"}}})
				fmt.Fprintln(out, string(b))
			}
		}
	case "logs":
		fmt.Fprintf(out, "log of %s (%s)\n", args[len(args)-1], strings.Join(args[1:len(args)-1], " "))
	case "restart":
	}
	return nil
}

// On a computer an agent sees its app's containers (state, exit code,
// restarts) and their logs within the window, never another app's; there
// are no events, and a restart touches only its service's container.
func TestMirarLocalOnlyItsApp(t *testing.T) {
	f := &motorFake{}
	l := &Local{Motor: &motor.Motor{Bin: "nerdctl", Exec: f.exec}}
	m, err := l.Mirar(context.Background(), "mirarc", Mirar{Que: []string{MirarPods, MirarLogs, MirarEventos}, Lineas: 30, Ventana: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	text := m.Texto()
	for _, want := range []string{"mirarc-api-71", "exit code 137", "4 restarts", "release #71", "log of mirarc-api-71 (--tail 30 --since 10m0s)", "keeps no events"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "otra") || m.Donde != "computer" {
		t.Fatalf("another app's, or not a computer:\n%s", text)
	}
	if err := l.ReiniciarServicio(context.Background(), "mirarc", "web"); err == nil {
		t.Fatal("restarted a service it does not have")
	}
	if err := l.ReiniciarServicio(context.Background(), "mirarc", "api"); err != nil {
		t.Fatal(err)
	}
	if last := f.calls[len(f.calls)-1]; last != "restart mirarc-api-71" {
		t.Fatalf("restarted %q", last)
	}
}
