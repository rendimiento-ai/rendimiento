package senales

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A comment a person writes to the team from this installation (decision
// 145) goes the way señales go, to the same place, but only when the
// person presses Send, and with only what they see before: their text,
// its language, and this installation's version, mode, engine and system.
// Never their name, the apps, or the installation's identity. It carries
// the hash of a random token that stays here: with it, and only with it,
// this installation fetches the answer.

// DatosComentario are the installation's data a comment carries.
type DatosComentario struct {
	Version string `json:"version"`
	Modo    string `json:"modo"`
	Motor   string `json:"motor"`
	SO      string `json:"so"`
}

// ComentarioSaliente is a comment exactly as it goes.
type ComentarioSaliente struct {
	Texto  string          `json:"texto"`
	Idioma string          `json:"idioma,omitempty"`
	Datos  DatosComentario `json:"datos"`
	Ficha  string          `json:"ficha"`
}

// FichaVista is what the preview shows for the token's hash, made only
// when the comment is sent.
const FichaVista = "<sha256 of a random token kept on this computer>"

// Comentario is the comment that would go with this text: what the person
// sees before sending (with FichaVista for the hash).
func (e *Enviador) Comentario(texto, idioma string) ComentarioSaliente {
	v := e.Version
	if v == "" {
		v = "dev"
	}
	return ComentarioSaliente{Texto: texto, Idioma: idioma, Datos: DatosComentario{Version: v, Modo: e.Modo, Motor: e.Motor, SO: runtime.GOOS}, Ficha: FichaVista}
}

func (e *Enviador) comentariosURL() string {
	u := e.URL
	if u == "" {
		u = URL
	}
	return strings.TrimRight(u, "/") + "/comentarios"
}

func (e *Enviador) client() *http.Client {
	if e.HTTP != nil {
		return e.HTTP
	}
	return http.DefaultClient
}

// ErrDemasiados: rendimiento.ai holds the sender back for now (429).
var ErrDemasiados = fmt.Errorf("too many comments for now: please try again in an hour")

// EnviarComentario sends a comment now: the number rendimiento.ai gave it,
// and the token to keep for its answer.
func (e *Enviador) EnviarComentario(ctx context.Context, texto, idioma string) (int64, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return 0, "", err
	}
	token := hex.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	c := e.Comentario(texto, idioma)
	c.Ficha = hex.EncodeToString(h[:])
	body, _ := json.Marshal(c)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.comentariosURL(), bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "rendimiento/"+c.Datos.Version)
	res, err := e.client().Do(req)
	if err != nil {
		return 0, "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		return 0, "", ErrDemasiados
	case res.StatusCode != http.StatusCreated:
		return 0, "", fmt.Errorf("%s answered %d", e.comentariosURL(), res.StatusCode)
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.ID <= 0 {
		return 0, "", fmt.Errorf("%s gave no number for the comment", e.comentariosURL())
	}
	return out.ID, token, nil
}

// RespuestaRecibida is a person's answer to a comment, as it came.
type RespuestaRecibida struct {
	Respuesta     string    `json:"respuesta"`
	RespondidoPor string    `json:"respondidoPor"`
	RespondidoAt  time.Time `json:"respondidoAt"`
}

// PedirRespuesta asks rendimiento.ai for the answer to comment remoto,
// with its token: ok is false while there is none yet.
func (e *Enviador) PedirRespuesta(ctx context.Context, remoto int64, token string) (RespuestaRecibida, bool, error) {
	var r RespuestaRecibida
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.comentariosURL()+"/"+strconv.FormatInt(remoto, 10)+"/respuesta", nil)
	if err != nil {
		return r, false, err
	}
	req.Header.Set("X-Ficha", token)
	res, err := e.client().Do(req)
	if err != nil {
		return r, false, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusNoContent:
		return r, false, nil
	case http.StatusOK:
		err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&r)
		return r, err == nil && r.Respuesta != "", err
	}
	return r, false, fmt.Errorf("%s answered %d", e.comentariosURL(), res.StatusCode)
}

// ComentariosStore is where this installation keeps its comments.
type ComentariosStore interface {
	ComentariosSinRespuesta(ctx context.Context) ([]store.MiComentario, error)
	GuardarRespuestaRecibida(ctx context.Context, id int64, respuesta, por string, at time.Time) error
}

// When an installation asks for the answers to its comments. The first ask
// comes soon after the process starts, so a computer that restarts or
// sleeps often still asks; then every RespuestasCada while comments wait.
// A comment that has waited longer than RespuestasViejo is asked about
// only every RespuestasCadaViejo. A comment older than 90 days is not
// asked about at all (ComentariosSinRespuesta).
const (
	RespuestasPrimera   = time.Minute
	RespuestasCada      = 30 * time.Minute
	RespuestasViejo     = 24 * time.Hour
	RespuestasCadaViejo = 3 * time.Hour
	// RespuestasPagina is how often opening the comments page may ask.
	RespuestasPagina = time.Minute
)

// Recogida is when this installation asks for its comments' answers; the
// zero value is the schedule above.
type Recogida struct {
	Primera, Cada, Viejo, CadaViejo, Pagina time.Duration
	// Ahora is the clock (time.Now when nil).
	Ahora func() time.Time

	mu         sync.Mutex
	preguntado map[int64]time.Time // comment → when it was last asked about
	pagina     time.Time           // when the page last asked
}

func (r *Recogida) ahora() time.Time {
	if r.Ahora != nil {
		return r.Ahora()
	}
	return time.Now()
}

func porOmision(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Toca is whether comment c is asked about now: always the first time
// since this process started, then once its interval has passed (a
// minute's slack, so a tick a little early still counts).
func (r *Recogida) Toca(c store.MiComentario, ahora time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	ultima, ok := r.preguntado[c.ID]
	if !ok {
		return true
	}
	cada := porOmision(r.Cada, RespuestasCada)
	if ahora.Sub(c.CreatedAt) >= porOmision(r.Viejo, RespuestasViejo) {
		cada = porOmision(r.CadaViejo, RespuestasCadaViejo)
	}
	return ahora.Sub(ultima)+time.Minute >= cada
}

func (r *Recogida) anotar(id int64, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.preguntado == nil {
		r.preguntado = map[int64]time.Time{}
	}
	r.preguntado[id] = at
}

// olvidar drops the comments that no longer wait.
func (r *Recogida) olvidar(espera []store.MiComentario) {
	r.mu.Lock()
	defer r.mu.Unlock()
	siguen := make(map[int64]bool, len(espera))
	for _, c := range espera {
		siguen[c.ID] = true
	}
	for id := range r.preguntado {
		if !siguen[id] {
			delete(r.preguntado, id)
		}
	}
}

// PuedePagina is whether opening the page may ask now (at most once per
// Pagina), and notes that it does.
func (r *Recogida) PuedePagina(ahora time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.pagina.IsZero() && ahora.Sub(r.pagina) < porOmision(r.Pagina, RespuestasPagina) {
		return false
	}
	r.pagina = ahora
	return true
}

// RecogerRespuestas asks for the answers of the comments that wait for one
// (see Recogida for when) until ctx ends. Asking says nothing but the
// comment's number and its token.
func (e *Enviador) RecogerRespuestas(ctx context.Context, st ComentariosStore) {
	r := &e.Respuestas
	espera := porOmision(r.Primera, RespuestasPrimera)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(espera):
		}
		e.recoger(ctx, st, false)
		espera = porOmision(r.Cada, RespuestasCada)
	}
}

// Recoger asks once for every answer that is missing, and keeps those that came.
func (e *Enviador) Recoger(ctx context.Context, st ComentariosStore) {
	e.recoger(ctx, st, true)
}

// RecogerAlVer asks for the missing answers because a person opened their
// comments: at most once per RespuestasPagina, and for no longer than
// espera, so the page does not wait long. A network failure stays quiet.
func (e *Enviador) RecogerAlVer(ctx context.Context, st ComentariosStore, espera time.Duration) {
	pendientes, err := st.ComentariosSinRespuesta(ctx)
	if err != nil || len(pendientes) == 0 || !e.Respuestas.PuedePagina(e.Respuestas.ahora()) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, espera)
	defer cancel()
	e.pedir(ctx, st, pendientes, true)
}

func (e *Enviador) recoger(ctx context.Context, st ComentariosStore, todos bool) {
	espera, err := st.ComentariosSinRespuesta(ctx)
	if err != nil {
		return
	}
	e.Respuestas.olvidar(espera)
	e.pedir(ctx, st, espera, todos)
}

func (e *Enviador) pedir(ctx context.Context, st ComentariosStore, espera []store.MiComentario, todos bool) {
	r := &e.Respuestas
	for _, c := range espera {
		ahora := r.ahora()
		if !todos && !r.Toca(c, ahora) {
			continue
		}
		res, ok, err := e.PedirRespuesta(ctx, c.Remoto, c.Ficha)
		if err != nil {
			if e.Log != nil {
				e.Log.Warn("comments: asking for an answer", "comment", c.ID, "err", err)
			}
			return // rendimiento.ai cannot be reached now: next time
		}
		r.anotar(c.ID, ahora)
		if ok {
			_ = st.GuardarRespuestaRecibida(ctx, c.ID, res.Respuesta, res.RespondidoPor, res.RespondidoAt)
		}
	}
}
