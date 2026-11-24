package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"
)

func sandboxes(t *testing.T) (*Sandboxes, *fake.Clientset) {
	t.Helper()
	cs := fake.NewSimpleClientset()
	// The fake API server starts every pod at once: the clone done, the
	// sandbox's two containers running.
	cs.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		p := a.(k8stesting.CreateAction).GetObject().(*corev1.Pod)
		p.Status.Phase = corev1.PodRunning
		p.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "clone", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{}}}}
		p.Status.ContainerStatuses = []corev1.ContainerStatus{
			{Name: "work", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			{Name: "git", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
		}
		return false, nil, nil
	})
	k := &KubeExecutor{Client: cs, Namespace: "rendimiento-builds", ExcludeNodes: []string{"apagado"},
		Token: func(context.Context, string) (string, error) { return "ghs_example", nil }}
	return &Sandboxes{K: k}, cs
}

// A sandbox's pod is shut tight: no token, no secrets or platform
// environment, not root, nothing privileged, no capabilities, seccomp, a
// workspace with a size limit and nothing of the node, never on the
// control plane, and only the clone sees the clone token.
func TestSandboxPod(t *testing.T) {
	s, _ := sandboxes(t)
	s.RuntimeClass = "gvisor"
	s.defaults()
	pod, err := s.Pod("sbx-encargo-7-abcde", map[string]string{SandboxLabel: "encargo-7"}, Source{Repo: "o/tienda", SHA: "abc123"}, "node:22-bookworm-slim")
	if err != nil {
		t.Fatal(err)
	}
	sp := pod.Spec
	if pod.Namespace != "rendimiento-builds" || pod.Labels[SandboxLabel] != "encargo-7" || pod.Labels["rendimiento.ai/run"] != "" {
		t.Fatalf("meta %+v", pod.ObjectMeta)
	}
	if sp.AutomountServiceAccountToken == nil || *sp.AutomountServiceAccountToken || sp.EnableServiceLinks == nil || *sp.EnableServiceLinks {
		t.Fatal("a service account token or the services' environment")
	}
	if sp.HostNetwork || sp.HostPID || sp.HostIPC || sp.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("host namespaces or restarts: %+v", sp)
	}
	if sp.RuntimeClassName == nil || *sp.RuntimeClassName != "gvisor" {
		t.Fatal("runtime class")
	}
	if sp.ActiveDeadlineSeconds == nil || *sp.ActiveDeadlineSeconds != int64((4 * time.Hour).Seconds()) {
		t.Fatal("no deadline")
	}
	psc := sp.SecurityContext
	if psc == nil || psc.RunAsNonRoot == nil || !*psc.RunAsNonRoot || psc.RunAsUser == nil || *psc.RunAsUser == 0 ||
		psc.SeccompProfile == nil || psc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("pod security %+v", psc)
	}
	if len(sp.Volumes) != 1 || sp.Volumes[0].EmptyDir == nil || sp.Volumes[0].EmptyDir.SizeLimit == nil || sp.Volumes[0].EmptyDir.SizeLimit.String() != "8Gi" {
		t.Fatalf("volumes %+v: only a sized emptyDir", sp.Volumes)
	}
	all := append(append([]corev1.Container{}, sp.InitContainers...), sp.Containers...)
	if len(all) != 3 || sp.InitContainers[0].Name != "clone" || sp.Containers[0].Name != "work" || sp.Containers[1].Name != "git" {
		t.Fatalf("containers %d", len(all))
	}
	for _, c := range all {
		sc := c.SecurityContext
		if sc == nil || sc.Privileged == nil || *sc.Privileged || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation ||
			sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot || sc.Capabilities == nil || len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" ||
			len(sc.Capabilities.Add) != 0 || sc.SeccompProfile == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
			t.Fatalf("%s's security %+v", c.Name, sc)
		}
		if len(c.EnvFrom) > 0 {
			t.Fatalf("%s loads env from %+v", c.Name, c.EnvFrom)
		}
		for _, e := range c.Env {
			if e.ValueFrom != nil && (c.Name != "clone" || e.Name != "GIT_TOKEN") {
				t.Fatalf("%s reads %s from elsewhere", c.Name, e.Name)
			}
		}
		for _, m := range c.VolumeMounts {
			if m.Name != "workspace" {
				t.Fatalf("%s mounts %s", c.Name, m.Name)
			}
		}
	}
	if !strings.Contains(sp.InitContainers[0].Command[2], "--unset-all http.extraHeader") {
		t.Fatal("the clone leaves the token in .git/config")
	}
	work := sp.Containers[0]
	if work.Image != "node:22-bookworm-slim" || work.Resources.Limits.Cpu().String() != "2" || work.Resources.Limits.Memory().String() != "4Gi" {
		t.Fatalf("work %s %+v", work.Image, work.Resources.Limits)
	}
	na := sp.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	var keys []string
	for _, term := range na.NodeSelectorTerms {
		for _, r := range term.MatchExpressions {
			keys = append(keys, r.Key+" "+string(r.Operator)+" "+strings.Join(r.Values, ","))
		}
	}
	got := strings.Join(keys, "; ")
	if !strings.Contains(got, "node-role.kubernetes.io/control-plane DoesNotExist") || !strings.Contains(got, "node-role.kubernetes.io/master DoesNotExist") ||
		!strings.Contains(got, "kubernetes.io/hostname NotIn apagado") {
		t.Fatalf("affinity %s", got)
	}
	s.RuntimeClass = ""
	if pod, _ := s.Pod("x", nil, Source{Repo: "o/tienda", SHA: "abc"}, "img"); pod.Spec.RuntimeClassName != nil {
		t.Fatal("a runtime class nobody set")
	}
}

// isolate-sandbox lets a sandbox reach DNS and the public internet only,
// is the same in deploy/networkpolicy.yaml and in the code that puts it
// back, and isolate-builds leaves sandboxes to it.
func TestSandboxNetworkPolicy(t *testing.T) {
	want := SandboxNetworkPolicy("rendimiento-builds")
	sp := want.Spec
	if len(sp.PodSelector.MatchExpressions) != 1 || sp.PodSelector.MatchExpressions[0].Key != SandboxLabel || sp.PodSelector.MatchExpressions[0].Operator != metav1.LabelSelectorOpExists {
		t.Fatalf("selector %+v", sp.PodSelector)
	}
	if len(sp.PolicyTypes) != 2 || len(sp.Ingress) != 0 || len(sp.Egress) != 2 {
		t.Fatalf("policy %+v", sp)
	}
	dns := sp.Egress[0]
	if dns.To[0].PodSelector.MatchLabels["k8s-app"] != "kube-dns" || len(dns.Ports) != 2 || dns.Ports[0].Port.IntValue() != 53 {
		t.Fatalf("dns %+v", dns)
	}
	web := sp.Egress[1]
	if len(web.Ports) != 0 || len(web.To) != 1 || web.To[0].IPBlock == nil || web.To[0].IPBlock.CIDR != "0.0.0.0/0" {
		t.Fatalf("internet %+v", web)
	}
	if got := strings.Join(web.To[0].IPBlock.Except, " "); got != "10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 169.254.0.0/16 100.64.0.0/10" {
		t.Fatalf("except %s", got)
	}
	raw, err := os.ReadFile("../../deploy/networkpolicy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]networkingv1.NetworkPolicy{}
	for _, doc := range strings.Split(string(raw), "\n---\n") {
		var np networkingv1.NetworkPolicy
		if err := yaml.Unmarshal([]byte(doc), &np); err != nil {
			t.Fatal(err)
		}
		found[np.Name] = np
	}
	if np, ok := found[SandboxPolicy]; !ok || !equality.Semantic.DeepEqual(np.Spec, sp) || np.Namespace != "rendimiento-builds" {
		t.Fatalf("deploy/networkpolicy.yaml's %s is not the code's: %+v", SandboxPolicy, np.Spec)
	}
	builds := found["isolate-builds"].Spec.PodSelector.MatchExpressions
	if len(builds) != 1 || builds[0].Key != SandboxLabel || builds[0].Operator != metav1.LabelSelectorOpDoesNotExist {
		t.Fatalf("isolate-builds also takes sandboxes: %+v", builds)
	}
	chart, err := os.ReadFile("../../chart/rendimiento/templates/builds.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chart), "name: "+SandboxPolicy) || !strings.Contains(string(chart), "pods/exec") {
		t.Fatal("the chart has no sandbox policy or exec")
	}

	// The code makes it when missing and puts it back when changed.
	s, cs := sandboxes(t)
	s.defaults()
	ctx := context.Background()
	if err := s.EnsurePolicy(ctx); err != nil {
		t.Fatal(err)
	}
	np, err := cs.NetworkingV1().NetworkPolicies("rendimiento-builds").Get(ctx, SandboxPolicy, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{})
	if _, err := cs.NetworkingV1().NetworkPolicies("rendimiento-builds").Update(ctx, np, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsurePolicy(ctx); err != nil {
		t.Fatal(err)
	}
	if np, _ = cs.NetworkingV1().NetworkPolicies("rendimiento-builds").Get(ctx, SandboxPolicy, metav1.GetOptions{}); len(np.Spec.Egress) != 2 {
		t.Fatalf("not put back: %d egress rules", len(np.Spec.Egress))
	}
}

// shellExec runs the sandbox's scripts here, with /workspace in dir: the
// git container's and the work container's are the same machine.
func shellExec(dir string, calls *[]string) ExecFunc {
	return func(ctx context.Context, _, pod, container string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
		*calls = append(*calls, pod+"/"+container)
		args := make([]string, len(cmd))
		for i, a := range cmd {
			switch {
			case a == "/workspace":
				a = dir
			case strings.HasPrefix(a, "/workspace/"):
				a = dir + strings.TrimPrefix(a, "/workspace")
			}
			args[i] = a
		}
		c := exec.CommandContext(ctx, args[0], args[1:]...)
		c.Stdin, c.Stdout, c.Stderr = stdin, stdout, stderr
		c.WaitDelay = time.Second
		err := c.Run()
		var exit *exec.ExitError
		switch {
		case err == nil:
			return 0, nil
		case ctx.Err() != nil:
			return -1, ctx.Err()
		case errors.As(err, &exit):
			if exit.ExitCode() < 0 { // killed by a signal
				return 137, nil
			}
			return exit.ExitCode(), nil
		}
		return -1, err
	}
}

// checkout is what the sandbox's clone leaves: the repository at a commit,
// marked as the first sync point.
func checkout(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git here")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	for p, c := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(src, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x"},
		{"update-ref", "refs/rendimiento/sync", "HEAD"}} {
		if out, err := exec.Command("git", append([]string{"-C", src}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

func changes(res SandboxResult) map[string]string {
	out := map[string]string{}
	for _, c := range res.Changes {
		if c.Deleted {
			out[c.Path] = "(deleted)"
		} else {
			out[c.Path] = string(c.Data)
		}
	}
	return out
}

// The caller's changes go in, the command sees them, and what it changed
// comes back (not ignored files); the sandbox is the same for the next
// command, which finds what the first installed, and a command that
// commits still sends back what it changed.
func TestSandboxRoundTrip(t *testing.T) {
	dir := checkout(t, map[string]string{".gitignore": "node_modules/\n", "a.txt": "A\n", "b.txt": "B\n", "web/hola.txt": "hola\n"})
	s, cs := sandboxes(t)
	var calls []string
	s.Exec = shellExec(dir, &calls)
	ctx := context.Background()
	res, err := s.Run(ctx, SandboxRun{Key: "encargo-7", Src: Source{Repo: "o/tienda", SHA: "abc"}, Image: "node:22", ImageFrom: "the test of service web",
		Command: "cat a.txt; test -e b.txt || echo sin-b; echo nuevo > c.txt; echo cambiado > a.txt; mkdir -p node_modules; echo x > node_modules/dep",
		Pending: map[string][]byte{"a.txt": []byte("pendiente\n")}, Removed: []string{"b.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "pendiente") || !strings.Contains(res.Stdout, "sin-b") || !res.Fresh || res.Image != "node:22" {
		t.Fatalf("first run %+v", res)
	}
	if got := changes(res); len(got) != 2 || got["a.txt"] != "cambiado\n" || got["c.txt"] != "nuevo\n" {
		t.Fatalf("came back %v (b.txt was the caller's own deletion; node_modules is ignored)", got)
	}
	pods, _ := cs.CoreV1().Pods("rendimiento-builds").List(ctx, metav1.ListOptions{})
	if len(pods.Items) != 1 {
		t.Fatalf("%d pods", len(pods.Items))
	}
	pod := pods.Items[0].Name
	if secrets, _ := cs.CoreV1().Secrets("rendimiento-builds").List(ctx, metav1.ListOptions{}); len(secrets.Items) != 0 {
		t.Fatal("the clone token's secret stayed after the clone")
	}
	if _, err := cs.NetworkingV1().NetworkPolicies("rendimiento-builds").Get(ctx, SandboxPolicy, metav1.GetOptions{}); err != nil {
		t.Fatalf("a sandbox without its network policy: %v", err)
	}

	// The caller took them in; the next command runs in the same sandbox,
	// finds node_modules, commits in its own git, and deletes c.txt.
	res, err = s.Run(ctx, SandboxRun{Key: "encargo-7", Src: Source{Repo: "o/tienda", SHA: "abc"}, Image: "node:22", Folder: "web",
		Command: "cat ../node_modules/dep; cat ../a.txt; echo adios > hola.txt; rm ../c.txt; git -c user.name=m -c user.email=m@example.com commit -qam suyo; exit 3",
		Pending: map[string][]byte{"a.txt": []byte("cambiado\n"), "c.txt": []byte("nuevo\n")}, Removed: []string{"b.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || res.Fresh || !strings.Contains(res.Stdout, "x\ncambiado") {
		t.Fatalf("second run %+v", res)
	}
	if got := changes(res); len(got) != 2 || got["web/hola.txt"] != "adios\n" || got["c.txt"] != "(deleted)" {
		t.Fatalf("came back %v", got)
	}
	if pods, _ = cs.CoreV1().Pods("rendimiento-builds").List(ctx, metav1.ListOptions{}); len(pods.Items) != 1 || pods.Items[0].Name != pod {
		t.Fatal("the second command got another sandbox")
	}

	// What a command changed and the caller did not take is set back by
	// the next run: the files are always the caller's.
	res, err = s.Run(ctx, SandboxRun{Key: "encargo-7", Src: Source{Repo: "o/tienda", SHA: "abc"}, Image: "node:22",
		Command: "cat web/hola.txt; test -e c.txt && echo c-sigue; true", Pending: map[string][]byte{"a.txt": []byte("cambiado\n"), "c.txt": []byte("nuevo\n")}, Removed: []string{"b.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "hola\nc-sigue\n" || len(res.Changes) != 0 {
		t.Fatalf("third run %q %v", res.Stdout, changes(res))
	}
	if _, err := s.Run(ctx, SandboxRun{Key: "encargo-7", Src: Source{Repo: "o/tienda", SHA: "abc"}, Image: "node:22", Folder: "no/existe", Command: "true"}); err == nil ||
		!strings.Contains(err.Error(), "no folder") {
		t.Fatalf("a folder that is not there: %v", err)
	}
	if _, err := s.Run(ctx, SandboxRun{Key: "encargo-7", Image: "node:22", Command: "true", Pending: map[string][]byte{"../fuera": nil}}); err == nil {
		t.Fatal("a pending file out of the repository")
	}
	for _, c := range calls {
		if !strings.HasSuffix(c, "/git") && !strings.HasSuffix(c, "/work") {
			t.Fatalf("exec into %s", c)
		}
	}

	// The job ended: the sandbox goes.
	s.Close("encargo-7")
	if pods, _ = cs.CoreV1().Pods("rendimiento-builds").List(ctx, metav1.ListOptions{}); len(pods.Items) != 0 {
		t.Fatal("the sandbox stayed after its job")
	}
}

// Only the end of what a command prints comes back, with how much it
// printed; a command past its time is stopped, and its sandbox removed.
func TestSandboxOutputCapAndTimeout(t *testing.T) {
	dir := checkout(t, map[string]string{"a.txt": "A\n"})
	s, cs := sandboxes(t)
	var calls []string
	s.Exec = shellExec(dir, &calls)
	s.MaxOutput = 100
	s.Timeout = 2 * time.Second
	ctx := context.Background()
	res, err := s.Run(ctx, SandboxRun{Key: "encargo-8", Src: Source{Repo: "o/tienda", SHA: "abc"}, Image: "img",
		Command: "i=0; while [ $i -lt 500 ]; do echo linea-$i; i=$((i+1)); done; echo fin >&2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stdout) != 100 || !strings.HasSuffix(res.Stdout, "linea-499\n") || res.StdoutBytes < 4000 || res.Stderr != "fin\n" {
		t.Fatalf("output %q (%d bytes)", res.Stdout, res.StdoutBytes)
	}
	res, err = s.Run(ctx, SandboxRun{Key: "encargo-8", Src: Source{Repo: "o/tienda", SHA: "abc"}, Image: "img", Command: "echo empieza; echo x > a.txt; sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.Took > 20*time.Second || len(res.Changes) != 0 || !strings.Contains(res.Stdout, "empieza") {
		t.Fatalf("timeout %+v", res)
	}
	if pods, _ := cs.CoreV1().Pods("rendimiento-builds").List(ctx, metav1.ListOptions{}); len(pods.Items) != 0 {
		t.Fatal("the sandbox of a command that ran out of time stayed")
	}
}

// The janitor removes, when it starts, what an earlier process left; then
// the sandboxes idle too long, and pods no sandbox holds.
func TestSandboxJanitor(t *testing.T) {
	s, cs := sandboxes(t)
	s.defaults()
	ctx := context.Background()
	left := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sbx-encargo-1-aaaaa", Namespace: "rendimiento-builds",
		Labels: map[string]string{"app.kubernetes.io/managed-by": "rendimiento", SandboxLabel: "encargo-1"}}}
	step := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "run-5-web-test-bbbbb", Namespace: "rendimiento-builds",
		Labels: map[string]string{"app.kubernetes.io/managed-by": "rendimiento", "rendimiento.ai/run": "5"}}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "sbx-encargo-1-aaaaa", Namespace: "rendimiento-builds", Labels: left.Labels}}
	for _, o := range []runtime.Object{left, step} {
		if _, err := cs.CoreV1().Pods("rendimiento-builds").Create(ctx, o.(*corev1.Pod), metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cs.CoreV1().Secrets("rendimiento-builds").Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	s.Sweep(ctx, true)
	pods, _ := cs.CoreV1().Pods("rendimiento-builds").List(ctx, metav1.ListOptions{})
	if len(pods.Items) != 1 || pods.Items[0].Name != step.Name {
		t.Fatalf("after the first sweep: %v (a step's pod is not the janitor's)", pods.Items)
	}
	if secrets, _ := cs.CoreV1().Secrets("rendimiento-builds").List(ctx, metav1.ListOptions{}); len(secrets.Items) != 0 {
		t.Fatal("a leftover secret stayed")
	}

	// A sandbox of this process: kept while in use, removed once idle.
	box := &sandbox{pod: "sbx-encargo-2-ccccc", image: "img", used: time.Now()}
	held := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: box.pod, Namespace: "rendimiento-builds", CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
		Labels: map[string]string{"app.kubernetes.io/managed-by": "rendimiento", SandboxLabel: "encargo-2"}}}
	if _, err := cs.CoreV1().Pods("rendimiento-builds").Create(ctx, held, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	s.boxes = map[string]*sandbox{"encargo-2": box}
	s.Sweep(ctx, false)
	if _, err := cs.CoreV1().Pods("rendimiento-builds").Get(ctx, box.pod, metav1.GetOptions{}); err != nil {
		t.Fatal("a sandbox in use was removed")
	}
	box.used = time.Now().Add(-31 * time.Minute)
	s.Sweep(ctx, false)
	if _, err := cs.CoreV1().Pods("rendimiento-builds").Get(ctx, box.pod, metav1.GetOptions{}); err == nil {
		t.Fatal("an idle sandbox stayed")
	}
	if len(s.boxes) != 0 {
		t.Fatal("the idle sandbox is still held")
	}
}

// The caller's changes travel as files/<path> and a list of the removed.
func TestSandboxTar(t *testing.T) {
	raw, err := sandboxTar(map[string][]byte{"web/a.ts": []byte("x")}, []string{"viejo.txt"})
	if err != nil || len(raw) == 0 {
		t.Fatal(err)
	}
	for _, bad := range []string{"../x", "/etc/passwd", ".git/config", "a\nb"} {
		if _, err := sandboxTar(map[string][]byte{bad: nil}, nil); err == nil {
			t.Errorf("%q went in", bad)
		}
		if _, err := sandboxTar(nil, []string{bad}); err == nil {
			t.Errorf("removing %q went in", bad)
		}
	}
	tb := &tailBuffer{max: 4}
	_, _ = tb.Write([]byte("abcdef"))
	_, _ = tb.Write(bytes.Repeat([]byte("z"), 3))
	if tb.String() != "fzzz" || tb.total != 9 {
		t.Fatalf("tail %q %d", tb.String(), tb.total)
	}
}
