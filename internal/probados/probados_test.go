package probados

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// memAlmacen keeps the saved settings in memory.
type memAlmacen struct {
	mu   sync.Mutex
	rows map[string]store.Ajuste
	fail string // a key that cannot be saved
}

func (m *memAlmacen) GetAjustes(context.Context) (map[string]store.Ajuste, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]store.Ajuste{}
	for k, v := range m.rows {
		out[k] = v
	}
	return out, nil
}

func (m *memAlmacen) SaveAjuste(_ context.Context, key, value, by string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key == m.fail {
		return errors.New("the database is gone")
	}
	if m.rows == nil {
		m.rows = map[string]store.Ajuste{}
	}
	m.rows[key] = store.Ajuste{Key: key, Value: value, UpdatedBy: by, UpdatedAt: time.Now()}
	return nil
}

func (m *memAlmacen) DeleteAjuste(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.rows[key]
	delete(m.rows, key)
	return ok, nil
}

type prueba struct {
	p          *Probador
	kube       client.Client
	st         *memAlmacen
	reinicios  int
	reiniciosM sync.Mutex
}

func nuevo(t *testing.T, objs ...client.Object) *prueba {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	e := &prueba{kube: kube, st: &memAlmacen{}}
	entorno := map[string]string{Registry: "registry.example.lan:5000", DNSZone: "example.com", DNSTarget: "203.0.113.7",
		Issuer: "letsencrypt-prod", StorageClass: "longhorn", IngressClass: "nginx", BaseURL: "https://rendimiento.example.com"}
	e.p = &Probador{Kube: kube, Namespace: "probados-plataforma", BuildNamespace: "probados-builds", Store: e.st, Entorno: entorno,
		Actual: func(k string) string { return entorno[k] }, Esperar: 3 * time.Second, Identidad: LlaveDeIdentidad("postgres://example"),
		Reiniciar: func(context.Context) (string, error) {
			e.reiniciosM.Lock()
			e.reinicios++
			e.reiniciosM.Unlock()
			return "restarting", nil
		}}
	return e
}

func pasos(res Resultado) string {
	var b strings.Builder
	for _, p := range res.Pasos {
		b.WriteString(p.ID)
		if !p.OK {
			b.WriteString("✗")
		}
		b.WriteString(" ")
	}
	return strings.TrimSpace(b.String())
}

// nada fails when a failed test saved or restarted anything.
func (e *prueba) nada(t *testing.T, res Resultado) {
	t.Helper()
	if res.Pasa || res.Ficha != "" || len(e.st.rows) != 0 || e.reinicios != 0 {
		t.Fatalf("a test that failed left something: %+v, saved %v, %d restarts", res, e.st.rows, e.reinicios)
	}
}

func issuer(name string, ready bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(clusterIssuerGVK)
	u.SetName(name)
	st := "False"
	if ready {
		st = "True"
	}
	_ = unstructured.SetNestedSlice(u.Object, []any{map[string]any{"type": "Ready", "status": st, "message": "ACME account not registered"}}, "status", "conditions")
	return u
}

// A value is checked before anything is tried, and the value in use is
// not a change.
func TestValidar(t *testing.T) {
	for _, c := range []struct {
		id  string
		in  map[string]string
		why string
	}{
		{"registro", map[string]string{Registry: "https://registry.example.com"}, "without http://"},
		{"registro", map[string]string{Registry: "registry.example.lan:5000"}, "in use already"},
		{"dns", map[string]string{DNSZone: "nada"}, "domain"},
		{"dns", map[string]string{DNSTarget: "no es un nombre"}, "spaces"},
		{"emisor", map[string]string{Issuer: "Letsencrypt_Prod"}, "Kubernetes"},
		{"emisor", map[string]string{Issuer: ""}, "empty"},
		{"direccion", map[string]string{BaseURL: "http://rendimiento.example.org"}, "https"},
		{"direccion", map[string]string{BaseURL: "https://rendimiento.example.org/app"}, "nothing after"},
	} {
		e := nuevo(t)
		res, err := e.p.Probar(context.Background(), c.id, c.in)
		if err != nil {
			t.Fatal(err)
		}
		if pasos(res) != "valid✗" || !strings.Contains(res.Pasos[0].Detail, c.why) {
			t.Errorf("%s %v: %s %q", c.id, c.in, pasos(res), res.Pasos[0].Detail)
		}
		e.nada(t, res)
	}
	if _, err := nuevo(t).p.Probar(context.Background(), "MODE", nil); !errors.Is(err, ErrDesconocido) {
		t.Fatalf("MODE: %v", err)
	}
}

// The issuer passes when it exists and is ready; only with the test's
// ficha, for the same value, is it saved, once, and the platform restarted.
func TestEmisor(t *testing.T) {
	e := nuevo(t, issuer("probados-listo", true), issuer("probados-no-listo", false))
	ctx := context.Background()
	for _, c := range []struct{ name, pasos string }{
		{"probados-falta", "valid exists✗"},
		{"probados-no-listo", "valid exists ready✗"},
	} {
		res, _ := e.p.Probar(ctx, "emisor", map[string]string{Issuer: c.name})
		if pasos(res) != c.pasos {
			t.Fatalf("%s: %s", c.name, pasos(res))
		}
		e.nada(t, res)
	}
	res, _ := e.p.Probar(ctx, "emisor", map[string]string{Issuer: "probados-listo"})
	if !res.Pasa || res.Ficha == "" || pasos(res) != "valid exists ready" || len(e.st.rows) != 0 {
		t.Fatalf("a test saves nothing: %+v %v", res, e.st.rows)
	}
	// Another value, or no ficha: refused.
	if _, err := e.p.Cambiar(ctx, "emisor", map[string]string{Issuer: "probados-no-listo"}, res.Ficha, "p0dxD"); !errors.Is(err, ErrSinPrueba) {
		t.Fatalf("other values: %v", err)
	}
	if _, err := e.p.Cambiar(ctx, "emisor", map[string]string{Issuer: "probados-listo"}, "inventada", "p0dxD"); !errors.Is(err, ErrSinPrueba) {
		t.Fatalf("no ficha: %v", err)
	}
	if len(e.st.rows) != 0 || e.reinicios != 0 {
		t.Fatal("saved without a test")
	}
	// The other values refused did not spend the ficha.
	cam, err := e.p.Cambiar(ctx, "emisor", map[string]string{Issuer: "probados-listo"}, res.Ficha, "p0dxD")
	if err != nil || !cam.Guardado || pasos(cam) != "save restart" || e.st.rows[Issuer].Value != "probados-listo" || e.st.rows[Issuer].UpdatedBy != "p0dxD" || e.reinicios != 1 {
		t.Fatalf("change: %+v %v %v", cam, err, e.st.rows)
	}
	if _, err := e.p.Cambiar(ctx, "emisor", map[string]string{Issuer: "probados-listo"}, res.Ficha, "p0dxD"); !errors.Is(err, ErrSinPrueba) {
		t.Fatalf("a ficha twice: %v", err)
	}
	// The way back: the ConfigMap's value holds again, after a restart.
	vuelta, err := e.p.Volver(ctx, "emisor", "p0dxD")
	if err != nil || !vuelta.Guardado || len(e.st.rows) != 0 || e.reinicios != 2 || vuelta.Valores[Issuer] != "letsencrypt-prod" {
		t.Fatalf("back: %+v %v %v", vuelta, err, e.st.rows)
	}
}

// When one value of two cannot be saved, the other gets its old one back.
func TestDosValoresOAmbos(t *testing.T) {
	e := nuevo(t)
	e.p.DNS = &fakeDNS{zonas: []string{"example.com", "example.org"}}
	e.st.fail = DNSTarget
	ctx := context.Background()
	in := map[string]string{DNSZone: "example.org", DNSTarget: "198.51.100.4"}
	res, _ := e.p.Probar(ctx, "dns", in)
	if !res.Pasa || pasos(res) != "valid zones record target" {
		t.Fatalf("%s %+v", pasos(res), res.Pasos)
	}
	cam, err := e.p.Cambiar(ctx, "dns", in, res.Ficha, "p0dxD")
	if err != nil || cam.Guardado || pasos(cam) != "save✗" || len(e.st.rows) != 0 || e.reinicios != 0 {
		t.Fatalf("%+v %v %v", cam, err, e.st.rows)
	}
}

type fakeDNS struct {
	zonas   []string
	records int
}

func (f *fakeDNS) Zones(context.Context) ([]string, error) { return f.zonas, nil }
func (f *fakeDNS) TestRecord(_ context.Context, zone string) error {
	if zone == "example.net" {
		return errors.New("Authentication error")
	}
	f.records++
	return nil
}

func TestDNS(t *testing.T) {
	for _, c := range []struct {
		zona, destino, pasos string
	}{
		{"example.org", "auto", "valid zones record target"},
		{"example.org", "203.0.113.9", "valid zones record target"},
		{"example.edu", "203.0.113.9", "valid zones✗"},
		{"example.net", "203.0.113.9", "valid zones record✗"},
		{"example.org", "no-existe.invalid", "valid zones record target✗"},
	} {
		e := nuevo(t)
		e.p.DNS = &fakeDNS{zonas: []string{"example.com", "example.org", "example.net"}}
		res, _ := e.p.Probar(context.Background(), "dns", map[string]string{DNSZone: c.zona, DNSTarget: c.destino})
		if pasos(res) != c.pasos {
			t.Errorf("%s → %s: %s %+v", c.zona, c.destino, pasos(res), res.Pasos)
		}
		if !strings.HasSuffix(c.pasos, "✗") != res.Pasa {
			t.Errorf("%s: pasa %v", c.zona, res.Pasa)
		}
		if !res.Pasa {
			e.nada(t, res)
		}
	}
	// With records made by hand there is nothing to test them with.
	e := nuevo(t)
	res, _ := e.p.Probar(context.Background(), "dns", map[string]string{DNSZone: "example.org"})
	if pasos(res) != "valid zones✗" {
		t.Fatalf("no DNS: %s", pasos(res))
	}
}

func nodo(name string, ready, cordoned bool) *corev1.Node {
	st := corev1.ConditionFalse
	if ready {
		st = corev1.ConditionTrue
	}
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: corev1.NodeSpec{Unschedulable: cordoned},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: st}}}}
}

// kubelet plays the nodes: each test pod gets the image's id, except on
// the nodes in sinRegistro, which cannot pull it.
func kubelet(ctx context.Context, kube client.Client, ns string, sinRegistro ...string) {
	for ctx.Err() == nil {
		var pods corev1.PodList
		_ = kube.List(ctx, &pods, client.InNamespace(ns))
		for i := range pods.Items {
			p := &pods.Items[i]
			if len(p.Status.ContainerStatuses) > 0 {
				continue
			}
			cs := corev1.ContainerStatus{Name: "prueba", ImageID: "registry.example.org/rendimiento-prueba@sha256:abc"}
			for _, n := range sinRegistro {
				if p.Spec.NodeName == n {
					cs = corev1.ContainerStatus{Name: "prueba", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
						Reason: "ErrImagePull", Message: "http: server gave HTTP response to HTTPS client"}}}
				}
			}
			p.Status.ContainerStatuses = []corev1.ContainerStatus{cs}
			estado(ctx, kube, p)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// estado writes an object's status, as the kubelet or a provisioner does.
func estado(ctx context.Context, kube client.Client, obj client.Object) {
	if err := kube.Status().Update(ctx, obj); err != nil {
		_ = kube.Update(ctx, obj)
	}
}

// fakeRegistry answers /v2/ and the test image's DELETE.
func fakeRegistry(t *testing.T, borrados *int) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/v2/rendimiento-prueba/manifests/sha256:abc":
			*borrados++
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return u.Host
}

// A registry passes when it answers, BuildKit pushes to it, every node
// that runs apps pulls the test image (a cordoned one is left out), and
// the image is deleted; the test's pods are cleaned up either way.
func TestRegistro(t *testing.T) {
	for _, c := range []struct {
		name        string
		sinRegistro []string
		empuja      error
		pasos       string
	}{
		{"todos", nil, nil, "valid answers push pull delete"},
		{"uno-no", []string{"probados-b"}, nil, "valid answers push pull✗"},
		{"buildkit-no", nil, errors.New("failed to push: connection refused"), "valid answers push✗"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := nuevo(t, nodo("probados-a", true, false), nodo("probados-b", true, false), nodo("probados-c", true, true), nodo("probados-d", false, false))
			borrados := 0
			reg := fakeRegistry(t, &borrados)
			e.p.RegistryInsecure = true
			var empujada string
			e.p.Empujar = func(_ context.Context, image, _ string) (string, error) {
				empujada = image
				return "sha256:abc", c.empuja
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go kubelet(ctx, e.kube, "probados-builds", c.sinRegistro...)
			res, err := e.p.Probar(ctx, "registro", map[string]string{Registry: reg})
			if err != nil {
				t.Fatal(err)
			}
			if pasos(res) != c.pasos {
				t.Fatalf("%s %+v", pasos(res), res.Pasos)
			}
			if !strings.HasPrefix(empujada, reg+"/rendimiento-prueba:") {
				t.Fatalf("pushed %q", empujada)
			}
			if c.empuja == nil && borrados != 1 {
				t.Fatalf("the test image was deleted %d times", borrados)
			}
			if c.name == "todos" && !strings.Contains(res.Pasos[3].Detail, "2 of 2") {
				t.Fatalf("nodes: %q", res.Pasos[3].Detail)
			}
			if c.name == "uno-no" && !strings.Contains(res.Pasos[3].Detail, "probados-b (http: server gave") {
				t.Fatalf("which node: %q", res.Pasos[3].Detail)
			}
			var pods corev1.PodList
			_ = e.kube.List(context.Background(), &pods, client.InNamespace("probados-builds"))
			if len(pods.Items) != 0 {
				t.Fatalf("%d test pods left", len(pods.Items))
			}
			if !res.Pasa {
				e.nada(t, res)
			}
		})
	}
}

// A storage class that waits for a consumer gets a pod for its test
// volume; the volume and the pod go afterwards.
func TestAlmacen(t *testing.T) {
	espera := storagev1.VolumeBindingWaitForFirstConsumer
	for _, c := range []struct {
		name, pasos string
		liga        bool
	}{
		{"probados-local", "valid exists bind", true},
		{"probados-local", "valid exists bind✗", false},
		{"probados-falta", "valid exists✗", false},
	} {
		e := nuevo(t, &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "probados-local"}, Provisioner: "rancher.io/local-path", VolumeBindingMode: &espera})
		ctx, cancel := context.WithCancel(context.Background())
		if c.liga {
			go func() { // the provisioner binds the claim once a pod uses it
				for ctx.Err() == nil {
					var pods corev1.PodList
					var pvcs corev1.PersistentVolumeClaimList
					_ = e.kube.List(ctx, &pods, client.InNamespace("probados-builds"))
					_ = e.kube.List(ctx, &pvcs, client.InNamespace("probados-builds"))
					if len(pods.Items) == 1 && len(pvcs.Items) == 1 {
						pvcs.Items[0].Status.Phase = corev1.ClaimBound
						estado(ctx, e.kube, &pvcs.Items[0])
					}
					time.Sleep(50 * time.Millisecond)
				}
			}()
		}
		res, _ := e.p.Probar(ctx, "almacenamiento", map[string]string{StorageClass: c.name})
		cancel()
		if pasos(res) != c.pasos {
			t.Errorf("%s (binds %v): %s %+v", c.name, c.liga, pasos(res), res.Pasos)
		}
		var pods corev1.PodList
		var pvcs corev1.PersistentVolumeClaimList
		_ = e.kube.List(context.Background(), &pods, client.InNamespace("probados-builds"))
		_ = e.kube.List(context.Background(), &pvcs, client.InNamespace("probados-builds"))
		if len(pods.Items)+len(pvcs.Items) != 0 {
			t.Errorf("left %d pods and %d volumes", len(pods.Items), len(pvcs.Items))
		}
		if !res.Pasa {
			e.nada(t, res)
		}
	}
}

// An ingress class passes when it exists and its controller runs.
func TestEntrada(t *testing.T) {
	ic := &networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: "probados-nginx"}, Spec: networkingv1.IngressClassSpec{Controller: "k8s.io/ingress-nginx"}}
	controlador := func(ready bool) *corev1.Pod {
		st := corev1.ConditionFalse
		if ready {
			st = corev1.ConditionTrue
		}
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "probados-ingress", Name: "ingress-nginx-controller-1"},
			Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "controller", Image: "registry.k8s.io/ingress-nginx/controller:v1.12.0"}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: st}}}}
	}
	for _, c := range []struct {
		pod   *corev1.Pod
		clase string
		pasos string
	}{
		{controlador(true), "probados-nginx", "valid exists controller"},
		{controlador(false), "probados-nginx", "valid exists controller✗"},
		{controlador(true), "probados-traefik", "valid exists✗"},
	} {
		e := nuevo(t, ic, c.pod)
		e.p.IngressNamespace = func(string) string { return "probados-ingress" }
		res, _ := e.p.Probar(context.Background(), "entrada", map[string]string{IngressClass: c.clase})
		if pasos(res) != c.pasos {
			t.Errorf("%s: %s %+v", c.clase, pasos(res), res.Pasos)
		}
	}
}

// A new address must answer, over HTTPS, as this platform: another
// server, or another rendimiento, does not pass.
func TestDireccion(t *testing.T) {
	llave := LlaveDeIdentidad("postgres://example")
	for _, c := range []struct {
		name  string
		firma func(n string) string
		pasos string
	}{
		{"esta", func(n string) string { return Firmar(llave, n) }, "valid resolves identity"},
		{"otra", func(n string) string { return Firmar(LlaveDeIdentidad("postgres://otra"), n) }, "valid resolves identity✗"},
		{"ninguna", nil, "valid resolves identity✗"},
	} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != RutaIdentidad || c.firma == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"firma": c.firma(r.URL.Query().Get("n"))})
		}))
		e := nuevo(t)
		e.p.HTTP = srv.Client()
		res, _ := e.p.Probar(context.Background(), "direccion", map[string]string{BaseURL: srv.URL + "/"})
		srv.Close()
		if pasos(res) != c.pasos {
			t.Errorf("%s: %s %+v", c.name, pasos(res), res.Pasos)
		}
		if c.name == "otra" && !strings.Contains(res.Pasos[2].Detail, "another rendimiento") {
			t.Errorf("why: %q", res.Pasos[2].Detail)
		}
	}
}

// At start the saved values win over the ConfigMap's, unless
// TESTED_SETTINGS=ignore.
func TestAlArrancar(t *testing.T) {
	st := &memAlmacen{}
	_ = st.SaveAjuste(context.Background(), Registry, "registry.example.org", "p0dxD")
	_ = st.SaveAjuste(context.Background(), "ORG_QUOTA_CPU", "4", "p0dxD") // a live setting: not this package's
	for _, ignorar := range []bool{false, true} {
		env := map[string]string{Registry: "registry.example.lan:5000", "ORG_QUOTA_CPU": "2"}
		if ignorar {
			env["TESTED_SETTINGS"] = Ignorar
		}
		entorno, cambiados, err := AlArrancar(context.Background(), st, func(k string) string { return env[k] },
			func(k, v string) error { env[k] = v; return nil })
		if err != nil || entorno[Registry] != "registry.example.lan:5000" {
			t.Fatalf("%v %v", entorno, err)
		}
		want := "registry.example.org"
		if ignorar {
			want = "registry.example.lan:5000"
		}
		if env[Registry] != want || env["ORG_QUOTA_CPU"] != "2" || (len(cambiados) == 1) == ignorar {
			t.Fatalf("ignore %v: env %v, changed %v", ignorar, env, cambiados)
		}
	}
}
