package platform

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kfake "k8s.io/client-go/kubernetes/fake"

	"github.com/rendimiento-ai/rendimiento/internal/destino"
	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
	"github.com/rendimiento-ai/rendimiento/internal/tachar"
	"github.com/rendimiento-ai/rendimiento/internal/uptime"
)

// A release whose rollout times out is rolled back, and what its pods
// showed is kept first (Ruta 184): the new ReplicaSet's pod in its crash
// loop, with its last lines, and the events about it, with the line that
// looks like a secret left out. A release that passed keeps nothing.
func TestFailedReleaseKeepsEvidence(t *testing.T) {
	p, fake, _, _ := setup(t)
	ctx := context.Background()
	const ns = "evidencia" // the app's own name: the envtest is shared (see kubeConfig)
	var failing atomic.Bool
	p.Verify = VerifySettings{
		Window: 200 * time.Millisecond, Every: 20 * time.Millisecond,
		Rollout: func(context.Context, string, int64) (RolloutState, string, error) {
			if failing.Load() {
				return RolloutFailed, `web: ReplicaSet "web-7c9d" has timed out progressing`, nil
			}
			return RolloutHealthy, "", nil
		},
		Check: func(_ context.Context, tg uptime.Target) store.Probe {
			return store.Probe{AppID: tg.AppID, Service: tg.Service, Kind: tg.Kind, OK: true, At: time.Now()}
		},
	}
	labels := map[string]string{render.LabelApp: ns, render.LabelService: "web", appsv1.DefaultDeploymentUniqueLabelKey: "7c9d"}
	p.Clientset = kfake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: ns, Annotations: map[string]string{"deployment.kubernetes.io/revision": "2"}},
			Status: appsv1.DeploymentStatus{Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse,
				Reason: "ProgressDeadlineExceeded", Message: `ReplicaSet "web-7c9d" has timed out progressing.`}}}},
		&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "web-7c9d", Namespace: ns, Labels: labels,
			Annotations: map[string]string{"deployment.kubernetes.io/revision": "2"}, OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "web"}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-7c9d-abcde", Namespace: ns, Labels: labels},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "web", RestartCount: 4,
				State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"}}}}}},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e1", Namespace: ns}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web-7c9d-abcde"},
			Type: corev1.EventTypeWarning, Reason: "BackOff", Message: "Back-off restarting failed container web", LastTimestamp: metav1.Now()},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e2", Namespace: ns}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web-7c9d-abcde"},
			Type: corev1.EventTypeWarning, Reason: "Failed", Message: "pulling with password=Xk29fjQ02mzP77aLq", LastTimestamp: metav1.Now()},
	)

	yaml := "services:\n  - name: web\n    domain: evidencia.example.com\n    health: { path: /health }\n"
	sha := func(c byte) string { return strings.Repeat(string(c), 40) }
	tree := fstest.MapFS{"rendimiento.yaml": {Data: []byte(yaml)}}
	for _, c := range "ab" {
		fake.files[sha(byte(c))] = tree
	}
	fake.files["main"] = tree
	sp, _ := spec.Parse([]byte(yaml))
	if _, err := p.Onboard(ctx, OnboardRequest{Installation: 7, Repo: "p0dxD/evidencia", DefaultBranch: "main", Name: ns, Spec: *sp}, false); err != nil {
		t.Fatal(err)
	}
	app, _ := p.Store.GetApp(ctx, ns)
	push := func(c byte) {
		t.Helper()
		runs, err := p.HandlePush(ctx, PushEvent{Installation: 7, Repo: "p0dxD/evidencia", Branch: "main", SHA: sha(c)})
		if err != nil || len(runs) != 1 {
			t.Fatalf("push: %v %v", runs, err)
		}
		waitRun(t, p, runs[0].ID)
	}
	verified := func(number int64) *store.Release {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			r, err := p.Store.GetRelease(ctx, app.ID, number)
			if err == nil && r.VerifyStatus != "" && r.VerifyStatus != store.VerifyRunning {
				return r
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("release %d was not verified", number)
		return nil
	}

	push('a')
	if r := verified(1); r.VerifyStatus != store.VerifyPassed {
		t.Fatalf("release 1: %s %q", r.VerifyStatus, r.VerifyMessage)
	}
	if _, err := p.Store.ReleaseEvidence(ctx, app.ID, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a release that passed kept evidence: %v", err)
	}
	failing.Store(true)
	push('b')
	if r := verified(2); r.VerifyStatus != store.VerifyFailed || !strings.Contains(r.VerifyMessage, "rolled back to release #1") {
		t.Fatalf("release 2: %s %q", r.VerifyStatus, r.VerifyMessage)
	}
	ev, err := p.Store.ReleaseEvidence(ctx, app.ID, 2)
	if err != nil {
		t.Fatalf("no evidence of the failed release: %v", err)
	}
	if len(ev.Services) != 1 || ev.Services[0].ReplicaSet != "web-7c9d" || !strings.Contains(ev.Services[0].Message, "has timed out progressing") || len(ev.Services[0].Pods) != 1 {
		t.Fatalf("evidence: %+v", ev)
	}
	c := ev.Services[0].Pods[0].Containers[0]
	if c.Reason != "CrashLoopBackOff" || c.Restarts != 4 || c.LastExitCode == nil || *c.LastExitCode != 1 || c.PreviousLog == "" {
		t.Fatalf("container: %+v", c)
	}
	var msgs []string
	for _, e := range ev.Events {
		msgs = append(msgs, e.Message)
	}
	if len(msgs) != 2 || !strings.Contains(strings.Join(msgs, "|"), "Back-off restarting") || !strings.Contains(strings.Join(msgs, "|"), tachar.Omitida) ||
		strings.Contains(strings.Join(msgs, "|"), "Xk29fjQ02mzP77aLq") {
		t.Fatalf("events: %q", msgs)
	}
	// The rollback (#3) keeps nothing of its own.
	if _, err := p.Store.ReleaseEvidence(ctx, app.ID, 3); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the rollback kept evidence: %v", err)
	}

	// A destination that cannot see its pods says so, and nothing is kept.
	p.Destino = destino.Ninguno{}
	if _, err := p.Evidence(ctx, app, &store.Release{Spec: *sp}); !errors.Is(err, ErrSinEvidencia) {
		t.Fatalf("without a witness: %v", err)
	}
}
