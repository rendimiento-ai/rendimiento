package platform

import (
	"context"
	"errors"
	"slices"
	"testing"

	gh "github.com/rendimiento-ai/rendimiento/internal/github"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A copy on the work board is closed by a person's button: its pull
// request without merging, and its branch. The original is not spare, so
// it is not closed.
func TestCloseSpare(t *testing.T) {
	p, fake, _, _ := setup(t)
	ctx := context.Background()
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	a := &store.App{Name: "mesa-sobra", Repo: "o/mesa-sobra", DefaultBranch: "main", Spec: sp}
	if err := p.Store.CreateApp(ctx, a); err != nil {
		t.Fatal(err)
	}
	pull := func(n int, branch string) gh.PullInfo {
		var pr gh.PullInfo
		pr.Number, pr.State, pr.Title = n, "open", branch
		pr.Head.Ref, pr.Head.SHA, pr.Base.Ref = branch, "c1", "main"
		return pr
	}
	fake.recent = []gh.PullInfo{pull(6, "una-copia"), pull(5, "una")}

	b, err := p.Mesa(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	var copia bool
	for _, l := range b.Lines {
		copia = copia || (l.Branch == "una-copia" && l.Group == "spare" && l.CopyOf == 5)
	}
	if !copia {
		t.Fatalf("board %+v: want una-copia spare, a copy of #5", b.Lines)
	}
	var no *NotApprovable
	if err := p.CloseSpare(ctx, a, "una", "p0dxD"); !errors.As(err, &no) {
		t.Fatalf("closing the original: %v, want it refused", err)
	}
	if err := p.CloseSpare(ctx, a, "main", "p0dxD"); !errors.As(err, &no) {
		t.Fatalf("closing main: %v, want it refused", err)
	}
	if err := p.CloseSpare(ctx, a, "una-copia", "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fake.closed, []int{6}) || !slices.Contains(fake.deleted, "una-copia") || slices.Contains(fake.deleted, "una") {
		t.Fatalf("closed %v, deleted %v", fake.closed, fake.deleted)
	}
}
