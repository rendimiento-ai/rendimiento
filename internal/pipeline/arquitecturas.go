package pipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ---- nodes «a ratos» and images for two architectures (decision 196) ----
//
// The cluster's nodes are of one architecture (the platform's own, arm64 at
// home). A node «a ratos» is on only sometimes and may be of another (a
// desktop, amd64): it carries the taint TaintARatos, so nothing lands on it
// but what tolerates it. When a BuildKit daemon of the pool runs on one, a
// build also makes its other half there, and the two become one image (an
// OCI index), so the image runs on any node. A test step whose images all
// have that architecture may run there too, preferring it. When it is off,
// everything is as before: one architecture, nothing waits for it.

// TaintARatos is the taint (and label) of a node that is on only sometimes,
// and LabelConstruye the label of the ones that take CI: "si" prefers a node
// for it, SinCI keeps it out (its pods and its BuildKit daemons).
const (
	TaintARatos    = "rendimiento.ai/a-ratos"
	LabelConstruye = "rendimiento.ai/construye"
	SinCI          = "no"
)

// archPrincipal is the cluster's architecture: the platform's own.
var archPrincipal = runtime.GOARCH

// nodeLabels are a node's labels, kept a while; nil when it cannot tell.
func (k *KubeExecutor) nodeLabels(ctx context.Context, node string) map[string]string {
	if node == "" || k.Client == nil {
		return nil
	}
	k.archMu.Lock()
	if l, ok := k.archDe[node]; ok {
		k.archMu.Unlock()
		return l
	}
	k.archMu.Unlock()
	n, err := k.Client.CoreV1().Nodes().Get(ctx, node, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	l := n.Labels
	if l == nil {
		l = map[string]string{}
	}
	k.archMu.Lock()
	if k.archDe == nil {
		k.archDe = map[string]map[string]string{}
	}
	k.archDe[node] = l
	k.archMu.Unlock()
	return l
}

// nodeArch is a node's architecture (its kubernetes.io/arch label); "" when
// it cannot tell.
func (k *KubeExecutor) nodeArch(ctx context.Context, node string) string {
	return k.nodeLabels(ctx, node)["kubernetes.io/arch"]
}

// sinCI says whether a pool daemon runs on a node kept out of CI.
func (k *KubeExecutor) sinCI(ctx context.Context, target string) bool {
	return k.nodeLabels(ctx, k.daemonNode(ctx, target))[LabelConstruye] == SinCI
}

// deArch says whether a pool daemon runs on a node of arch: a daemon whose
// node cannot be told counts as the cluster's own architecture, as before.
func (k *KubeExecutor) deArch(ctx context.Context, target, arch string) bool {
	a := k.nodeArch(ctx, k.daemonNode(ctx, target))
	if a == "" {
		a = archPrincipal
	}
	return a == arch
}

// ---- the registry: reading manifests and writing the index ----

// mediaTypes a manifest may come back as.
const (
	mtOCIIndex     = "application/vnd.oci.image.index.v1+json"
	mtOCIManifest  = "application/vnd.oci.image.manifest.v1+json"
	mtDockerList   = "application/vnd.docker.distribution.manifest.list.v2+json"
	mtDockerManif  = "application/vnd.docker.distribution.manifest.v2+json"
	aceptaManifest = mtOCIIndex + ", " + mtOCIManifest + ", " + mtDockerList + ", " + mtDockerManif
)

type plataforma struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
}

type descriptor struct {
	MediaType string      `json:"mediaType"`
	Digest    string      `json:"digest"`
	Size      int64       `json:"size"`
	Platform  *plataforma `json:"platform,omitempty"`
}

type indice struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Manifests     []descriptor `json:"manifests"`
}

// partirImagen splits host/repo[:tag|@digest] into host, repo and reference.
func partirImagen(image string) (host, repo, ref string, ok bool) {
	host, rest, found := strings.Cut(image, "/")
	if !found || !strings.ContainsAny(host, ".:") {
		return "", "", "", false
	}
	if r, d, isDigest := strings.Cut(rest, "@"); isDigest {
		return host, r, d, true
	}
	if i := strings.LastIndex(rest, ":"); i > 0 && !strings.Contains(rest[i:], "/") {
		return host, rest[:i], rest[i+1:], true
	}
	return host, rest, "latest", true
}

func (k *KubeExecutor) registroHTTP() *http.Client {
	if k.httpRegistro != nil {
		return k.httpRegistro
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if k.InsecureRegistry {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // the home registry's self-signed certificate (REGISTRY_INSECURE)
	}
	return &http.Client{Transport: tr, Timeout: 30 * time.Second}
}

// manifiesto reads a manifest (or index) of the registry: its media type,
// its bytes and their digest.
func (k *KubeExecutor) manifiesto(ctx context.Context, host, repo, ref string) (string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://%s/v2/%s/manifests/%s", host, repo, ref), nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", aceptaManifest)
	res, err := k.registroHTTP().Do(req)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return "", nil, err
	}
	if res.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("the registry answered %s for %s/%s@%s", res.Status, host, repo, ref)
	}
	mt := strings.TrimSpace(strings.Split(res.Header.Get("Content-Type"), ";")[0])
	if mt == "" {
		var m struct {
			MediaType string `json:"mediaType"`
		}
		_ = json.Unmarshal(body, &m)
		mt = m.MediaType
	}
	return mt, body, nil
}

// descriptorDe is the image manifest of arch within what a build pushed:
// the manifest itself, or its entry in the index BuildKit wrote (one that
// carries attestations, say).
func (k *KubeExecutor) descriptorDe(ctx context.Context, host, repo, digest, arch string) (descriptor, error) {
	mt, body, err := k.manifiesto(ctx, host, repo, digest)
	if err != nil {
		return descriptor{}, err
	}
	switch mt {
	case mtOCIIndex, mtDockerList:
		var ix indice
		if err := json.Unmarshal(body, &ix); err != nil {
			return descriptor{}, err
		}
		for _, m := range ix.Manifests {
			if m.Platform != nil && m.Platform.Architecture == arch {
				return m, nil
			}
		}
		return descriptor{}, fmt.Errorf("%s has no %s manifest", digest, arch)
	default:
		return descriptor{MediaType: mt, Digest: digest, Size: int64(len(body)), Platform: &plataforma{Architecture: arch, OS: "linux"}}, nil
	}
}

// archDeImagen is the architecture an image manifest was built for (its
// config's), "" when it cannot tell or the digest is an index.
func (k *KubeExecutor) archDeImagen(ctx context.Context, image, digest string) string {
	host, repo, _, ok := partirImagen(image)
	if !ok {
		return ""
	}
	mt, body, err := k.manifiesto(ctx, host, repo, digest)
	if err != nil || mt == mtOCIIndex || mt == mtDockerList {
		return ""
	}
	var m struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
	}
	if json.Unmarshal(body, &m) != nil || m.Config.Digest == "" {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://%s/v2/%s/blobs/%s", host, repo, m.Config.Digest), nil)
	if err != nil {
		return ""
	}
	res, err := k.registroHTTP().Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	var c struct {
		Architecture string `json:"architecture"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&c) != nil {
		return ""
	}
	return c.Architecture
}

// unirArquitecturas writes, at image:tag, one OCI index of the two halves a
// build pushed (by their digests), and returns its digest.
func (k *KubeExecutor) unirArquitecturas(ctx context.Context, image, tag string, mitades map[string]string) (string, error) {
	host, repo, _, ok := partirImagen(image)
	if !ok {
		return "", fmt.Errorf("%s is not an image of a registry", image)
	}
	ix := indice{SchemaVersion: 2, MediaType: mtOCIIndex}
	for _, arch := range []string{archPrincipal, archOtra(archPrincipal)} {
		d, ok := mitades[arch]
		if !ok {
			continue
		}
		desc, err := k.descriptorDe(ctx, host, repo, d, arch)
		if err != nil {
			return "", err
		}
		ix.Manifests = append(ix.Manifests, desc)
	}
	body, err := json.Marshal(ix)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("https://%s/v2/%s/manifests/%s", host, repo, tag), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mtOCIIndex)
	res, err := k.registroHTTP().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return "", fmt.Errorf("the registry refused the index: %s %s", res.Status, strings.TrimSpace(string(msg)))
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// archOtra is the other architecture of a build: amd64 for arm64 clusters,
// arm64 for amd64 ones.
func archOtra(arch string) string {
	if arch == "amd64" {
		return "arm64"
	}
	return "amd64"
}

// ---- test steps on a node «a ratos» ----

// archsCache keeps which images have the other architecture: a digest's
// answer never changes, a tag's is asked again after a while.
type archsCache struct {
	mu sync.Mutex
	de map[string]archsVisto
}

type archsVisto struct {
	tiene bool
	at    time.Time
}

// tieneOtra says whether an image has the other architecture. An image of
// this platform's registry is asked; any other (a public image, multi-arch
// as a rule) is taken to have it.
func (k *KubeExecutor) tieneOtra(ctx context.Context, image string) bool {
	host, repo, ref, ok := partirImagen(image)
	if !ok || k.Registry == "" || host != k.Registry {
		return true
	}
	k.archs.mu.Lock()
	if v, ok := k.archs.de[image]; ok && (strings.HasPrefix(ref, "sha256:") || time.Since(v.at) < 10*time.Minute) {
		k.archs.mu.Unlock()
		return v.tiene
	}
	k.archs.mu.Unlock()
	tiene := false
	if mt, body, err := k.manifiesto(ctx, host, repo, ref); err == nil && (mt == mtOCIIndex || mt == mtDockerList) {
		var ix indice
		if json.Unmarshal(body, &ix) == nil {
			for _, m := range ix.Manifests {
				tiene = tiene || (m.Platform != nil && m.Platform.Architecture == archOtra(archPrincipal))
			}
		}
	}
	k.archs.mu.Lock()
	if k.archs.de == nil {
		k.archs.de = map[string]archsVisto{}
	}
	k.archs.de[image] = archsVisto{tiene: tiene, at: time.Now()}
	k.archs.mu.Unlock()
	return tiene
}

// aRatos lets a test pod go to a node «a ratos», preferring it, when every
// image it runs has that node's architecture: it tolerates the taint, and
// the scheduler leans to the nodes labelled for CI. Without that, nothing
// changes: the taint keeps it off.
func (k *KubeExecutor) aRatos(ctx context.Context, pod *corev1.Pod) bool {
	for _, c := range append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...) {
		if !k.tieneOtra(ctx, c.Image) {
			return false
		}
	}
	pod.Spec.Tolerations = append(pod.Spec.Tolerations, corev1.Toleration{Key: TaintARatos, Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule})
	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &corev1.Affinity{}
	}
	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}
	na := pod.Spec.Affinity.NodeAffinity
	na.PreferredDuringSchedulingIgnoredDuringExecution = append(na.PreferredDuringSchedulingIgnoredDuringExecution, corev1.PreferredSchedulingTerm{
		Weight: 60,
		Preference: corev1.NodeSelectorTerm{MatchExpressions: []corev1.NodeSelectorRequirement{{
			Key: LabelConstruye, Operator: corev1.NodeSelectorOpIn, Values: []string{"si"},
		}}},
	})
	return true
}

// ---- a node «a ratos» turned off under a step ----

// seApagoBajo says whether a pod ended because its node shut down: the
// kubelet's graceful shutdown ends it with this reason and message.
func seApagoBajo(p *corev1.Pod) bool {
	if p == nil || p.Spec.NodeName == "" {
		return false
	}
	r, m := p.Status.Reason, strings.ToLower(p.Status.Message)
	return r == "Shutdown" || r == "NodeShutdown" || (r == "Terminated" && strings.Contains(m, "shutdown"))
}

// esARatos says whether a node is one «a ratos» (its label).
func (k *KubeExecutor) esARatos(ctx context.Context, node string) bool {
	if node == "" || k.Client == nil {
		return false
	}
	n, err := k.Client.CoreV1().Nodes().Get(ctx, node, metav1.GetOptions{})
	return err == nil && n.Labels[TaintARatos] == "true"
}

// evitarNodos keeps a pod off some nodes, on top of its affinity.
func evitarNodos(pod *corev1.Pod, nodos []string) {
	if len(nodos) == 0 {
		return
	}
	req := corev1.NodeSelectorRequirement{Key: "kubernetes.io/hostname", Operator: corev1.NodeSelectorOpNotIn, Values: nodos}
	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &corev1.Affinity{}
	}
	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}
	na := pod.Spec.Affinity.NodeAffinity
	if na.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		na.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{}}}
	}
	terms := na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	for i := range terms {
		terms[i].MatchExpressions = append(terms[i].MatchExpressions, req)
	}
}
