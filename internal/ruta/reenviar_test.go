package ruta

import (
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// encargo_leer and encargo_tomar say why it was given back last and what
// the person told the agent when sending it again, besides its history.
func TestEncargoOutSaysWhyAndWhatThePersonSaid(t *testing.T) {
	e := store.Encargo{ID: 7, Title: "Endurecer", Status: store.EncargoPending, Log: []store.EncargoLog{
		{What: "released", Who: "agente de código", Note: "Falta saber si quiere lock_timeout."},
		{What: "routed", Who: "p0dxD", Note: "local"},
		{What: "told", Who: "p0dxD", Note: "Sí, con lock_timeout."},
	}}
	o := encargoOut(e, true)
	if o.Solto != "Falta saber si quiere lock_timeout." || len(o.LaPersonaDijo) != 1 || o.LaPersonaDijo[0] != "Sí, con lock_timeout." {
		t.Fatalf("what it says: %q %q", o.Solto, o.LaPersonaDijo)
	}
	if len(o.Historia) != 3 || o.Historia[2].Que != "la persona le dijo" || o.Historia[2].Nota != "Sí, con lock_timeout." {
		t.Fatalf("its history: %+v", o.Historia)
	}
	if o := encargoOut(e, false); o.Solto != "" || o.LaPersonaDijo != nil {
		t.Fatalf("in a list: %+v", o)
	}
}
