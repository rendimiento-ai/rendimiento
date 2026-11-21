package llaves

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The S3 keys of these tests: an access key's id, and its secret.
const (
	s3Bueno     = "AKBUENO"
	s3Malo      = "AKMALO"    // refused
	s3SoloLee   = "AKSOLOLEE" // may read, not write
	s3SinBorrar = "AKSINBORRAR"
	s3Secreto   = "s3-secreto-123456"
	s3Viejo     = "AKVIEJO"
)

// fakeS3 answers what the log archive's key test asks of an S3 server, for
// one bucket: its location, HEAD, and PUT, GET and DELETE of an object. It
// takes any SigV4 signature, and knows the key by the access key's id in
// it.
type fakeS3 struct {
	bucket string
	mu     sync.Mutex
	objs   map[string][]byte
	puts   int
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s3err := func(status int, code string) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
		}
	}
	auth := r.Header.Get("Authorization")
	_, cred, _ := strings.Cut(auth, "Credential=")
	key, _, _ := strings.Cut(cred, "/")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") || key == "" || key == s3Malo {
		s3err(http.StatusForbidden, "InvalidAccessKeyId")
		return
	}
	bucket, obj, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if bucket != f.bucket {
		s3err(http.StatusNotFound, "NoSuchBucket")
		return
	}
	switch {
	case obj == "" && r.Method == http.MethodGet && r.URL.Query().Has("location"):
		io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`)
	case obj == "" && r.Method == http.MethodHead:
		w.WriteHeader(http.StatusOK)
	case obj != "" && r.Method == http.MethodPut:
		if key == s3SoloLee {
			s3err(http.StatusForbidden, "AccessDenied")
			return
		}
		data, err := cuerpoS3(r)
		if err != nil {
			s3err(http.StatusBadRequest, "IncompleteBody")
			return
		}
		f.mu.Lock()
		f.objs[obj] = data
		f.puts++
		f.mu.Unlock()
		w.Header().Set("ETag", `"e1"`)
		w.WriteHeader(http.StatusOK)
	case obj != "" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		f.mu.Lock()
		data, ok := f.objs[obj]
		f.mu.Unlock()
		if !ok {
			s3err(http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("ETag", `"e1"`)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Type", "text/plain")
		if r.Method == http.MethodGet {
			w.Write(data)
		}
	case obj != "" && r.Method == http.MethodDelete:
		if key == s3SinBorrar {
			s3err(http.StatusForbidden, "AccessDenied")
			return
		}
		f.mu.Lock()
		delete(f.objs, obj)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		s3err(http.StatusNotImplemented, "NotImplemented")
	}
}

// cuerpoS3 is a PUT's object: over plain HTTP the client sends it in signed
// chunks (aws-chunked), each "size;chunk-signature=…\r\n<data>\r\n".
func cuerpoS3(r *http.Request) ([]byte, error) {
	if !strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
		return io.ReadAll(r.Body)
	}
	br := bufio.NewReader(r.Body)
	var out []byte
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, _, _ := strings.Cut(strings.TrimSpace(line), ";")
		n, err := strconv.ParseInt(size, 16, 64)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return out, nil
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(br, buf); err != nil {
			return nil, err
		}
		out = append(out, buf...)
		if _, err := br.ReadString('\n'); err != nil {
			return nil, err
		}
	}
}

func (e *entorno) conArchivo(t *testing.T) *fakeS3 {
	t.Helper()
	f := &fakeS3{bucket: "rendimiento-logs", objs: map[string][]byte{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	e.c.ArchivoEndpoint, e.c.ArchivoBucket = u.Host, "rendimiento-logs"
	if err := e.kube.Create(context.Background(), secret(ns, "rendimiento-logs", "LOG_ARCHIVE_ACCESS_KEY", s3Viejo)); err != nil {
		t.Fatal(err)
	}
	return f
}

// The log archive's new key passes when the bucket is there and it can
// write, read back and delete an object: then both values are saved, in
// one write, and used at once. Any step that fails saves nothing.
func TestArchivo(t *testing.T) {
	for _, c := range []struct {
		access, bucket, pasos, dice string
	}{
		{s3Bueno, "", "verify bucket object save apply", "rendimiento-system/rendimiento-logs"},
		{s3Malo, "", "verify✗", "InvalidAccessKeyId"},
		{s3Bueno, "otra-cubeta", "verify bucket✗", "otra-cubeta"},
		{s3SoloLee, "", "verify bucket object✗", "cannot write"},
		{s3SinBorrar, "", "verify bucket object✗", "cannot delete"},
	} {
		t.Run(c.access+c.bucket, func(t *testing.T) {
			e := nuevo(t, nil)
			f := e.conArchivo(t)
			if c.bucket != "" {
				e.c.ArchivoBucket = c.bucket
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			res, err := e.c.Cambiar(ctx, Registros, Nueva{Usuario: c.access, Valor: s3Secreto}, "p0dxD")
			if err != nil {
				t.Fatal(err)
			}
			if pasos(res) != c.pasos {
				t.Fatalf("steps: %s (%+v)", pasos(res), res.Pasos)
			}
			var donde string
			for _, p := range res.Pasos {
				if !p.OK || p.ID == "save" {
					donde = p.Detail
				}
			}
			if !strings.Contains(donde, c.dice) {
				t.Fatalf("it says %q, not %q", donde, c.dice)
			}
			ok := c.pasos == "verify bucket object save apply"
			if res.Guardada != ok {
				t.Fatalf("saved: %v", res.Guardada)
			}
			got := e.valor(t, ns, "rendimiento-logs", "LOG_ARCHIVE_ACCESS_KEY") + "|" + e.valor(t, ns, "rendimiento-logs", "LOG_ARCHIVE_SECRET_KEY")
			rows, _ := e.store.Llaves(context.Background())
			if ok {
				if got != c.access+"|"+s3Secreto || e.usadas[Registros] != c.access+"|"+s3Secreto || rows[Registros].CambiadaPor != "p0dxD" {
					t.Fatalf("secret %q, in use %q, recorded %+v", got, e.usadas[Registros], rows)
				}
				if f.puts != 1 {
					t.Fatalf("%d test objects written", f.puts)
				}
			} else if got != s3Viejo+"|" || len(rows) != 0 || len(e.usadas) != 0 {
				t.Fatalf("a failed test changed something: secret %q, recorded %v, used %v", got, rows, e.usadas)
			}
			if c.access != s3SinBorrar && len(f.objs) != 0 {
				t.Fatalf("the test object stayed: %v", f.objs)
			}
			e.limpio(t, res, s3Secreto)
		})
	}
}

// fakeUmami answers Umami's login and the websites a user sees.
func fakeUmami(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost:
			var in struct{ Username, Password string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Password != "umami-bueno" {
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, `{"error":"Incorrect username and/or password."}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok-" + in.Username})
		case r.URL.Path == "/api/me/websites":
			if r.Header.Get("Authorization") == "Bearer tok-sin-sitios" {
				io.WriteString(w, `{"data":[]}`)
				return
			}
			io.WriteString(w, `{"data":[{"id":"w1","name":"tienda","domain":"shop.example.org"},{"id":"w2","name":"rendimiento","domain":"rendimiento.example.com"}]}`)
		case r.URL.Path == "/api/me/teams":
			io.WriteString(w, `{"data":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A new Umami user is saved only when Umami lets it in and it sees the
// websites; the user name and password go to the secret together.
func TestUmami(t *testing.T) {
	srv := fakeUmami(t)
	for _, c := range []struct {
		user, pass, pasos string
	}{
		{"lector", "umami-bueno", "verify websites save apply"},
		{"lector", "umami-malo", "verify✗"},
		{"sin-sitios", "umami-bueno", "verify websites✗"},
	} {
		t.Run(c.user+c.pass, func(t *testing.T) {
			e := nuevo(t, nil)
			e.c.UmamiURL = srv.URL
			res, err := e.c.Cambiar(context.Background(), Umami, Nueva{Usuario: c.user, Valor: c.pass}, "p0dxD")
			if err != nil {
				t.Fatal(err)
			}
			if pasos(res) != c.pasos {
				t.Fatalf("steps: %s (%+v)", pasos(res), res.Pasos)
			}
			var s corev1.Secret
			err = e.kube.Get(context.Background(), client.ObjectKey{Namespace: ns, Name: "rendimiento-umami"}, &s)
			if res.Guardada {
				if err != nil || string(s.Data["UMAMI_USERNAME"]) != c.user || string(s.Data["UMAMI_PASSWORD"]) != c.pass || e.usadas[Umami] != c.user+"|"+c.pass {
					t.Fatalf("secret %v %v, in use %q", s.Data, err, e.usadas[Umami])
				}
			} else if err == nil || len(e.usadas) != 0 {
				t.Fatalf("a failed test saved: %v, used %v", s.Data, e.usadas)
			}
			e.limpio(t, res, c.pass)
		})
	}
}

// Another process follows a changed log archive key only once both values
// are in its secret.
func TestRecargadorDosValores(t *testing.T) {
	e := nuevo(t, nil)
	e.conArchivo(t)
	var dado Valores
	r := &Recargador{Store: e.store, Aplicar: map[string]func(Valores){Registros: func(v Valores) { dado = v }},
		Leer: func(ctx context.Context, id string) (Valores, error) { return Leer(ctx, e.kube, ns, id) }}
	if res, _ := e.c.Cambiar(context.Background(), Registros, Nueva{Usuario: s3Bueno, Valor: s3Secreto}, "p0dxD"); !res.Guardada {
		t.Fatalf("%+v", res)
	}
	if err := r.Revisar(context.Background()); err != nil || dado["LOG_ARCHIVE_ACCESS_KEY"] != s3Bueno || dado["LOG_ARCHIVE_SECRET_KEY"] != s3Secreto {
		t.Fatalf("it follows: %v %v", dado, err)
	}
}
