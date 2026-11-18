package pipeline

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// A fix's pod: the clone, the uncommitted files laid over it, the command
// (in its image, in its folder, with nothing of the app's), and collect.
func TestFixPod(t *testing.T) {
	k := &KubeExecutor{Namespace: "rendimiento-builds"}
	k.defaults()
	job := FixJob{ID: "7", Image: "node:22-bookworm-slim", Command: "npm install --package-lock-only --ignore-scripts", Path: "web",
		Writes: []string{"package-lock.json"}, Timeout: 5 * time.Minute, Pending: map[string][]byte{"web/package.json": []byte("{}")}, MaxBytes: 1 << 20}
	pod, err := k.fixPod("fix-7-fix-abcde", nil, Source{Repo: "p0dxD/tienda", SHA: "abc"}, job, "/workspace/src/web", job.Timeout)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range pod.Spec.InitContainers {
		names = append(names, c.Name)
	}
	if strings.Join(names, " ") != "clone apply fix" || pod.Spec.Containers[0].Name != "collect" {
		t.Fatalf("containers %v %s", names, pod.Spec.Containers[0].Name)
	}
	fix := pod.Spec.InitContainers[2]
	if fix.Image != job.Image || fix.WorkingDir != "/workspace/src/web" || fix.Command[2] != job.Command || len(fix.EnvFrom) > 0 {
		t.Fatalf("fix container %+v", fix)
	}
	for _, e := range fix.Env {
		if e.ValueFrom != nil {
			t.Fatalf("the fix reads a secret: %+v", e)
		}
	}
	if *pod.Spec.AutomountServiceAccountToken {
		t.Fatal("the fix gets a service account token")
	}
	if items := pod.Spec.Volumes[len(pod.Spec.Volumes)-1].Secret.Items; len(items) != 2 {
		t.Fatalf("pending volume %+v", items)
	}
	if _, err := k.fixPod("x", nil, Source{}, FixJob{Path: "../.."}, "/", time.Minute); err == nil {
		t.Fatal("a path out of the workspace")
	}
}

// What collect writes is read back: the files it sent, the deleted, and
// the others listed; a cut output is an error.
func TestParseFixOutput(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "web/package-lock.json", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("{}"))
	_ = tw.Close()
	_ = gz.Close()
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	var wrapped []string
	for len(b64) > 76 {
		wrapped, b64 = append(wrapped, b64[:76]), b64[76:]
	}
	wrapped = append(wrapped, b64)
	raw := "RENDIMIENTO-FIX-BEGIN\nRENDIMIENTO-FIX-OTHER web/node_modules/x\nRENDIMIENTO-FIX-CHANGED web/package-lock.json\nRENDIMIENTO-FIX-DELETED web/old.lock\nRENDIMIENTO-FIX-TAR\n" +
		strings.Join(wrapped, "\n") + "\nRENDIMIENTO-FIX-TAREND\nRENDIMIENTO-FIX-END\n"
	out, err := ParseFixOutput([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Changes) != 2 || out.Changes[0].Path != "web/old.lock" || !out.Changes[0].Deleted || string(out.Changes[1].Data) != "{}" || out.Others[0] != "web/node_modules/x" {
		t.Fatalf("%+v", out)
	}
	if _, err := ParseFixOutput([]byte("RENDIMIENTO-FIX-BEGIN\nRENDIMIENTO-FIX-TAR\nabc")); err == nil {
		t.Fatal("a cut output was read")
	}
	if out, _ := ParseFixOutput([]byte("RENDIMIENTO-FIX-BEGIN\nRENDIMIENTO-FIX-TOOBIG 30000000\n")); !out.Failed {
		t.Fatal("too big")
	}
	if _, err := pendingTar(map[string][]byte{"../x": nil}); err == nil {
		t.Fatal("a pending file out of the repo")
	}
}
