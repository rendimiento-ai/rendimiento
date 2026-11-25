package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// A sandbox's cache (Ruta 178): one volume per app, sandbox-cache-<app>,
// that only that app's sandbox pods mount, read-write, at /cache: Go's
// modules and build cache, npm's, pnpm's, yarn's and pip's. Without it
// every encargo started cold, downloading and compiling every module on two
// cores, and a first go test of the platform ran out of its 15 minutes.
//
// It is never a CI cache, and CI never sees it: a command the model chose
// may write anything there (a module's source changed in place, a build
// cache entry), so it stays with the sandboxes, which run only the model's
// commands anyway. CI's caches (cache-<app>-<step>) go the other way, once:
// when an app's sandbox cache is new, a short seed pod mounts the matching
// CI cache read-only and copies its Go modules and npm cache in (Go checks
// modules against go.sum when it downloads them; a copy of CI's is what CI
// would have downloaded). Nothing is ever copied from a sandbox to CI.
//
// The volume is ReadWriteOnce, and the coder never works on two encargos of
// one app at once (agentes.Pick); should a second sandbox of the app start
// while one holds it, the second gets an empty folder instead, never the
// same volume. The janitor removes a cache unused for CacheIdle (14 days),
// and deleting the app removes it (KubeExecutor.DeleteCaches).

// SandboxCacheLabel marks an app's sandbox cache; its value is the app.
// CacheAppLabel (CI's caches) is never on it, so nothing of CI lists it.
const (
	SandboxCacheLabel = "rendimiento.ai/sandbox-cache-of"
	// sandboxCacheUsed is when a sandbox last mounted it (RFC 3339).
	sandboxCacheUsed = "rendimiento.ai/used-at"
	// sandboxSeedLabel marks a seed pod (beside SandboxLabel, so
	// isolate-sandbox holds it too).
	sandboxSeedLabel = "rendimiento.ai/sandbox-seed"
)

// SandboxCacheName is the app's sandbox cache volume. CI's are cache-...,
// so the names never meet.
func SandboxCacheName(app string) string {
	n := nonName.ReplaceAllString(strings.ToLower("sandbox-cache-"+app), "-")
	if len(n) > 63 {
		n = n[:63]
	}
	return strings.TrimRight(n, "-")
}

// CacheVolumes are the CI cache volumes a test or task step keeps (its
// first two slots), the ones a sandbox cache is seeded from.
func CacheVolumes(app, stepID string) []string {
	return []string{cacheName(Step{App: app, ID: stepID}), cacheName(Step{App: app, ID: stepID, CacheSlot: 1})}
}

// sandboxCacheEnv points the usual tool caches at /cache, as a step's
// cacheEnv does, and pnpm's and yarn's too.
var sandboxCacheEnv = append(append([]corev1.EnvVar{}, cacheEnv...),
	corev1.EnvVar{Name: "npm_config_store_dir", Value: "/cache/pnpm-store"},
	corev1.EnvVar{Name: "YARN_CACHE_FOLDER", Value: "/cache/yarn"},
)

// sandboxDatabaseURL is TEST_DATABASE_URL in a sandbox with Postgres: the
// steps' address with synchronous_commit=off, as hack/ci-test.sh sets it
// (a throwaway database need not wait for its disk at each commit).
func sandboxDatabaseURL() string {
	return withSyncCommitOff(testDatabaseURL)
}

// withSyncCommitOff adds synchronous_commit=off to a postgres:// address
// that does not say it, as hack/ci-test.sh does.
func withSyncCommitOff(u string) string {
	switch {
	case strings.Contains(u, "synchronous_commit="):
		return u
	case !strings.HasPrefix(u, "postgres://") && !strings.HasPrefix(u, "postgresql://"):
		return u
	case strings.Contains(u, "?"):
		return u + "&synchronous_commit=off"
	}
	return u + "?synchronous_commit=off"
}

// postgresUID is the user the sandbox's Postgres runs as: postgres in the
// alpine image (70); the Debian images fake a passwd entry for any user.
const postgresUID = 70

// sandboxPostgres is the steps' throwaway Postgres (testPostgres), as a
// sandbox has it: not root, nothing privileged, its data in a sized
// emptyDir of its own, and a CPU request a database can work with (a model
// runs whole suites against it, not one step's).
func (s *Sandboxes) sandboxPostgres() corev1.Container {
	c := s.K.testPostgres()
	c.Env = append(c.Env, corev1.EnvVar{Name: "PGDATA", Value: "/var/lib/sandbox-postgres/data"})
	c.VolumeMounts = []corev1.VolumeMount{{Name: "postgres", MountPath: "/var/lib/sandbox-postgres"}}
	c.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")},
	}
	c.SecurityContext = &corev1.SecurityContext{
		Privileged:               ptr(false),
		AllowPrivilegeEscalation: ptr(false),
		RunAsNonRoot:             ptr(true),
		RunAsUser:                ptr(int64(postgresUID)),
		RunAsGroup:               ptr(int64(postgresUID)),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
	return c
}

// cacheOn says whether sandboxes keep a cache (CacheSize not "0" or "off").
func (s *Sandboxes) cacheOn() bool {
	return s.CacheSize != "0" && s.CacheSize != "off"
}

// appLock is held while an app's sandbox checks that nobody has its cache
// and creates its pod, so two of this process never both take it.
func (s *Sandboxes) appLock(app string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appMu == nil {
		s.appMu = map[string]*sync.Mutex{}
	}
	m := s.appMu[app]
	if m == nil {
		m = &sync.Mutex{}
		s.appMu[app] = m
	}
	return m
}

// ensureSandboxCache creates the app's sandbox cache when it has none, and
// says whether it was created now.
func (s *Sandboxes) ensureSandboxCache(ctx context.Context, app string) (bool, error) {
	k := s.K
	name := SandboxCacheName(app)
	pvcs := k.Client.CoreV1().PersistentVolumeClaims(k.Namespace)
	if _, err := pvcs.Get(ctx, name, metav1.GetOptions{}); err == nil {
		return false, nil
	} else if !apierrors.IsNotFound(err) {
		return false, err
	}
	size, err := resource.ParseQuantity(s.CacheSize)
	if err != nil {
		return false, fmt.Errorf("sandbox cache size %q: %v", s.CacheSize, err)
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: k.Namespace,
			Labels:      map[string]string{"app.kubernetes.io/managed-by": "rendimiento", SandboxCacheLabel: nonName.ReplaceAllString(app, "-")},
			Annotations: map[string]string{sandboxCacheUsed: time.Now().UTC().Format(time.RFC3339)}},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: size}},
		},
	}
	if k.CacheStorageClass != "" {
		pvc.Spec.StorageClassName = ptr(k.CacheStorageClass)
	}
	created := true
	err = transientRetry(ctx, func() error {
		_, err := pvcs.Create(ctx, pvc, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			created = false
			return nil
		}
		return err
	})
	return created, err
}

// touchSandboxCache notes that a sandbox mounts the cache now, for the
// janitor's idle count.
func (s *Sandboxes) touchSandboxCache(ctx context.Context, name string) {
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{
		sandboxCacheUsed: time.Now().UTC().Format(time.RFC3339)}}})
	_, _ = s.K.Client.CoreV1().PersistentVolumeClaims(s.K.Namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
}

// claimUsers are the pods that have the volume in their spec and have not
// ended (scheduled or not), and whether each is being deleted.
func (s *Sandboxes) claimUsers(ctx context.Context, claim string) (live, leaving []string, err error) {
	l, err := s.K.Client.CoreV1().Pods(s.K.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	for _, p := range l.Items {
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == claim {
				if p.DeletionTimestamp != nil {
					leaving = append(leaving, p.Name)
				} else {
					live = append(live, p.Name)
				}
				break
			}
		}
	}
	return live, leaving, nil
}

// cacheReleaseWait is how long a new sandbox waits for an earlier one being
// removed (its own, after a command ran out of time) to let the cache go.
var cacheReleaseWait = time.Minute

// cacheFree waits for pods being deleted to let the volume go, up to
// cacheReleaseWait, and says whether no pod has it; a live pod that has it
// answers false at once (the caller then goes without it).
func (s *Sandboxes) cacheFree(ctx context.Context, claim string) (bool, string) {
	deadline := time.Now().Add(cacheReleaseWait)
	for {
		live, leaving, err := s.claimUsers(ctx, claim)
		switch {
		case err != nil:
			return false, "could not list who uses it: " + err.Error()
		case len(live) > 0:
			return false, "another sandbox (" + strings.Join(live, ", ") + ") is using it"
		case len(leaving) == 0:
			return true, ""
		case !time.Now().Before(deadline):
			return false, "an earlier sandbox (" + strings.Join(leaving, ", ") + ") has not let it go yet"
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err().Error()
		case <-time.After(2 * time.Second):
		}
	}
}

// sandboxSeedScript copies CI's Go modules and npm cache ($1, mounted
// read-only) into a new sandbox cache ($2): each folder whole into a
// scratch folder beside it first, then renamed into place, so a copy cut
// short leaves nothing half there. A folder the cache already has is left as it is.
// It never writes to $1 (the mount refuses it anyway).
const sandboxSeedScript = `set -u
from=$1; to=$2
for d in go-mod npm; do
  tmp=$to/.$d.seeding
  chmod -R u+w "$tmp" 2>/dev/null; rm -rf "$tmp"
  if [ -d "$from/$d" ] && [ ! -e "$to/$d" ]; then
    if cp -R "$from/$d" "$tmp" && mv "$tmp" "$to/$d"; then
      echo "seeded $d: $(du -sh "$to/$d" 2>/dev/null | cut -f1)"
    else
      echo "could not copy $d"
    fi
  fi
  chmod -R u+w "$tmp" 2>/dev/null; rm -rf "$tmp"
done
exit 0
`

// seedPod is the short pod that seeds a sandbox cache: CI's cache mounted
// read-only (the volume and the mount both say so), the sandbox cache
// read-write, no network (isolate-sandbox holds it), hardened as a
// sandbox, and gone after deadline.
func (s *Sandboxes) seedPod(name string, labels map[string]string, ci, claim string, deadline time.Duration) *corev1.Pod {
	k := s.K
	life := int64(deadline.Seconds())
	grace := int64(1)
	small := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("256Mi")},
	}
	l := map[string]string{}
	for key, v := range labels {
		l[key] = v
	}
	l[sandboxSeedLabel] = "true"
	onRoot := corev1.FSGroupChangeOnRootMismatch
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: k.Namespace, Labels: l},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			Affinity:                      s.affinity(),
			ActiveDeadlineSeconds:         &life,
			TerminationGracePeriodSeconds: &grace,
			AutomountServiceAccountToken:  ptr(false),
			EnableServiceLinks:            ptr(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:        ptr(true),
				RunAsUser:           ptr(int64(sandboxUID)),
				RunAsGroup:          ptr(int64(sandboxUID)),
				FSGroup:             ptr(int64(sandboxUID)),
				FSGroupChangePolicy: &onRoot,
				SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name: "seed", Image: k.GitImage, Command: []string{"sh", "-c", sandboxSeedScript, "seed", "/ci", "/cache"},
				Env:          []corev1.EnvVar{{Name: "HOME", Value: "/tmp"}},
				VolumeMounts: []corev1.VolumeMount{{Name: "ci", MountPath: "/ci", ReadOnly: true}, {Name: "cache", MountPath: "/cache"}},
				Resources:    small,
				SecurityContext: &corev1.SecurityContext{
					Privileged: ptr(false), AllowPrivilegeEscalation: ptr(false), RunAsNonRoot: ptr(true),
					RunAsUser: ptr(int64(sandboxUID)), RunAsGroup: ptr(int64(sandboxUID)),
					Capabilities:   &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: "ci", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: ci, ReadOnly: true}}},
				{Name: "cache", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim}}},
			},
		},
	}
}

// seed copies the first of the CI caches that exists and no pod is using
// into the app's new sandbox cache, within SeedWait; it says what it did.
// It never waits for a CI cache in use: the sandbox goes on without it.
func (s *Sandboxes) seed(ctx context.Context, labels map[string]string, claim string, from []string) string {
	k := s.K
	pvcs := k.Client.CoreV1().PersistentVolumeClaims(k.Namespace)
	var notes []string
	for _, ci := range from {
		if ci == "" || ci == claim {
			continue
		}
		if _, err := pvcs.Get(ctx, ci, metav1.GetOptions{}); err != nil {
			continue
		}
		if live, leaving, err := s.claimUsers(ctx, ci); err != nil || len(live)+len(leaving) > 0 {
			notes = append(notes, ci+" is in use")
			continue
		}
		start := time.Now()
		name := "sbx-seed-" + SandboxKey(labels[SandboxLabel]) + "-" + randSuffix()
		pods := k.Client.CoreV1().Pods(k.Namespace)
		if err := k.createPod(ctx, s.seedPod(name, labels, ci, claim, s.SeedWait)); err != nil {
			return "not seeded: " + err.Error()
		}
		phase, log := s.waitSeed(ctx, name)
		zero := int64(0)
		_ = pods.Delete(context.WithoutCancel(ctx), name, metav1.DeleteOptions{GracePeriodSeconds: &zero})
		s.waitGone(context.WithoutCancel(ctx), name, 30*time.Second)
		took := time.Since(start).Round(time.Second)
		if phase != corev1.PodSucceeded {
			s.Log.Info("sandbox: seed did not finish", "from", ci, "to", claim, "phase", phase, "log", log)
			return fmt.Sprintf("not seeded from %s: it did not finish in %s", ci, took)
		}
		s.Log.Info("sandbox: cache seeded", "from", ci, "to", claim, "took", took)
		return fmt.Sprintf("seeded from CI's %s in %s %s", ci, took, strings.Join(strings.Fields(log), " "))
	}
	if len(notes) > 0 {
		return "not seeded: " + strings.Join(notes, ", ")
	}
	return ""
}

// waitSeed waits for the seed pod to end, up to SeedWait, and answers its
// phase and the end of its log.
func (s *Sandboxes) waitSeed(ctx context.Context, name string) (corev1.PodPhase, string) {
	k := s.K
	wctx, cancel := context.WithTimeout(ctx, s.SeedWait)
	defer cancel()
	for {
		p, err := k.Client.CoreV1().Pods(k.Namespace).Get(wctx, name, metav1.GetOptions{})
		if err == nil && (p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed) {
			var log bytes.Buffer
			lctx, lcancel := context.WithTimeout(ctx, 15*time.Second)
			_ = k.streamLogs(lctx, name, "seed", &limited{w: &log, n: 2 << 10})
			lcancel()
			return p.Status.Phase, log.String()
		}
		select {
		case <-wctx.Done():
			return corev1.PodPending, ""
		case <-time.After(2 * time.Second):
		}
	}
}

// waitGone waits up to wait for a pod to be gone.
func (s *Sandboxes) waitGone(ctx context.Context, name string, wait time.Duration) {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if _, err := s.K.Client.CoreV1().Pods(s.K.Namespace).Get(ctx, name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// takeCache gets the app's sandbox cache ready for a new sandbox pod: made
// (and seeded) the first time, then only if no other pod has it. It answers
// the volume to mount ("" for an empty folder instead) and what happened.
// The caller holds appLock(app) until its pod is created.
func (s *Sandboxes) takeCache(ctx context.Context, req SandboxRun, labels map[string]string) (string, string) {
	claim := SandboxCacheName(req.App)
	created, err := s.ensureSandboxCache(ctx, req.App)
	if err != nil {
		s.Log.Warn("sandbox: no cache", "app", req.App, "err", err)
		return "", "none (an empty folder): the cache could not be made: " + err.Error()
	}
	if free, why := s.cacheFree(ctx, claim); !free {
		return "", "none (an empty folder): " + why
	}
	note := "the app's sandbox cache (kept between encargos)"
	if created {
		note = "the app's sandbox cache, new"
		if seeded := s.seed(ctx, labels, claim, req.SeedFrom); seeded != "" {
			note += "; " + seeded
		}
		if free, why := s.cacheFree(ctx, claim); !free {
			return "", "none (an empty folder): " + why
		}
	}
	s.touchSandboxCache(ctx, claim)
	return claim, note
}

// sweepCaches removes the sandbox caches no pod has used for CacheIdle.
func (s *Sandboxes) sweepCaches(ctx context.Context, now time.Time) {
	pvcs := s.K.Client.CoreV1().PersistentVolumeClaims(s.K.Namespace)
	l, err := pvcs.List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/managed-by=rendimiento," + SandboxCacheLabel})
	if err != nil {
		return
	}
	for _, c := range l.Items {
		used := c.CreationTimestamp.Time
		if t, err := time.Parse(time.RFC3339, c.Annotations[sandboxCacheUsed]); err == nil && t.After(used) {
			used = t
		}
		if now.Sub(used) <= s.CacheIdle {
			continue
		}
		if live, leaving, err := s.claimUsers(ctx, c.Name); err != nil || len(live)+len(leaving) > 0 {
			continue
		}
		s.Log.Info("sandbox: cache idle, removed", "cache", c.Name, "used", used)
		_ = pvcs.Delete(ctx, c.Name, metav1.DeleteOptions{})
	}
}
