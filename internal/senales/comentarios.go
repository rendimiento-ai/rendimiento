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

// RecogerRespuestas asks for the answers of the comments that wait for one,
// every few hours (each), until ctx ends. Asking says nothing but the
// comment's number and its token.
func (e *Enviador) RecogerRespuestas(ctx context.Context, st ComentariosStore, cada time.Duration) {
	if cada <= 0 {
		cada = 3 * time.Hour
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cada):
		}
		e.Recoger(ctx, st)
	}
}

// Recoger asks once for the answers that are missing, and keeps those that came.
func (e *Enviador) Recoger(ctx context.Context, st ComentariosStore) {
	espera, err := st.ComentariosSinRespuesta(ctx)
	if err != nil {
		return
	}
	for _, c := range espera {
		r, ok, err := e.PedirRespuesta(ctx, c.Remoto, c.Ficha)
		if err != nil {
			if e.Log != nil {
				e.Log.Warn("comments: asking for an answer", "comment", c.ID, "err", err)
			}
			return // rendimiento.ai cannot be reached now: next time
		}
		if ok {
			_ = st.GuardarRespuestaRecibida(ctx, c.ID, r.Respuesta, r.RespondidoPor, r.RespondidoAt)
		}
	}
}
