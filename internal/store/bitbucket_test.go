package store

import (
	"context"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// A Bitbucket workspace is kept once (connecting it again keeps its id),
// and its apps say so.
func TestBitbucketConnections(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	c, err := s.AddBitbucketConnection(ctx, "tienda", "llave-1", "p0dxD")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.AddBitbucketConnection(ctx, "Tienda", "llave-2", "ana")
	if err != nil || again.ID != c.ID || again.ConsumerKey != "llave-2" {
		t.Fatalf("connecting the same workspace again: %+v %v", again, err)
	}
	if err := s.SetBitbucketUsername(ctx, c.ID, "rendimiento-bot"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.BitbucketConnections(ctx); len(list) != 1 || list[0].Username != "rendimiento-bot" || list[0].Workspace != "tienda" {
		t.Fatalf("connections: %+v", list)
	}
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	bb := &App{Name: "de-bitbucket", Repo: "bitbucket.org/tienda/catalogo", InstallationID: c.ID, DefaultBranch: "main", Spec: sp, Forja: "bitbucket"}
	if err := s.CreateApp(ctx, bb); err != nil {
		t.Fatal(err)
	}
	if apps, _ := s.AppsForRepo(ctx, "bitbucket.org/tienda/catalogo"); len(apps) != 1 || apps[0].Forja != "bitbucket" {
		t.Fatalf("the Bitbucket app: %+v", apps)
	}
	if err := s.DeleteBitbucketConnection(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BitbucketConnection(ctx, c.ID); err != ErrNotFound {
		t.Fatalf("deleted: %v", err)
	}
}
