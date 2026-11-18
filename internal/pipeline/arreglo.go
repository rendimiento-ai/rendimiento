package pipeline

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FixJob is one declared fix to run (see internal/arreglos): the command,
// in an image, in a folder of the branch checked out at a commit, with the
// caller's uncommitted files on top.
type FixJob struct {
	// ID names the pod (fix-<id>), e.g. the fix run's number.
	ID      string
	Image   string
	Command string
	// Path is the folder the command runs in, relative to the repo root.
	Path string
	// Writes are the patterns (relative to Path) of the files it may write.
	// The pod sends back only changed files that may match; the caller
	// checks them again, exactly.
	Writes  []string
	Timeout time.Duration
	// Pending are the caller's uncommitted files (path → content) and
	// Removed the ones it deleted: they are laid over the commit first.
	Pending map[string][]byte
	Removed []string
	// MaxBytes is the most the files sent back may add up to (compressed).
	MaxBytes int
}

// FixChange is a file the command changed: its content, or that it is gone.
type FixChange struct {
	Path    string
	Data    []byte
	Deleted bool
}

// FixOutput is what a fix's pod sent back: every file it changed under
// the fix's folder (Changes holds the content of those that may match its
// patterns; Others the paths of the rest), or why it failed.
type FixOutput struct {
	Changes []FixChange
	Others  []string
	// Failed is true when the command did not succeed (Message says how).
	Failed  bool
	Message string
}

// maxPending is the most the uncommitted files may be, compressed: they
// travel in the pod's secret, which Kubernetes caps at 1 MiB.
const maxPending = 900 << 10

// fixApplyScript lays the caller's uncommitted files over the checkout and
// commits them locally (never pushed), so that afterwards git tells what
// the fix's command changed.
const fixApplyScript = `set -eu
cd /workspace/src
git config --global --add safe.directory '*'
if [ -f /pending/pending.tar.gz ]; then tar -xzf /pending/pending.tar.gz; fi
if [ -f /pending/removed ]; then
  while IFS= read -r f; do [ -n "$f" ] && rm -f -- "$f"; done < /pending/removed
fi
git add -A
if ! git diff --cached --quiet; then
  git -c user.name=rendimiento -c user.email=rendimiento@example.invalid commit -q -m "uncommitted changes"
  echo "laid the uncommitted changes over $(git rev-parse --short HEAD~1)"
fi
`

// fixCollectScript lists what the command changed under the fix's folder
// and sends back, as a compressed tar in base64 between markers, the files
// that may match its patterns (shell patterns, looser than the caller's).
const fixCollectScript = `set -eu
cd /workspace/src
git config --global --add safe.directory '*'
git -c core.quotepath=off status --porcelain=v1 --untracked-files=all -- "$FIXPATH" | head -n 2000 > /tmp/changed
: > /tmp/keep
echo "RENDIMIENTO-FIX-BEGIN"
while IFS= read -r line; do
  st=$(printf '%s' "$line" | cut -c1-2); f=$(printf '%s' "$line" | cut -c4-)
  case "$f" in *' -> '*) f=${f##*' -> '};; esac
  case "$f" in '"'*) continue;; esac
  rel=$f
  if [ "$FIXPATH" != "." ]; then rel=${f#"$FIXPATH"/}; fi
  hit=
  while IFS= read -r pat; do
    [ -n "$pat" ] || continue
    case "$rel" in $pat) hit=1;; esac
  done <<EOF
$WRITES
EOF
  if [ -z "$hit" ]; then echo "RENDIMIENTO-FIX-OTHER $f"; continue; fi
  if [ -e "$f" ]; then echo "$f" >> /tmp/keep; echo "RENDIMIENTO-FIX-CHANGED $f"
  else echo "RENDIMIENTO-FIX-DELETED $f"; fi
done < /tmp/changed
if [ -s /tmp/keep ]; then
  tar -czf /tmp/out.tar.gz -T /tmp/keep
  size=$(wc -c < /tmp/out.tar.gz)
  if [ "$size" -gt "$MAXBYTES" ]; then echo "RENDIMIENTO-FIX-TOOBIG $size"; else
    echo "RENDIMIENTO-FIX-TAR"; base64 /tmp/out.tar.gz; echo "RENDIMIENTO-FIX-TAREND"
  fi
fi
echo "RENDIMIENTO-FIX-END"
`

// Fix runs a declared fix as a pod in the build namespace: the clone of the
// commit, the uncommitted files laid over it, the command (in its image,
// without the app's secrets or environment: CI=true is all it gets), and
// then a container that sends back what it changed. The clone's and the
// command's logs go to w; the pod and its secret are removed afterwards.
func (k *KubeExecutor) Fix(ctx context.Context, src Source, job FixJob, w io.Writer) (FixOutput, error) {
	k.defaults()
	timeout := job.Timeout
	if timeout <= 0 || timeout > k.Timeout {
		timeout = k.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+2*time.Minute) // the clone and the image pull
	defer cancel()
	workdir := path.Join("/workspace/src", path.Clean(job.Path))
	if !strings.HasPrefix(workdir, "/workspace/src") {
		return FixOutput{}, fmt.Errorf("fix path %q escapes the workspace", job.Path)
	}
	pending, err := pendingTar(job.Pending)
	if err != nil {
		return FixOutput{}, err
	}
	if len(pending) > maxPending {
		return FixOutput{}, fmt.Errorf("the uncommitted changes are %d KB compressed, more than the %d KB a fix takes; commit them first", len(pending)>>10, maxPending>>10)
	}
	name := podName("fix-"+job.ID, "fix") + "-" + randSuffix()
	labels := map[string]string{"app.kubernetes.io/managed-by": "rendimiento", "rendimiento.ai/run": nonName.ReplaceAllString("fix-"+job.ID, "-")}
	token := ""
	if k.Token != nil {
		if token, err = k.Token(ctx, src.Repo); err != nil {
			return FixOutput{}, fmt.Errorf("could not get a clone token: %v", err)
		}
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: k.Namespace, Labels: labels},
		Data:       map[string][]byte{"token": []byte(token), "removed": []byte(strings.Join(job.Removed, "\n"))},
	}
	if len(pending) > 0 {
		secret.Data["pending.tar.gz"] = pending
	}
	if err := transientRetry(ctx, func() error {
		_, err := k.Client.CoreV1().Secrets(k.Namespace).Create(ctx, secret, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}); err != nil {
		return FixOutput{}, fmt.Errorf("create the fix's secret: %v", err)
	}
	defer func() {
		bg := context.Background()
		_ = k.Client.CoreV1().Pods(k.Namespace).Delete(bg, name, metav1.DeleteOptions{})
		_ = k.Client.CoreV1().Secrets(k.Namespace).Delete(bg, name, metav1.DeleteOptions{})
	}()
	pod, err := k.fixPod(name, labels, src, job, workdir, timeout)
	if err != nil {
		return FixOutput{}, err
	}
	if err := k.createPod(ctx, pod); err != nil {
		return FixOutput{}, fmt.Errorf("create pod: %v", err)
	}
	for _, c := range []string{"clone", "apply", "fix"} {
		if err := k.waitStarted(ctx, name, c); err != nil {
			return FixOutput{Failed: true, Message: fmt.Sprintf("%s: %v", c, err)}, nil
		}
		fmt.Fprintf(w, "::group::%s\n", c)
		if err := k.streamLogs(ctx, name, c, w); err != nil {
			fmt.Fprintf(w, "rendimiento: log stream interrupted: %v\n", err)
		}
		fmt.Fprintln(w, "::endgroup::")
	}
	var out bytes.Buffer
	if err := k.waitStarted(ctx, name, "collect"); err == nil {
		_ = k.streamLogs(ctx, name, "collect", &limited{w: &out, n: 3 * int64(job.MaxBytes)})
	}
	final, err := k.waitDone(ctx, name)
	if err != nil {
		if ctx.Err() != nil {
			return FixOutput{Failed: true, Message: fmt.Sprintf("the fix timed out after %s", timeout)}, nil
		}
		return FixOutput{}, err
	}
	if final.Status.Phase != corev1.PodSucceeded {
		if final.Status.Reason == "DeadlineExceeded" {
			return FixOutput{Failed: true, Message: fmt.Sprintf("the fix timed out after %s", timeout)}, nil
		}
		return FixOutput{Failed: true, Message: "the fix failed: " + terminationSummary(final)}, nil
	}
	return ParseFixOutput(out.Bytes())
}

// fixPod is the pod a fix runs in: clone, apply and the command as init
// containers, one after another, and collect last.
func (k *KubeExecutor) fixPod(name string, labels map[string]string, src Source, job FixJob, workdir string, timeout time.Duration) (*corev1.Pod, error) {
	base, err := k.pod(name, labels, src, Step{ID: "fix", Kind: KindTest, Path: job.Path, Image: job.Image, Command: job.Command}, "")
	if err != nil {
		return nil, err
	}
	clone := base.Spec.InitContainers[0]
	ws := corev1.VolumeMount{Name: "workspace", MountPath: "/workspace"}
	pendingMount := corev1.VolumeMount{Name: "pending", MountPath: "/pending", ReadOnly: true}
	// Small's requests, so it fits where a step would; a higher memory
	// limit than small's, since npm resolving a big tree needs more.
	res := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("1Gi")},
	}
	k.capCPU(&res)
	fixPath := path.Clean(job.Path)
	deadline := int64((timeout + 2*time.Minute).Seconds())
	base.Spec.ActiveDeadlineSeconds = &deadline
	base.Spec.InitContainers = []corev1.Container{
		clone,
		{Name: "apply", Image: k.GitImage, Command: []string{"sh", "-c", fixApplyScript},
			VolumeMounts: []corev1.VolumeMount{ws, pendingMount}},
		{Name: "fix", Image: job.Image, Command: []string{"sh", "-c", job.Command},
			WorkingDir: workdir, Resources: res, VolumeMounts: []corev1.VolumeMount{ws},
			Env: []corev1.EnvVar{{Name: "CI", Value: "true"},
				{Name: "COREPACK_ENABLE_DOWNLOAD_PROMPT", Value: "0"}, {Name: "npm_config_update_notifier", Value: "false"}}},
	}
	base.Spec.Containers = []corev1.Container{{
		Name: "collect", Image: k.GitImage, Command: []string{"sh", "-c", fixCollectScript},
		Env: []corev1.EnvVar{{Name: "FIXPATH", Value: fixPath}, {Name: "WRITES", Value: strings.Join(job.Writes, "\n")},
			{Name: "MAXBYTES", Value: fmt.Sprint(job.MaxBytes)}},
		VolumeMounts: []corev1.VolumeMount{ws},
	}}
	base.Spec.Volumes = append(base.Spec.Volumes, corev1.Volume{Name: "pending", VolumeSource: corev1.VolumeSource{
		Secret: &corev1.SecretVolumeSource{SecretName: name, Items: []corev1.KeyToPath{{Key: "removed", Path: "removed"}}}}})
	if len(job.Pending) > 0 {
		v := &base.Spec.Volumes[len(base.Spec.Volumes)-1]
		v.Secret.Items = append(v.Secret.Items, corev1.KeyToPath{Key: "pending.tar.gz", Path: "pending.tar.gz"})
	}
	return base, nil
}

// pendingTar packs the uncommitted files as a compressed tar.
func pendingTar(files map[string][]byte) ([]byte, error) {
	if len(files) == 0 {
		return nil, nil
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		clean := path.Clean(p)
		if path.IsAbs(clean) || strings.HasPrefix(clean, "..") || clean == ".git" || strings.HasPrefix(clean, ".git/") {
			return nil, fmt.Errorf("not a file of the repository: %s", p)
		}
		if err := tw.WriteHeader(&tar.Header{Name: clean, Mode: 0o644, Size: int64(len(files[p])), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(files[p]); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParseFixOutput reads what the collect container wrote.
func ParseFixOutput(raw []byte) (FixOutput, error) {
	var out FixOutput
	var tarB64 strings.Builder
	inTar, begun, ended := false, false, false
	deleted := map[string]bool{}
	changed := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case inTar && line == "RENDIMIENTO-FIX-TAREND":
			inTar = false
		case inTar:
			tarB64.WriteString(line)
		case line == "RENDIMIENTO-FIX-BEGIN":
			begun = true
		case line == "RENDIMIENTO-FIX-END":
			ended = true
		case line == "RENDIMIENTO-FIX-TAR":
			inTar = true
		case strings.HasPrefix(line, "RENDIMIENTO-FIX-TOOBIG "):
			return FixOutput{Failed: true, Message: fmt.Sprintf("the files it wrote are %s bytes compressed, more than a fix may send back", strings.TrimPrefix(line, "RENDIMIENTO-FIX-TOOBIG "))}, nil
		case strings.HasPrefix(line, "RENDIMIENTO-FIX-OTHER "):
			out.Others = append(out.Others, strings.TrimPrefix(line, "RENDIMIENTO-FIX-OTHER "))
		case strings.HasPrefix(line, "RENDIMIENTO-FIX-CHANGED "):
			changed[strings.TrimPrefix(line, "RENDIMIENTO-FIX-CHANGED ")] = true
		case strings.HasPrefix(line, "RENDIMIENTO-FIX-DELETED "):
			deleted[strings.TrimPrefix(line, "RENDIMIENTO-FIX-DELETED ")] = true
		}
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	if !begun || !ended {
		return out, errors.New("the fix's pod did not say what it changed (its output was cut)")
	}
	if tarB64.Len() > 0 {
		data, err := base64.StdEncoding.DecodeString(tarB64.String())
		if err != nil {
			return out, fmt.Errorf("the files the fix sent back: %v", err)
		}
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return out, fmt.Errorf("the files the fix sent back: %v", err)
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return out, fmt.Errorf("the files the fix sent back: %v", err)
			}
			if h.Typeflag != tar.TypeReg || !changed[h.Name] {
				continue
			}
			b, err := io.ReadAll(tr)
			if err != nil {
				return out, err
			}
			out.Changes = append(out.Changes, FixChange{Path: h.Name, Data: b})
		}
	}
	for p := range deleted {
		out.Changes = append(out.Changes, FixChange{Path: p, Deleted: true})
	}
	sort.Slice(out.Changes, func(i, j int) bool { return out.Changes[i].Path < out.Changes[j].Path })
	return out, nil
}

// limited writes at most n bytes to w, dropping the rest.
type limited struct {
	w io.Writer
	n int64
}

func (l *limited) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	q := p
	if int64(len(q)) > l.n {
		q = q[:l.n]
	}
	l.n -= int64(len(q))
	if _, err := l.w.Write(q); err != nil {
		return 0, err
	}
	return len(p), nil
}
