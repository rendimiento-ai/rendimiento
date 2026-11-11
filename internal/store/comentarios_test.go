package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A person comments on a handed-over cargo and on an encargo (which stays
// as it was); an agent answers one; the answer is new for the person until
// they see it, and only people's comments wait for an answer.
func TestComentarios(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	k, err := s.CreateAgentKey(ctx, DefaultOrg, "claude", "hc", RoleCollaborator, nil, "p0dxD", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.TakeCargo(ctx, k.ID, "arreglar la tienda")
	if err := s.HandOver(ctx, c.ID, "Arreglé el carrito", ""); err != nil {
		t.Fatal(err)
	}
	e, _ := s.CreateEncargo(ctx, Encargo{Title: "Revisar la caché", App: "shop", CreatedBy: "p0dxD"})

	enCargo, err := s.ComentarCargo(ctx, DefaultOrg, c.ID, "p0dxD", "El botón quedó muy chico")
	if err != nil || enCargo.About != "arreglar la tienda" || enCargo.WorkOf == nil || *enCargo.WorkOf != k.ID {
		t.Fatalf("on the cargo %+v (%v)", enCargo, err)
	}
	enEncargo, err := s.ComentarEncargo(ctx, DefaultOrg, e.ID, "abi", "Mire también la de imágenes")
	if err != nil || enEncargo.About != "Revisar la caché" || enEncargo.App != "shop" {
		t.Fatalf("on the encargo %+v (%v)", enEncargo, err)
	}
	if e, _ = s.Encargo(ctx, e.ID); e.Status != EncargoPending {
		t.Fatalf("a comment changed the encargo: %s", e.Status)
	}
	if _, err := s.ComentarCargo(ctx, DefaultOrg, 9999, "p0dxD", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("on no cargo: %v", err)
	}
	if _, err := s.ComentarEncargo(ctx, 9999, e.ID, "p0dxD", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("on another organization's encargo: %v", err)
	}

	waiting, err := s.ComentariosSinResponder(ctx, DefaultOrg, 10)
	if err != nil || len(waiting) != 2 || waiting[0].ID != enCargo.ID {
		t.Fatalf("waiting %+v (%v)", waiting, err)
	}
	answer, err := s.ResponderComentario(ctx, DefaultOrg, enCargo.ID, k.ID, "claude", "Lo agrandé en la rama boton-grande")
	if err != nil || !answer.ByAgent || answer.AnswersTo == nil || *answer.AnswersTo != enCargo.ID || answer.CargoID == nil || *answer.CargoID != c.ID {
		t.Fatalf("answer %+v (%v)", answer, err)
	}
	if _, err := s.ResponderComentario(ctx, DefaultOrg, answer.ID, k.ID, "claude", "a mí mismo"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("answering an agent: %v", err)
	}
	if waiting, _ = s.ComentariosSinResponder(ctx, DefaultOrg, 10); len(waiting) != 1 || waiting[0].ID != enEncargo.ID {
		t.Fatalf("still waiting %+v", waiting)
	}

	thread, err := s.ListComentarios(ctx, DefaultOrg, c.ID, 0, "p0dxD", 0)
	if err != nil || len(thread) != 2 || thread[0].ID != enCargo.ID || thread[0].AnsweredAt == nil || !thread[1].New {
		t.Fatalf("thread %+v (%v)", thread, err)
	}
	if thread, _ = s.ListComentarios(ctx, DefaultOrg, c.ID, 0, "abi", 0); thread[1].New {
		t.Fatal("new for someone who did not comment")
	}
	if all, _ := s.ListComentarios(ctx, DefaultOrg, 0, 0, "p0dxD", 0); len(all) != 3 || all[0].ID != enCargo.ID {
		t.Fatalf("all %+v", all)
	}

	news, err := s.RespuestasSinVer(ctx, DefaultOrg, "p0dxD")
	if err != nil || len(news) != 1 || news[0].ID != answer.ID {
		t.Fatalf("new answers %+v (%v)", news, err)
	}
	if err := s.VerRespuestas(ctx, DefaultOrg, "abi", []int64{answer.ID}); err != nil {
		t.Fatal(err)
	}
	if news, _ = s.RespuestasSinVer(ctx, DefaultOrg, "p0dxD"); len(news) != 1 {
		t.Fatal("someone else saw it for the person")
	}
	if err := s.VerRespuestas(ctx, DefaultOrg, "p0dxD", []int64{answer.ID}); err != nil {
		t.Fatal(err)
	}
	if news, _ = s.RespuestasSinVer(ctx, DefaultOrg, "p0dxD"); len(news) != 0 {
		t.Fatalf("seen, still new: %+v", news)
	}
}
