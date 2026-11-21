package llaves

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Versiones are the keys changed and their versions (store.Store).
type Versiones interface {
	Llaves(ctx context.Context) (map[string]store.Llave, error)
}

// Recargador makes one process of the platform follow the keys changed on
// the Environment page, with no restart: every 15 seconds it looks at
// their versions in the database, and for each one newer than the one it
// uses it reads the value from its Secret and hands it to Aplicar (the DNS
// provider, the email sender, the AI gateway). A key without Aplicar is not
// used in this process.
type Recargador struct {
	Store Versiones
	// Leer reads a key's value from where it is kept (its Secret).
	Leer    func(ctx context.Context, id string) (string, error)
	Aplicar map[string]func(valor string)
	Log     *slog.Logger

	mu    sync.Mutex
	visto map[string]int64
}

// Revisar follows the keys changed since the last look. At the first one,
// a key changed before this process started is read once more: its Secret
// may have been written after the process read its environment.
func (r *Recargador) Revisar(ctx context.Context) error {
	rows, err := r.Store.Llaves(ctx)
	if err != nil {
		return err
	}
	for id, l := range rows {
		aplicar := r.Aplicar[id]
		if aplicar == nil {
			continue
		}
		r.mu.Lock()
		visto := r.visto[id]
		r.mu.Unlock()
		if visto >= l.Version {
			continue
		}
		valor, err := r.Leer(ctx, id)
		if err != nil {
			if r.Log != nil {
				r.Log.Warn("a changed key could not be read; trying again in 15 seconds", "llave", id, "err", err)
			}
			continue
		}
		r.mu.Lock()
		if r.visto[id] < l.Version { // not used here meanwhile (Usar)
			if valor != "" {
				aplicar(valor)
			}
			r.marcar(id, l.Version)
			if r.Log != nil {
				r.Log.Info("following a changed key", "llave", id, "version", l.Version)
			}
		}
		r.mu.Unlock()
	}
	return nil
}

// Usar uses a key just changed in this process, at once.
func (r *Recargador) Usar(id, valor string, version int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if aplicar := r.Aplicar[id]; aplicar != nil {
		aplicar(valor)
	}
	if version > 0 {
		r.marcar(id, version)
	}
}

func (r *Recargador) marcar(id string, version int64) {
	if r.visto == nil {
		r.visto = map[string]int64{}
	}
	if version > r.visto[id] {
		r.visto[id] = version
	}
}

// Run looks every 15 seconds until ctx ends.
func (r *Recargador) Run(ctx context.Context) {
	for {
		if err := r.Revisar(ctx); err != nil && r.Log != nil {
			r.Log.Warn("could not read which keys changed; keeping the ones in use", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
	}
}
