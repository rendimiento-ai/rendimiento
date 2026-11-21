// Package probados changes, from the Environment page (Llaves y ajustes),
// the settings the platform reads when it starts and that change what it
// makes from then on: the image registry, the DNS zone and target, the
// certificate issuer, the storage class, the ingress class and its public
// address.
//
// A new value is tested first, against the cluster and the services
// themselves (the registry takes an image from BuildKit and every node
// pulls it; the zone takes a test record; the issuer is ready; a test
// volume binds; the ingress controller runs; the address answers as this
// platform). The test hands back a ficha; only with it, for the same
// values and within 15 minutes, is the change saved: a platform setting
// that wins over the ConfigMap's value when the platform starts
// (AlArrancar), followed by a restart of the platform's Deployments, since
// these values are read only at start. If a test fails, nothing is saved.
// Forgetting the saved value (Volver) brings the ConfigMap's back, with
// another restart.
package probados

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The settings that change here, by their environment variable.
const (
	Registry     = "REGISTRY"
	DNSZone      = "DNS_ZONE"
	DNSTarget    = "DNS_TARGET"
	Issuer       = "CLUSTER_ISSUER"
	StorageClass = "STORAGE_CLASS"
	IngressClass = "INGRESS_CLASS"
	BaseURL      = "BASE_URL"
)

// Ajuste is one thing the page changes: one setting, or two that go
// together (the DNS zone and its target).
type Ajuste struct {
	ID   string   `json:"id"`
	Keys []string `json:"keys"`
}

// Ajustes are the settings tested before they change, in the order the
// page lists them.
var Ajustes = []Ajuste{
	{"registro", []string{Registry}},
	{"dns", []string{DNSZone, DNSTarget}},
	{"emisor", []string{Issuer}},
	{"almacenamiento", []string{StorageClass}},
	{"entrada", []string{IngressClass}},
	{"direccion", []string{BaseURL}},
}

// Buscar finds a setting by its id.
func Buscar(id string) (Ajuste, bool) {
	i := slices.IndexFunc(Ajustes, func(a Ajuste) bool { return a.ID == id })
	if i < 0 {
		return Ajuste{}, false
	}
	return Ajustes[i], true
}

// Claves are every variable that changes here.
func Claves() []string {
	var out []string
	for _, a := range Ajustes {
		out = append(out, a.Keys...)
	}
	return out
}

// Almacen keeps the saved values: platform settings, beside the live ones
// (store.Store). The live settings' reader ignores these keys.
type Almacen interface {
	GetAjustes(ctx context.Context) (map[string]store.Ajuste, error)
	SaveAjuste(ctx context.Context, key, value, by string) error
	DeleteAjuste(ctx context.Context, key string) (bool, error)
}

// Ignorar is the value of TESTED_SETTINGS that makes the platform start with
// the ConfigMap's values, whatever is saved: the way back when the page
// cannot be reached.
const Ignorar = "ignore"

// AlArrancar puts the saved values in the environment before the platform
// reads it, so they win over the ConfigMap's. It returns the values the
// environment had (the ConfigMap's), by variable, and the ones it changed.
func AlArrancar(ctx context.Context, st Almacen, getenv func(string) string, setenv func(k, v string) error) (map[string]string, []string, error) {
	entorno := map[string]string{}
	for _, k := range Claves() {
		entorno[k] = getenv(k)
	}
	if getenv("TESTED_SETTINGS") == Ignorar {
		return entorno, nil, nil
	}
	saved, err := st.GetAjustes(ctx)
	if err != nil {
		return entorno, nil, err
	}
	var cambiados []string
	for _, k := range Claves() {
		if a, ok := saved[k]; ok && a.Value != "" && a.Value != entorno[k] {
			if err := setenv(k, a.Value); err != nil {
				return entorno, cambiados, err
			}
			cambiados = append(cambiados, k)
		}
	}
	return entorno, cambiados, nil
}

// DNS is the DNS provider the platform uses now (the Cloudflare token in
// use): what the new zone is tested with.
type DNS interface {
	Zones(ctx context.Context) ([]string, error)
	TestRecord(ctx context.Context, zone string) error
}

// Paso is one step of a test or a change. Detail says what it found, or
// why it failed.
type Paso struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Resultado is how a test (or a change) went: the steps tried, in order
// (they stop at the first that fails). A test that passed has its Ficha,
// for the change.
type Resultado struct {
	Ajuste  string            `json:"ajuste"`
	Valores map[string]string `json:"valores"`
	Pasa    bool              `json:"pasa"`
	Pasos   []Paso            `json:"pasos"`
	Ficha   string            `json:"ficha,omitempty"`
	// Guardado: the change was saved; Reinicio says how the platform takes
	// it (a restart of its Deployments).
	Guardado bool   `json:"guardado,omitempty"`
	Reinicio string `json:"reinicio,omitempty"`
}

// ErrDesconocido is a setting that does not change here, ErrSinPrueba a
// change without a passed test of the same values.
var (
	ErrDesconocido = errors.New("no such setting changes here")
	ErrSinPrueba   = errors.New("test it first: a change is saved only with a test that passed, for the same values, in the last 15 minutes")
)

// Vigencia is how long a passed test allows its change.
const Vigencia = 15 * time.Minute

// Probador tests a new value of a setting and saves it.
type Probador struct {
	Kube client.Client
	// Namespace is the platform's; BuildNamespace where the test's pods and
	// volume go (the builds' namespace, fenced off like builds).
	Namespace      string
	BuildNamespace string
	Store          Almacen
	HTTP           *http.Client
	// Entorno are the ConfigMap's values (AlArrancar), and Actual the ones
	// in use, by variable.
	Entorno map[string]string
	Actual  func(key string) string
	// The registry: whether it is reached over plain HTTP or a self-signed
	// certificate (REGISTRY_INSECURE), what pushes a test image to it
	// (BuildKit, as builds do) and the image every node pulls it with.
	RegistryInsecure bool
	Empujar          func(ctx context.Context, image, nonce string) (digest string, err error)
	// ImagenPrueba has a shell, for the pod that uses a test volume (the
	// builds' BuildKit client, which the nodes already pull).
	ImagenPrueba string
	// DNS is the provider in use (nil: records are made by hand).
	DNS DNS
	// IngressNamespace is where the ingress controller of a class runs ("":
	// the usual one for it).
	IngressNamespace func(class string) string
	// Identidad signs a nonce as this platform: what its address must
	// answer (Firmar, with the same key in every process).
	Identidad []byte
	// Reiniciar restarts the platform's Deployments, so they read the
	// settings again; it says what it did.
	Reiniciar func(ctx context.Context) (string, error)
	// Esperar is how long a pod or volume of a test may take (3 minutes
	// when 0).
	Esperar time.Duration
	Log     *slog.Logger

	mu     sync.Mutex
	fichas map[string]pasada
	una    sync.Mutex // one test or change at a time
}

type pasada struct {
	ajuste  string
	valores map[string]string
	hasta   time.Time
}

func (p *Probador) http() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (p *Probador) esperar() time.Duration {
	if p.Esperar > 0 {
		return p.Esperar
	}
	return 3 * time.Minute
}

func (p *Probador) imagenPrueba() string {
	if p.ImagenPrueba != "" {
		return p.ImagenPrueba
	}
	return "moby/buildkit:v0.18.2"
}

func (p *Probador) actual(k string) string {
	if p.Actual != nil {
		return p.Actual(k)
	}
	return p.Entorno[k]
}

// Valores are a setting's new values, trimmed, with every key of it (the
// ones not given keep the value in use).
func (p *Probador) valores(a Ajuste, in map[string]string) map[string]string {
	out := map[string]string{}
	for _, k := range a.Keys {
		v, ok := in[k]
		if !ok {
			v = p.actual(k)
		}
		v = strings.TrimSpace(v)
		if k == BaseURL {
			v = strings.TrimRight(v, "/")
		}
		out[k] = v
	}
	return out
}

// Probar tests new values of the setting id. A failed step is in the
// result, not an error: the error is for a setting that does not change
// here.
func (p *Probador) Probar(ctx context.Context, id string, in map[string]string) (Resultado, error) {
	a, ok := Buscar(id)
	if !ok {
		return Resultado{}, ErrDesconocido
	}
	p.una.Lock()
	defer p.una.Unlock()
	v := p.valores(a, in)
	res := Resultado{Ajuste: id, Valores: v, Pasos: []Paso{}}
	if !p.validar(a, v, &res) {
		return res, nil
	}
	var pasa bool
	switch id {
	case "registro":
		pasa = p.probarRegistro(ctx, v[Registry], &res)
	case "dns":
		pasa = p.probarDNS(ctx, v[DNSZone], v[DNSTarget], &res)
	case "emisor":
		pasa = p.probarEmisor(ctx, v[Issuer], &res)
	case "almacenamiento":
		pasa = p.probarAlmacen(ctx, v[StorageClass], &res)
	case "entrada":
		pasa = p.probarEntrada(ctx, v[IngressClass], &res)
	case "direccion":
		pasa = p.probarDireccion(ctx, v[BaseURL], &res)
	}
	if !pasa {
		return res, nil
	}
	res.Pasa = true
	res.Ficha = nonce(16)
	p.mu.Lock()
	if p.fichas == nil {
		p.fichas = map[string]pasada{}
	}
	for f, x := range p.fichas {
		if time.Now().After(x.hasta) {
			delete(p.fichas, f)
		}
	}
	p.fichas[res.Ficha] = pasada{ajuste: id, valores: v, hasta: time.Now().Add(Vigencia)}
	p.mu.Unlock()
	return res, nil
}

// Cambiar saves the values a test passed with (its ficha) and restarts the
// platform so it takes them. por is who changes it.
func (p *Probador) Cambiar(ctx context.Context, id string, in map[string]string, ficha, por string) (Resultado, error) {
	a, ok := Buscar(id)
	if !ok {
		return Resultado{}, ErrDesconocido
	}
	v := p.valores(a, in)
	p.mu.Lock()
	x, ok := p.fichas[ficha]
	if ok && x.ajuste == id && time.Now().Before(x.hasta) && mismos(x.valores, v) {
		delete(p.fichas, ficha) // once
	} else {
		ok = false
	}
	p.mu.Unlock()
	if !ok {
		return Resultado{}, ErrSinPrueba
	}
	p.una.Lock()
	defer p.una.Unlock()
	res := Resultado{Ajuste: id, Valores: v, Pasa: true, Pasos: []Paso{}}
	antes, err := p.Store.GetAjustes(ctx)
	if err == nil {
		err = p.guardar(ctx, a, v, antes, por)
	}
	if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "save", Detail: M("nothing was saved: %s", err.Error())})
		return res, nil
	}
	res.Guardado = true
	res.Pasos = append(res.Pasos, Paso{ID: "save", OK: true, Detail: M("saved as a platform setting: it wins over the ConfigMap's value")})
	p.reiniciar(ctx, &res)
	if p.Log != nil {
		p.Log.Info("tested setting changed", "ajuste", id, "values", v, "by", por)
	}
	return res, nil
}

// Volver forgets the saved values of a setting: the ConfigMap's hold
// again, after a restart.
func (p *Probador) Volver(ctx context.Context, id, por string) (Resultado, error) {
	a, ok := Buscar(id)
	if !ok {
		return Resultado{}, ErrDesconocido
	}
	p.una.Lock()
	defer p.una.Unlock()
	v := map[string]string{}
	for _, k := range a.Keys {
		v[k] = p.Entorno[k]
	}
	res := Resultado{Ajuste: id, Valores: v, Pasa: true, Pasos: []Paso{}}
	borrado := false
	for _, k := range a.Keys {
		ok, err := p.Store.DeleteAjuste(ctx, k)
		if err != nil {
			res.Pasos = append(res.Pasos, Paso{ID: "save", Detail: M("the saved value could not be forgotten: %s", err.Error())})
			return res, nil
		}
		borrado = borrado || ok
	}
	res.Guardado = true
	res.Pasos = append(res.Pasos, Paso{ID: "save", OK: true, Detail: M("the ConfigMap's value holds again")})
	if borrado {
		p.reiniciar(ctx, &res)
	}
	if p.Log != nil {
		p.Log.Info("tested setting back to the ConfigMap's value", "ajuste", id, "by", por)
	}
	return res, nil
}

// guardar saves every value, or none: when one cannot be saved, the ones
// before it get their earlier value back.
func (p *Probador) guardar(ctx context.Context, a Ajuste, v map[string]string, antes map[string]store.Ajuste, por string) error {
	var hechos []string
	for _, k := range a.Keys {
		var err error
		if v[k] == p.Entorno[k] {
			_, err = p.Store.DeleteAjuste(ctx, k) // the ConfigMap's value: nothing to win over it
		} else {
			err = p.Store.SaveAjuste(ctx, k, v[k], por)
		}
		if err != nil {
			for _, h := range hechos {
				if b, ok := antes[h]; ok {
					_ = p.Store.SaveAjuste(context.WithoutCancel(ctx), h, b.Value, b.UpdatedBy)
				} else {
					_, _ = p.Store.DeleteAjuste(context.WithoutCancel(ctx), h)
				}
			}
			return err
		}
		hechos = append(hechos, k)
	}
	return nil
}

func (p *Probador) reiniciar(ctx context.Context, res *Resultado) {
	if p.Reiniciar == nil {
		res.Reinicio = M("restart the platform to use it: kubectl -n %s rollout restart deploy", p.Namespace)
		res.Pasos = append(res.Pasos, Paso{ID: "restart", Detail: res.Reinicio})
		return
	}
	que, err := p.Reiniciar(context.WithoutCancel(ctx))
	if err != nil {
		res.Reinicio = M("it is saved, but the platform could not be restarted (%s): restart it with kubectl -n %s rollout restart deploy", err.Error(), p.Namespace)
		res.Pasos = append(res.Pasos, Paso{ID: "restart", Detail: res.Reinicio})
		return
	}
	res.Reinicio = que
	res.Pasos = append(res.Pasos, Paso{ID: "restart", OK: true, Detail: que})
}

func mismos(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func nonce(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// validar checks the values are well formed and that something changes.
func (p *Probador) validar(a Ajuste, v map[string]string, res *Resultado) bool {
	falla := func(msg string) bool {
		res.Pasos = append(res.Pasos, Paso{ID: "valid", Detail: msg})
		return false
	}
	igual := true
	for _, k := range a.Keys {
		if v[k] != p.actual(k) {
			igual = false
		}
		if v[k] == "" {
			return falla(M("%s cannot be empty here; only Helm leaves it empty", k))
		}
		if len(v[k]) > 253 || strings.ContainsAny(v[k], " \t\r\n") {
			return falla(M("%s has spaces or is too long", k))
		}
	}
	if igual {
		return falla(M("that is the value in use already"))
	}
	for _, k := range a.Keys {
		val := v[k]
		var err string
		switch k {
		case Registry:
			host, _, _ := strings.Cut(val, "/")
			if strings.Contains(val, "://") || !hostValido(host) {
				err = M("a registry is a host, with its port if it has one (registry.example.lan:5000), without http://")
			}
		case DNSZone:
			if !dominio(val) {
				err = M("a DNS zone is a domain, such as example.com")
			}
		case DNSTarget:
			if !strings.EqualFold(val, "auto") && net.ParseIP(val) == nil && !dominio(val) {
				err = M("the target is a public IP, a hostname, or auto (dynamic DNS)")
			}
		case Issuer, StorageClass, IngressClass:
			if len(validation.IsDNS1123Subdomain(val)) > 0 {
				err = M("%s must be the name of a Kubernetes object", k)
			}
		case BaseURL:
			u, perr := url.Parse(val)
			switch {
			case perr != nil || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "":
				err = M("the address is https://, a host and nothing after it, such as https://rendimiento.example.com")
			case u.Scheme != "https" && !(u.Scheme == "http" && strings.HasSuffix(u.Hostname(), ".localhost")):
				err = M("the address must be https://: sign-in and webhooks need it")
			}
		}
		if err != "" {
			return falla(err)
		}
	}
	res.Pasos = append(res.Pasos, Paso{ID: "valid", OK: true, Detail: M("well formed")})
	return true
}

func hostValido(hostport string) bool {
	h, port, err := net.SplitHostPort(hostport)
	if err != nil {
		h, port = hostport, ""
	}
	if port != "" {
		for _, c := range port {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return net.ParseIP(h) != nil || dominio(h) || h == "localhost"
}

// dominio: a name of labels, with a dot.
func dominio(s string) bool {
	s = strings.TrimSuffix(s, ".")
	return strings.Contains(s, ".") && len(validation.IsDNS1123Subdomain(strings.ToLower(s))) == 0
}

// ---- The image registry ----

// pruebaImagen is the repository of the test image in a registry.
const pruebaImagen = "rendimiento-prueba"

func (p *Probador) registroHTTP() *http.Client {
	c := *p.http()
	if p.RegistryInsecure {
		c.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // REGISTRY_INSECURE: a registry with a self-signed certificate, as builds push to it
	}
	return &c
}

// registroBase is how the registry answers: over HTTPS, or (with
// REGISTRY_INSECURE) over plain HTTP when HTTPS does not answer.
func (p *Probador) registroBase(ctx context.Context, reg string) (string, int, error) {
	esquemas := []string{"https"}
	if p.RegistryInsecure {
		esquemas = append(esquemas, "http")
	}
	var last error
	for _, s := range esquemas {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s+"://"+reg+"/v2/", nil)
		resp, err := p.registroHTTP().Do(req)
		if err != nil {
			last = err
			continue
		}
		resp.Body.Close()
		return s + "://" + reg, resp.StatusCode, nil
	}
	return "", 0, last
}

// probarRegistro: the registry answers its API; BuildKit pushes a tiny
// image to it; every node that runs apps pulls it; the image is deleted.
func (p *Probador) probarRegistro(ctx context.Context, reg string, res *Resultado) bool {
	base, status, err := p.registroBase(ctx, reg)
	switch {
	case err != nil:
		res.Pasos = append(res.Pasos, Paso{ID: "answers", Detail: M("it does not answer: %s", err.Error())})
		return false
	case status == http.StatusOK:
		res.Pasos = append(res.Pasos, Paso{ID: "answers", OK: true, Detail: M("it answers at %s", base)})
	case status == http.StatusUnauthorized:
		res.Pasos = append(res.Pasos, Paso{ID: "answers", OK: true, Detail: M("it answers at %s, and asks for a password: the nodes and BuildKit must have it", base)})
	default:
		res.Pasos = append(res.Pasos, Paso{ID: "answers", Detail: M("%s answered %d to the registry API (/v2/): it is not a registry", base, status)})
		return false
	}
	if p.Empujar == nil {
		res.Pasos = append(res.Pasos, Paso{ID: "push", Detail: M("BuildKit is not set up in this process")})
		return false
	}
	tag := nonce(5)
	image := reg + "/" + pruebaImagen + ":" + tag
	digest, err := p.Empujar(ctx, image, tag)
	if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "push", Detail: M("BuildKit could not push a test image to it: %s", err.Error())})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "push", OK: true, Detail: M("BuildKit pushed a test image to it (%s)", pruebaImagen+":"+tag)})
	ok := p.bajanTodos(ctx, image, tag, res)
	p.borrarImagen(ctx, base, digest, ok, res)
	return ok
}

// nodos are the nodes apps can run on: ready, and not cordoned.
func (p *Probador) nodos(ctx context.Context) ([]string, error) {
	var list corev1.NodeList
	if err := p.Kube.List(ctx, &list); err != nil {
		return nil, err
	}
	var out []string
	for _, n := range list.Items {
		if n.Spec.Unschedulable {
			continue
		}
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				out = append(out, n.Name)
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

// bajanTodos runs a pod of the test image on every node: a node pulled it
// when its container has the image's id, whatever the container does then
// (the image has nothing to run).
func (p *Probador) bajanTodos(ctx context.Context, image, tag string, res *Resultado) bool {
	nodos, err := p.nodos(ctx)
	if err != nil || len(nodos) == 0 {
		if err == nil {
			err = errors.New(M("no node is ready"))
		}
		res.Pasos = append(res.Pasos, Paso{ID: "pull", Detail: M("cannot list the nodes: %s", err.Error())})
		return false
	}
	nombres := map[string]string{}
	for i, n := range nodos {
		name := fmt.Sprintf("prueba-registro-%s-%d", tag, i)
		nombres[n] = name
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.BuildNamespace, Labels: map[string]string{"rendimiento.ai/prueba": "registro"}},
			Spec: corev1.PodSpec{
				NodeName:                     n,
				RestartPolicy:                corev1.RestartPolicyNever,
				AutomountServiceAccountToken: ptr(false),
				Tolerations:                  []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
				ActiveDeadlineSeconds:        ptr(int64(p.esperar().Seconds())),
				Containers: []corev1.Container{{
					Name: "prueba", Image: image, ImagePullPolicy: corev1.PullAlways, Command: []string{"/prueba"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("8Mi")},
						Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("16Mi")},
					},
				}},
			},
		}
		if err := p.Kube.Create(ctx, pod); err != nil {
			p.borrarPods(ctx, nombres)
			res.Pasos = append(res.Pasos, Paso{ID: "pull", Detail: M("cannot start the test on %s: %s", n, err.Error())})
			return false
		}
	}
	defer p.borrarPods(ctx, nombres)
	bajaron := map[string]bool{}
	fallas := map[string]string{}
	limite := time.Now().Add(p.esperar())
	for len(bajaron)+len(fallas) < len(nodos) && time.Now().Before(limite) {
		for _, n := range nodos {
			if bajaron[n] || fallas[n] != "" {
				continue
			}
			var pod corev1.Pod
			if err := p.Kube.Get(ctx, client.ObjectKey{Namespace: p.BuildNamespace, Name: nombres[n]}, &pod); err != nil {
				continue
			}
			for _, cs := range pod.Status.ContainerStatuses {
				switch {
				case cs.ImageID != "":
					bajaron[n] = true
				case cs.State.Waiting != nil && slices.Contains([]string{"ErrImagePull", "ImagePullBackOff", "InvalidImageName", "ErrImageNeverPull"}, cs.State.Waiting.Reason):
					fallas[n] = cs.State.Waiting.Message
					if fallas[n] == "" {
						fallas[n] = cs.State.Waiting.Reason
					}
				}
			}
		}
		if len(bajaron)+len(fallas) < len(nodos) {
			select {
			case <-ctx.Done():
				limite = time.Now()
			case <-time.After(2 * time.Second):
			}
		}
	}
	if len(bajaron) == len(nodos) {
		res.Pasos = append(res.Pasos, Paso{ID: "pull", OK: true, Detail: M("every node pulled it: %d of %d", len(nodos), len(nodos))})
		return true
	}
	var no []string
	for _, n := range nodos {
		switch {
		case bajaron[n]:
		case fallas[n] != "":
			no = append(no, n+" ("+recortar(fallas[n], 160)+")")
		default:
			no = append(no, n+" ("+M("it did not pull it in %s", p.esperar().String())+")")
		}
	}
	res.Pasos = append(res.Pasos, Paso{ID: "pull", Detail: M("%d of %d nodes pulled it; not %s", len(bajaron), len(nodos), strings.Join(no, "; "))})
	return false
}

func (p *Probador) borrarPods(ctx context.Context, nombres map[string]string) {
	ctx = context.WithoutCancel(ctx)
	for _, name := range nombres {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: p.BuildNamespace, Name: name}}
		_ = p.Kube.Delete(ctx, pod, client.GracePeriodSeconds(0))
	}
}

// borrarImagen deletes the test image from the registry. A registry that
// does not allow deleting keeps it (it is a few bytes): that is said, and
// it is not a failure.
func (p *Probador) borrarImagen(ctx context.Context, base, digest string, siguio bool, res *Resultado) {
	if !siguio { // a step failed before: the image goes, and nothing more is said
		_, _ = p.borrarManifiesto(ctx, base, digest)
		return
	}
	switch status, err := p.borrarManifiesto(ctx, base, digest); {
	case err == nil && (status == http.StatusAccepted || status == http.StatusOK || status == http.StatusNotFound):
		res.Pasos = append(res.Pasos, Paso{ID: "delete", OK: true, Detail: M("the test image was deleted")})
	case err != nil:
		res.Pasos = append(res.Pasos, Paso{ID: "delete", OK: true, Detail: M("the test image could not be deleted (%s): it stays as %s, a few bytes", err.Error(), pruebaImagen)})
	default:
		res.Pasos = append(res.Pasos, Paso{ID: "delete", OK: true, Detail: M("the registry does not allow deleting (%d): the test image stays as %s, a few bytes", status, pruebaImagen)})
	}
}

func (p *Probador) borrarManifiesto(ctx context.Context, base, digest string) (int, error) {
	req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodDelete, base+"/v2/"+pruebaImagen+"/manifests/"+digest, nil)
	if err != nil {
		return 0, err
	}
	resp, err := p.registroHTTP().Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// ---- DNS ----

// probarDNS: the token in use sees the zone and can create and delete a
// test record in it; the target is an IP, resolves, or is auto.
func (p *Probador) probarDNS(ctx context.Context, zona, destino string, res *Resultado) bool {
	if p.DNS == nil {
		res.Pasos = append(res.Pasos, Paso{ID: "zones", Detail: M("DNS automation is not set up (CLOUDFLARE_API_TOKEN): with records made by hand, the zone and target change with Helm")})
		return false
	}
	zonas, err := p.DNS.Zones(ctx)
	if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "zones", Detail: M("the Cloudflare token in use cannot list the zones: %s", err.Error())})
		return false
	}
	zona = strings.ToLower(strings.TrimSuffix(zona, "."))
	if !slices.Contains(zonas, zona) {
		res.Pasos = append(res.Pasos, Paso{ID: "zones", Detail: M("the Cloudflare token in use does not see %s: add it under «Zone Resources», or change the token first", zona)})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "zones", OK: true, Detail: M("the Cloudflare token in use sees %s", zona)})
	if err := p.DNS.TestRecord(ctx, zona); err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "record", Detail: M("it cannot edit the DNS of %s (it needs Zone · DNS · Edit): %s", zona, err.Error())})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "record", OK: true, Detail: M("it created and deleted a test record in %s", zona)})
	switch {
	case strings.EqualFold(destino, "auto"):
		res.Pasos = append(res.Pasos, Paso{ID: "target", OK: true, Detail: M("auto: the records follow this network's public IP")})
	case net.ParseIP(destino) != nil:
		res.Pasos = append(res.Pasos, Paso{ID: "target", OK: true, Detail: M("%s is an IP address", destino)})
	default:
		ips, err := net.DefaultResolver.LookupHost(ctx, destino)
		if err != nil || len(ips) == 0 {
			res.Pasos = append(res.Pasos, Paso{ID: "target", Detail: M("%s does not resolve: the apps' records would point nowhere", destino)})
			return false
		}
		res.Pasos = append(res.Pasos, Paso{ID: "target", OK: true, Detail: M("%s resolves to %s", destino, strings.Join(ips, ", "))})
	}
	return true
}

// ---- The certificate issuer ----

var clusterIssuerGVK = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "ClusterIssuer"}

// probarEmisor: the ClusterIssuer exists and cert-manager says it is ready.
func (p *Probador) probarEmisor(ctx context.Context, name string, res *Resultado) bool {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(clusterIssuerGVK)
	if err := p.Kube.Get(ctx, client.ObjectKey{Name: name}, u); err != nil {
		if apierrors.IsNotFound(err) || meta404(err) {
			res.Pasos = append(res.Pasos, Paso{ID: "exists", Detail: M("there is no ClusterIssuer %s", name)})
		} else {
			res.Pasos = append(res.Pasos, Paso{ID: "exists", Detail: M("cannot read the ClusterIssuer %s: %s", name, err.Error())})
		}
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "exists", OK: true, Detail: M("the ClusterIssuer %s exists", name)})
	conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	for _, c := range conds {
		m, _ := c.(map[string]any)
		if m["type"] != "Ready" {
			continue
		}
		if m["status"] == "True" {
			res.Pasos = append(res.Pasos, Paso{ID: "ready", OK: true, Detail: M("cert-manager says it is ready")})
			return true
		}
		msg, _ := m["message"].(string)
		res.Pasos = append(res.Pasos, Paso{ID: "ready", Detail: M("cert-manager says it is not ready: %s", msg)})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "ready", Detail: M("cert-manager has not said it is ready")})
	return false
}

// meta404 is a kind the cluster does not have (no cert-manager).
func meta404(err error) bool {
	return strings.Contains(err.Error(), "no matches for kind")
}

// ---- The storage class ----

// probarAlmacen: the StorageClass exists, and a small test volume of it
// binds (with a pod that uses it, when the class waits for one); then both
// are deleted.
func (p *Probador) probarAlmacen(ctx context.Context, class string, res *Resultado) bool {
	var sc storagev1.StorageClass
	if err := p.Kube.Get(ctx, client.ObjectKey{Name: class}, &sc); err != nil {
		if apierrors.IsNotFound(err) {
			res.Pasos = append(res.Pasos, Paso{ID: "exists", Detail: M("there is no StorageClass %s", class)})
		} else {
			res.Pasos = append(res.Pasos, Paso{ID: "exists", Detail: M("cannot read the StorageClass %s: %s", class, err.Error())})
		}
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "exists", OK: true, Detail: M("the StorageClass %s exists (%s)", class, sc.Provisioner)})
	name := "prueba-almacen-" + nonce(3)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.BuildNamespace, Labels: map[string]string{"rendimiento.ai/prueba": "almacenamiento"}},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &class,
			Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("64Mi")}},
		},
	}
	if err := p.Kube.Create(ctx, pvc); err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "bind", Detail: M("cannot make a test volume: %s", err.Error())})
		return false
	}
	var pod *corev1.Pod
	defer func() {
		del := context.WithoutCancel(ctx)
		if pod != nil {
			_ = p.Kube.Delete(del, pod, client.GracePeriodSeconds(0))
		}
		_ = p.Kube.Delete(del, pvc)
	}()
	if sc.VolumeBindingMode != nil && *sc.VolumeBindingMode == storagev1.VolumeBindingWaitForFirstConsumer {
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.BuildNamespace, Labels: map[string]string{"rendimiento.ai/prueba": "almacenamiento"}},
			Spec: corev1.PodSpec{
				RestartPolicy:                corev1.RestartPolicyNever,
				AutomountServiceAccountToken: ptr(false),
				ActiveDeadlineSeconds:        ptr(int64(p.esperar().Seconds())),
				Containers: []corev1.Container{{
					Name: "prueba", Image: p.imagenPrueba(), Command: []string{"true"},
					VolumeMounts: []corev1.VolumeMount{{Name: "v", MountPath: "/v"}},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("8Mi")},
						Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("16Mi")},
					},
				}},
				Volumes: []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name}}}},
			},
		}
		if err := p.Kube.Create(ctx, pod); err != nil {
			pod = nil
			res.Pasos = append(res.Pasos, Paso{ID: "bind", Detail: M("cannot start a pod for the test volume: %s", err.Error())})
			return false
		}
	}
	desde := time.Now()
	limite := desde.Add(p.esperar())
	for {
		var got corev1.PersistentVolumeClaim
		if err := p.Kube.Get(ctx, client.ObjectKeyFromObject(pvc), &got); err == nil && got.Status.Phase == corev1.ClaimBound {
			res.Pasos = append(res.Pasos, Paso{ID: "bind", OK: true, Detail: M("a test volume bound in %s, and was deleted", time.Since(desde).Round(time.Second).String())})
			return true
		}
		if time.Now().After(limite) || ctx.Err() != nil {
			res.Pasos = append(res.Pasos, Paso{ID: "bind", Detail: M("a test volume did not bind in %s: look at its provisioner (%s)", p.esperar().String(), sc.Provisioner)})
			return false
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

// ---- The ingress class ----

// controladores are the images of the usual ingress controllers, by the
// controller an IngressClass names.
var controladores = map[string]string{
	"k8s.io/ingress-nginx":           "ingress-nginx",
	"traefik.io/ingress-controller":  "traefik",
	"haproxy.org/ingress-controller": "haproxy",
}

// probarEntrada: the IngressClass exists, and a ready pod of its
// controller runs.
func (p *Probador) probarEntrada(ctx context.Context, class string, res *Resultado) bool {
	var ic networkingv1.IngressClass
	if err := p.Kube.Get(ctx, client.ObjectKey{Name: class}, &ic); err != nil {
		if apierrors.IsNotFound(err) {
			res.Pasos = append(res.Pasos, Paso{ID: "exists", Detail: M("there is no IngressClass %s", class)})
		} else {
			res.Pasos = append(res.Pasos, Paso{ID: "exists", Detail: M("cannot read the IngressClass %s: %s", class, err.Error())})
		}
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "exists", OK: true, Detail: M("the IngressClass %s exists (%s)", class, ic.Spec.Controller)})
	imagen := controladores[ic.Spec.Controller]
	if imagen == "" {
		imagen = ic.Spec.Controller[strings.LastIndex(ic.Spec.Controller, "/")+1:]
	}
	var opts []client.ListOption
	if p.IngressNamespace != nil {
		if ns := p.IngressNamespace(class); ns != "" {
			opts = append(opts, client.InNamespace(ns))
		}
	}
	var pods corev1.PodList
	if err := p.Kube.List(ctx, &pods, opts...); err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "controller", Detail: M("cannot list the pods: %s", err.Error())})
		return false
	}
	for _, pod := range pods.Items {
		if !listo(&pod) {
			continue
		}
		for _, c := range pod.Spec.Containers {
			if imagen != "" && strings.Contains(c.Image, imagen) {
				res.Pasos = append(res.Pasos, Paso{ID: "controller", OK: true, Detail: M("its controller runs: %s/%s", pod.Namespace, pod.Name)})
				return true
			}
		}
	}
	res.Pasos = append(res.Pasos, Paso{ID: "controller", Detail: M("no ready pod of its controller (%s) runs: the apps would not be reached", ic.Spec.Controller)})
	return false
}

func listo(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// ---- The public address ----

// Firmar signs a nonce as this platform: every process has the same key.
func Firmar(llave []byte, n string) string {
	m := hmac.New(sha256.New, llave)
	m.Write([]byte("rendimiento-identidad:" + n))
	return hex.EncodeToString(m.Sum(nil))
}

// LlaveDeIdentidad is the key Firmar signs with: one every process of the
// platform has and nobody outside it does (from DATABASE_URL), never shown.
func LlaveDeIdentidad(secreto string) []byte {
	h := sha256.Sum256([]byte("rendimiento-identidad|" + secreto))
	return h[:]
}

// RutaIdentidad is where the platform answers who it is: GET with ?n=nonce,
// it answers {"firma": Firmar(n)}.
const RutaIdentidad = "/api/identidad"

// probarDireccion: the address resolves, and answers over HTTPS (with a
// valid certificate) as this platform, so a typo cannot lock people out.
func (p *Probador) probarDireccion(ctx context.Context, base string, res *Resultado) bool {
	u, _ := url.Parse(base)
	ips, err := net.DefaultResolver.LookupHost(ctx, u.Hostname())
	if err != nil || len(ips) == 0 {
		res.Pasos = append(res.Pasos, Paso{ID: "resolves", Detail: M("%s does not resolve: make its DNS record first", u.Hostname())})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "resolves", OK: true, Detail: M("%s resolves to %s", u.Hostname(), strings.Join(ips, ", "))})
	n := nonce(16)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+RutaIdentidad+"?n="+n, nil)
	// A redirect is not an answer: an old address that sends people to
	// the one in use (REDIRECT_HOSTS) is not where the platform answers.
	c := *p.http()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "identity", Detail: M("it does not answer over HTTPS: %s", err.Error())})
		return false
	}
	defer resp.Body.Close()
	var out struct {
		Firma string `json:"firma"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &out) != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "identity", Detail: M("it answers, but not as rendimiento (%s)", resp.Status)})
		return false
	}
	if !hmac.Equal([]byte(out.Firma), []byte(Firmar(p.Identidad, n))) {
		res.Pasos = append(res.Pasos, Paso{ID: "identity", Detail: M("it answers as another rendimiento, not this one")})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "identity", OK: true, Detail: M("it answers over HTTPS as this platform")})
	return true
}

func recortar(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func ptr[T any](v T) *T { return &v }
