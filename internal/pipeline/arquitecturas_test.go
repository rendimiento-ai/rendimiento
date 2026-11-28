package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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
	cs := fake.NewSimpleClientset(daemon("buildkitd-0", "worker1"), daemon("buildkitd-1", "worker2"), daemon("buildkitd-amd64-0", "desktop-node"),
		nodo("worker1", archPrincipal), nodo("worker2", archPrincipal), nodo("desktop-node", otra))
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
	p := &corev1.Pod{Spec: corev1.PodSpec{NodeName: "desktop-node"}, Status: corev1.PodStatus{Reason: "Terminated", Message: "Pod was terminated in response to imminent node shutdown."}}
	if !seApagoBajo(p) {
		t.Fatal("a node shutdown was not seen")
	}
	if seApagoBajo(&corev1.Pod{Spec: corev1.PodSpec{NodeName: "worker1"}, Status: corev1.PodStatus{Reason: "Evicted"}}) {
		t.Fatal("an eviction was taken for a shutdown")
	}
	cs := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "desktop-node", Labels: map[string]string{TaintARatos: "true"}}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker1"}})
	k := &KubeExecutor{Client: cs}
	if !k.esARatos(context.Background(), "desktop-node") || k.esARatos(context.Background(), "worker1") {
		t.Fatal("which nodes are «a ratos»")
	}
	pod := &corev1.Pod{}
	evitarNodos(pod, []string{"desktop-node"})
	e := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0]
	if e.Operator != corev1.NodeSelectorOpNotIn || e.Values[0] != "desktop-node" {
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

// A build's place is the node of the daemon at its BuildKit address.
func TestNodoDe(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "buildkitd-2", Namespace: "devops-tools"}, Spec: corev1.PodSpec{NodeName: "node-b"}})
	k := &KubeExecutor{Client: cs}
	if n := k.nodoDe(context.Background(), "tcp://buildkitd-2.buildkitd-pool.devops-tools.svc.cluster.local:1234"); n != "node-b" {
		t.Fatalf("node = %q", n)
	}
	if n := k.nodoDe(context.Background(), "tcp://buildkitd.example.lan:1234"); n != "" {
		t.Fatalf("an address outside the pool has a node: %q", n)
	}
}

// An image's architecture is its config's: a main half built by a daemon of
// the other architecture is told apart (run 1975's docs: amd64 under arm64's
// name), and an index says nothing.
func TestArchDeImagen(t *testing.T) {
	otra := archOtra(archPrincipal)
	f := &registroFalso{manifests: map[string]struct{ mt, body string }{
		"sha256:m":  {mtDockerManif, `{"schemaVersion":2,"config":{"digest":"sha256:c"}}`},
		"sha256:c":  {"application/json", `{"architecture":"` + otra + `","os":"linux"}`},
		"sha256:ix": {mtOCIIndex, `{"schemaVersion":2,"manifests":[]}`},
	}}
	srv := httptest.NewTLSServer(f)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	k := &KubeExecutor{InsecureRegistry: true, httpRegistro: srv.Client()}
	ctx := context.Background()
	if a := k.archDeImagen(ctx, host+"/app-web", "sha256:m"); a != otra {
		t.Fatalf("the image's architecture: %q", a)
	}
	if a := k.archDeImagen(ctx, host+"/app-web", "sha256:ix"); a != "" {
		t.Fatalf("an index has one: %q", a)
	}
	if a := k.archDeImagen(ctx, host+"/app-web", "sha256:nada"); a != "" {
		t.Fatalf("a missing manifest has one: %q", a)
	}
}

// A node «a ratos» does what its drawer says: no halves, no tests, and a
// build that waits for it while it is off.
func TestAjustesARatos(t *testing.T) {
	daemon := func(name, node string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "devops-tools"}, Spec: corev1.PodSpec{NodeName: node}}
	}
	otra := archOtra(archPrincipal)
	nodo := func(name, arch string, ratos bool) *corev1.Node {
		l := map[string]string{"kubernetes.io/arch": arch}
		if ratos {
			l[TaintARatos] = "true"
		}
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: l}}
	}
	cs := fake.NewSimpleClientset(daemon("buildkitd-0", "nodo-b"), daemon("buildkitd-amd64-0", "desktop-node"),
		nodo("nodo-b", archPrincipal, false), nodo("desktop-node", otra, true), nodo("laptop-node", otra, true))
	ajustes := map[string]AjustesARatos{}
	k := &KubeExecutor{Client: cs, BuildkitPool: "buildkitd-pool.devops-tools.svc.cluster.local",
		Resolver: fakeSRV{targets: []string{"buildkitd-0", "buildkitd-amd64-0"}},
		ARatos:   func(n string) AjustesARatos { return ajustes[n] }}
	ctx := context.Background()
	if _, d := k.buildkitOtro(ctx, "reg/a"); d != "buildkitd-amd64-0" {
		t.Fatalf("by default it builds the other half: %q", d)
	}
	ajustes["desktop-node"] = AjustesARatos{SinOtraMitad: true}
	if addr, _ := k.buildkitOtro(ctx, "reg/a"); addr != "" {
		t.Fatal("a node whose drawer said no halves built one")
	}

	// Tests: one node off for them is avoided; both off, no toleration.
	pod := func() *corev1.Pod {
		return &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: "node:22"}}}}
	}
	ajustes["desktop-node"] = AjustesARatos{SinPruebas: true}
	p := pod()
	if !k.aRatos(ctx, p) {
		t.Fatal("the other node «a ratos» still takes tests")
	}
	if e := p.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0]; e.Values[0] != "desktop-node" || len(e.Values) != 1 {
		t.Fatalf("the node without tests: %+v", e)
	}
	ajustes["laptop-node"] = AjustesARatos{SinPruebas: true}
	if p := pod(); k.aRatos(ctx, p) || len(p.Spec.Tolerations) != 0 {
		t.Fatal("a test went «a ratos» with every drawer saying no")
	}

	// Waiting: off now, on a moment later.
	defer func(e, p time.Duration) { EsperaARatos, esperaPaso = e, p }(EsperaARatos, esperaPaso)
	EsperaARatos, esperaPaso = time.Second, 10*time.Millisecond
	ajustes = map[string]AjustesARatos{"desktop-node": {Esperar: true}}
	var mu sync.Mutex
	srv := &cambiante{targets: []string{"buildkitd-0"}, mu: &mu}
	k.Resolver = srv
	go func() {
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		srv.targets = []string{"buildkitd-0", "buildkitd-amd64-0"}
		mu.Unlock()
	}()
	var log strings.Builder
	if _, d := k.esperarOtro(ctx, &syncWriter{w: &log, mu: &mu}, "reg/a"); d != "buildkitd-amd64-0" {
		t.Fatalf("it did not wait for the node: %q\n%s", d, log.String())
	}
	// Not coming on: it gives up, with one architecture.
	k.Resolver = fakeSRV{targets: []string{"buildkitd-0"}}
	log.Reset()
	if addr, _ := k.esperarOtro(ctx, &log, "reg/a"); addr != "" || !strings.Contains(log.String(), "did not come on") {
		t.Fatalf("giving up: %q\n%s", addr, log.String())
	}
	// Nobody waits without the drawer saying so.
	ajustes = map[string]AjustesARatos{}
	log.Reset()
	if _, _ = k.esperarOtro(ctx, &log, "reg/a"); log.Len() != 0 {
		t.Fatalf("it waited unasked: %s", log.String())
	}
}

// cambiante is an SRV resolver whose daemons change while a build waits.
type cambiante struct {
	targets []string
	mu      *sync.Mutex
}

func (c *cambiante) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	c.mu.Lock()
	t := append([]string{}, c.targets...)
	c.mu.Unlock()
	return fakeSRV{targets: t}.LookupSRV(ctx, service, proto, name)
}

type syncWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
