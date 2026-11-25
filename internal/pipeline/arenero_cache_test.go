package pipeline

import (
	"context"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

func envOf(c corev1.Container) map[string]string {
	out := map[string]string{}
	for _, e := range c.Env {
		out[e.Name] = e.Value
	}
	return out
}

// A sandbox with the app's cache mounts it read-write in the command's
// container only, with the tools' caches pointed at it; with Postgres, the
// steps' database runs beside it as a hardened sidecar, and the command
// sees where; without them, neither is there.
func TestSandboxCacheAndPostgresPod(t *testing.T) {
	s, _ := sandboxes(t)
	s.defaults()
	pod, err := s.Pod("sbx-encargo-9-abcde", map[string]string{SandboxLabel: "encargo-9"}, Source{Repo: "o/tienda", SHA: "abc"},
		SandboxPodOptions{Image: "golang:1.27", CacheDir: true, Cache: "sandbox-cache-tienda", Postgres: true})
	if err != nil {
		t.Fatal(err)
	}
	sp := pod.Spec
	vols := map[string]corev1.Volume{}
	for _, v := range sp.Volumes {
		vols[v.Name] = v
		if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName != "sandbox-cache-tienda" {
			t.Fatalf("the sandbox mounts %s: only its own cache, never CI's", v.PersistentVolumeClaim.ClaimName)
		}
	}
	if c := vols["cache"].PersistentVolumeClaim; c == nil || c.ClaimName != "sandbox-cache-tienda" || c.ReadOnly {
		t.Fatalf("cache volume %+v", vols["cache"])
	}
	if vols["postgres"].EmptyDir == nil || vols["postgres"].EmptyDir.SizeLimit == nil {
		t.Fatalf("postgres's data %+v", vols["postgres"])
	}
	if sp.SecurityContext.FSGroupChangePolicy == nil || *sp.SecurityContext.FSGroupChangePolicy != corev1.FSGroupChangeOnRootMismatch {
		t.Fatal("every mount would change the group of every file in the cache")
	}
	for _, c := range append(append([]corev1.Container{}, sp.InitContainers...), sp.Containers...) {
		for _, m := range c.VolumeMounts {
			if m.Name == "cache" && c.Name != "work" {
				t.Fatalf("%s mounts the cache", c.Name)
			}
			if m.Name == "postgres" && c.Name != "postgres" {
				t.Fatalf("%s mounts postgres's data", c.Name)
			}
		}
	}
	work := sp.Containers[0]
	env := envOf(work)
	for k, v := range map[string]string{"GOMODCACHE": "/cache/go-mod", "GOCACHE": "/cache/go-build", "npm_config_cache": "/cache/npm",
		"PIP_CACHE_DIR": "/cache/pip", "YARN_CACHE_FOLDER": "/cache/yarn", "npm_config_store_dir": "/cache/pnpm-store", "HOME": "/workspace/home",
		"DATABASE_URL": testDatabaseURL, "TEST_DATABASE_URL": testDatabaseURL + "&synchronous_commit=off"} {
		if env[k] != v {
			t.Errorf("work's %s = %q, want %q", k, env[k], v)
		}
	}
	if len(sp.InitContainers) != 2 || sp.InitContainers[0].Name != "clone" || sp.InitContainers[1].Name != "postgres" {
		t.Fatalf("init containers %d: the clone, then Postgres", len(sp.InitContainers))
	}
	pg := sp.InitContainers[1]
	sc := pg.SecurityContext
	if pg.RestartPolicy == nil || *pg.RestartPolicy != corev1.ContainerRestartPolicyAlways || pg.StartupProbe == nil || pg.Image != "postgres:17-alpine" {
		t.Fatalf("postgres is not the steps' sidecar: %+v", pg)
	}
	if sc == nil || sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot || sc.RunAsUser == nil || *sc.RunAsUser == 0 || sc.Privileged == nil || *sc.Privileged ||
		sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation || sc.Capabilities == nil || len(sc.Capabilities.Drop) != 1 || len(sc.Capabilities.Add) != 0 ||
		sc.SeccompProfile == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("postgres's security %+v", sc)
	}
	if pg.Resources.Requests.Cpu().MilliValue() < 200 || pg.Resources.Limits.Memory().IsZero() {
		t.Fatalf("postgres's resources %+v", pg.Resources)
	}
	if envOf(pg)["PGDATA"] == "" || !strings.HasPrefix(envOf(pg)["PGDATA"], pg.VolumeMounts[0].MountPath+"/") {
		t.Fatalf("PGDATA %q outside its volume", envOf(pg)["PGDATA"])
	}

	// No Postgres asked: no sidecar, no address. No cache volume: an empty
	// folder at /cache instead. No cache at all: nothing at /cache.
	pod, _ = s.Pod("x", nil, Source{Repo: "o/tienda", SHA: "abc"}, SandboxPodOptions{Image: "golang:1.27", CacheDir: true})
	env = envOf(pod.Spec.Containers[0])
	if len(pod.Spec.InitContainers) != 1 || env["DATABASE_URL"] != "" || env["TEST_DATABASE_URL"] != "" {
		t.Fatalf("a database nobody asked for: %v", env)
	}
	for _, v := range pod.Spec.Volumes {
		if v.Name == "cache" && (v.EmptyDir == nil || v.PersistentVolumeClaim != nil) {
			t.Fatalf("cache without a volume: %+v", v)
		}
	}
	if env["GOMODCACHE"] != "/cache/go-mod" {
		t.Fatal("the empty /cache is not used")
	}
	pod, _ = s.Pod("x", nil, Source{Repo: "o/tienda", SHA: "abc"}, SandboxPodOptions{Image: "golang:1.27"})
	if len(pod.Spec.Volumes) != 1 || envOf(pod.Spec.Containers[0])["GOMODCACHE"] != "" {
		t.Fatalf("a cache nobody asked for: %+v", pod.Spec.Volumes)
	}
}

// The test database is on the pod's own localhost, which NetworkPolicy
// never filters (the traffic does not leave the pod): isolate-sandbox
// still lets it be, and its address says synchronous_commit=off as
// hack/ci-test.sh does.
func TestSandboxDatabaseOnLocalhost(t *testing.T) {
	u, err := url.Parse(sandboxDatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(u.Hostname()); ip == nil || !ip.IsLoopback() || u.Query().Get("synchronous_commit") != "off" || u.Query().Get("sslmode") != "disable" {
		t.Fatalf("TEST_DATABASE_URL %s", u)
	}
	np := SandboxNetworkPolicy("rendimiento-builds")
	for _, e := range np.Spec.Egress {
		for _, to := range e.To {
			if to.IPBlock == nil {
				continue
			}
			for _, ex := range to.IPBlock.Except {
				if _, block, err := net.ParseCIDR(ex); err != nil || block.Contains(net.ParseIP(u.Hostname())) {
					t.Fatalf("the policy names loopback (%s)", ex)
				}
			}
		}
	}
	for in, want := range map[string]string{
		"postgres://a@h/db":                       "postgres://a@h/db?synchronous_commit=off",
		"postgresql://a@h/db?sslmode=disable":     "postgresql://a@h/db?sslmode=disable&synchronous_commit=off",
		"postgres://a@h/db?synchronous_commit=on": "postgres://a@h/db?synchronous_commit=on",
		"host=h dbname=db":                        "host=h dbname=db",
	} {
		if got := withSyncCommitOff(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

// seeds records the seed pods the fake API server was asked to create.
func seeds(cs interface {
	PrependReactor(verb, resource string, reaction k8stesting.ReactionFunc)
}) func() []*corev1.Pod {
	var mu sync.Mutex
	var got []*corev1.Pod
	cs.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		p := a.(k8stesting.CreateAction).GetObject().(*corev1.Pod)
		if p.Labels[sandboxSeedLabel] != "" {
			mu.Lock()
			got = append(got, p.DeepCopy())
			mu.Unlock()
		}
		return false, nil, nil
	})
	return func() []*corev1.Pod {
		mu.Lock()
		defer mu.Unlock()
		return append([]*corev1.Pod{}, got...)
	}
}

// The first sandbox of an app makes its cache and seeds it once from CI's,
// mounted read-only in the seed pod alone; the sandbox mounts its own cache
// and never CI's; a second sandbox of the app while the first holds the
// cache gets an empty folder; a later one finds the cache and does not
// seed it again.
func TestSandboxCacheSeededOnce(t *testing.T) {
	dir := checkout(t, map[string]string{"go.mod": "module x\n"})
	s, cs := sandboxes(t)
	s.K.CacheStorageClass = "longhorn-cache"
	seeded := seeds(cs)
	var calls []string
	s.Exec = shellExec(dir, &calls)
	ctx := context.Background()
	pvcs := cs.CoreV1().PersistentVolumeClaims("rendimiento-builds")
	ci := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "cache-tienda-web", Namespace: "rendimiento-builds",
		Labels: map[string]string{CacheAppLabel: "tienda"}}}
	if _, err := pvcs.Create(ctx, ci, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	run := func(key string) SandboxResult {
		t.Helper()
		res, err := s.Run(ctx, SandboxRun{Key: key, Src: Source{Repo: "o/tienda", SHA: "abc"}, Image: "golang:1.27", Command: "true",
			App: "tienda", SeedFrom: CacheVolumes("tienda", "web:test"), Postgres: true})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := run("encargo-20")
	if !res.CacheKept || !strings.Contains(res.Cache, "seeded from CI's cache-tienda-web") || !res.Postgres || !res.Fresh {
		t.Fatalf("first sandbox: %+v", res)
	}
	pvc, err := pvcs.Get(ctx, "sandbox-cache-tienda", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pvc.Labels[SandboxCacheLabel] != "tienda" || pvc.Labels[CacheAppLabel] != "" || pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "longhorn-cache" ||
		pvc.Spec.Resources.Requests.Storage().String() != "10Gi" || pvc.Annotations[sandboxCacheUsed] == "" {
		t.Fatalf("the sandbox cache %+v", pvc.ObjectMeta)
	}
	got := seeded()
	if len(got) != 1 {
		t.Fatalf("%d seed pods", len(got))
	}
	sp := got[0].Spec
	if sp.ActiveDeadlineSeconds == nil || *sp.ActiveDeadlineSeconds > 600 || sp.AutomountServiceAccountToken == nil || *sp.AutomountServiceAccountToken ||
		got[0].Labels[SandboxLabel] == "" {
		t.Fatalf("the seed pod is not bounded and isolated: %+v", got[0])
	}
	var ciVol, cacheVol string
	for _, v := range sp.Volumes {
		switch v.Name {
		case "ci":
			if !v.PersistentVolumeClaim.ReadOnly {
				t.Fatal("CI's cache is mounted writable in the seed pod")
			}
			ciVol = v.PersistentVolumeClaim.ClaimName
		case "cache":
			cacheVol = v.PersistentVolumeClaim.ClaimName
		}
	}
	if ciVol != "cache-tienda-web" || cacheVol != "sandbox-cache-tienda" {
		t.Fatalf("seed %s into %s", ciVol, cacheVol)
	}
	for _, m := range sp.Containers[0].VolumeMounts {
		if m.Name == "ci" && !m.ReadOnly {
			t.Fatal("CI's cache's mount is writable")
		}
	}
	if args := sp.Containers[0].Command; args[len(args)-2] != "/ci" || args[len(args)-1] != "/cache" {
		t.Fatalf("the seed copies %v: from CI's (/ci) into the sandbox's (/cache) only", args)
	}
	pods, _ := cs.CoreV1().Pods("rendimiento-builds").List(ctx, metav1.ListOptions{})
	if len(pods.Items) != 1 || pods.Items[0].Labels[sandboxSeedLabel] != "" {
		t.Fatalf("pods %d: the seed pod stayed", len(pods.Items))
	}
	first := pods.Items[0]
	claims := map[string]bool{}
	for _, v := range first.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			claims[v.PersistentVolumeClaim.ClaimName] = true
		}
	}
	if len(claims) != 1 || !claims["sandbox-cache-tienda"] {
		t.Fatalf("the sandbox mounts %v", claims)
	}

	// A second sandbox of the app while the first holds the cache: an
	// empty folder, never the same volume.
	res = run("encargo-21")
	if res.CacheKept || !strings.Contains(res.Cache, "another sandbox") {
		t.Fatalf("second sandbox: %+v", res)
	}
	pods, _ = cs.CoreV1().Pods("rendimiento-builds").List(ctx, metav1.ListOptions{LabelSelector: SandboxLabel + "=encargo-21"})
	for _, v := range pods.Items[0].Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			t.Fatalf("the second sandbox mounts %s", v.PersistentVolumeClaim.ClaimName)
		}
	}
	s.Close("encargo-21")

	// The first goes; the next finds the cache, already seeded.
	s.Close("encargo-20")
	res = run("encargo-22")
	if !res.CacheKept || strings.Contains(res.Cache, "seeded") || strings.Contains(res.Cache, "new") {
		t.Fatalf("third sandbox: %+v", res)
	}
	if len(seeded()) != 1 {
		t.Fatal("seeded again")
	}
	s.Close("encargo-22")
}

// A CI cache in use is not waited for: the sandbox cache starts empty.
func TestSandboxCacheSeedSkipsBusyCI(t *testing.T) {
	dir := checkout(t, map[string]string{"go.mod": "module x\n"})
	s, cs := sandboxes(t)
	seeded := seeds(cs)
	var calls []string
	s.Exec = shellExec(dir, &calls)
	ctx := context.Background()
	for _, n := range CacheVolumes("mercado", "pruebas:task") {
		if _, err := cs.CoreV1().PersistentVolumeClaims("rendimiento-builds").Create(ctx, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "rendimiento-builds"}}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		step := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "run-3-" + n, Namespace: "rendimiento-builds"},
			Spec: corev1.PodSpec{NodeName: "nodo", Volumes: []corev1.Volume{{Name: "cache", VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: n}}}}}}
		if _, err := cs.CoreV1().Pods("rendimiento-builds").Create(ctx, step, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	res, err := s.Run(ctx, SandboxRun{Key: "encargo-30", Src: Source{Repo: "o/mercado", SHA: "abc"}, Image: "golang:1.27", Command: "true",
		App: "mercado", SeedFrom: CacheVolumes("mercado", "pruebas:task")})
	if err != nil {
		t.Fatal(err)
	}
	if len(seeded()) != 0 || !res.CacheKept || !strings.Contains(res.Cache, "in use") || time.Since(start) > 30*time.Second {
		t.Fatalf("busy CI cache: %+v (%d seed pods)", res, len(seeded()))
	}
	if CacheVolumes("mercado", "pruebas:task")[0] != "cache-mercado-pruebas-task" || CacheVolumes("mercado", "web:test")[1] != "cache-mercado-web-2" {
		t.Fatalf("CI's cache names %v", CacheVolumes("mercado", "web:test"))
	}
	if strings.HasPrefix(SandboxCacheName("mercado"), "cache-") {
		t.Fatal("a sandbox cache could be taken for a CI one")
	}
	s.Close("encargo-30")
}

// tree lists a folder's files with their mode and content.
func tree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, _ := d.Info()
		line := strings.TrimPrefix(p, root) + " " + info.Mode().String()
		if !d.IsDir() {
			b, _ := os.ReadFile(p)
			line += " " + string(b)
		}
		out = append(out, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// The seed copies CI's Go modules (read-only folders, as Go leaves them)
// and npm cache into the sandbox cache, leaves CI's exactly as it was, and
// never overwrites what the sandbox cache already has.
func TestSandboxSeedScriptOneWay(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh here")
	}
	root := t.TempDir()
	ci, to := filepath.Join(root, "ci"), filepath.Join(root, "cache")
	t.Cleanup(func() { _ = exec.Command("chmod", "-R", "u+w", root).Run() })
	for p, c := range map[string]string{"go-mod/example.com/m@v1.0.0/m.go": "package m\n", "go-mod/cache/download/example.com/m/@v/v1.0.0.zip": "zip",
		"npm/_cacache/index": "npm", "go-build/aa/bb": "built"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(ci, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ci, p), []byte(c), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(ci, "go-mod/example.com/m@v1.0.0"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	before := tree(t, ci)
	out, err := exec.Command("sh", "-c", sandboxSeedScript, "seed", ci, to).CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if after := tree(t, ci); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("CI's cache changed:\n%v\n%v", before, after)
	}
	for p, want := range map[string]string{"go-mod/example.com/m@v1.0.0/m.go": "package m\n", "npm/_cacache/index": "npm"} {
		if b, err := os.ReadFile(filepath.Join(to, p)); err != nil || string(b) != want {
			t.Errorf("%s: %q %v", p, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(to, "go-build")); err == nil {
		t.Error("the build cache was copied (only modules and npm's are)")
	}
	for _, d := range []string{".go-mod.seeding", ".npm.seeding"} {
		if _, err := os.Stat(filepath.Join(to, d)); err == nil {
			t.Errorf("the scratch folder %s stayed", d)
		}
	}
	if !strings.Contains(string(out), "seeded go-mod") || !strings.Contains(string(out), "seeded npm") {
		t.Errorf("output %s", out)
	}
	// What the sandbox cache has stays its own.
	if err := os.Chmod(filepath.Join(to, "npm/_cacache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(to, "npm/_cacache/mine"), []byte("sandbox"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-c", sandboxSeedScript, "seed", ci, to).CombinedOutput(); err != nil || strings.Contains(string(out), "seeded") {
		t.Fatalf("a second seed: %v %s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(to, "npm/_cacache/mine")); string(b) != "sandbox" {
		t.Fatal("the sandbox cache was overwritten")
	}
}

// The janitor removes the sandbox caches unused for 14 days that no pod
// has, never CI's; deleting the app removes its sandbox cache too.
func TestSandboxCacheJanitor(t *testing.T) {
	s, cs := sandboxes(t)
	s.defaults()
	ctx := context.Background()
	pvcs := cs.CoreV1().PersistentVolumeClaims("rendimiento-builds")
	old := time.Now().Add(-15 * 24 * time.Hour).UTC().Format(time.RFC3339)
	recent := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	mk := func(name string, labels map[string]string, used string) {
		t.Helper()
		pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "rendimiento-builds", Labels: labels,
			Annotations: map[string]string{sandboxCacheUsed: used}, CreationTimestamp: metav1.NewTime(time.Now().Add(-30 * 24 * time.Hour))}}
		if _, err := pvcs.Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	sbx := func(app string) map[string]string {
		return map[string]string{"app.kubernetes.io/managed-by": "rendimiento", SandboxCacheLabel: app}
	}
	mk("sandbox-cache-viejo", sbx("viejo"), old)
	mk("sandbox-cache-reciente", sbx("reciente"), recent)
	mk("sandbox-cache-ocupado", sbx("ocupado"), old)
	mk("cache-viejo-web", map[string]string{"app.kubernetes.io/managed-by": "rendimiento", CacheAppLabel: "viejo"}, old)
	user := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sbx-encargo-40-aaaaa", Namespace: "rendimiento-builds"},
		Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "cache", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "sandbox-cache-ocupado"}}}}}}
	if _, err := cs.CoreV1().Pods("rendimiento-builds").Create(ctx, user, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	s.sweepCaches(ctx, time.Now())
	l, _ := pvcs.List(ctx, metav1.ListOptions{})
	var left []string
	for _, c := range l.Items {
		left = append(left, c.Name)
	}
	sort.Strings(left)
	if strings.Join(left, " ") != "cache-viejo-web sandbox-cache-ocupado sandbox-cache-reciente" {
		t.Fatalf("left %v", left)
	}
	if err := s.K.DeleteCaches(ctx, "reciente"); err != nil {
		t.Fatal(err)
	}
	if _, err := pvcs.Get(ctx, "sandbox-cache-reciente", metav1.GetOptions{}); err == nil {
		t.Fatal("the app's sandbox cache stayed after the app")
	}
}
