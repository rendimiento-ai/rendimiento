package store

import (
	"context"
	"crypto/subtle"
	"time"
)

// MiComentario is a comment a person of this installation wrote to the
// team (decision 145), with its answer once it came. Ficha, the token that
// fetches the answer, never leaves this database: only its hash went out.
type MiComentario struct {
	ID            int64      `json:"id"`
	Login         string     `json:"-"`
	Texto         string     `json:"texto"`
	Remoto        int64      `json:"remoto"`
	Ficha         string     `json:"-"`
	Respuesta     string     `json:"respuesta,omitempty"`
	RespondidoPor string     `json:"respondidoPor,omitempty"`
	RespondidoAt  *time.Time `json:"respondidoAt,omitempty"`
	Visto         bool       `json:"visto"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// GuardarMiComentario keeps a comment this installation sent.
func (s *Store) GuardarMiComentario(ctx context.Context, login, texto string, remoto int64, ficha string) (MiComentario, error) {
	c := MiComentario{Login: login, Texto: texto, Remoto: remoto, Ficha: ficha}
	err := s.pool.QueryRow(ctx, `INSERT INTO mis_comentarios (login, texto, remoto, ficha) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		login, texto, remoto, ficha).Scan(&c.ID, &c.CreatedAt)
	return c, err
}

const misCols = `id, login, texto, remoto, ficha, respuesta, respondido_por, respondido_at, visto, created_at`

func (s *Store) misComentarios(ctx context.Context, where string, args ...any) ([]MiComentario, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+misCols+` FROM mis_comentarios `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MiComentario{}
	for rows.Next() {
		var c MiComentario
		if err := rows.Scan(&c.ID, &c.Login, &c.Texto, &c.Remoto, &c.Ficha, &c.Respuesta, &c.RespondidoPor, &c.RespondidoAt, &c.Visto, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MisComentarios are a person's comments, newest first.
func (s *Store) MisComentarios(ctx context.Context, login string) ([]MiComentario, error) {
	return s.misComentarios(ctx, `WHERE lower(login) = lower($1) ORDER BY created_at DESC LIMIT 100`, login)
}

// ComentariosSinRespuesta are the comments still waiting for an answer,
// written within the last 90 days (an older one is not asked about again).
func (s *Store) ComentariosSinRespuesta(ctx context.Context) ([]MiComentario, error) {
	return s.misComentarios(ctx, `WHERE respondido_at IS NULL AND created_at > now() - interval '90 days' ORDER BY id`)
}

// GuardarRespuestaRecibida keeps the answer that came for a comment.
func (s *Store) GuardarRespuestaRecibida(ctx context.Context, id int64, respuesta, por string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE mis_comentarios SET respuesta = $2, respondido_por = $3, respondido_at = $4 WHERE id = $1 AND respondido_at IS NULL`,
		id, respuesta, por, at)
	return err
}

// RespuestasEquipoSinVer is how many answers a person has not seen.
func (s *Store) RespuestasEquipoSinVer(ctx context.Context, login string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM mis_comentarios WHERE lower(login) = lower($1) AND respondido_at IS NOT NULL AND NOT visto`, login).Scan(&n)
	return n, err
}

// VerRespuestas marks a person's answers as seen.
func (s *Store) VerRespuestasEquipo(ctx context.Context, login string) error {
	_, err := s.pool.Exec(ctx, `UPDATE mis_comentarios SET visto = true WHERE lower(login) = lower($1) AND respondido_at IS NOT NULL AND NOT visto`, login)
	return err
}

// RespuestaDeOpinion is the answer to comment id, for the installation that
// holds the token whose hash is ficha: ok is false when there is no such
// comment or the token is not its own, and answered false while no person
// has answered it.
func (s *Store) RespuestaDeOpinion(ctx context.Context, id int64, ficha string) (o OpinionDetalle, ok, answered bool, err error) {
	d, err := s.GetOpinion(ctx, id)
	if err == ErrNotFound {
		return o, false, false, nil
	}
	if err != nil {
		return o, false, false, err
	}
	var suya string
	if err := s.pool.QueryRow(ctx, `SELECT ficha FROM opiniones WHERE id = $1`, id).Scan(&suya); err != nil {
		return o, false, false, err
	}
	if suya == "" || subtle.ConstantTimeCompare([]byte(suya), []byte(ficha)) != 1 {
		return o, false, false, nil
	}
	return d, true, d.Estado == OpinionRespondida, nil
}
