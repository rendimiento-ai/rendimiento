package ruta

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Comentarios is where people's comments on agents' work live (the
// store): on a handed-over cargo or on an encargo, without sending it
// back. Agents read them and answer.
type Comentarios interface {
	Comentario(ctx context.Context, id int64) (store.Comentario, error)
	ListComentarios(ctx context.Context, orgID, cargoID, encargoID int64, login string, limit int) ([]store.Comentario, error)
	ComentariosSinResponder(ctx context.Context, orgID int64, limit int) ([]store.Comentario, error)
	ResponderComentario(ctx context.Context, orgID, id, keyID int64, author, body string) (store.Comentario, error)
}

// ComentarioOut is a comment as agents see it.
type ComentarioOut struct {
	ID        int64     `json:"id"`
	Cargo     *int64    `json:"cargo,omitempty"`
	Encargo   *int64    `json:"encargo,omitempty"`
	Sobre     string    `json:"sobre"` // the cargo's purpose or the encargo's title
	RespondeA *int64    `json:"responde_a,omitempty"`
	Autor     string    `json:"autor"`
	PorAgente bool      `json:"por_agente"`
	Texto     string    `json:"texto"`
	Cuando    time.Time `json:"cuando"`

	// Respondido: an agent already answered this person's comment.
	// ParaUsted: it is about this key's own work (its cargo, its encargo).
	Respondido bool `json:"respondido,omitempty"`
	ParaUsted  bool `json:"para_usted,omitempty"`
}

func comentarioOut(c store.Comentario, k store.AgentKey) ComentarioOut {
	return ComentarioOut{ID: c.ID, Cargo: c.CargoID, Encargo: c.EncargoID, Sobre: c.About, RespondeA: c.AnswersTo, Autor: c.Author,
		PorAgente: c.ByAgent, Texto: c.Body, Cuando: c.CreatedAt, Respondido: c.AnsweredAt != nil,
		ParaUsted: c.WorkOf != nil && *c.WorkOf == k.ID}
}

type comentariosIn struct {
	Cargo   int64 `json:"cargo,omitempty" jsonschema:"el número del cargo"`
	Encargo int64 `json:"encargo,omitempty" jsonschema:"el número del encargo"`
}

type comentariosOut struct {
	Comentarios []ComentarioOut `json:"comentarios"`
}

type responderIn struct {
	ID        int64  `json:"id" jsonschema:"el número del comentario de la persona que responde"`
	Respuesta string `json:"respuesta" jsonschema:"qué hizo con lo que pidió la persona, o por qué no; ella lo ve en la campana"`
}

// addComentarioTools registers the comment tools: reading a thread for any
// key, answering for keys that write, inside a cargo.
func (s *Server) addComentarioTools(srv herramientas, write bool) {
	addTool(s, srv, &mcp.Tool{Name: "comentarios_leer", Description: "Los comentarios de una persona sobre una entrega de cargo o un encargo (dé uno de los dos), con las respuestas de los agentes, del más viejo al más nuevo."}, s.comentariosLeer)
	if write {
		addTool(s, srv, &mcp.Tool{Name: "comentario_responder", Description: "Responde al comentario de una persona sobre una entrega o un encargo: qué hizo con lo que pidió, o por qué no. La persona lo ve en la campana. Necesita un cargo abierto."}, s.comentarioResponder)
	}
}

// visible reports whether the key may see a comment: its organization's,
// and of an app it may see.
func visible(c store.Comentario, k store.AgentKey) bool {
	return c.OrgID == k.OrgID && (c.App == "" || k.AllowsApp(c.App))
}

// comentariosLeer (comentarios_leer) is the thread of a cargo or an encargo.
func (s *Server) comentariosLeer(ctx context.Context, k store.AgentKey, in comentariosIn) (comentariosOut, error) {
	if (in.Cargo == 0) == (in.Encargo == 0) {
		return comentariosOut{}, errors.New("diga el cargo o el encargo, uno de los dos")
	}
	if in.Encargo != 0 && s.Encargos != nil {
		if _, err := s.encargoFor(ctx, k, in.Encargo); err != nil {
			return comentariosOut{}, err
		}
	}
	list, err := s.Comentarios.ListComentarios(ctx, k.OrgID, in.Cargo, in.Encargo, "", 0)
	if err != nil {
		return comentariosOut{}, err
	}
	out := comentariosOut{Comentarios: []ComentarioOut{}}
	for _, c := range list {
		if visible(c, k) {
			out.Comentarios = append(out.Comentarios, comentarioOut(c, k))
		}
	}
	return out, nil
}

// comentarioResponder (comentario_responder) answers a person's comment,
// in the agent's cargo.
func (s *Server) comentarioResponder(ctx context.Context, k store.AgentKey, in responderIn) (ComentarioOut, error) {
	if _, err := s.cargoFor(ctx, k); err != nil {
		return ComentarioOut{}, err
	}
	c, err := s.Comentarios.Comentario(ctx, in.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !visible(c, k)) {
		return ComentarioOut{}, fmt.Errorf("no existe el comentario %d", in.ID)
	}
	if err != nil {
		return ComentarioOut{}, err
	}
	if c.ByAgent {
		return ComentarioOut{}, fmt.Errorf("el comentario %d es de un agente; responda al de la persona (responde_a)", in.ID)
	}
	answer := strings.TrimSpace(in.Respuesta)
	switch {
	case answer == "":
		return ComentarioOut{}, errors.New("diga qué hizo con el comentario, o por qué no")
	case len([]rune(answer)) > maxBody:
		return ComentarioOut{}, fmt.Errorf("la respuesta pasa de %d caracteres", maxBody)
	case LooksSecret(answer):
		return ComentarioOut{}, errSecret
	}
	a, err := s.Comentarios.ResponderComentario(ctx, k.OrgID, c.ID, k.ID, k.Name, answer)
	if err != nil {
		return ComentarioOut{}, err
	}
	return comentarioOut(a, k), nil
}

// sinResponder are people's comments no agent has answered, for
// ruta_inicio: the ones on this key's own work first.
func (s *Server) sinResponder(ctx context.Context, k store.AgentKey) ([]ComentarioOut, error) {
	list, err := s.Comentarios.ComentariosSinResponder(ctx, k.OrgID, 20)
	if err != nil {
		return nil, err
	}
	var suyos, otros []ComentarioOut
	for _, c := range list {
		if !visible(c, k) {
			continue
		}
		if o := comentarioOut(c, k); o.ParaUsted {
			suyos = append(suyos, o)
		} else {
			otros = append(otros, o)
		}
	}
	return append(suyos, otros...), nil
}

// hiloEncargo is an encargo's thread (nil when comments are off).
func (s *Server) hiloEncargo(ctx context.Context, k store.AgentKey, id int64) ([]ComentarioOut, error) {
	if s.Comentarios == nil {
		return nil, nil
	}
	list, err := s.Comentarios.ListComentarios(ctx, k.OrgID, 0, id, "", 0)
	if err != nil {
		return nil, err
	}
	var out []ComentarioOut
	for _, c := range list {
		out = append(out, comentarioOut(c, k))
	}
	return out, nil
}

// nuevos are the people's comments in a thread no agent has answered yet.
func nuevos(hilo []ComentarioOut) []ComentarioOut {
	var out []ComentarioOut
	for _, c := range hilo {
		if !c.PorAgente && !c.Respondido {
			out = append(out, c)
		}
	}
	return out
}
