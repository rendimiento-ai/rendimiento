package llaves

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

const ns = "rendimiento-system"

// The values of the keys in these tests: none may ever show in an answer
// or a log line.
const (
	tokenBueno   = "cf-bueno-123456"
	tokenMalo    = "cf-malo-123456"
	tokenCorto   = "cf-corto-123456" // sees example.com only
	tokenSoloLee = "cf-sololee-123456"
	tokenViejo   = "cf-viejo-123456"
)

// fakeCloudflare answers Cloudflare's API for the tokens above, and counts
// the test records made and deleted.
type fakeCloudflare struct {
	mu               sync.Mutex
	creados, borrado int
}

func (f *fakeCloudflare) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	fail := func(msg string) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]string{{"message": msg}}})
	}
	ok := func(result any) { _ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result}) }
	if tok == tokenMalo {
		w.WriteHeader(http.StatusUnauthorized)
		fail("Invalid API Token")
		return
	}
	switch {
	case r.URL.Path == "/user/tokens/verify":
		ok(map[string]any{"status": "active", "expires_on": "2027-11-20T00:00:00Z"})
	case r.URL.Path == "/zones":
		zones := []map[string]string{{"id": "z1", "name": "example.com"}, {"id": "z2", "name": "example.org"}}
		if tok == tokenCorto {
			zones = zones[:1]
		}
		ok(zones)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/dns_records"):
		if tok == tokenSoloLee {
			fail("Authentication error")
			return
		}
		var rec map[string]any
		_ = json.NewDecoder(r.Body).Decode(&rec)
		if rec["type"] != "TXT" || !strings.HasPrefix(rec["name"].(string), "_rendimiento-prueba.") {
			fail("unexpected record")
			return
		}
		f.mu.Lock()
		f.creados++
		f.mu.Unlock()
		ok(map[string]string{"id": "rec-1"})
	case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/dns_records/rec-1"):
		f.mu.Lock()
		f.borrado++
		f.mu.Unlock()
		ok(map[string]string{"id": "rec-1"})
	default:
		w.WriteHeader(http.StatusNotFound)
		fail("not found: " + r.Method + " " + r.URL.Path)
	}
}

// memStore keeps the keys changed in memory.
type memStore struct {
	mu   sync.Mutex
	rows map[string]store.Llave
}

func (m *memStore) AnotarLlave(_ context.Context, llave, por string, vence *time.Time) (store.Llave, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = map[string]store.Llave{}
	}
	l := m.rows[llave]
	l.Llave, l.Version, l.CambiadaPor, l.CambiadaEn, l.VenceEn = llave, l.Version+1, por, time.Now(), vence
	m.rows[llave] = l
	return l, nil
}

func (m *memStore) Llaves(context.Context) (map[string]store.Llave, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]store.Llave{}
	for k, v := range m.rows {
		out[k] = v
	}
	return out, nil
}

func issuer() *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("cert-manager.io/v1")
	u.SetKind("ClusterIssuer")
	u.SetName("letsencrypt-prod")
	_ = unstructured.SetNestedSlice(u.Object, []any{
		map[string]any{"dns01": map[string]any{"cloudflare": map[string]any{"apiTokenSecretRef": map[string]any{"name": "cloudflare-api-token-secret", "key": "api-token"}}}},
		map[string]any{"http01": map[string]any{"ingress": map[string]any{"class": "nginx"}}},
	}, "spec", "acme", "solvers")
	return u
}

func secret(namespace, name, key, value string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Data: map[string][]byte{key: []byte(value), "OTRA": []byte("se queda")}}
}

type entorno struct {
	c       *Cambiador
	kube    client.Client
	store   *memStore
	logs    *bytes.Buffer
	cf      *fakeCloudflare
	usadas  map[string]string // what each applier was given
	usadasM sync.Mutex
}

func nuevo(t *testing.T, funcs *interceptor.Funcs) *entorno {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	b := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		issuer(),
		secret(ns, "rendimiento-dns", "CLOUDFLARE_API_TOKEN", tokenViejo),
		secret("cert-manager", "cloudflare-api-token-secret", "api-token", tokenViejo),
		secret(ns, "rendimiento-ia", "AI_PLATFORM_KEY", "sk-viejo"),
	)
	if funcs != nil {
		b = b.WithInterceptorFuncs(*funcs)
	}
	kube := b.Build()
	cf := &fakeCloudflare{}
	cfSrv := httptest.NewServer(cf)
	t.Cleanup(cfSrv.Close)
	e := &entorno{kube: kube, store: &memStore{}, logs: &bytes.Buffer{}, cf: cf, usadas: map[string]string{}}
	log := slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	aplicar := func(id string) func(string) {
		return func(v string) { e.usadasM.Lock(); e.usadas[id] = v; e.usadasM.Unlock() }
	}
	rec := &Recargador{Store: e.store, Log: log, Aplicar: map[string]func(string){Cloudflare: aplicar(Cloudflare), Correo: aplicar(Correo), IA: aplicar(IA)},
		Leer: func(ctx context.Context, id string) (string, error) { return Leer(ctx, kube, ns, id) }}
	e.c = &Cambiador{Kube: kube, Namespace: ns, ClusterIssuer: "letsencrypt-prod", CertManagerNamespace: "cert-manager",
		Store: e.store, Recargador: rec, CloudflareURL: cfSrv.URL, ZonaPrueba: "example.com", Log: log,
		Zonas: func(context.Context) ([]string, error) {
			return ZonasUsadas([]string{"shop.example.org", "rendimiento.example.com"}, []string{"example.com", "example.org"}, "example.com"), nil
		}}
	return e
}

func (e *entorno) valor(t *testing.T, namespace, name, key string) string {
	t.Helper()
	var s corev1.Secret
	if err := e.kube.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: name}, &s); err != nil {
		t.Fatal(err)
	}
	if string(s.Data["OTRA"]) != "se queda" {
		t.Fatalf("%s/%s lost its other keys: %v", namespace, name, s.Data)
	}
	return string(s.Data[key])
}

// limpio fails when a value shows in the answer or in the logs.
func (e *entorno) limpio(t *testing.T, res Resultado, valores ...string) {
	t.Helper()
	out, _ := json.Marshal(res)
	for _, v := range valores {
		if strings.Contains(string(out), v) {
			t.Errorf("the answer shows the key: %s", out)
		}
		if strings.Contains(e.logs.String(), v) {
			t.Errorf("the logs show the key: %s", e.logs.String())
		}
	}
}

func pasos(res Resultado) string {
	var b strings.Builder
	for _, p := range res.Pasos {
		if p.OK {
			b.WriteString(p.ID + " ")
		} else {
			b.WriteString(p.ID + "✗ ")
		}
	}
	return strings.TrimSpace(b.String())
}

// A good Cloudflare token: accepted, it sees every zone in use, it makes
// and deletes a test record; then it is written to the platform's secret
// and the issuer's, recorded with its expiry, and used at once.
func TestCloudflareSeGuardaCuandoPasaTodo(t *testing.T) {
	e := nuevo(t, nil)
	res, err := e.c.Cambiar(context.Background(), Cloudflare, tokenBueno, "p0dxD", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Guardada || pasos(res) != "verify zones record save apply" {
		t.Fatalf("steps: %s (%+v)", pasos(res), res)
	}
	if e.cf.creados != 1 || e.cf.borrado != 1 {
		t.Fatalf("test record: made %d, deleted %d", e.cf.creados, e.cf.borrado)
	}
	if got := e.valor(t, ns, "rendimiento-dns", "CLOUDFLARE_API_TOKEN"); got != tokenBueno {
		t.Fatalf("the platform's secret has %q", got)
	}
	if got := e.valor(t, "cert-manager", "cloudflare-api-token-secret", "api-token"); got != tokenBueno {
		t.Fatalf("the issuer's secret has %q", got)
	}
	if e.usadas[Cloudflare] != tokenBueno {
		t.Fatal("this process does not use the new token")
	}
	want := time.Date(2027, 11, 20, 0, 0, 0, 0, time.UTC)
	if res.VenceEn == nil || !res.VenceEn.Equal(want) || res.AvisoEn == nil || !res.AvisoEn.Equal(want.Add(-Aviso)) {
		t.Fatalf("expiry: %v, warning %v", res.VenceEn, res.AvisoEn)
	}
	rows, _ := e.store.Llaves(context.Background())
	if l := rows[Cloudflare]; l.Version != 1 || l.CambiadaPor != "p0dxD" || l.VenceEn == nil || !l.VenceEn.Equal(want) {
		t.Fatalf("recorded: %+v", l)
	}
	if !strings.Contains(res.Pasos[3].Detail, "cert-manager/cloudflare-api-token-secret") {
		t.Fatalf("where it was saved: %q", res.Pasos[3].Detail)
	}
	e.limpio(t, res, tokenBueno, tokenViejo)
}

// Any test that fails saves nothing: the secrets keep the token before,
// nothing is recorded, this process does not change, and the answer says
// which step failed and why.
func TestCloudflareNoSeGuardaSiFalla(t *testing.T) {
	for _, c := range []struct {
		token, pasos, dice string
	}{
		{tokenMalo, "verify✗", "Invalid API Token"},
		{tokenCorto, "verify zones✗", "example.org"},
		{tokenSoloLee, "verify zones record✗", "Authentication error"},
	} {
		t.Run(c.token, func(t *testing.T) {
			e := nuevo(t, nil)
			res, err := e.c.Cambiar(context.Background(), Cloudflare, c.token, "p0dxD", false)
			if err != nil {
				t.Fatal(err)
			}
			if res.Guardada || pasos(res) != c.pasos {
				t.Fatalf("steps: %s", pasos(res))
			}
			if last := res.Pasos[len(res.Pasos)-1].Detail; !strings.Contains(last, c.dice) {
				t.Fatalf("why: %q", last)
			}
			if e.valor(t, ns, "rendimiento-dns", "CLOUDFLARE_API_TOKEN") != tokenViejo || e.valor(t, "cert-manager", "cloudflare-api-token-secret", "api-token") != tokenViejo {
				t.Fatal("a secret changed")
			}
			if rows, _ := e.store.Llaves(context.Background()); len(rows) != 0 || len(e.usadas) != 0 {
				t.Fatalf("recorded %v, used %v", rows, e.usadas)
			}
			e.limpio(t, res, c.token, tokenViejo)
		})
	}
}

// When the issuer's secret cannot be written, the platform's gets its old
// token back: both or neither.
func TestCloudflareAmbosONinguno(t *testing.T) {
	e := nuevo(t, &interceptor.Funcs{Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
		if obj.GetNamespace() == "cert-manager" {
			return errors.New("forbidden")
		}
		return c.Update(ctx, obj, opts...)
	}})
	res, err := e.c.Cambiar(context.Background(), Cloudflare, tokenBueno, "p0dxD", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Guardada || pasos(res) != "verify zones record save✗" || !strings.Contains(res.Pasos[3].Detail, "forbidden") {
		t.Fatalf("steps: %s %+v", pasos(res), res.Pasos)
	}
	if got := e.valor(t, ns, "rendimiento-dns", "CLOUDFLARE_API_TOKEN"); got != tokenViejo {
		t.Fatalf("the platform's secret was left with %q", got)
	}
	if rows, _ := e.store.Llaves(context.Background()); len(rows) != 0 || len(e.usadas) != 0 {
		t.Fatalf("recorded %v, used %v", rows, e.usadas)
	}
	e.limpio(t, res, tokenBueno, tokenViejo)
}

// fakeResend answers GET /domains and POST /emails for three keys.
func fakeResend(t *testing.T, enviados *int) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch {
		case key == "re_malo":
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"statusCode":403,"name":"invalid_api_key","message":"API key is invalid"}`)
		case r.URL.Path == "/domains" && key == "re_solo_envia":
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"statusCode":401,"name":"restricted_api_key","message":"This API key is restricted to only send emails"}`)
		case r.URL.Path == "/domains":
			io.WriteString(w, `{"data":[{"name":"example.com","status":"verified"}]}`)
		case r.URL.Path == "/emails" && r.Method == http.MethodPost:
			*enviados++
			io.WriteString(w, `{"id":"e1"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A new email key is checked without sending anything, unless a test
// email is asked for; a bad one is not saved.
func TestCorreo(t *testing.T) {
	enviados := 0
	srv := fakeResend(t, &enviados)
	for _, c := range []struct {
		key    string
		enviar bool
		pasos  string
		envios int
	}{
		{"re_bueno", false, "verify save apply", 0},
		{"re_solo_envia", true, "verify send save apply", 1},
		{"re_malo", false, "verify✗", 0},
	} {
		t.Run(c.key, func(t *testing.T) {
			enviados = 0
			e := nuevo(t, nil)
			e.c.ResendURL, e.c.ResendFrom, e.c.ResendTo = srv.URL, "rendimiento <alerts@example.com>", []string{"ops@example.com"}
			res, err := e.c.Cambiar(context.Background(), Correo, c.key, "p0dxD", c.enviar)
			if err != nil {
				t.Fatal(err)
			}
			if pasos(res) != c.pasos || enviados != c.envios {
				t.Fatalf("steps %s, %d emails sent", pasos(res), enviados)
			}
			var s corev1.Secret
			err = e.kube.Get(context.Background(), client.ObjectKey{Namespace: ns, Name: "rendimiento-notify"}, &s)
			if res.Guardada != (err == nil) || (err == nil && string(s.Data["RESEND_API_KEY"]) != c.key) {
				t.Fatalf("saved %v, secret %v %v", res.Guardada, s.Data, err)
			}
			e.limpio(t, res, c.key)
		})
	}
}

// The AI key is proved by asking Anthropic for the platform's model.
func TestIA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models/claude-haiku-5-5" || r.Header.Get("x-api-key") != "sk-bueno" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
			return
		}
		io.WriteString(w, `{"id":"claude-haiku-5-5","type":"model"}`)
	}))
	defer srv.Close()
	e := nuevo(t, nil)
	e.c.AnthropicURL = srv.URL
	res, _ := e.c.Cambiar(context.Background(), IA, "sk-malo", "p0dxD", false)
	if res.Guardada || pasos(res) != "verify✗" || !strings.Contains(res.Pasos[0].Detail, "invalid x-api-key") || e.valor(t, ns, "rendimiento-ia", "AI_PLATFORM_KEY") != "sk-viejo" {
		t.Fatalf("a bad key: %+v", res)
	}
	res, _ = e.c.Cambiar(context.Background(), IA, "sk-bueno", "p0dxD", false)
	if !res.Guardada || e.valor(t, ns, "rendimiento-ia", "AI_PLATFORM_KEY") != "sk-bueno" || e.usadas[IA] != "sk-bueno" {
		t.Fatalf("a good key: %+v", res)
	}
	e.limpio(t, res, "sk-bueno", "sk-malo", "sk-viejo")
}

// Another process follows a change within one look: it reads the new value
// from the secret once per version, and keys it does not use are left.
func TestRecargador(t *testing.T) {
	e := nuevo(t, nil)
	otro := map[string]string{}
	r := &Recargador{Store: e.store, Aplicar: map[string]func(string){Cloudflare: func(v string) { otro[Cloudflare] = v }},
		Leer: func(ctx context.Context, id string) (string, error) { return Leer(ctx, e.kube, ns, id) }}
	ctx := context.Background()
	if err := r.Revisar(ctx); err != nil || len(otro) != 0 {
		t.Fatalf("nothing changed yet: %v %v", otro, err)
	}
	if res, _ := e.c.Cambiar(ctx, Cloudflare, tokenBueno, "p0dxD", false); !res.Guardada {
		t.Fatalf("%+v", res)
	}
	if err := r.Revisar(ctx); err != nil || otro[Cloudflare] != tokenBueno {
		t.Fatalf("it follows: %v %v", otro, err)
	}
	otro[Cloudflare] = "tocado"
	_ = r.Revisar(ctx)
	if otro[Cloudflare] != "tocado" {
		t.Fatal("the same version was read twice")
	}
}

// A key changes here only where the platform uses that service already.
func TestDisponible(t *testing.T) {
	c := &Cambiador{Recargador: &Recargador{Aplicar: map[string]func(string){IA: func(string) {}}}}
	if c.Disponible(IA) != "" || c.Disponible(Cloudflare) == "" || c.Disponible(Correo) == "" || c.Disponible("umami") == "" {
		t.Fatal("availability")
	}
	if _, err := c.Cambiar(context.Background(), "umami", "x", "p0dxD", false); !errors.Is(err, ErrDesconocida) {
		t.Fatalf("umami: %v", err)
	}
}

func TestZonasUsadas(t *testing.T) {
	got := ZonasUsadas([]string{"a.shop.example.org", "example.com", "otra.lan", "b.example.com"}, []string{"example.com", "example.org", "shop.example.org"}, "example.net")
	if strings.Join(got, ",") != "example.net,shop.example.org,example.com" {
		t.Fatalf("%v", got)
	}
}
