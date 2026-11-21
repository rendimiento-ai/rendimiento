package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// pushTestScript builds the smallest image there is (FROM scratch, one
// file with the test's nonce) on a BuildKit daemon and pushes it, as a
// build pushes an app's image; the digest goes to the termination log.
const pushTestScript = `set -eu
mkdir -p /tmp/c
printf '%s\n' "$NONCE" > /tmp/c/prueba
printf 'FROM scratch\nCOPY prueba /prueba\n' > /tmp/c/Dockerfile
buildctl --addr "$BUILDKIT_ADDR" build --frontend dockerfile.v0 --local context=/tmp/c --local dockerfile=/tmp/c \
  --output "type=image,name=$IMAGE,push=true$INSECURE" --metadata-file /tmp/metadata.json
digest=$(grep -o '"containerimage.digest": *"sha256:[a-f0-9]*"' /tmp/metadata.json | grep -o 'sha256:[a-f0-9]*')
printf '%s' "$digest" > /dev/termination-log
`

// PushTestImage has a BuildKit daemon build a tiny image and push it to
// image (registry/name:tag), as builds do: the test of a new image
// registry. It returns the image's digest, or why the push failed (the
// pod's last lines). The pod is deleted afterwards.
func (k *KubeExecutor) PushTestImage(ctx context.Context, image, nonce string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	addr, _ := k.buildkitFor(ctx, "rendimiento-prueba")
	insecure := ""
	if k.InsecureRegistry {
		insecure = ",registry.insecure=true"
	}
	name := "prueba-registro-" + randSuffix()
	deadline := int64(240)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: k.Namespace, Labels: map[string]string{"rendimiento.ai/prueba": "registro"}},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			Affinity:                     k.affinity(),
			ActiveDeadlineSeconds:        &deadline,
			AutomountServiceAccountToken: ptr(false),
			Containers: []corev1.Container{{
				Name:    "step",
				Image:   buildImageOr(k.BuildImage),
				Command: []string{"sh", "-c", pushTestScript},
				Env: []corev1.EnvVar{
					{Name: "BUILDKIT_ADDR", Value: addr},
					{Name: "IMAGE", Value: image},
					{Name: "NONCE", Value: nonce},
					{Name: "INSECURE", Value: insecure},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
				},
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			}},
		},
	}
	if err := k.createPod(ctx, pod); err != nil {
		return "", err
	}
	defer func() {
		del, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_ = k.Client.CoreV1().Pods(k.Namespace).Delete(del, name, metav1.DeleteOptions{})
	}()
	p, err := k.waitDone(ctx, name)
	if err != nil {
		return "", errors.New("the push did not finish within 5 minutes")
	}
	msg := ""
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Terminated != nil {
			msg = strings.TrimSpace(cs.State.Terminated.Message)
		}
	}
	if p.Status.Phase != corev1.PodSucceeded || !strings.HasPrefix(msg, "sha256:") {
		if msg == "" {
			msg = terminationSummary(p)
		}
		lines := strings.Split(msg, "\n")
		if len(lines) > 3 {
			lines = lines[len(lines)-3:]
		}
		return "", fmt.Errorf("%s", strings.Join(lines, " · "))
	}
	return msg, nil
}

func buildImageOr(img string) string {
	if img == "" {
		return "moby/buildkit:v0.18.2"
	}
	return img
}
