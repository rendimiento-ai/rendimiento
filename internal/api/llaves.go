package api

import (
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/llaves"
)

// LlaveView is one key on the Keys and settings page: whether it is set,
// whether it changes here (and why not), who changed it last and when it
// expires. Never its value.
type LlaveView struct {
	ID  string `json:"id"`
	Set bool   `json:"set"`
	// Cambiable: it changes here now. Externa: it is not the platform's
	// (something outside it uses it). Reason is why it does not change
	// here, when it does not.
	Cambiable bool `json:"cambiable"`
	Externa   bool `json:"externa,omitempty"`
	// Usuario: the key is two values, who (an access key's id, a user
	// name) and its secret.
	Usuario     bool       `json:"usuario,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	CambiadaPor string     `json:"cambiadaPor,omitempty"`
	CambiadaEn  *time.Time `json:"cambiadaEn,omitempty"`
	VenceEn     *time.Time `json:"venceEn,omitempty"`
	// Zonas is how many DNS zones the Cloudflare token in use manages.
	Zonas int `json:"zonas,omitempty"`
}

// LlavesView is the Keys and settings page: the keys, the settings that
// will change here once they are tested first (read-only for now), and
// the ones that change only with Helm.
type LlavesView struct {
	Llaves  []LlaveView    `json:"llaves"`
	Ajustes []FixedSetting `json:"ajustes"`
	Helm    []string       `json:"helm"`
}

// ajustesProbados are the settings that will change here, tested first.
var ajustesProbados = []string{"REGISTRY", "DNS_ZONE", "DNS_TARGET", "CLUSTER_ISSUER", "STORAGE_CLASS", "INGRESS_CLASS", "BASE_URL"}

// getLlaves is the Keys and settings page.
func (s *Server) getLlaves(w http.ResponseWriter, r *http.Request, _ string) {
	v := LlavesView{Llaves: []LlaveView{}, Ajustes: []FixedSetting{}, Helm: []string{"DATABASE_URL", "SECRETS_KEY_FILE", "MODE"}}
	rows, err := s.Store.Llaves(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var dnsZones int
	var dnsExpira *time.Time
	if s.Environment != nil && s.Kube != nil {
		for _, p := range s.Environment.Report(r.Context(), false).Providers {
			if p.Slot == "dns" {
				dnsZones, dnsExpira = p.Zones, p.ExpiresAt
			}
		}
	}
	for _, id := range llaves.Cambiables {
		l := LlaveView{ID: id}
		switch id {
		case llaves.Cloudflare:
			l.Set, l.Zonas, l.VenceEn = os.Getenv("CLOUDFLARE_API_TOKEN") != "", dnsZones, dnsExpira
		case llaves.Correo:
			l.Set = os.Getenv("RESEND_API_KEY") != ""
		case llaves.IA:
			l.Set = s.IA != nil && s.IA.PlatformKeyNow() != ""
		case llaves.Registros:
			l.Set = os.Getenv("LOG_ARCHIVE_ACCESS_KEY") != ""
		case llaves.Umami:
			l.Set = os.Getenv("UMAMI_PASSWORD") != ""
		}
		l.Usuario = llaves.ConUsuario(id)
		if row, ok := rows[id]; ok {
			l.Set, l.CambiadaPor = true, row.CambiadaPor
			at := row.CambiadaEn
			l.CambiadaEn = &at
			if l.VenceEn == nil {
				l.VenceEn = row.VenceEn
			}
		}
		switch {
		case s.Kube == nil:
			l.Reason = i18n.M("keys change here only on a cluster; on a computer they are in its settings file")
		case s.Llaves == nil:
			l.Reason = i18n.M("keys do not change in this process")
		default:
			l.Reason = s.Llaves.Disponible(id)
		}
		l.Cambiable = l.Reason == ""
		v.Llaves = append(v.Llaves, l)
	}
	for _, id := range llaves.Externas {
		v.Llaves = append(v.Llaves, LlaveView{ID: id, Externa: true, Reason: (&llaves.Cambiador{}).Disponible(id)})
	}
	for _, k := range ajustesProbados {
		val := os.Getenv(k)
		if k == "BASE_URL" {
			val = s.BaseURL
		}
		v.Ajustes = append(v.Ajustes, FixedSetting{Key: k, Value: val, Set: val != ""})
	}
	writeJSON(w, v)
}

// postLlave tests a new key and, only when every test passes, saves it
// where it is used and makes the platform use it, with no restart. The
// answer has every step; it never has the key. The bitácora keeps which
// key, and its expiry; the value is never written anywhere but its Secrets.
func (s *Server) postLlave(w http.ResponseWriter, r *http.Request, login string) {
	id := r.PathValue("llave")
	if slices.Contains(llaves.Externas, id) {
		httpError(w, http.StatusConflict, (&llaves.Cambiador{}).Disponible(id))
		return
	}
	if !slices.Contains(llaves.Cambiables, id) {
		httpError(w, http.StatusNotFound, i18n.M("no such key changes here"))
		return
	}
	if s.Kube == nil || s.Llaves == nil {
		httpError(w, http.StatusConflict, i18n.M("keys change here only on a cluster; on a computer they are in its settings file"))
		return
	}
	var in struct {
		Value    string `json:"value"`
		User     string `json:"user"`
		SendTest bool   `json:"sendTest"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	value, user := strings.TrimSpace(in.Value), strings.TrimSpace(in.User)
	switch {
	case value == "":
		httpError(w, http.StatusBadRequest, i18n.M("paste the new key"))
		return
	case len(value) > 4096 || strings.ContainsAny(value, " \t\r\n"):
		httpError(w, http.StatusBadRequest, i18n.M("that does not look like a key: it has spaces or is too long"))
		return
	case llaves.ConUsuario(id) && user == "":
		httpError(w, http.StatusBadRequest, i18n.M("write who the key is for: the access key's id, or the user name"))
		return
	case len(user) > 256 || strings.ContainsAny(user, "\r\n"):
		httpError(w, http.StatusBadRequest, i18n.M("that does not look like a user: it is too long or has line breaks"))
		return
	}
	anotarDetalle(r, "llave", id)
	res, err := s.Llaves.Cambiar(r.Context(), id, llaves.Nueva{Usuario: user, Valor: value, EnviarPrueba: in.SendTest}, login)
	if errors.Is(err, llaves.ErrDesconocida) {
		httpError(w, http.StatusConflict, strings.TrimPrefix(err.Error(), llaves.ErrDesconocida.Error()+": "))
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anotarDetalle(r, "guardada", res.Guardada)
	if res.VenceEn != nil {
		anotarDetalle(r, "vence", res.VenceEn.UTC().Format(time.RFC3339))
	}
	for _, p := range res.Pasos {
		if !p.OK {
			anotarDetalle(r, "fallo", p.ID)
		}
	}
	if res.Guardada {
		if s.Environment != nil {
			s.Environment.Invalidate()
		}
	} else {
		s.Log.Info("a new key did not pass its test; nothing was saved", "llave", id, "by", login)
	}
	writeJSON(w, res)
}
