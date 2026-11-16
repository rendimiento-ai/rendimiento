package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Opinion is what someone wrote to the team (decision 145): from an
// installation (Origen "app", with its Datos and the hash of its token for
// the answer) or from the public form (Origen "formulario", with an email
// kept apart, in opinion_correos).
type Opinion struct {
	ID        int64           `json:"id"`
	Origen    string          `json:"origen"`
	Texto     string          `json:"texto"`
	Idioma    string          `json:"idioma"`
	Nombre    string          `json:"nombre,omitempty"`
	Datos     json.RawMessage `json:"datos,omitempty"`
	Estado    string          `json:"estado"`
	Clase     string          `json:"clase,omitempty"`
	Resumen   string          `json:"resumen,omitempty"`
	Borrador  string          `json:"borrador,omitempty"`
	Respuesta string          `json:"respuesta,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	// What the platform's AI made of it: what it matches ("libro:guide/ai/",
	// "ruta:105", "opinion:7"), the earlier comment it groups with, when it
	// was classified, and why it could not be ("" when it was).
	Coincidencias []string   `json:"coincidencias"`
	Grupo         *int64     `json:"grupo,omitempty"`
	ClasificadaAt *time.Time `json:"clasificadaAt,omitempty"`
	IAError       string     `json:"iaError,omitempty"`
}

// The classes of decision 145.
const (
	ClaseYaExiste   = "ya_existe"
	ClaseNuevo      = "nuevo"
	ClaseFalla      = "falla"
	ClaseSinSentido = "sin_sentido"
)

// Clasificacion is what the platform's AI made of a comment, or why it
// could not (Error).
type Clasificacion struct {
	Clase, Resumen, Borrador string
	Coincidencias            []string
	Grupo                    *int64
	Error                    string
}

// GuardarClasificacion keeps what the AI made of a comment. A comment is
// classified once; a person may change it later.
func (s *Store) GuardarClasificacion(ctx context.Context, id int64, c Clasificacion) error {
	if c.Coincidencias == nil {
		c.Coincidencias = []string{}
	}
	co, _ := json.Marshal(c.Coincidencias)
	tag, err := s.pool.Exec(ctx, `UPDATE opiniones SET clase = $2, resumen = $3, borrador = $4, coincidencias = $5, grupo = $6,
		ia_error = $7, clasificada_at = now() WHERE id = $1`, id, c.Clase, c.Resumen, c.Borrador, co, c.Grupo, c.Error)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// OpinionesSinClasificar are the comments the AI has not looked at yet,
// oldest first.
func (s *Store) OpinionesSinClasificar(ctx context.Context, limite int) ([]Opinion, error) {
	return s.opiniones(ctx, `WHERE clasificada_at IS NULL ORDER BY id LIMIT $1`, limite)
}

// OpinionesClasificadas are the latest comments the AI classified, newest
// first: what a new one may group with.
func (s *Store) OpinionesClasificadas(ctx context.Context, limite int) ([]Opinion, error) {
	return s.opiniones(ctx, `WHERE clasificada_at IS NOT NULL AND resumen <> '' ORDER BY id DESC LIMIT $1`, limite)
}

// Where a comment comes from.
const (
	OpinionApp        = "app"
	OpinionFormulario = "formulario"
)

// The limits of decision 145: per sender in an hour, and in all in a day.
const (
	OpinionesPorHora = 3
	OpinionesPorDia  = 200
)

// ErrDemasiados: a limit was reached; the sender should try later.
var ErrDemasiados = errors.New("too many comments: try again later")

// NuevaOpinion is a comment to keep: Remitente is who sent it (an
// address, never kept as it is), Ficha the hash of an installation's token
// and Correo the form's email.
type NuevaOpinion struct {
	Origen, Texto, Idioma, Nombre string
	Datos                         json.RawMessage
	Ficha, Correo, Remitente      string
}

// GuardarOpinion keeps a comment, within the limits: at most
// OpinionesPorHora from one sender in an hour and OpinionesPorDia in
// all in a day (ErrDemasiados). The sender is kept only as a hash with a
// salt of this platform's and the day, so the address itself is never
// stored and a day's hash says nothing about another day's.
func (s *Store) GuardarOpinion(ctx context.Context, c NuevaOpinion) (int64, error) {
	sal, err := s.salOpiniones(ctx)
	if err != nil {
		return 0, err
	}
	h := sha256.Sum256([]byte(sal + "|" + time.Now().UTC().Format("2006-01-02") + "|" + c.Remitente))
	remitente := hex.EncodeToString(h[:])
	if len(c.Datos) == 0 {
		c.Datos = json.RawMessage(`{}`)
	}
	var id int64
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// One at a time, so two at once cannot both pass a limit.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(4404)`); err != nil {
			return err
		}
		var hora, dia int
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE remitente = $1 AND created_at > now() - interval '1 hour'),
			count(*) FROM opiniones WHERE created_at > now() - interval '1 day'`, remitente).Scan(&hora, &dia); err != nil {
			return err
		}
		if hora >= OpinionesPorHora || dia >= OpinionesPorDia {
			return ErrDemasiados
		}
		if err := tx.QueryRow(ctx, `INSERT INTO opiniones (origen, texto, idioma, nombre, datos, ficha, remitente)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			c.Origen, c.Texto, c.Idioma, c.Nombre, c.Datos, c.Ficha, remitente).Scan(&id); err != nil {
			return err
		}
		if c.Correo != "" {
			_, err := tx.Exec(ctx, `INSERT INTO opinion_correos (opinion_id, correo) VALUES ($1, $2)`, id, c.Correo)
			return err
		}
		return nil
	})
	return id, err
}

// salOpiniones is this platform's salt for the senders' hashes, made
// the first time (32 random bytes, in platform_settings).
func (s *Store) salOpiniones(ctx context.Context) (string, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM platform_settings WHERE key = 'opiniones_sal'`).Scan(&raw)
	if err == nil {
		var sal string
		return sal, json.Unmarshal(raw, &sal)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	nueva, _ := json.Marshal(hex.EncodeToString(b))
	// Two processes at once keep the same salt: the first wins.
	if _, err := s.pool.Exec(ctx, `INSERT INTO platform_settings (key, value) VALUES ('opiniones_sal', $1) ON CONFLICT (key) DO NOTHING`, nueva); err != nil {
		return "", err
	}
	return s.salOpiniones(ctx)
}

// Comentarios are the comments kept here, newest first (limite at most),
// without the emails: those only go out with an answer.
func (s *Store) Opiniones(ctx context.Context, limite int) ([]Opinion, error) {
	return s.opiniones(ctx, `ORDER BY created_at DESC, id DESC LIMIT $1`, limite)
}

func (s *Store) opiniones(ctx context.Context, where string, args ...any) ([]Opinion, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, origen, texto, idioma, nombre, datos, estado, clase, resumen, borrador, respuesta, created_at,
		coincidencias, grupo, clasificada_at, ia_error FROM opiniones `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Opinion{}
	for rows.Next() {
		var c Opinion
		var co []byte
		if err := rows.Scan(&c.ID, &c.Origen, &c.Texto, &c.Idioma, &c.Nombre, &c.Datos, &c.Estado, &c.Clase, &c.Resumen, &c.Borrador, &c.Respuesta, &c.CreatedAt,
			&co, &c.Grupo, &c.ClasificadaAt, &c.IAError); err != nil {
			return nil, err
		}
		if json.Unmarshal(co, &c.Coincidencias) != nil || c.Coincidencias == nil {
			c.Coincidencias = []string{}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
