package pipeline

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

// The coding agent's sandbox (its run tool): a pod in the build namespace
// that holds the app's repository at the agent's branch, with its
// uncommitted changes laid over it, where any shell command runs. It lives
// for the agent's job, so what one command installs (node_modules, Go's
// cache) is there for the next, and is removed when the job ends, after
// Idle without a command, or by the janitor.
//
// What a command may do there is what a test step's code may do, less:
// no service account token, no secrets or platform environment, not root,
// no capabilities, RuntimeDefault seccomp, a workspace that is an emptyDir
// with a size limit, never the control plane, and a NetworkPolicy of its
// own (isolate-sandbox) that lets it reach DNS and the public internet
// only: not the cluster, the nodes, the home network or BuildKit. The
// tools' caches live in the app's sandbox cache (arenero_cache.go), never
// in CI's; a folder whose test or task has postgres: true gets the steps'
// throwaway Postgres beside it, on localhost.

// SandboxLabel marks a sandbox's pod and secret; its value is the
// sandbox's key (the encargo's). Step pods never carry it: isolate-builds
// leaves these pods out and isolate-sandbox takes them.
const (
	SandboxLabel  = "rendimiento.ai/sandbox"
	SandboxPolicy = "isolate-sandbox"
)

// Defaults of a sandbox.
const (
	DefaultSandboxImage = "golang:1.27-bookworm"
	sandboxUID          = 1000
	sandboxMaxBytes     = 16 << 20 // what the files a command changed may add up to, compressed
	sandboxMaxFiles     = 400      // how many files one command may change and send back
	sandboxMaxPending   = 64 << 20 // the uncommitted changes sent in, compressed
)

// ExecFunc runs a command in a container of a pod: what it printed goes to
// stdout and stderr, and it answers the command's exit code (an error only
// when it could not run, or ctx ended).
type ExecFunc func(ctx context.Context, namespace, pod, container string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) (int, error)

// Sandboxes runs the coding agent's commands in sandbox pods (see above).
type Sandboxes struct {
	// K is the executor whose namespace, clone token, git image and nodes
	// the sandboxes share with the steps.
	K *KubeExecutor
	// Rest is the platform's connection, for exec (pods/exec in the build
	// namespace); Exec replaces it in tests.
	Rest *rest.Config
	Exec ExecFunc
	// Image is the general image, for an app with no test or task image
	// for the folder (CODER_SANDBOX_IMAGE).
	Image string
	// RuntimeClass is the pods' runtimeClassName (CODER_SANDBOX_RUNTIME_CLASS;
	// empty: the cluster's default runtime). gVisor's runsc or Kata, once a
	// node has them, isolate the kernel too.
	RuntimeClass string
	// CPU and Memory are the command container's limits (3 and 4Gi), Disk
	// the workspace's (8Gi). Its CPU request stays small (250m): a sandbox
	// lives for hours mostly waiting for the model, and reserving cores for
	// it would keep CI's steps off the node; with the cores free, a compile
	// gets up to CPU of them.
	CPU, Memory, Disk string
	// CacheSize is each app's sandbox cache (CODER_SANDBOX_CACHE_SIZE, 10Gi;
	// "off": none, the caches stay in the workspace), CacheIdle how long one
	// unused stays (14 days), and SeedWait the most seeding a new one from
	// CI's cache may take (5 minutes). The storage class is the test
	// caches' (K.CacheStorageClass).
	CacheSize string
	CacheIdle time.Duration
	SeedWait  time.Duration
	// Timeout is the most one command runs (15 minutes), Idle how long a
	// sandbox waits for the next one (30 minutes), and MaxLife the most a
	// sandbox lives at all (4 hours, past the agent's 3-hour job).
	Timeout, Idle, MaxLife time.Duration
	// MaxOutput is how much of the end of stdout and of stderr comes back
	// (16 KB each).
	MaxOutput int
	Log       *slog.Logger

	mu    sync.Mutex
	boxes map[string]*sandbox
	appMu map[string]*sync.Mutex
}

// sandbox is one live sandbox pod.
type sandbox struct {
	mu       sync.Mutex
	pod      string
	image    string
	postgres bool
	cache    string // what /cache is
	kept     bool   // the app's sandbox cache, not an empty folder
	used     time.Time
	gone     bool
}

// SandboxRun is one command to run.
type SandboxRun struct {
	// Key names the sandbox (one per encargo): runs with the same key share
	// its pod.
	Key string
	// Src is the repository and the commit the sandbox is made from, the
	// first time.
	Src Source
	// Image is the command's image (empty: Image); ImageFrom says why.
	Image, ImageFrom string
	// Folder is where it runs, from the repository's root.
	Folder  string
	Command string
	// Pending and Removed are the caller's uncommitted changes: the
	// sandbox's files are set to the commit's with them before it runs.
	Pending map[string][]byte
	Removed []string
	// App is the encargo's app, whose sandbox cache the sandbox mounts
	// (empty: none). SeedFrom are the CI caches a new one is seeded from
	// (CacheVolumes of the folder's test or task), read-only.
	App      string
	SeedFrom []string
	// Postgres gives the sandbox the steps' throwaway Postgres (the
	// folder's test or task has postgres: true).
	Postgres bool
}

// SandboxResult is how a command ran.
type SandboxResult struct {
	ExitCode int
	TimedOut bool
	Took     time.Duration
	// Stdout and Stderr are the end of what it printed (MaxOutput each);
	// StdoutBytes and StderrBytes all it printed.
	Stdout, Stderr           string
	StdoutBytes, StderrBytes int64
	// Changes are the repository's files it changed (content, or deleted),
	// to join the caller's changes; Skipped those it changed that do not
	// come back (a name git quotes, a link), and TooMany or TooBig why
	// nothing came back.
	Changes []FixChange
	Skipped []string
	TooMany int
	TooBig  bool
	// Image is what it ran in, and Fresh whether the sandbox was new.
	Image, ImageFrom string
	Fresh            bool
	// Setup is how long making the sandbox took (the image, the clone,
	// seeding its cache), Sync laying the changes in; Cache says what /cache
	// was, and Postgres whether a database was beside it.
	Setup, Sync time.Duration
	Cache       string
	CacheKept   bool
	Postgres    bool
}

func (s *Sandboxes) defaults() {
	if s.Image == "" {
		s.Image = DefaultSandboxImage
	}
	if s.CPU == "" {
		s.CPU = "3"
	}
	if s.Memory == "" {
		s.Memory = "4Gi"
	}
	if s.Disk == "" {
		s.Disk = "8Gi"
	}
	if s.Timeout <= 0 {
		s.Timeout = 15 * time.Minute
	}
	if s.Idle <= 0 {
		s.Idle = 30 * time.Minute
	}
	if s.MaxLife <= 0 {
		s.MaxLife = 4 * time.Hour
	}
	if s.CacheSize == "" {
		s.CacheSize = "10Gi"
	}
	if s.CacheIdle <= 0 {
		s.CacheIdle = 14 * 24 * time.Hour
	}
	if s.SeedWait <= 0 {
		s.SeedWait = 5 * time.Minute
	}
	if s.MaxOutput <= 0 {
		s.MaxOutput = 16 << 10
	}
	if s.Log == nil {
		s.Log = slog.New(slog.DiscardHandler)
	}
	s.K.defaults()
}

// Run runs a command in the key's sandbox, making it first when there is
// none (or it ran in another image): the files are set to the commit with
// the pending changes, the command runs in its folder, and the files it
// changed come back. A command that runs past Timeout, or whose caller
// gives up, takes the sandbox with it: the next one starts a fresh one.
func (s *Sandboxes) Run(ctx context.Context, req SandboxRun) (SandboxResult, error) {
	s.mu.Lock()
	s.defaults()
	if s.boxes == nil {
		s.boxes = map[string]*sandbox{}
	}
	box := s.boxes[req.Key]
	if box == nil {
		box = &sandbox{}
		s.boxes[req.Key] = box
	}
	s.mu.Unlock()
	box.mu.Lock()
	defer box.mu.Unlock()
	box.used = time.Now()
	defer func() { box.used = time.Now() }()

	workdir := path.Join("/workspace/src", path.Clean("/"+req.Folder))
	image, from := req.Image, req.ImageFrom
	if image == "" {
		image, from = s.Image, "default"
	}
	res := SandboxResult{Image: image, ImageFrom: from, Postgres: req.Postgres}
	pending, err := sandboxTar(req.Pending, req.Removed)
	if err != nil {
		return res, err
	}
	if len(pending) > sandboxMaxPending {
		return res, fmt.Errorf("the uncommitted changes are %d MB compressed, more than the %d MB a sandbox takes; commit them first", len(pending)>>20, sandboxMaxPending>>20)
	}
	if box.pod != "" && (box.image != image || box.postgres != req.Postgres || box.gone) {
		s.drop(box)
	}
	begin := time.Now()
	if box.pod == "" {
		if err := s.start(ctx, req, box, image); err != nil {
			return res, err
		}
		res.Fresh = true
	}
	res.Setup = time.Since(begin).Round(100 * time.Millisecond)
	// The files as the caller has them; a sandbox whose git a command broke
	// is made again, once.
	begin = time.Now()
	if msg, err := s.script(ctx, box.pod, []string{"sh", "-c", sandboxSyncScript, "sync", "/workspace"}, bytes.NewReader(pending), io.Discard); err != nil || msg != "" {
		s.drop(box)
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if err := s.start(ctx, req, box, image); err != nil {
			return res, err
		}
		res.Fresh = true
		if msg, err := s.script(ctx, box.pod, []string{"sh", "-c", sandboxSyncScript, "sync", "/workspace"}, bytes.NewReader(pending), io.Discard); err != nil || msg != "" {
			s.drop(box)
			return res, fmt.Errorf("could not lay your changes in the sandbox: %v %s", err, msg)
		}
	}
	res.Sync = time.Since(begin).Round(100 * time.Millisecond)
	res.Cache, res.CacheKept = box.cache, box.kept

	out := &tailBuffer{max: s.MaxOutput}
	errOut := &tailBuffer{max: s.MaxOutput}
	secs := int(s.Timeout.Seconds())
	rctx, cancel := context.WithTimeout(ctx, s.Timeout+30*time.Second)
	start := time.Now()
	code, err := s.execFn()(rctx, s.K.Namespace, box.pod, "work",
		[]string{"sh", "-c", sandboxRunScript, "run", workdir, strconv.Itoa(secs), req.Command}, nil, out, errOut)
	expired := rctx.Err() == context.DeadlineExceeded // before cancel, which sets it too
	cancel()
	res.Took = time.Since(start).Round(100 * time.Millisecond)
	res.Stdout, res.Stderr, res.StdoutBytes, res.StderrBytes = out.String(), errOut.String(), out.total, errOut.total
	res.ExitCode = code
	switch {
	case ctx.Err() != nil:
		s.drop(box) // the caller gave up: nothing it ran is left running
		return res, ctx.Err()
	case expired || (code == 137 && res.Took >= s.Timeout-time.Second):
		// The command ran out of time: its processes may still run, so the
		// sandbox goes, and what it changed with it.
		res.TimedOut, res.ExitCode = true, -1
		s.drop(box)
		return res, nil
	case err != nil:
		s.drop(box)
		return res, fmt.Errorf("the command could not run in the sandbox: %v", err)
	case code == 97 && strings.HasPrefix(res.Stderr, "no such folder: "):
		return res, fmt.Errorf("no folder %s in the repository", req.Folder)
	}

	// What it changed comes back.
	var raw bytes.Buffer
	msg, err := s.script(ctx, box.pod, []string{"sh", "-c", sandboxCollectScript, "collect", "/workspace", strconv.Itoa(sandboxMaxBytes), strconv.Itoa(sandboxMaxFiles)}, nil,
		&limited{w: &raw, n: 2 * sandboxMaxBytes})
	if err != nil || msg != "" {
		s.drop(box)
		return res, fmt.Errorf("could not read what the command changed: %v %s", err, msg)
	}
	if err := res.read(raw.Bytes()); err != nil {
		s.drop(box)
		return res, err
	}
	return res, nil
}

// read reads what the collect script printed into the result.
func (res *SandboxResult) read(raw []byte) error {
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "RENDIMIENTO-RUN-TOOMANY "):
			res.TooMany, _ = strconv.Atoi(strings.TrimPrefix(line, "RENDIMIENTO-RUN-TOOMANY "))
			return nil
		case strings.HasPrefix(line, "RENDIMIENTO-FIX-TOOBIG "):
			res.TooBig = true
			return nil
		case strings.HasPrefix(line, "RENDIMIENTO-FIX-CHANGED "):
			kept = append(kept, strings.TrimPrefix(line, "RENDIMIENTO-FIX-CHANGED "))
		}
	}
	out, err := ParseFixOutput(raw)
	if err != nil {
		return err
	}
	res.Changes, res.Skipped = out.Changes, out.Others
	got := map[string]bool{}
	for _, c := range out.Changes {
		got[c.Path] = true
	}
	for _, p := range kept {
		if !got[p] { // a link, or a folder: not a file that comes back
			res.Skipped = append(res.Skipped, p)
		}
	}
	sort.Strings(res.Skipped)
	return nil
}

// SandboxKey is a key as a label value and a piece of a pod's name.
func SandboxKey(key string) string {
	k := strings.Trim(nonName.ReplaceAllString(strings.ToLower(key), "-"), "-")
	if len(k) > 40 {
		k = strings.TrimRight(k[:40], "-")
	}
	return k
}

// Close removes the key's sandbox, if it has one (the agent's job ended).
func (s *Sandboxes) Close(key string) {
	s.mu.Lock()
	box := s.boxes[key]
	delete(s.boxes, key)
	s.mu.Unlock()
	if box == nil {
		return
	}
	box.mu.Lock()
	defer box.mu.Unlock()
	s.drop(box)
}

// Janitor removes the sandboxes left by an earlier process when it starts,
// and then, every minute, those idle past Idle, older than MaxLife, or
// that no sandbox of this process holds. Only the process that runs the
// coding agent may run it.
func (s *Sandboxes) Janitor(ctx context.Context) {
	s.mu.Lock()
	s.defaults()
	s.mu.Unlock()
	s.Sweep(ctx, true)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(ctx, false)
		}
	}
}

// Sweep is one round of the janitor: all leftovers go when first is true.
func (s *Sandboxes) Sweep(ctx context.Context, first bool) {
	s.mu.Lock()
	s.defaults()
	now := time.Now()
	held := map[string]bool{}
	var idle []string
	for key, box := range s.boxes {
		if box.mu.TryLock() {
			if box.pod != "" && now.Sub(box.used) > s.Idle {
				idle = append(idle, key)
			}
			if box.pod != "" {
				held[box.pod] = true
			}
			box.mu.Unlock()
		} else {
			held["*"+SandboxKey(key)] = true // running a command now: its pod stays
		}
	}
	s.mu.Unlock()
	for _, key := range idle {
		s.Log.Info("sandbox: idle, removed", "key", key)
		s.Close(key)
	}
	sel := metav1.ListOptions{LabelSelector: "app.kubernetes.io/managed-by=rendimiento," + SandboxLabel}
	pods, err := s.K.Client.CoreV1().Pods(s.K.Namespace).List(ctx, sel)
	if err == nil {
		for _, p := range pods.Items {
			age := now.Sub(p.CreationTimestamp.Time)
			orphan := !held[p.Name] && !held["*"+p.Labels[SandboxLabel]] && (first || age > 2*time.Minute)
			if orphan || age > s.MaxLife {
				s.Log.Info("sandbox: leftover, removed", "pod", p.Name)
				_ = s.K.Client.CoreV1().Pods(s.K.Namespace).Delete(ctx, p.Name, metav1.DeleteOptions{})
			}
		}
	}
	if secrets, err := s.K.Client.CoreV1().Secrets(s.K.Namespace).List(ctx, sel); err == nil {
		for _, sec := range secrets.Items {
			if first || now.Sub(sec.CreationTimestamp.Time) > 15*time.Minute {
				_ = s.K.Client.CoreV1().Secrets(s.K.Namespace).Delete(ctx, sec.Name, metav1.DeleteOptions{})
			}
		}
	}
	s.sweepCaches(ctx, now)
}

// drop deletes a sandbox's pod (and its clone secret, if still there).
func (s *Sandboxes) drop(box *sandbox) {
	if box.pod == "" {
		return
	}
	bg := context.Background()
	zero := int64(0)
	_ = s.K.Client.CoreV1().Pods(s.K.Namespace).Delete(bg, box.pod, metav1.DeleteOptions{GracePeriodSeconds: &zero})
	_ = s.K.Client.CoreV1().Secrets(s.K.Namespace).Delete(bg, box.pod, metav1.DeleteOptions{})
	box.pod, box.image, box.postgres, box.cache, box.kept, box.gone = "", "", false, "", false, false
}

// start makes the key's sandbox: its network policy first (no pod without
// it), the app's cache (seeded from CI's the first time), the clone token's
// secret, the pod, and, once the clone is done and the sandbox runs, the
// secret goes.
func (s *Sandboxes) start(ctx context.Context, req SandboxRun, box *sandbox, image string) error {
	k := s.K
	if err := s.EnsurePolicy(ctx); err != nil {
		return fmt.Errorf("the sandbox's network policy (%s) could not be set, so no sandbox runs: %v", SandboxPolicy, err)
	}
	key := SandboxKey(req.Key)
	name := "sbx-" + key + "-" + randSuffix()
	labels := map[string]string{"app.kubernetes.io/managed-by": "rendimiento", SandboxLabel: key}
	opts := SandboxPodOptions{Image: image, Postgres: req.Postgres}
	release := func() {}
	if req.App != "" && s.cacheOn() {
		// Held until the pod is created: from then on it is listed as the
		// cache's, and another sandbox of the app goes without.
		lock := s.appLock(req.App)
		lock.Lock()
		var once sync.Once
		release = func() { once.Do(lock.Unlock) }
		defer release()
		opts.Cache, box.cache = s.takeCache(ctx, req, labels)
		opts.CacheDir, box.kept = true, opts.Cache != ""
	}
	token := ""
	if k.Token != nil {
		var err error
		if token, err = k.Token(ctx, req.Src.Repo); err != nil {
			return fmt.Errorf("could not get a clone token: %v", err)
		}
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: k.Namespace, Labels: labels},
		Data: map[string][]byte{"token": []byte(token)}}
	if err := transientRetry(ctx, func() error {
		_, err := k.Client.CoreV1().Secrets(k.Namespace).Create(ctx, secret, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}); err != nil {
		return fmt.Errorf("create the sandbox's secret: %v", err)
	}
	box.pod, box.image, box.postgres = name, image, req.Postgres
	pod, err := s.Pod(name, labels, req.Src, opts)
	if err != nil {
		s.drop(box)
		return err
	}
	if err := k.createPod(ctx, pod); err != nil {
		s.drop(box)
		return fmt.Errorf("create the sandbox's pod: %v", err)
	}
	release()
	wctx, cancel := context.WithTimeout(ctx, 10*time.Minute) // the image pull and the clone
	defer cancel()
	for _, c := range []string{"clone", "work", "git"} {
		if err := k.waitStarted(wctx, name, c); err != nil {
			s.drop(box)
			return fmt.Errorf("the sandbox's %s container did not start: %v", c, err)
		}
	}
	p, err := k.Client.CoreV1().Pods(k.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil && p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodPending {
		var log bytes.Buffer
		_ = k.streamLogs(ctx, name, "clone", &limited{w: &log, n: 4 << 10})
		s.drop(box)
		return fmt.Errorf("the sandbox could not check out %s: %s %s", req.Src.Ref(), terminationSummary(p), strings.TrimSpace(log.String()))
	}
	// The token was only for the clone (and is gone from .git/config).
	_ = k.Client.CoreV1().Secrets(k.Namespace).Delete(context.WithoutCancel(ctx), name, metav1.DeleteOptions{})
	return nil
}

// sandboxCloneScript checks out the commit, as the steps' clone does, then
// takes the token out of .git/config before anything else runs, and marks
// the checkout as the first sync point.
const sandboxCloneScript = `set -eu
mkdir -p /workspace/src /workspace/home && cd /workspace/src
git init -q
git remote add origin "$CLONE_URL"
if [ -n "${GIT_TOKEN:-}" ]; then
  case "$GIT_TOKEN" in
    *:*) cred=$GIT_TOKEN ;;
    *) cred=x-access-token:$GIT_TOKEN ;;
  esac
  git config http.extraHeader "Authorization: basic $(printf '%s' "$cred" | base64 | tr -d '\n')"
fi
git fetch -q --depth 1 origin "$REF"
git config --unset-all http.extraHeader || true
git checkout -q FETCH_HEAD
git update-ref refs/rendimiento/sync HEAD
echo "checked out $(git rev-parse HEAD)"
`

// sandboxGit is git as the platform's scripts call it: no hooks a command
// may have planted, any owner, a fixed author.
const sandboxGit = `G() { git -c core.hooksPath=/dev/null -c safe.directory='*' -c core.quotepath=off -c user.name=rendimiento -c user.email=rendimiento@example.invalid "$@"; }
`

// sandboxSyncScript sets the repository's files to the last sync point
// (what the caller had then; untracked files go, ignored ones such as
// node_modules stay), lays the caller's changes over them from stdin, and
// makes that the new sync point. It prints nothing when it worked.
const sandboxSyncScript = `set -eu
` + sandboxGit + `in=$1/sync; cd "$1/src"
rm -rf "$in"; mkdir -p "$in"
tar -xzf - -C "$in"
G reset -q --hard refs/rendimiento/sync >/dev/null
G clean -fdq >/dev/null
if [ -d "$in/files" ]; then cp -R "$in/files/." .; fi
if [ -f "$in/removed" ]; then
  while IFS= read -r f; do [ -n "$f" ] && rm -f -- "$f"; done < "$in/removed"
fi
G add -A >/dev/null
G commit -q --allow-empty --no-verify -m sync >/dev/null
G update-ref refs/rendimiento/sync HEAD
rm -rf "$in"
`

// sandboxRunScript runs the command in its folder, killed at its time when
// the image has timeout (else the platform's deadline removes the pod).
// 97: no such folder.
const sandboxRunScript = `cd "$1" 2>/dev/null || { echo "no such folder: $1" >&2; exit 97; }
if command -v timeout >/dev/null 2>&1; then exec timeout -s KILL "$2" sh -c "$3"; fi
exec sh -c "$3"
`

// sandboxCollectScript lists what changed since the sync point (committed
// or not: a command may commit) and sends the changed files back as the
// fix's collect does, between the same markers.
const sandboxCollectScript = `set -eu
` + sandboxGit + `cd "$1/src"
G add -A >/dev/null
G diff --cached --no-renames --name-status refs/rendimiento/sync > /tmp/changed
G reset -q >/dev/null
echo "RENDIMIENTO-FIX-BEGIN"
n=$(wc -l < /tmp/changed)
if [ "$n" -gt "$3" ]; then echo "RENDIMIENTO-RUN-TOOMANY $n"; echo "RENDIMIENTO-FIX-END"; exit 0; fi
: > /tmp/keep
tab=$(printf '\t')
while IFS="$tab" read -r st f; do
  [ -n "$f" ] || continue
  case "$f" in '"'*) echo "RENDIMIENTO-FIX-OTHER $f"; continue;; esac
  case "$st" in
    D) echo "RENDIMIENTO-FIX-DELETED $f" ;;
    *) if [ -f "$f" ] && [ ! -L "$f" ]; then echo "$f" >> /tmp/keep; echo "RENDIMIENTO-FIX-CHANGED $f"; else echo "RENDIMIENTO-FIX-OTHER $f"; fi ;;
  esac
done < /tmp/changed
if [ -s /tmp/keep ]; then
  tar -czf /tmp/out.tar.gz -T /tmp/keep
  size=$(wc -c < /tmp/out.tar.gz)
  if [ "$size" -gt "$2" ]; then echo "RENDIMIENTO-FIX-TOOBIG $size"; else
    echo "RENDIMIENTO-FIX-TAR"; base64 /tmp/out.tar.gz; echo "RENDIMIENTO-FIX-TAREND"
  fi
fi
echo "RENDIMIENTO-FIX-END"
`

// SandboxPodOptions are what a sandbox's pod is made with beyond its
// commit: the command's image; Postgres for the steps' throwaway database
// beside it; CacheDir for /cache in the command's container, the volume
// Cache (the app's sandbox cache), or an empty folder when Cache is "".
type SandboxPodOptions struct {
	Image    string
	Postgres bool
	CacheDir bool
	Cache    string
}

// Pod is a sandbox's pod: the clone as an init container (the only one
// with the token), then the command's container in the image and a git
// container beside it, both idle until the platform execs into them,
// sharing the workspace. Only the command's container mounts /cache; the
// Postgres sidecar, when asked for, starts after the clone and before it.
func (s *Sandboxes) Pod(name string, labels map[string]string, src Source, opts SandboxPodOptions) (*corev1.Pod, error) {
	k := s.K
	image := opts.Image
	cloneURL, ref := src.CloneURL(), src.Ref()
	if ref == "" || image == "" {
		return nil, errors.New("a sandbox needs a commit and an image")
	}
	cpu, err := resource.ParseQuantity(s.CPU)
	if err != nil {
		return nil, fmt.Errorf("sandbox CPU %q: %v", s.CPU, err)
	}
	mem, err := resource.ParseQuantity(s.Memory)
	if err != nil {
		return nil, fmt.Errorf("sandbox memory %q: %v", s.Memory, err)
	}
	disk, err := resource.ParseQuantity(s.Disk)
	if err != nil {
		return nil, fmt.Errorf("sandbox disk %q: %v", s.Disk, err)
	}
	ws := corev1.VolumeMount{Name: "workspace", MountPath: "/workspace"}
	harden := func() *corev1.SecurityContext {
		return &corev1.SecurityContext{
			Privileged:               ptr(false),
			AllowPrivilegeEscalation: ptr(false),
			RunAsNonRoot:             ptr(true),
			RunAsUser:                ptr(int64(sandboxUID)),
			RunAsGroup:               ptr(int64(sandboxUID)),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		}
	}
	idle := []string{"sh", "-c", "trap 'exit 0' TERM INT; while true; do sleep 3600 & wait $!; done"}
	small := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("512Mi")},
	}
	work := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("512Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: cpu, corev1.ResourceMemory: mem, corev1.ResourceEphemeralStorage: resource.MustParse("2Gi")},
	}
	k.capCPU(&work)
	// Plain values only: nothing of the platform's, and HOME in the
	// workspace so npm's and Go's caches have somewhere to go as uid 1000.
	env := []corev1.EnvVar{{Name: "HOME", Value: "/workspace/home"}, {Name: "CI", Value: "true"},
		{Name: "COREPACK_ENABLE_DOWNLOAD_PROMPT", Value: "0"}, {Name: "npm_config_update_notifier", Value: "false"}}
	gitEnv := []corev1.EnvVar{{Name: "HOME", Value: "/tmp"}}
	life := int64(s.MaxLife.Seconds())
	grace := int64(5)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: k.Namespace, Labels: labels},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			Affinity:                      s.affinity(),
			ActiveDeadlineSeconds:         &life,
			TerminationGracePeriodSeconds: &grace,
			AutomountServiceAccountToken:  ptr(false),
			EnableServiceLinks:            ptr(false),
			HostNetwork:                   false,
			HostPID:                       false,
			HostIPC:                       false,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   ptr(true),
				RunAsUser:      ptr(int64(sandboxUID)),
				RunAsGroup:     ptr(int64(sandboxUID)),
				FSGroup:        ptr(int64(sandboxUID)),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			InitContainers: []corev1.Container{{
				Name: "clone", Image: k.GitImage, Command: []string{"sh", "-c", sandboxCloneScript},
				Env: []corev1.EnvVar{{Name: "HOME", Value: "/tmp"}, {Name: "CLONE_URL", Value: cloneURL}, {Name: "REF", Value: ref},
					{Name: "GIT_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: "token"}}}},
				VolumeMounts: []corev1.VolumeMount{ws}, Resources: small, SecurityContext: harden(),
			}},
			Containers: []corev1.Container{
				{Name: "work", Image: image, Command: idle, WorkingDir: "/workspace/src", Env: env,
					VolumeMounts: []corev1.VolumeMount{ws}, Resources: work, SecurityContext: harden()},
				{Name: "git", Image: k.GitImage, Command: idle, WorkingDir: "/workspace/src", Env: gitEnv,
					VolumeMounts: []corev1.VolumeMount{ws}, Resources: small, SecurityContext: harden()},
			},
			Volumes: []corev1.Volume{{Name: "workspace", VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &disk}}}},
		},
	}
	work0 := &pod.Spec.Containers[0]
	if opts.CacheDir {
		work0.Env = append(work0.Env, sandboxCacheEnv...)
		work0.VolumeMounts = append(work0.VolumeMounts, corev1.VolumeMount{Name: "cache", MountPath: "/cache"})
		vs := corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &disk}}
		if opts.Cache != "" {
			vs = corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: opts.Cache}}
			// The cache holds many thousands of files: change their group
			// only when its root's is not the sandbox's, not at every mount.
			onRoot := corev1.FSGroupChangeOnRootMismatch
			pod.Spec.SecurityContext.FSGroupChangePolicy = &onRoot
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{Name: "cache", VolumeSource: vs})
	}
	if opts.Postgres {
		// The steps' Postgres, on the pod's own localhost: NetworkPolicy
		// never sees that traffic (it does not leave the pod), so
		// isolate-sandbox, which takes everything else, lets it be.
		work0.Env = append(work0.Env, corev1.EnvVar{Name: "DATABASE_URL", Value: testDatabaseURL},
			corev1.EnvVar{Name: "TEST_DATABASE_URL", Value: sandboxDatabaseURL()})
		pgDisk := resource.MustParse("2Gi")
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, s.sandboxPostgres())
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{Name: "postgres", VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &pgDisk}}})
	}
	if s.RuntimeClass != "" {
		pod.Spec.RuntimeClassName = ptr(s.RuntimeClass)
	}
	return pod, nil
}

// affinity keeps a sandbox off the control plane and off the nodes that
// take no runs, and away from busy ones as a step is.
func (s *Sandboxes) affinity() *corev1.Affinity {
	a := s.K.affinity()
	if a == nil {
		a = &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}}
	}
	na := a.NodeAffinity
	reqs := []corev1.NodeSelectorRequirement{
		{Key: "node-role.kubernetes.io/control-plane", Operator: corev1.NodeSelectorOpDoesNotExist},
		{Key: "node-role.kubernetes.io/master", Operator: corev1.NodeSelectorOpDoesNotExist},
	}
	if na.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		na.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{}}}
	}
	terms := na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	for i := range terms {
		terms[i].MatchExpressions = append(terms[i].MatchExpressions, reqs...)
	}
	return a
}

// SandboxNetworkPolicy is isolate-sandbox, as deploy/networkpolicy.yaml
// has it: nothing connects to a sandbox, and it reaches DNS and the public
// internet only, not the cluster's pods and services, the nodes, the home
// network, link-local addresses (cloud metadata) or BuildKit.
func SandboxNetworkPolicy(namespace string) *networkingv1.NetworkPolicy {
	udp, tcp := corev1.ProtocolUDP, corev1.ProtocolTCP
	dns := intstr.FromInt32(53)
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: SandboxPolicy, Namespace: namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "rendimiento"}},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: SandboxLabel, Operator: metav1.LabelSelectorOpExists}}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			Ingress:     []networkingv1.NetworkPolicyIngressRule{},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					To: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}},
						PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}},
					}},
					Ports: []networkingv1.NetworkPolicyPort{{Protocol: &udp, Port: &dns}, {Protocol: &tcp, Port: &dns}},
				},
				{
					To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0",
						Except: []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10"}}}},
				},
			},
		},
	}
}

// EnsurePolicy makes isolate-sandbox as it should be: created when
// missing, put back when changed.
func (s *Sandboxes) EnsurePolicy(ctx context.Context) error {
	want := SandboxNetworkPolicy(s.K.Namespace)
	api := s.K.Client.NetworkingV1().NetworkPolicies(s.K.Namespace)
	have, err := api.Get(ctx, SandboxPolicy, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		_, err = api.Create(ctx, want, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	case err != nil:
		return err
	case equality.Semantic.DeepEqual(have.Spec, want.Spec):
		return nil
	}
	have.Spec = want.Spec
	_, err = api.Update(ctx, have, metav1.UpdateOptions{})
	return err
}

func (s *Sandboxes) execFn() ExecFunc {
	if s.Exec != nil {
		return s.Exec
	}
	return func(ctx context.Context, namespace, pod, container string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
		if s.Rest == nil {
			return -1, errors.New("the platform has no connection to exec in pods")
		}
		opts := &corev1.PodExecOptions{Container: container, Command: cmd, Stdout: true, Stderr: true, Stdin: stdin != nil}
		req := s.K.Client.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(pod).
			SubResource("exec").VersionedParams(opts, scheme.ParameterCodec)
		ex, err := remotecommand.NewSPDYExecutor(s.Rest, "POST", req.URL())
		if err != nil {
			return -1, err
		}
		err = ex.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin: stdin, Stdout: stdout, Stderr: stderr})
		var exit utilexec.ExitError
		switch {
		case err == nil:
			return 0, nil
		case errors.As(err, &exit):
			return exit.ExitStatus(), nil
		}
		return -1, err
	}
}

// script runs one of the platform's scripts in the git container, its
// stdout to w: a message, with the end of its stderr, when it failed.
func (s *Sandboxes) script(ctx context.Context, pod string, cmd []string, stdin io.Reader, w io.Writer) (string, error) {
	errOut := &tailBuffer{max: 4 << 10}
	code, err := s.execFn()(ctx, s.K.Namespace, pod, "git", cmd, stdin, w, errOut)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return fmt.Sprintf("exit %d: %s", code, strings.TrimSpace(errOut.String())), nil
	}
	return "", nil
}

// sandboxTar packs the caller's changes for the sync script: files/<path>
// for each changed file, and removed, one path a line.
func sandboxTar(files map[string][]byte, removed []string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	clean := func(p string) (string, error) {
		c := path.Clean(p)
		if p == "" || path.IsAbs(c) || c == ".." || strings.HasPrefix(c, "../") || c == ".git" || strings.HasPrefix(c, ".git/") || strings.Contains(c, "\n") {
			return "", fmt.Errorf("not a file of the repository: %q", p)
		}
		return c, nil
	}
	for _, p := range names {
		c, err := clean(p)
		if err != nil {
			return nil, err
		}
		if err := tw.WriteHeader(&tar.Header{Name: "files/" + c, Mode: 0o644, Size: int64(len(files[p])), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(files[p]); err != nil {
			return nil, err
		}
	}
	var gone []string
	for _, p := range removed {
		c, err := clean(p)
		if err != nil {
			return nil, err
		}
		gone = append(gone, c)
	}
	list := []byte(strings.Join(gone, "\n"))
	if len(gone) > 0 {
		list = append(list, '\n')
	}
	if err := tw.WriteHeader(&tar.Header{Name: "removed", Mode: 0o644, Size: int64(len(list)), Typeflag: tar.TypeReg}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(list); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// tailBuffer keeps the last max bytes written, and counts them all.
type tailBuffer struct {
	max   int
	buf   []byte
	total int64
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.total += int64(len(p))
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2*t.max {
		t.buf = append([]byte{}, t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

// String is the end it kept.
func (t *tailBuffer) String() string {
	if len(t.buf) > t.max {
		return string(t.buf[len(t.buf)-t.max:])
	}
	return string(t.buf)
}
