// Package llaves changes the keys the platform talks to other services
// with, from the Environment page (Llaves y ajustes), while it runs: the
// Cloudflare token, the email (Resend) key, the platform's AI key, the log
// archive's S3 key and the Umami user.
//
// A new key is tested first, against the service itself: Cloudflare
// accepts it, sees every zone the apps use and can create and delete a
// record; Resend accepts it; Anthropic answers with it; the log archive's
// bucket is there and the key can write, read and delete an object in it;
// Umami lets the user in and shows it the websites. Only when every
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
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/analytics"
	"github.com/rendimiento-ai/rendimiento/internal/dns"
	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/logarchive"
	"github.com/rendimiento-ai/rendimiento/internal/notify"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The keys that change here.
const (
	Cloudflare = "cloudflare"
	Correo     = "correo"
	IA         = "ia"
	Registros  = "registros" // the log archive's S3 key
	Umami      = "umami"     // the view-only Umami user
	// R2 is the off-site backups' key: listed, but not the platform's (see
	// Externas).
	R2 = "r2"
)

// Cambiables are the keys that change here, in the order the page lists them.
var Cambiables = []string{Cloudflare, Correo, IA, Registros, Umami}

// Externas are keys the page lists that are not the platform's to change:
// something outside it uses them, and it does not write secrets it does
// not own (Disponible says why).
var Externas = []string{R2}

// Aviso is how long before a key expires the Environment page warns.
const Aviso = 30 * 24 * time.Hour

// Secreto is a platform's own Secret and the keys in it that make one of
// its keys: the environment variables the platform reads at start
// (envFrom). With two, the first is who (an access key's id, a user name)
// and the second the secret part.
type Secreto struct {
	Name string
	Keys []string
}

// The platform's own Secrets, by key.
var propios = map[string]Secreto{
	Cloudflare: {Name: "rendimiento-dns", Keys: []string{"CLOUDFLARE_API_TOKEN"}},
	Correo:     {Name: "rendimiento-notify", Keys: []string{"RESEND_API_KEY"}},
	IA:         {Name: "rendimiento-ia", Keys: []string{"AI_PLATFORM_KEY"}},
	Registros:  {Name: "rendimiento-logs", Keys: []string{"LOG_ARCHIVE_ACCESS_KEY", "LOG_ARCHIVE_SECRET_KEY"}},
	Umami:      {Name: "rendimiento-umami", Keys: []string{"UMAMI_USERNAME", "UMAMI_PASSWORD"}},
}

// ConUsuario says whether a key is two values: who, and its secret.
func ConUsuario(id string) bool { return len(propios[id].Keys) == 2 }

// Valores are a key's values, by the key of its Secret
// (LOG_ARCHIVE_ACCESS_KEY, ...). They never go to a log or an answer.
type Valores map[string]string

// completos: every value of the key is there.
func (v Valores) completos(id string) bool {
	for _, k := range propios[id].Keys {
		if v[k] == "" {
			return false
		}
	}
	return len(propios[id].Keys) > 0
}

// Nueva is a new key as it is pasted: its secret and, for a key of two
// values, who (Usuario). EnviarPrueba sends a test email with a new email
// key.
type Nueva struct {
	Usuario      string
	Valor        string
	EnviarPrueba bool
}

// valores are n's values, by the keys of id's Secret.
func (n Nueva) valores(id string) Valores {
	keys := propios[id].Keys
	if len(keys) == 2 {
		return Valores{keys[0]: n.Usuario, keys[1]: n.Valor}
	}
	return Valores{keys[0]: n.Valor}
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
	ID     string `json:"id"` // verify, zones, record, send, bucket, object, websites, save, apply
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
	// The log archive: its S3 endpoint (host:port), bucket and whether it
	// is reached over TLS (LOG_ARCHIVE_*).
	ArchivoEndpoint string
	ArchivoBucket   string
	ArchivoSeguro   bool
	// Umami: its address inside the cluster (UMAMI_URL).
	UmamiURL string
	Log      *slog.Logger

	mu sync.Mutex // one change at a time
}

// Disponible says why a key cannot change here now ("" when it can): it
// changes a service the platform already uses, set up when it was
// installed.
func (c *Cambiador) Disponible(id string) string {
	if id == R2 {
		return M("the off-site backups are copied to R2 by a job outside rendimiento, with its own secret: change its key where that job is set up")
	}
	if !slices.Contains(Cambiables, id) {
		return M("this key does not change here yet")
	}
	if c.Recargador == nil || c.Recargador.Aplicar[id] == nil {
		switch id {
		case Cloudflare:
			return M("DNS automation is not set up: set CLOUDFLARE_API_TOKEN and DNS_TARGET with Helm (or in the rendimiento-dns secret) first")
		case Correo:
			return M("email is not set up: set RESEND_API_KEY with Helm (or in the rendimiento-notify secret) first")
		case Registros:
			return M("the log archive is not set up: set LOG_ARCHIVE_ENDPOINT in the rendimiento ConfigMap, with its key in the rendimiento-logs secret, first")
		case Umami:
			return M("visitor numbers are not set up: set UMAMI_URL in the rendimiento ConfigMap, with its user in the rendimiento-umami secret, first")
		default:
			return M("the platform's AI is not set up in this process")
		}
	}
	return ""
}

// Cambiar tests n as the new key id and, only when every test passes,
// saves it and uses it. por is who changes it. A failed step is in the
// result, not an error: the error is for a key that does not change here.
func (c *Cambiador) Cambiar(ctx context.Context, id string, n Nueva, por string) (Resultado, error) {
	if why := c.Disponible(id); why != "" {
		return Resultado{}, fmt.Errorf("%w: %s", ErrDesconocida, why)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	res := Resultado{Llave: id, Pasos: []Paso{}}
	var ok bool
	switch id {
	case Cloudflare:
		ok = c.probarCloudflare(ctx, n.Valor, &res)
	case Correo:
		ok = c.probarCorreo(ctx, n.Valor, n.EnviarPrueba, &res)
	case IA:
		ok = c.probarIA(ctx, n.Valor, &res)
	case Registros:
		ok = c.probarArchivo(ctx, n.Usuario, n.Valor, &res)
	case Umami:
		ok = c.probarUmami(ctx, n.Usuario, n.Valor, &res)
	}
	if !ok {
		return res, nil
	}
	valores := n.valores(id)
	destinos, err := c.Destinos(ctx, id)
	if err == nil {
		err = c.guardar(ctx, destinos, valores, n.Valor)
	}
	if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "save", Detail: M("nothing was saved: %s", err.Error())})
		return res, nil
	}
	var names []string
	for _, d := range destinos {
		if !slices.Contains(names, d.String()) {
			names = append(names, d.String())
		}
	}
	res.Guardada = true
	res.Pasos = append(res.Pasos, Paso{ID: "save", OK: true, Detail: M("saved in %s", strings.Join(names, ", "))})
	if res.VenceEn != nil {
		aviso := res.VenceEn.Add(-Aviso)
		res.AvisoEn = &aviso
	}
	l, err := c.Store.AnotarLlave(context.WithoutCancel(ctx), id, por, res.VenceEn)
	c.Recargador.Usar(id, valores, l.Version)
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

// probarArchivo: the log archive's S3 accepts the key, its bucket is
// there, and the key can write, read back and delete a small object in it.
func (c *Cambiador) probarArchivo(ctx context.Context, access, secret string, res *Resultado) bool {
	m, err := logarchive.NewMinIO(c.ArchivoEndpoint, access, secret, c.ArchivoBucket, c.ArchivoSeguro)
	if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "verify", Detail: M("cannot reach the archive: %s", err.Error())})
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	hay, err := m.Client.BucketExists(ctx, m.Bucket)
	if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "verify", Detail: M("the archive at %s does not accept it: %s", c.ArchivoEndpoint, err.Error())})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "verify", OK: true, Detail: M("the archive at %s accepts it", c.ArchivoEndpoint)})
	if !hay {
		res.Pasos = append(res.Pasos, Paso{ID: "bucket", Detail: M("the bucket %s does not exist, or this key cannot see it", m.Bucket)})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "bucket", OK: true, Detail: M("the bucket %s is there", m.Bucket)})
	return probarObjeto(ctx, m.Client, m.Bucket, res)
}

// probarObjeto writes a small object, reads it back and deletes it: a key
// that may only read, or only write, does not pass.
func probarObjeto(ctx context.Context, cl *minio.Client, bucket string, res *Resultado) bool {
	nombre := fmt.Sprintf("_rendimiento-prueba/%d.txt", time.Now().UnixNano())
	cuerpo := []byte("rendimiento: a test of a new key; it is deleted at once\n")
	if _, err := cl.PutObject(ctx, bucket, nombre, strings.NewReader(string(cuerpo)), int64(len(cuerpo)), minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "object", Detail: M("it cannot write to the bucket: %s", err.Error())})
		return false
	}
	borrar := func() error { return cl.RemoveObject(context.WithoutCancel(ctx), bucket, nombre, minio.RemoveObjectOptions{}) }
	obj, err := cl.GetObject(ctx, bucket, nombre, minio.GetObjectOptions{})
	var leido []byte
	if err == nil {
		leido, err = io.ReadAll(io.LimitReader(obj, 4096))
		obj.Close()
	}
	if err == nil && string(leido) != string(cuerpo) {
		err = errors.New(M("what came back is not what was written"))
	}
	if err != nil {
		_ = borrar()
		res.Pasos = append(res.Pasos, Paso{ID: "object", Detail: M("it wrote a test object but cannot read it back: %s", err.Error())})
		return false
	}
	if err := borrar(); err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "object", Detail: M("it wrote and read a test object but cannot delete it (%s): the archive deletes old logs with it", err.Error())})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "object", OK: true, Detail: M("it wrote, read back and deleted a test object")})
	return true
}

// probarUmami: Umami lets the user in, and the user sees websites (a
// view-only user is enough).
func (c *Cambiador) probarUmami(ctx context.Context, usuario, clave string, res *Resultado) bool {
	u := &analytics.Umami{URL: c.UmamiURL, Username: usuario, Password: clave, HTTP: c.http()}
	if err := u.SignIn(ctx); errors.Is(err, analytics.ErrAuth) {
		res.Pasos = append(res.Pasos, Paso{ID: "verify", Detail: M("Umami does not let %s in: the user name or the password is wrong", usuario)})
		return false
	} else if err != nil {
		res.Pasos = append(res.Pasos, Paso{ID: "verify", Detail: M("cannot reach Umami: %s", err.Error())})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "verify", OK: true, Detail: M("Umami lets %s in", usuario)})
	sitios, err := u.Websites(ctx)
	switch {
	case err != nil:
		res.Pasos = append(res.Pasos, Paso{ID: "websites", Detail: M("it cannot list the websites: %s", err.Error())})
		return false
	case len(sitios) == 0:
		res.Pasos = append(res.Pasos, Paso{ID: "websites", Detail: M("it sees no website: add the user to the team that owns them")})
		return false
	}
	res.Pasos = append(res.Pasos, Paso{ID: "websites", OK: true, Detail: M("it sees %d websites", len(sitios))})
	return true
}

var clusterIssuerGVK = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "ClusterIssuer"}

// Destinos are the Secrets a key is written to: the platform's own, and
// for Cloudflare also the one the ClusterIssuer's DNS-01 solvers read
// (apiTokenSecretRef), in cert-manager's namespace.
func (c *Cambiador) Destinos(ctx context.Context, id string) ([]Destino, error) {
	p, ok := propios[id]
	if !ok {
		return nil, ErrDesconocida
	}
	var out []Destino
	for _, k := range p.Keys {
		out = append(out, Destino{Namespace: c.Namespace, Name: p.Name, Key: k})
	}
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

// guardar writes the values to every destino, or to none: when one
// Secret cannot be written, the ones written before it get their old
// values back. A destino whose key is not one of valores (the
// ClusterIssuer's) gets secreto.
func (c *Cambiador) guardar(ctx context.Context, destinos []Destino, valores Valores, secreto string) error {
	type antes struct {
		ns, name string
		sec      *corev1.Secret     // nil: it did not exist
		nuevos   map[string][]byte  // what is written
		previos  map[string][]byte  // the keys it had before (nil: it did not have it)
		teniaKey map[string]bool
	}
	var leidos []*antes
	porSecreto := map[string]*antes{}
	for _, d := range destinos {
		a := porSecreto[d.String()]
		if a == nil {
			a = &antes{ns: d.Namespace, name: d.Name, nuevos: map[string][]byte{}, previos: map[string][]byte{}, teniaKey: map[string]bool{}}
			var s corev1.Secret
			err := c.Kube.Get(ctx, client.ObjectKey{Namespace: d.Namespace, Name: d.Name}, &s)
			switch {
			case apierrors.IsNotFound(err):
			case err != nil:
				return errors.New(M("cannot read the secret %s: %s", d.String(), err.Error()))
			default:
				a.sec = &s
			}
			porSecreto[d.String()] = a
			leidos = append(leidos, a)
		}
		v, ok := valores[d.Key]
		if !ok {
			v = secreto
		}
		a.nuevos[d.Key] = []byte(v)
		if a.sec != nil {
			a.previos[d.Key], a.teniaKey[d.Key] = a.sec.Data[d.Key]
		}
	}
	var escritos []*antes
	for _, a := range leidos {
		var err error
		if a.sec == nil {
			s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: a.ns, Name: a.name,
				Labels: map[string]string{"app.kubernetes.io/managed-by": "rendimiento"}},
				Data: a.nuevos}
			err = c.Kube.Create(ctx, s)
		} else {
			s := a.sec.DeepCopy()
			if s.Data == nil {
				s.Data = map[string][]byte{}
			}
			for k, v := range a.nuevos {
				s.Data[k] = v
			}
			err = c.Kube.Update(ctx, s)
		}
		if err != nil {
			for _, w := range escritos {
				c.devolver(ctx, w.ns, w.name, w.sec == nil, w.previos, w.teniaKey)
			}
			return errors.New(M("it could not be written to the secret %s: %s", a.ns+"/"+a.name, err.Error()))
		}
		escritos = append(escritos, a)
	}
	return nil
}

// devolver puts a Secret's keys back as they were before a change that
// could not be finished (nuevo: the change made the Secret).
func (c *Cambiador) devolver(ctx context.Context, ns, name string, nuevo bool, previos map[string][]byte, tenia map[string]bool) {
	ctx = context.WithoutCancel(ctx)
	var s corev1.Secret
	if err := c.Kube.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, &s); err != nil {
		return
	}
	if nuevo {
		_ = c.Kube.Delete(ctx, &s)
		return
	}
	for k, v := range previos {
		if tenia[k] {
			s.Data[k] = v
		} else {
			delete(s.Data, k)
		}
	}
	if err := c.Kube.Update(ctx, &s); err != nil && c.Log != nil {
		c.Log.Error("a key could not be put back after a failed change", "secret", ns+"/"+name, "err", err)
	}
}

// Leer reads a key's values from the platform's own Secret ("" for those
// not there), for the processes that follow a change.
func Leer(ctx context.Context, kube client.Client, namespace, id string) (Valores, error) {
	p, ok := propios[id]
	if !ok {
		return nil, ErrDesconocida
	}
	var s corev1.Secret
	if err := kube.Get(ctx, client.ObjectKey{Namespace: namespace, Name: p.Name}, &s); err != nil {
		return nil, err
	}
	v := Valores{}
	for _, k := range p.Keys {
		v[k] = string(s.Data[k])
	}
	return v, nil
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
