package api

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// MaxOpinion is the longest comment, in characters (decision 145).
const MaxOpinion = 2000

// fichaHuella is the sha256, in hex, of the token an installation keeps
// to fetch the answer to its comment.
var fichaHuella = regexp.MustCompile(`^[0-9a-f]{64}$`)

// OpinionEntrante is a comment as it comes: from an installation (Ficha
// and Datos) or from the public form (Correo, Nombre). Sitio is a field
// people do not see and robots fill.
type OpinionEntrante struct {
	Texto  string `json:"texto"`
	Idioma string `json:"idioma,omitempty"`
	Nombre string `json:"nombre,omitempty"`
	Correo string `json:"correo,omitempty"`
	Ficha  string `json:"ficha,omitempty"`
	Datos  *struct {
		Version string `json:"version,omitempty"`
		Modo    string `json:"modo,omitempty"`
		Motor   string `json:"motor,omitempty"`
		SO      string `json:"so,omitempty"`
	} `json:"datos,omitempty"`
	Sitio string `json:"sitio,omitempty"`
}

// recibirOpinion keeps a comment for the team (POST
// /api/senales/comentarios, no session), only where señales are received.
// Whatever a comment says is someone's words, kept as data: nothing here
// acts on it. The sender's address only counts toward the limits, as a
// salted hash of the day (store.GuardarOpinion).
func (s *Server) recibirOpinion(w http.ResponseWriter, r *http.Request) {
	if !s.SenalesRecibir {
		http.NotFound(w, r)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
	if err != nil {
		httpError(w, http.StatusBadRequest, "unreadable")
		return
	}
	var in OpinionEntrante
	if err := json.Unmarshal(raw, &in); err != nil {
		httpError(w, http.StatusBadRequest, "not a comment")
		return
	}
	if in.Sitio != "" {
		w.WriteHeader(http.StatusNoContent) // a robot: as if kept
		return
	}
	in.Texto = strings.TrimSpace(in.Texto)
	if in.Texto == "" || !utf8.ValidString(in.Texto) || utf8.RuneCountInString(in.Texto) > MaxOpinion {
		httpError(w, http.StatusBadRequest, "a comment is 1 to 2,000 characters")
		return
	}
	c := store.NuevaOpinion{Texto: in.Texto, Remitente: remitente(r)}
	if in.Idioma == "es" || in.Idioma == "en" {
		c.Idioma = in.Idioma
	}
	switch {
	case in.Correo != "":
		a, err := mail.ParseAddress(strings.TrimSpace(in.Correo))
		if err != nil || a.Address != strings.TrimSpace(in.Correo) || len(a.Address) > 254 {
			httpError(w, http.StatusBadRequest, "that email does not look right")
			return
		}
		nombre := strings.TrimSpace(in.Nombre)
		if utf8.RuneCountInString(nombre) > 80 {
			httpError(w, http.StatusBadRequest, "a name is at most 80 characters")
			return
		}
		c.Origen, c.Correo, c.Nombre = store.OpinionFormulario, a.Address, nombre
	case fichaHuella.MatchString(in.Ficha):
		// From an installation: no name, no email, only what its person saw
		// would go (version, mode, engine, system), each kept short.
		c.Origen, c.Ficha = store.OpinionApp, in.Ficha
		if in.Datos != nil {
			corto := func(v string) string {
				if len(v) > 40 {
					return v[:40]
				}
				return v
			}
			d := *in.Datos
			d.Version, d.Modo, d.Motor, d.SO = corto(d.Version), corto(d.Modo), corto(d.Motor), corto(d.SO)
			c.Datos, _ = json.Marshal(d)
		}
	default:
		httpError(w, http.StatusBadRequest, "a comment needs an email to answer it")
		return
	}
	id, err := s.Store.GuardarOpinion(r.Context(), c)
	switch {
	case errors.Is(err, store.ErrDemasiados):
		httpError(w, http.StatusTooManyRequests, "too many comments for now: please try again in an hour")
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSONStatus(w, http.StatusCreated, map[string]int64{"id": id})
	}
}

// remitente is who sent a request, for the limits: the visitor's address
// as Cloudflare or the ingress pass it on, else the connection's.
func remitente(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		return strings.TrimSpace(strings.Split(f, ",")[0])
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
