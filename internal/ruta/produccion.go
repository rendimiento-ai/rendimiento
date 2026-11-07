package ruta

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Produccion is what the production tools need: an agent may ask for a
// change to production, never make it. A person decides (in the UI), and
// the platform does it in their name.
type Produccion interface {
	// PedirReversion records a request to roll app back to version.
	PedirReversion(ctx context.Context, app *store.App, version int64, motivo string, k store.AgentKey) (store.Aprobacion, error)
	GetAprobacion(ctx context.Context, id int64) (store.Aprobacion, error)
}

type pedirIn struct {
	App     string `json:"app" jsonschema:"el nombre de la aplicación"`
	Version int64  `json:"version" jsonschema:"el número de la versión a la que se vuelve"`
	Motivo  string `json:"motivo" jsonschema:"por qué: la persona lo lee para decidir"`
}

type esperarAprobacionIn struct {
	ID       int64 `json:"id" jsonschema:"el número de la solicitud"`
	Segundos int   `json:"segundos,omitempty" jsonschema:"cuánto esperar como máximo en esta llamada, hasta 50 (50 si se omite)"`
}

// addProduccionTools adds produccion_pedir and produccion_esperar, for keys
// that write.
func (s *Server) addProduccionTools(srv herramientas, write bool) {
	if !write {
		return
	}
	addTool(s, srv, &mcp.Tool{Name: "produccion_pedir", Description: "Pide revertir una aplicación a una versión anterior. Usted no puede hacerlo: una persona lo aprueba o lo rechaza, y entonces la plataforma lo hace a su nombre. Diga por qué. Necesita un cargo abierto. Espere la decisión con produccion_esperar."}, s.produccionPedir)
	addTool(s, srv, &mcp.Tool{Name: "produccion_esperar", Description: "Espera la decisión de una persona sobre su solicitud (hasta 50 segundos por llamada) y dice cómo quedó: aprobada (con la versión nueva), rechazada (con el comentario), vencida o fallida. Si sigue pendiente, vuelva a llamar."}, s.produccionEsperar)
}

// produccionPedir (produccion_pedir) asks a person to roll an app back.
func (s *Server) produccionPedir(ctx context.Context, k store.AgentKey, in pedirIn) (store.Aprobacion, error) {
	if _, err := s.cargoFor(ctx, k); err != nil {
		return store.Aprobacion{}, err
	}
	a, err := s.appFor(ctx, k, in.App)
	if err != nil {
		return store.Aprobacion{}, err
	}
	if in.Version <= 0 {
		return store.Aprobacion{}, errors.New("falta la versión a la que se vuelve")
	}
	return s.Produccion.PedirReversion(ctx, a, in.Version, in.Motivo, k)
}

// produccionEsperar (produccion_esperar) waits for a person's decision.
func (s *Server) produccionEsperar(ctx context.Context, k store.AgentKey, in esperarAprobacionIn) (store.Aprobacion, error) {
	wait := time.Duration(in.Segundos) * time.Second
	if wait <= 0 || wait > 50*time.Second {
		wait = 50 * time.Second
	}
	deadline := time.Now().Add(wait)
	for {
		a, err := s.Produccion.GetAprobacion(ctx, in.ID)
		if err == nil && a.OrgID != k.OrgID {
			err = store.ErrNotFound
		}
		if errors.Is(err, store.ErrNotFound) {
			return a, fmt.Errorf("no existe la solicitud %d", in.ID)
		}
		if err != nil || a.Estado != store.AprobacionPendiente || time.Now().After(deadline) {
			return a, err
		}
		select {
		case <-ctx.Done():
			return a, nil
		case <-time.After(s.pausaAprobacion()):
		}
	}
}

// pausaAprobacion is the wait between looks at a request (short in tests).
func (s *Server) pausaAprobacion() time.Duration {
	if s.PausaAprobacion > 0 {
		return s.PausaAprobacion
	}
	return 2 * time.Second
}
