package pipeline

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// logFalso is a container's log whose first follow stream goes mute after
// what was written so far, the way a node's did in run 1955.
type logFalso struct {
	mu       sync.Mutex
	texto    string
	abiertos int
}

func (l *logFalso) escribir(s string) { l.mu.Lock(); l.texto += s; l.mu.Unlock() }

func (l *logFalso) fuente(ctx context.Context, _, _ string, follow bool) (io.ReadCloser, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !follow {
		return io.NopCloser(strings.NewReader(l.texto)), nil
	}
	l.abiertos++
	pr, pw := io.Pipe()
	ya := l.texto
	mudo := l.abiertos == 1
	go func() {
		pw.Write([]byte(ya))
		n := len(ya)
		for {
			select {
			case <-ctx.Done():
				pw.CloseWithError(ctx.Err())
				return
			case <-time.After(20 * time.Millisecond):
			}
			if mudo {
				continue
			}
			l.mu.Lock()
			nuevo := l.texto[n:]
			l.mu.Unlock()
			if nuevo != "" {
				pw.Write([]byte(nuevo))
				n += len(nuevo)
			}
		}
	}()
	return pr, nil
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// A stream that goes mute while the step goes on is noticed: what it missed
// is written, it is followed again from there, and nothing comes twice.
func TestRegistroMudo(t *testing.T) {
	defer func(p, g, s time.Duration) { streamPoll, streamGrace, streamStall = p, g, s }(streamPoll, streamGrace, streamStall)
	streamPoll, streamGrace, streamStall = 50*time.Millisecond, 200*time.Millisecond, 300*time.Millisecond

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "b"}, Status: corev1.PodStatus{
		ContainerStatuses: []corev1.ContainerStatus{{Name: "step", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	cs := fake.NewSimpleClientset(pod)
	l := &logFalso{texto: "== build the UI\n"}
	k := &KubeExecutor{Client: cs, Namespace: "b", logFuente: l.fuente}
	ctx := context.Background()
	var out syncBuf
	fin := make(chan error, 1)
	go func() { fin <- k.streamLogs(ctx, "p", "step", &out) }()

	time.Sleep(100 * time.Millisecond)
	l.escribir("== browser tests\n✓ 1\n")
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "✓ 1") {
		if time.Now().After(deadline) {
			t.Fatalf("the mute stream was not noticed: %q", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	l.escribir("✓ 2\n")
	for !strings.Contains(out.String(), "✓ 2") {
		if time.Now().After(deadline) {
			t.Fatalf("the stream was not followed again: %q", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	l.escribir("✓ 3\n")
	time.Sleep(100 * time.Millisecond)
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{}}
	if _, err := cs.CoreV1().Pods("b").UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := <-fin; err != nil {
		t.Fatal(err)
	}
	if want := "== build the UI\n== browser tests\n✓ 1\n✓ 2\n✓ 3\n"; !strings.HasPrefix(out.String(), want) || strings.Count(out.String(), "✓ 1") != 1 {
		t.Fatalf("the log: %q", out.String())
	}
}
