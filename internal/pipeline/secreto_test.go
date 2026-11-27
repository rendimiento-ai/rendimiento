package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A kubelet that could not sync its secret cache yet retries by itself: the
// step waits for it a while, starts when it does, and fails only if it never
// does. A missing secret still fails at once.
func TestCacheDeSecretos(t *testing.T) {
	esperando := func(name, msg string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "b"}, Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{{Name: "clone", State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{Reason: "CreateContainerConfigError", Message: msg}}}}}}
	}
	sync := "failed to sync secret cache: timed out waiting for the condition"
	cs := fake.NewSimpleClientset(esperando("lento", sync), esperando("nunca", sync), esperando("sin-secreto", `secret "x" not found`))
	k := &KubeExecutor{Client: cs, Namespace: "b"}
	ctx := context.Background()
	defer func(g time.Duration) { cacheGrace = g }(cacheGrace)
	cacheGrace = 3 * time.Second

	go func() {
		time.Sleep(1500 * time.Millisecond)
		p, _ := cs.CoreV1().Pods("b").Get(ctx, "lento", metav1.GetOptions{})
		p.Status.InitContainerStatuses[0].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
		_, _ = cs.CoreV1().Pods("b").UpdateStatus(ctx, p, metav1.UpdateOptions{})
	}()
	if err := k.waitStarted(ctx, "lento", "clone"); err != nil {
		t.Fatalf("a cache that synced late: %v", err)
	}
	start := time.Now()
	if err := k.waitStarted(ctx, "nunca", "clone"); err == nil || !strings.Contains(err.Error(), "failed to sync") || time.Since(start) < 2*time.Second {
		t.Fatalf("a cache that never synced: %v after %s", err, time.Since(start))
	}
	start = time.Now()
	if err := k.waitStarted(ctx, "sin-secreto", "clone"); err == nil || time.Since(start) > time.Second {
		t.Fatalf("a missing secret: %v after %s", err, time.Since(start))
	}
}
