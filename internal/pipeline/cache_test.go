package pipeline

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A test that goes without its cache gets an empty folder at /cache, and
// keeps its other volumes.
func TestWithoutCache(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{
		{Name: "src", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "cache", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "cache-ai-platform"}}},
	}}}
	withoutCache(pod)
	if v := pod.Spec.Volumes[1]; v.PersistentVolumeClaim != nil || v.EmptyDir == nil {
		t.Fatalf("cache volume %+v", v)
	}
	if pod.Spec.Volumes[0].EmptyDir == nil {
		t.Fatal("the other volume changed")
	}
}

// A test waits for another run's test to let the cache go, up to a limit.
func TestWaitCacheFree(t *testing.T) {
	ctx := context.Background()
	step := Step{ID: "platform:test", App: "ai"}
	other := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "otra", Namespace: "b"},
		Spec: corev1.PodSpec{NodeName: "worker1", Volumes: []corev1.Volume{{Name: "cache", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: cacheName(step)}}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	cs := fake.NewSimpleClientset(other)
	k := &KubeExecutor{Client: cs, Namespace: "b"}
	if _, free := k.waitCacheFree(ctx, "esta", step, 1, 0); free {
		t.Fatal("free while another test has it")
	}
	// With a second cache, the test takes that one instead of waiting.
	if slot, free := k.waitCacheFree(ctx, "esta", step, 2, 0); !free || slot != 1 {
		t.Fatalf("second cache: slot %d, free %v", slot, free)
	}
	_ = cs.CoreV1().Pods("b").Delete(ctx, "otra", metav1.DeleteOptions{})
	if slot, free := k.waitCacheFree(ctx, "esta", step, 1, 0); !free || slot != 0 {
		t.Fatal("still in use once the other test is gone")
	}
}

// The first cache keeps the name it always had; the others add their number.
func TestCacheNameOfSlot(t *testing.T) {
	step := Step{ID: "platform:test", App: "ai"}
	if n := cacheName(step); n != "cache-ai-platform" {
		t.Fatalf("first: %s", n)
	}
	step.CacheSlot = 1
	if n := cacheName(step); n != "cache-ai-platform-2" {
		t.Fatalf("second: %s", n)
	}
	step.App = strings.Repeat("a", 80)
	if n := cacheName(step); len(n) > 63 || !strings.HasSuffix(n, "-2") {
		t.Fatalf("long: %s", n)
	}
}

// Without a number from the Environment page a test keeps DefaultCaches,
// and never more than MaxCaches.
func TestCachesCount(t *testing.T) {
	k := &KubeExecutor{}
	if k.caches() != DefaultCaches {
		t.Fatalf("default: %d", k.caches())
	}
	n := 0
	k.Caches = func() int { return n }
	if k.caches() != DefaultCaches {
		t.Fatalf("zero: %d", k.caches())
	}
	n = 1
	if k.caches() != 1 {
		t.Fatalf("one: %d", k.caches())
	}
	n = 99
	if k.caches() != MaxCaches {
		t.Fatalf("too many: %d", k.caches())
	}
}
