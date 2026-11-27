package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A pool with a daemon on a node «a ratos» of the other architecture: images
// keep building on the cluster's own daemons, and the other one is offered
// only for the other half.
func TestBuildkitPorArquitectura(t *testing.T) {
	daemon := func(name, node string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "devops-tools"}, Spec: corev1.PodSpec{NodeName: node}}
	}
	nodo := func(name, arch string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"kubernetes.io/arch": arch}}}
	}
	otra := archOtra(archPrincipal)
	cs := fake.NewSimpleClientset(daemon("buildkitd-0", "worker1"), daemon("buildkitd-1", "worker2"), daemon("buildkitd-amd64-0", "p0dxd"),
		nodo("worker1", archPrincipal), nodo("worker2", archPrincipal), nodo("p0dxd", otra))
	k := &KubeExecutor{Client: cs, BuildkitPool: "buildkitd-pool.devops-tools.svc.cluster.local",
		Resolver: fakeSRV{targets: []string{"buildkitd-0", "buildkitd-1", "buildkitd-amd64-0"}}}
	ctx := context.Background()
	for _, key := range []string{"reg/a", "reg/b", "reg/c", "reg/d", "reg/e", "reg/f"} {
		if _, d := k.buildkitFor(ctx, key); d == "buildkitd-amd64-0" {
			t.Fatalf("%s builds its main half on the node «a ratos»", key)
		}
		if _, d := k.buildkitOtro(ctx, key); d != "buildkitd-amd64-0" {
			t.Fatalf("%s: its other half goes to %q", key, d)
		}
	}
	// With the node «a ratos» off (its daemon out of the SRV records): no other half.
	k.Resolver = fakeSRV{targets: []string{"buildkitd-0", "buildkitd-1"}}
	if addr, _ := k.buildkitOtro(ctx, "reg/a"); addr != "" {
		t.Fatalf("an off node «a ratos» was offered: %s", addr)
	}
}

func TestPartirImagen(t *testing.T) {
	for in, want := range map[string][3]string{
		"registry.cube.local:5000/app-web:0123456789ab": {"registry.cube.local:5000", "app-web", "0123456789ab"},
		"registry.cube.local:5000/app-web@sha256:abc":   {"registry.cube.local:5000", "app-web", "sha256:abc"},
		"registry.example.lan/a/b":                      {"registry.example.lan", "a/b", "latest"},
	} {
		h, r, ref, ok := partirImagen(in)
		if !ok || [3]string{h, r, ref} != want {
			t.Fatalf("%s: %s %s %s %v", in, h, r, ref, ok)
		}
	}
	if _, _, _, ok := partirImagen("postgres:16"); ok {
		t.Fatal("a public image was taken for a registry's")
	}
}

// registroFalso serves manifests by digest or tag and keeps what is PUT.
type registroFalso struct {
	mu        sync.Mutex
	manifests map[string]struct{ mt, body string }
	puesto    string
	tipo      string
}

func (f *registroFalso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ref := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	switch r.Method {
	case http.MethodGet:
		m, ok := f.manifests[ref]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", m.mt)
		io.WriteString(w, m.body)
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		f.puesto, f.tipo = string(b), r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusCreated)
	}
}

// The two halves a build pushed become one OCI index at its tag: the main
// half as it is, the other taken from the index BuildKit wrote for it.
func TestUnirArquitecturas(t *testing.T) {
	otra := archOtra(archPrincipal)
	f := &registroFalso{manifests: map[string]struct{ mt, body string }{
		"sha256:aaa": {mtDockerManif, `{"schemaVersion":2,"mediaType":"` + mtDockerManif + `"}`},
		"sha256:bbb": {mtOCIIndex, `{"schemaVersion":2,"manifests":[{"mediaType":"` + mtOCIManifest + `","digest":"sha256:ccc","size":512,"platform":{"architecture":"` + otra + `","os":"linux"}},{"mediaType":"` + mtOCIManifest + `","digest":"sha256:ddd","size":100,"platform":{"architecture":"unknown","os":"unknown"}}]}`},
	}}
	srv := httptest.NewTLSServer(f)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	k := &KubeExecutor{InsecureRegistry: true, httpRegistro: srv.Client()}
	got, err := k.unirArquitecturas(context.Background(), host+"/app-web", "0123456789ab", map[string]string{archPrincipal: "sha256:aaa", otra: "sha256:bbb"})
	if err != nil {
		t.Fatal(err)
	}
	var ix indice
	if err := json.Unmarshal([]byte(f.puesto), &ix); err != nil || f.tipo != mtOCIIndex || len(ix.Manifests) != 2 {
		t.Fatalf("the index: %s (%s) %v", f.puesto, f.tipo, err)
	}
	if ix.Manifests[0].Digest != "sha256:aaa" || ix.Manifests[0].Platform.Architecture != archPrincipal ||
		ix.Manifests[1].Digest != "sha256:ccc" || ix.Manifests[1].Platform.Architecture != otra || ix.Manifests[1].Size != 512 {
		t.Fatalf("its manifests: %+v", ix.Manifests)
	}
	sum := sha256.Sum256([]byte(f.puesto))
	if got != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("its digest: %s", got)
	}
}

// A test pod may go to a node «a ratos» only when every image it runs has
// that architecture: an image of the platform's registry is asked, a public
// one is taken to have it.
func TestARatos(t *testing.T) {
	otra := archOtra(archPrincipal)
	f := &registroFalso{manifests: map[string]struct{ mt, body string }{
		"dos": {mtOCIIndex, `{"schemaVersion":2,"manifests":[{"digest":"sha256:1","platform":{"architecture":"` + archPrincipal + `","os":"linux"}},{"digest":"sha256:2","platform":{"architecture":"` + otra + `","os":"linux"}}]}`},
		"una": {mtDockerManif, `{"schemaVersion":2}`},
	}}
	srv := httptest.NewTLSServer(f)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	k := &KubeExecutor{Registry: host, InsecureRegistry: true, httpRegistro: srv.Client()}
	pod := func(img string) *corev1.Pod {
		return &corev1.Pod{Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Image: "alpine/git:2.47"}}, Containers: []corev1.Container{{Image: img}}}}
	}
	ctx := context.Background()
	p := pod(host + "/app-test:dos")
	if !k.aRatos(ctx, p) || len(p.Spec.Tolerations) != 1 || p.Spec.Tolerations[0].Key != TaintARatos ||
		p.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].Preference.MatchExpressions[0].Key != LabelConstruye {
		t.Fatalf("an image with both architectures: %+v", p.Spec)
	}
	if p := pod(host + "/app-test:una"); k.aRatos(ctx, p) || len(p.Spec.Tolerations) != 0 {
		t.Fatal("an image of one architecture went to a node «a ratos»")
	}
	if p := pod("node:22"); !k.aRatos(ctx, p) {
		t.Fatal("a public image was refused")
	}
}

// A pod ended by its node's shutdown is told apart, and its second try is
// kept off that node, whatever affinity it had.
func TestSeApago(t *testing.T) {
	p := &corev1.Pod{Spec: corev1.PodSpec{NodeName: "p0dxd"}, Status: corev1.PodStatus{Reason: "Terminated", Message: "Pod was terminated in response to imminent node shutdown."}}
	if !seApagoBajo(p) {
		t.Fatal("a node shutdown was not seen")
	}
	if seApagoBajo(&corev1.Pod{Spec: corev1.PodSpec{NodeName: "worker1"}, Status: corev1.PodStatus{Reason: "Evicted"}}) {
		t.Fatal("an eviction was taken for a shutdown")
	}
	cs := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "p0dxd", Labels: map[string]string{TaintARatos: "true"}}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker1"}})
	k := &KubeExecutor{Client: cs}
	if !k.esARatos(context.Background(), "p0dxd") || k.esARatos(context.Background(), "worker1") {
		t.Fatal("which nodes are «a ratos»")
	}
	pod := &corev1.Pod{}
	evitarNodos(pod, []string{"p0dxd"})
	e := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0]
	if e.Operator != corev1.NodeSelectorOpNotIn || e.Values[0] != "p0dxd" {
		t.Fatalf("the second try: %+v", e)
	}
}

// A node labelled rendimiento.ai/construye=no takes no CI: its BuildKit
// daemon is left out of the pool, whatever its architecture.
func TestNodoSinCI(t *testing.T) {
	daemon := func(name, node string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "devops-tools"}, Spec: corev1.PodSpec{NodeName: node}}
	}
	nodo := func(name string, labels map[string]string) *corev1.Node {
		labels["kubernetes.io/arch"] = archPrincipal
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	}
	cs := fake.NewSimpleClientset(daemon("buildkitd-0", "node-a"), daemon("buildkitd-1", "gpu-node"),
		nodo("node-a", map[string]string{}), nodo("gpu-node", map[string]string{LabelConstruye: SinCI}))
	k := &KubeExecutor{Client: cs, BuildkitPool: "buildkitd-pool.devops-tools.svc.cluster.local",
		Resolver: fakeSRV{targets: []string{"buildkitd-0", "buildkitd-1"}}}
	for _, key := range []string{"reg/a", "reg/b", "reg/c", "reg/d", "reg/e", "reg/f"} {
		if _, d := k.buildkitFor(context.Background(), key); d != "buildkitd-0" {
			t.Fatalf("%s builds on %q, a node kept out of CI", key, d)
		}
	}
}
