package store

import (
	"context"
	"errors"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// An app is GitHub's unless it says otherwise; a GitLab is kept once per
// address (connecting it again keeps its id); an account is linked to one
// person only.
func TestForjas(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	old := &App{Name: "de-github", Repo: "o/de-github", InstallationID: 7, DefaultBranch: "main", Spec: sp}
	if err := s.CreateApp(ctx, old); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.GetApp(ctx, "de-github"); a.Forja != ForjaGitHub {
		t.Fatalf("an app without a forja is GitHub's: %q", a.Forja)
	}

	c, err := s.AddGitLabConnection(ctx, "https://git.example.com", "app-1", "p0dxD")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.AddGitLabConnection(ctx, "https://GIT.example.com", "app-2", "ana")
	if err != nil || again.ID != c.ID || again.ApplicationID != "app-2" {
		t.Fatalf("connecting the same address again: %+v %v", again, err)
	}
	if err := s.SetGitLabUsername(ctx, c.ID, "rendimiento-bot"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.GitLabConnections(ctx); len(list) != 1 || list[0].Username != "rendimiento-bot" {
		t.Fatalf("connections: %+v", list)
	}
	gl := &App{Name: "de-gitlab", Repo: "git.example.com/tienda/catalogo", InstallationID: c.ID, DefaultBranch: "main", Spec: sp, Forja: "gitlab"}
	if err := s.CreateApp(ctx, gl); err != nil {
		t.Fatal(err)
	}
	if apps, _ := s.AppsForRepo(ctx, "git.example.com/tienda/catalogo"); len(apps) != 1 || apps[0].Forja != "gitlab" || apps[0].InstallationID != c.ID {
		t.Fatalf("the GitLab app: %+v", apps)
	}

	id := Identity{Login: "p0dxD", Forja: "gitlab", BaseURL: "https://git.example.com", ExternalID: "42", Username: "jose"}
	if err := s.LinkIdentity(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkIdentity(ctx, id); err != nil {
		t.Fatalf("linking it again to the same person: %v", err)
	}
	other := id
	other.Login = "ana"
	if err := s.LinkIdentity(ctx, other); !errors.Is(err, ErrIdentityTaken) {
		t.Fatalf("someone else's account: %v", err)
	}
	if login, _ := s.IdentityLogin(ctx, "gitlab", "https://git.example.com", "42"); login != "p0dxD" {
		t.Fatalf("who signs in: %q", login)
	}
	if login, _ := s.IdentityLogin(ctx, "gitlab", "https://git.example.com", "43"); login != "" {
		t.Fatalf("an account linked to no one: %q", login)
	}
	if ids, _ := s.Identities(ctx, "P0DXD"); len(ids) != 1 || ids[0].Username != "jose" {
		t.Fatalf("identities: %+v", ids)
	}
	if err := s.UnlinkIdentity(ctx, "ana", "gitlab", "https://git.example.com", "42"); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.Identities(ctx, "p0dxD"); len(ids) != 1 {
		t.Fatal("someone else cannot unlink your account")
	}
	if err := s.UnlinkIdentity(ctx, "p0dxD", "gitlab", "https://git.example.com", "42"); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.Identities(ctx, "p0dxD"); len(ids) != 0 {
		t.Fatalf("unlinked: %+v", ids)
	}
}
