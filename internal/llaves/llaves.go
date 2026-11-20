// Package llaves changes the keys the platform talks to other services
// with, from the Environment page (Llaves y ajustes), while it runs: the
// Cloudflare token, the email (Resend) key and the platform's AI key.
//
// A new key is tested first, against the service itself: Cloudflare
// accepts it, sees every zone the apps use and can create and delete a
// record; Resend accepts it; Anthropic answers with it. Only when every
// test passes is it written to the Kubernetes Secrets where it is used (for
// Cloudflare, also the cert-manager ClusterIssuer's), and then the running
// platform uses it with no restart: this process at once, the others
// within 15 seconds (Recargador). When a test fails nothing is saved and
// the key before keeps working.
//
// The value is never logged, returned or stored in the database: the
// database keeps who changed which key, when, and when it expires.
package llaves

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"slices"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/dns"
	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/notify"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The keys that change here.
const (
	Cloudflare = "cloudflare"
	Correo     = "correo"
	IA         = "ia"
)

// Cambiables are the keys that change here, in the order the page lists them.
var Cambiables = []string{Cloudflare, Correo, IA}

// Aviso is how long before a key expires the Environment page warns.
const Aviso = 30 * 24 * time.Hour

// The platform's own Secrets, and the key in each, by key: the
// environment variable the platform reads at start (envFrom).
var propios = map[string]Destino{
	Cloudflare: {Name: "rendimiento-dns", Key: "CLOUDFLARE_API_TOKEN"},
	Correo:     {Name: "rendimiento-notify", Key: "RESEND_API_KEY"},
	IA:         {Name: "rendimiento-ia", Key: "AI_PLATFORM_KEY"},
}

// Destino is where a key's value is kept: one key of a Secret.
type Destino struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Key       string `json:"key"`
}

func (d Destino) String() string { return d.Namespace + "/" + d.Name }

// Paso is one step of a change: a test against the service, saving the
// value, using it. Detail says what it found, or why it failed.
type Paso struct {
	ID     string `json:"id"` // verify, zones, record, send, save, apply
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Resultado is how a change went: every step tried, in order (they stop at
// the first that fails), and whether the new key was saved.
type Resultado struct {
	Llave    string `json:"llave"`
	Guardada bool   `json:"guardada"`
	Pasos    []Paso `json:"pasos"`
	// VenceEn is when the new key expires (nil: it does not, or its service
	// does not say), and AvisoEn when the page starts warning about it.
	VenceEn *time.Time `json:"venceEn,omitempty"`
	AvisoEn *time.Time `json:"avisoEn,omitempty"`
}

// Anotador keeps who changed which key and when (store.Store).
type Anotador interface {
	AnotarLlave(ctx context.Context, llave, por string, venceEn *time.Time) (store.Llave, error)
	Llaves(ctx context.Context) (map[string]store.Llave, error)
}

// ErrDesconocida is a key that does not change here.
var ErrDesconocida = errors.New("no such key changes here")

// Cambiador tests a new key, saves it where it is used and makes the
// platform use it.
type Cambiador struct {
	Kube client.Client
	// Namespace is the platform's, where its Secrets are.
	Namespace string
	// ClusterIssuer is the cert-manager ClusterIssuer of the apps'
	// certificates ("": none), and CertManagerNamespace where cert-manager
	// reads its Secrets (its cluster resource namespace, cert-manager).
	ClusterIssuer        string
	CertManagerNamespace string
	Store                Anotador
	Recargador           *Recargador
	HTTP                 *http.Client
	// Cloudflare: its API (tests), the zones a new token must see (those
	// the apps' and the platform's hosts are in, and DNS_ZONE) and the zone
	// the test record is made in (DNS_ZONE; "": the first it must see).
	CloudflareURL string
	Zonas         func(ctx context.Context) ([]string, error)
	ZonaPrueba    string
	// Resend: its API (tests), whom emails come from and go to, and in
	// which language (for the test email, sent only when asked).
	ResendURL  string
	ResendFrom string
	ResendTo   []string
	Lang       Lang
	// Anthropic: its API (tests) and the platform's model.
	AnthropicURL string
	Modelo       string
	Log          *slog.Logger

	mu sync.Mutex // one change at a time
}

// Disponible says why a key cannot change here now ("" when it can): it
// changes a service the platform already uses, set up when it was
// installed.
func (c *Cambiador) Disponible(id string) string {
	if !slices.Contains(Cambiables, id) {
		return M("this key does not change here yet")
	}
	if c.Recargador == nil || c.Recargador.Aplicar[id] == nil {
		switch id {
		case Cloudflare:
			return M("DNS automation is not set up: set CLOUDFLARE_API_TOKEN and DNS_TARGET with Helm (or in the rendimiento-dns secret) first")
		case Correo:
			return M("email is not set up: set RESEND_API_KEY with Helm (or in the rendimiento-notify secret) first")
		default:
			return M("the platform's AI is not set up in this process")
		}
	}
	return ""
}

// Cambiar tests valor as the new key id and, only when every test passes,
// saves it and uses it. por is who changes it; enviarPrueba sends a test
// email with a new email key. A failed step is in the result, not an error:
// the error is for a key that does not change here.
func (c *Cambiador) Cambiar(ctx context.Context, id, valor, por string, enviarPrueba bool) (Resultado, error) {
	if why := c.Disponible(id); why != "" {
		return Resultado{}, fmt.Errorf("%w: %s", ErrDesconocida, why)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	res := Resultado{Llave: id, Pasos: []Paso{}}
	var ok bool
	switch id {
	case Cloudflare:
		ok = c.probarCloudflare(ctx, valor, &res)
	case Correo:
		ok = c.probarCorreo(ctx, valor, enviarPrueba, &res)
	case IA:
		ok = c.probarIA(ctx, valor, &res)
	}
	if !ok {
		return res, nil
	}
	destinos, err := c.Destinos(ctx, id)
	if err == nil {
		err = c.guardar(ctx, destinos, valor)
	}
	if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "save", Detail: M("nothing was saved: %s", err.Error())})
		return res, nil
	}
	names := make([]string, len(destinos))
	for i, d := range destinos {
		names[i] = d.String()
	}
	res.Guardada = true
	res.Pasos = append(res.Pasos, Paso{ID: "save", OK: true, Detail: M("saved in %s", strings.Join(names, ", "))})
	if res.VenceEn != nil {
		aviso := res.VenceEn.Add(-Aviso)
		res.AvisoEn = &aviso
	}
	l, err := c.Store.AnotarLlave(context.WithoutCancel(ctx), id, por, res.VenceEn)
	c.Recargador.Usar(id, valor, l.Version)
	if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "apply", Detail: M("in use in this process, but the others could not be told (%s): they take it when they restart", err.Error())})
	} else {
		res.Pasos = append(res.Pasos, Paso{ID: "apply", OK: true, Detail: M("in use now, with no restart; every process of the platform follows within 15 seconds")})
	}
	if c.Log != nil {
		c.Log.Info("key changed", "llave", id, "by", por, "secrets", strings.Join(names, ", "))
	}
	return res, nil
}

func (c *Cambiador) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// probarCloudflare: Cloudflare accepts the token and says until when, it
// sees every zone in use, and it can create and delete a record.
func (c *Cambiador) probarCloudflare(ctx context.Context, token string, res *Resultado) bool {
	cf := &dns.Cloudflare{Token: token, BaseURL: c.CloudflareURL, HTTP: c.http()}
	info, err := cf.Verify(ctx)
	switch {
	case err != nil:
		res.Pasos = append(res.Pasos, Paso{ID: "verify", Detail: M("Cloudflare does not accept it: %s", err.Error())})
		return false
	case info.Status != "active":
		res.Pasos = append(res.Pasos, Paso{ID: "verify", Detail: M("Cloudflare says the token is %s, not active", info.Status)})
		return false
	case info.ExpiresOn != nil && time.Until(*info.ExpiresOn) <= 0:
		res.Pasos = append(res.Pasos, Paso{ID: "verify", Detail: M("the token expired on %s", info.ExpiresOn.UTC().Format("2006-01-02"))})
		return false
	case info.ExpiresOn != nil:
		res.VenceEn = info.ExpiresOn
		res.Pasos = append(res.Pasos, Paso{ID: "verify", OK: true, Detail: M("active until %s", info.ExpiresOn.UTC().Format("2006-01-02"))})
	default:
		res.Pasos = append(res.Pasos, Paso{ID: "verify", OK: true, Detail: M("active, with no expiry")})
	}

	zonas, err := cf.Zones(ctx)
	if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "zones", Detail: M("it cannot list the zones (it needs Zone · Zone · Read): %s", err.Error())})
		return false
	}
	if len(zonas) == 0 {
		res.Pasos = append(res.Pasos, Paso{ID: "zones", Detail: M("it sees no zone: give it your zones under «Zone Resources»")})
		return false
	}
	var usadas []string
	if c.Zonas != nil {
		usadas, _ = c.Zonas(ctx) // the zones known now; when the token before is already broken, only DNS_ZONE
	}
	var faltan []string
	for _, z := range usadas {
		if !slices.Contains(zonas, z) {
			faltan = append(faltan, z)
		}
	}
	if len(faltan) > 0 {
		res.Pasos = append(res.Pasos, Paso{ID: "zones", Detail: M("it sees %d of the %d zones your apps use; it does not see %s: add them under «Zone Resources»", len(usadas)-len(faltan), len(usadas), strings.Join(faltan, ", "))})
		return false
	}
	if len(usadas) > 0 {
		res.Pasos = append(res.Pasos, Paso{ID: "zones", OK: true, Detail: M("it sees %d zones, every one your apps use", len(zonas))})
	} else {
		res.Pasos = append(res.Pasos, Paso{ID: "zones", OK: true, Detail: M("it sees %d zones", len(zonas))})
	}

	zona := c.ZonaPrueba
	if zona == "" || !slices.Contains(zonas, zona) {
		zona = zonas[0]
		if len(usadas) > 0 {
			zona = usadas[0]
		}
	}
	if err := cf.TestRecord(ctx, zona); err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "record", Detail: M("it cannot edit the DNS of %s (it needs Zone · DNS · Edit): %s", zona, err.Error())})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "record", OK: true, Detail: M("it created and deleted a test record in %s", zona)})
	return true
}

// probarCorreo: Resend accepts the key (asking for its domains, which
// sends nothing); its account has the domain emails come from; and, only
// when asked, a test email goes out with it.
func (c *Cambiador) probarCorreo(ctx context.Context, key string, enviar bool, res *Resultado) bool {
	dominios, soloEnvia, err := notify.CheckResendKey(ctx, c.http(), c.ResendURL, key)
	if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "verify", Detail: M("Resend does not accept it: %s", err.Error())})
		return false
	}
	desde := dominioDe(c.ResendFrom)
	switch {
	case soloEnvia:
		res.Pasos = append(res.Pasos, Paso{ID: "verify", OK: true, Detail: M("Resend accepts it (a key that may only send)")})
	case desde != "" && !slices.Contains(dominios, desde):
		res.Pasos = append(res.Pasos, Paso{ID: "verify", Detail: M("Resend accepts it, but its account does not have %s, the domain the emails come from", desde)})
		return false
	default:
		res.Pasos = append(res.Pasos, Paso{ID: "verify", OK: true, Detail: M("Resend accepts it; its account has %d domains", len(dominios))})
	}
	if !enviar {
		return true
	}
	if len(c.ResendTo) == 0 {
		res.Pasos = append(res.Pasos, Paso{ID: "send", Detail: M("there is nobody to send it to: NOTIFY_EMAIL_TO is empty")})
		return false
	}
	url := ""
	if c.ResendURL != "" {
		url = strings.TrimRight(c.ResendURL, "/") + "/emails"
	}
	texto := Translate(c.Lang, M("The new email key works: rendimiento sends its notices with it once it is saved."))
	s := &notify.Resend{APIKey: key, From: c.ResendFrom, URL: url, HTTP: c.http()}
	if err := s.Send(ctx, c.ResendTo, Translate(c.Lang, M("rendimiento: a test of the new email key")), "<p>"+texto+"</p>", texto); err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "send", Detail: M("the test email did not go out: %s", err.Error())})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "send", OK: true, Detail: M("a test email went to %s", strings.Join(c.ResendTo, ", "))})
	return true
}

// dominioDe is the domain of an address such as "rendimiento <alerts@example.com>".
func dominioDe(from string) string {
	a, err := mail.ParseAddress(from)
	if err != nil {
		return ""
	}
	_, d, _ := strings.Cut(a.Address, "@")
	return strings.ToLower(d)
}

// probarIA: Anthropic answers with the key, for the platform's model.
func (c *Cambiador) probarIA(ctx context.Context, key string, res *Resultado) bool {
	modelo := c.Modelo
	if modelo == "" {
		modelo = ia.DefaultAnthropicModel
	}
	if err := ia.ProbarLlave(ctx, c.http(), c.AnthropicURL, key, modelo); err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "verify", Detail: M("Anthropic does not accept it: %s", err.Error())})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "verify", OK: true, Detail: M("Anthropic accepts it, and it can use %s", modelo)})
	return true
}

var clusterIssuerGVK = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "ClusterIssuer"}

// Destinos are the Secrets a key is written to: the platform's own, and
// for Cloudflare also the one the ClusterIssuer's DNS-01 solvers read
// (apiTokenSecretRef), in cert-manager's namespace.
func (c *Cambiador) Destinos(ctx context.Context, id string) ([]Destino, error) {
	d, ok := propios[id]
	if !ok {
		return nil, ErrDesconocida
	}
	d.Namespace = c.Namespace
	out := []Destino{d}
	if id != Cloudflare || c.ClusterIssuer == "" {
		return out, nil
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(clusterIssuerGVK)
	if err := c.Kube.Get(ctx, client.ObjectKey{Name: c.ClusterIssuer}, u); apierrors.IsNotFound(err) {
		return out, nil
	} else if err != nil {
		return nil, errors.New(M("cannot read the ClusterIssuer %s: %s", c.ClusterIssuer, err.Error()))
	}
	ns := c.CertManagerNamespace
	if ns == "" {
		ns = "cert-manager"
	}
	solvers, _, _ := unstructured.NestedSlice(u.Object, "spec", "acme", "solvers")
	for _, s := range solvers {
		m, _ := s.(map[string]any)
		name, _, _ := unstructured.NestedString(m, "dns01", "cloudflare", "apiTokenSecretRef", "name")
		key, _, _ := unstructured.NestedString(m, "dns01", "cloudflare", "apiTokenSecretRef", "key")
		if name == "" || key == "" {
			continue
		}
		d := Destino{Namespace: ns, Name: name, Key: key}
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out, nil
}

// guardar writes valor to every destino, or to none: when one cannot be
// written, the ones written before it get their old value back.
func (c *Cambiador) guardar(ctx context.Context, destinos []Destino, valor string) error {
	type antes struct {
		d      Destino
		sec    *corev1.Secret // nil: it did not exist
		tenia  bool
		previo []byte
	}
	var leidos []antes
	for _, d := range destinos {
		var s corev1.Secret
		err := c.Kube.Get(ctx, client.ObjectKey{Namespace: d.Namespace, Name: d.Name}, &s)
		switch {
		case apierrors.IsNotFound(err):
			leidos = append(leidos, antes{d: d})
		case err != nil:
			return errors.New(M("cannot read the secret %s: %s", d.String(), err.Error()))
		default:
			v, tenia := s.Data[d.Key]
			leidos = append(leidos, antes{d: d, sec: &s, tenia: tenia, previo: v})
		}
	}
	var escritos []antes
	for _, a := range leidos {
		var err error
		if a.sec == nil {
			s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: a.d.Namespace, Name: a.d.Name,
				Labels: map[string]string{"app.kubernetes.io/managed-by": "rendimiento"}},
				Data: map[string][]byte{a.d.Key: []byte(valor)}}
			err = c.Kube.Create(ctx, s)
		} else {
			s := a.sec.DeepCopy()
			if s.Data == nil {
				s.Data = map[string][]byte{}
			}
			s.Data[a.d.Key] = []byte(valor)
			err = c.Kube.Update(ctx, s)
		}
		if err != nil {
			for _, w := range escritos {
				c.devolver(ctx, w.d, w.sec, w.tenia, w.previo)
			}
			return errors.New(M("it could not be written to the secret %s: %s", a.d.String(), err.Error()))
		}
		escritos = append(escritos, a)
	}
	return nil
}

// devolver puts a Secret's key back as it was before a change that could
// not be finished.
func (c *Cambiador) devolver(ctx context.Context, d Destino, antes *corev1.Secret, tenia bool, previo []byte) {
	ctx = context.WithoutCancel(ctx)
	var s corev1.Secret
	if err := c.Kube.Get(ctx, client.ObjectKey{Namespace: d.Namespace, Name: d.Name}, &s); err != nil {
		return
	}
	if antes == nil {
		_ = c.Kube.Delete(ctx, &s)
		return
	}
	if tenia {
		s.Data[d.Key] = previo
	} else {
		delete(s.Data, d.Key)
	}
	if err := c.Kube.Update(ctx, &s); err != nil && c.Log != nil {
		c.Log.Error("a key could not be put back after a failed change", "secret", d.String(), "err", err)
	}
}

// Leer reads a key's value from the platform's own Secret ("" when it is
// not there), for the processes that follow a change.
func Leer(ctx context.Context, kube client.Client, namespace, id string) (string, error) {
	d, ok := propios[id]
	if !ok {
		return "", ErrDesconocida
	}
	var s corev1.Secret
	if err := kube.Get(ctx, client.ObjectKey{Namespace: namespace, Name: d.Name}, &s); err != nil {
		return "", err
	}
	return string(s.Data[d.Key]), nil
}

// ZonasUsadas are the zones (of those the DNS provider knows) that hold a
// host in hosts, plus zona (DNS_ZONE) when set: what a new token must see.
func ZonasUsadas(hosts, zonas []string, zona string) []string {
	var out []string
	if zona != "" {
		out = append(out, zona)
	}
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSuffix(h, "."))
		best := ""
		for _, z := range zonas {
			if (h == z || strings.HasSuffix(h, "."+z)) && len(z) > len(best) {
				best = z
			}
		}
		if best != "" && !slices.Contains(out, best) {
			out = append(out, best)
		}
	}
	return out
}
