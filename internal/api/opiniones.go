package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rendimiento-ai/rendimiento/internal/senales"
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

// ---- the team's page of comments (decision 145, step 3) ----

// opinionesVisibles: the comments' routes answer only where comments are
// received; elsewhere, 404.
func (s *Server) opinionesVisibles(w http.ResponseWriter) bool {
	if !s.SenalesRecibir {
		httpError(w, http.StatusNotFound, "this platform does not receive comments")
		return false
	}
	return true
}

// listarOpiniones are the comments, newest first, without emails.
func (s *Server) listarOpiniones(w http.ResponseWriter, r *http.Request, _ string) {
	if !s.opinionesVisibles(w) {
		return
	}
	list, err := s.Store.Opiniones(r.Context(), 500)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, list)
}

func opinionID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpError(w, http.StatusBadRequest, "not a comment's number")
		return 0, false
	}
	return id, true
}

// verOpinion is one comment with its email (seen only here) and answer.
func (s *Server) verOpinion(w http.ResponseWriter, r *http.Request, _ string) {
	if !s.opinionesVisibles(w) {
		return
	}
	id, ok := opinionID(w, r)
	if !ok {
		return
	}
	d, err := s.Store.GetOpinion(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such comment")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, d)
}

// cambiarOpinion changes a comment's class, state, group or draft.
func (s *Server) cambiarOpinion(w http.ResponseWriter, r *http.Request, _ string) {
	if !s.opinionesVisibles(w) {
		return
	}
	id, ok := opinionID(w, r)
	if !ok {
		return
	}
	var c store.CambioOpinion
	if !readJSON(w, r, &c) {
		return
	}
	if c.Clase != nil {
		switch *c.Clase {
		case store.ClaseYaExiste, store.ClaseNuevo, store.ClaseFalla, store.ClaseSinSentido:
			anotarDetalle(r, "clase", *c.Clase)
		default:
			httpError(w, http.StatusBadRequest, "not a class")
			return
		}
	}
	if c.Estado != nil {
		if *c.Estado != store.OpinionPorRevisar && *c.Estado != store.OpinionDescartada {
			httpError(w, http.StatusBadRequest, "a comment is set aside or back to review; it is answered with its own button")
			return
		}
		anotarDetalle(r, "estado", *c.Estado)
	}
	if c.Borrador != nil && utf8.RuneCountInString(*c.Borrador) > 8000 {
		httpError(w, http.StatusBadRequest, "a draft is at most 8,000 characters")
		return
	}
	switch err := s.Store.CambiarOpinion(r.Context(), id, c); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "no such comment")
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		s.verOpinion(w, r, "")
	}
}

// reclasificarOpinion asks the AI to sort and draft a comment again.
func (s *Server) reclasificarOpinion(w http.ResponseWriter, r *http.Request, _ string) {
	if !s.opinionesVisibles(w) {
		return
	}
	id, ok := opinionID(w, r)
	if !ok {
		return
	}
	switch err := s.Store.Reclasificar(r.Context(), id); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "no such comment, or it was answered")
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// responderOpinion sends a person's answer to a comment, with their name:
// by email to whoever wrote from the form; kept for the installation to
// fetch when it came from the app. Once: what was sent was sent.
func (s *Server) responderOpinion(w http.ResponseWriter, r *http.Request, login string) {
	if !s.opinionesVisibles(w) {
		return
	}
	id, ok := opinionID(w, r)
	if !ok {
		return
	}
	var in struct {
		Respuesta string `json:"respuesta"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Respuesta = strings.TrimSpace(in.Respuesta)
	if in.Respuesta == "" || utf8.RuneCountInString(in.Respuesta) > 8000 {
		httpError(w, http.StatusBadRequest, "an answer is 1 to 8,000 characters")
		return
	}
	ctx := r.Context()
	d, err := s.Store.GetOpinion(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such comment")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if d.Estado == store.OpinionRespondida {
		httpError(w, http.StatusConflict, "this comment was already answered")
		return
	}
	if d.Origen == store.OpinionFormulario {
		if d.Correo == "" {
			httpError(w, http.StatusConflict, "this comment's email was deleted: it cannot be answered")
			return
		}
		if s.Notify == nil || s.Notify.Sender == nil {
			httpError(w, http.StatusServiceUnavailable, "email is not set up here (RESEND_API_KEY)")
			return
		}
		subject, html, text := correoRespuesta(d, in.Respuesta, login)
		if err := s.Notify.Sender.Send(ctx, []string{d.Correo}, subject, html, text); err != nil {
			httpError(w, http.StatusBadGateway, "the email could not be sent: "+err.Error())
			return
		}
	}
	if err := s.Store.ResponderOpinion(ctx, id, in.Respuesta, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anotarDetalle(r, "origen", d.Origen)
	s.verOpinion(w, r, login)
}

// correoRespuesta is the email with a person's answer: from them, with the
// comment it answers, in the comment's language; one email, no links.
func correoRespuesta(d store.OpinionDetalle, respuesta, login string) (subject, html, text string) {
	es := d.Idioma != "en"
	subject, saludo, dijo := "Your comment to rendimiento", "Hello", "You wrote:"
	if es {
		subject, saludo, dijo = "Su comentario a rendimiento", "Hola", "Usted escribió:"
	}
	if d.Nombre != "" {
		saludo += " " + d.Nombre
	}
	text = saludo + ",\n\n" + respuesta + "\n\n— " + login + "\n\n" + dijo + "\n> " + strings.ReplaceAll(d.Texto, "\n", "\n> ") + "\n"
	esc := template.HTMLEscapeString
	html = `<div style="font-family:Georgia,serif;font-size:16px;line-height:1.5;color:#2B1B12;max-width:560px">` +
		"<p>" + esc(saludo) + ",</p><p>" + strings.ReplaceAll(esc(respuesta), "\n", "<br>") + "</p><p>— " + esc(login) + "</p>" +
		`<p style="color:#6B5444;font-size:14px">` + esc(dijo) + `</p><blockquote style="color:#6B5444;font-size:14px;border-left:3px solid #E9DCC8;margin:0;padding-left:12px">` +
		strings.ReplaceAll(esc(d.Texto), "\n", "<br>") + "</blockquote></div>"
	return subject, html, text
}

// cuentaOpiniones is how many comments wait for a person, for the bell
// (0 where comments are not received).
func (s *Server) cuentaOpiniones(w http.ResponseWriter, r *http.Request, _ string) {
	n := 0
	if s.SenalesRecibir {
		var err error
		if n, err = s.Store.OpinionesPorRevisar(r.Context()); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, map[string]int{"porRevisar": n})
}

// ---- an installation's own comments (decision 145, step 4) ----

// respuestaOpinion gives an installation the answer to its comment (GET
// /api/senales/comentarios/{id}/respuesta, no session): only to whoever
// holds the token whose hash came with it (X-Ficha). 204 while no person
// answered it.
func (s *Server) respuestaOpinion(w http.ResponseWriter, r *http.Request) {
	if !s.SenalesRecibir {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	token := r.Header.Get("X-Ficha")
	if err != nil || id <= 0 || len(token) != 64 {
		httpError(w, http.StatusNotFound, "no such comment")
		return
	}
	h := sha256.Sum256([]byte(token))
	d, ok, answered, err := s.Store.RespuestaDeOpinion(r.Context(), id, hex.EncodeToString(h[:]))
	switch {
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	case !ok:
		httpError(w, http.StatusNotFound, "no such comment")
	case !answered:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSON(w, map[string]any{"respuesta": d.Respuesta, "respondidoPor": d.RespondidoPor, "respondidoAt": d.RespondidoAt})
	}
}

// MisComentariosView is the page to write to the team: whether comments
// can go from here (señales on) and why not, the comment exactly as it
// would go, and the person's comments with their answers.
type MisComentariosView struct {
	Encendidos bool                       `json:"encendidos"`
	Porque     string                     `json:"porque,omitempty"`
	Vista      senales.ComentarioSaliente `json:"vista"`
	Mios       []store.MiComentario       `json:"mios"`
}

func idiomaDe(r *http.Request) string {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "es") {
		return "es"
	}
	return "en"
}

// verMisComentarios is the page to write to the team; it marks the
// person's answers as seen.
func (s *Server) verMisComentarios(w http.ResponseWriter, r *http.Request, login string) {
	ctx := r.Context()
	v := MisComentariosView{}
	if s.Senales == nil {
		v.Porque = "señales are not set up here"
	} else {
		on, porque, err := s.Senales.Encendidas(ctx)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		v.Encendidos, v.Porque, v.Vista = on, porque, s.Senales.Comentario("", idiomaDe(r))
	}
	mios, err := s.Store.MisComentarios(ctx, login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v.Mios = mios
	_ = s.Store.VerRespuestasEquipo(context.WithoutCancel(ctx), login)
	writeJSON(w, v)
}

// enviarMiComentario sends a person's comment to the team, as the page
// showed it would go, and keeps it here with its token.
func (s *Server) enviarMiComentario(w http.ResponseWriter, r *http.Request, login string) {
	var in struct {
		Texto string `json:"texto"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Texto = strings.TrimSpace(in.Texto)
	if in.Texto == "" || utf8.RuneCountInString(in.Texto) > MaxOpinion {
		httpError(w, http.StatusBadRequest, "a comment is 1 to 2,000 characters")
		return
	}
	ctx := r.Context()
	if s.Senales == nil {
		httpError(w, http.StatusConflict, "señales are not set up here")
		return
	}
	if on, porque, err := s.Senales.Encendidas(ctx); err != nil || !on {
		httpError(w, http.StatusConflict, "nothing leaves this installation: señales are off ("+porque+")")
		return
	}
	remoto, token, err := s.Senales.EnviarComentario(ctx, in.Texto, idiomaDe(r))
	switch {
	case errors.Is(err, senales.ErrDemasiados):
		httpError(w, http.StatusTooManyRequests, err.Error())
		return
	case err != nil:
		httpError(w, http.StatusBadGateway, "the comment could not be sent: "+err.Error())
		return
	}
	if _, err := s.Store.GuardarMiComentario(context.WithoutCancel(ctx), login, in.Texto, remoto, token); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.verMisComentarios(w, r, login)
}

// respuestasEquipoSinVer is how many answers to the person's comments to
// the team they have not seen, for the bell.
func (s *Server) respuestasEquipoSinVer(w http.ResponseWriter, r *http.Request, login string) {
	n, err := s.Store.RespuestasEquipoSinVer(r.Context(), login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]int{"sinVer": n})
}
