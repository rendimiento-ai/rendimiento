package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// A comment is kept with its email apart, and the limits hold: three an
// hour from one sender, whoever else writes.
func TestOpiniones(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	id, err := s.GuardarOpinion(ctx, NuevaOpinion{Origen: OpinionFormulario, Texto: "¿Funciona con GitLab?", Idioma: "es",
		Nombre: "Ana", Correo: "ana@example.com", Remitente: "203.0.113.7"})
	if err != nil || id == 0 {
		t.Fatalf("keep: %d %v", id, err)
	}
	var correo string
	if err := s.pool.QueryRow(ctx, `SELECT correo FROM opinion_correos WHERE opinion_id = $1`, id).Scan(&correo); err != nil || correo != "ana@example.com" {
		t.Fatalf("email: %q %v", correo, err)
	}
	var remitente string
	if err := s.pool.QueryRow(ctx, `SELECT remitente FROM opiniones WHERE id = $1`, id).Scan(&remitente); err != nil || len(remitente) != 64 || remitente == "203.0.113.7" {
		t.Fatalf("the sender is kept as a hash: %q %v", remitente, err)
	}
	for i := 0; i < OpinionesPorHora-1; i++ {
		if _, err := s.GuardarOpinion(ctx, NuevaOpinion{Origen: OpinionFormulario, Texto: "otro", Correo: "ana@example.com", Remitente: "203.0.113.7"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.GuardarOpinion(ctx, NuevaOpinion{Origen: OpinionFormulario, Texto: "uno más", Correo: "ana@example.com", Remitente: "203.0.113.7"}); !errors.Is(err, ErrDemasiados) {
		t.Fatalf("a fourth in the hour: %v", err)
	}
	datos := json.RawMessage(`{"version":"0.2.2","modo":"local"}`)
	if _, err := s.GuardarOpinion(ctx, NuevaOpinion{Origen: OpinionApp, Texto: "Me gusta", Datos: datos, Ficha: "ab", Remitente: "198.51.100.2"}); err != nil {
		t.Fatalf("another sender is not held back: %v", err)
	}
	got, err := s.Opiniones(ctx, 10)
	if err != nil || len(got) != OpinionesPorHora+1 || got[0].Origen != OpinionApp || got[0].Estado != "por_revisar" || got[len(got)-1].Nombre != "Ana" {
		t.Fatalf("list: %+v %v", got, err)
	}
}

// The AI's classification is kept once, and a comment classified leaves
// the queue for the classifier and joins those a new one may group with.
func TestClasificarOpiniones(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	a, _ := s.GuardarOpinion(ctx, NuevaOpinion{Origen: OpinionFormulario, Texto: "¿Y GitLab?", Correo: "a@example.com", Remitente: "192.0.2.1"})
	b, _ := s.GuardarOpinion(ctx, NuevaOpinion{Origen: OpinionFormulario, Texto: "Quiero GitLab", Correo: "b@example.com", Remitente: "192.0.2.2"})
	cola, err := s.OpinionesSinClasificar(ctx, 10)
	if err != nil || len(cola) != 2 || cola[0].ID != a {
		t.Fatalf("queue: %+v %v", cola, err)
	}
	if err := s.GuardarClasificacion(ctx, a, Clasificacion{Clase: ClaseNuevo, Resumen: "Soporte para GitLab", Borrador: "Gracias", Coincidencias: []string{"ruta:102"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.GuardarClasificacion(ctx, b, Clasificacion{Clase: ClaseNuevo, Resumen: "GitLab", Grupo: &a}); err != nil {
		t.Fatal(err)
	}
	if cola, _ := s.OpinionesSinClasificar(ctx, 10); len(cola) != 0 {
		t.Fatalf("still queued: %+v", cola)
	}
	hechas, err := s.OpinionesClasificadas(ctx, 10)
	if err != nil || len(hechas) != 2 || hechas[0].ID != b || hechas[0].Grupo == nil || *hechas[0].Grupo != a || hechas[1].Coincidencias[0] != "ruta:102" {
		t.Fatalf("classified: %+v %v", hechas, err)
	}
	if err := s.GuardarClasificacion(ctx, 999999, Clasificacion{}); err != ErrNotFound {
		t.Fatalf("no such comment: %v", err)
	}
}
