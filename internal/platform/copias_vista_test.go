package platform

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The Caminos tab's copies on a cluster (envtest): a pull request's copy
// and a short one, what each runs, its tests and its deadline; keeping it
// longer is bounded by previews' 30 days; making it again takes the
// branch's last run that passed; a person's delete is said in its comment
// and the sweep does not make it again before the next push; a merged pull
// request's copy says it went when merged.
func TestCopiasEnCaminos(t *testing.T) {
	p, fake, _, kube := setup(t)
	ctx := context.Background()
	yaml := "services:\n  - name: web\n    port: 3000\n    domain: caminitos.example.com\npreviews: {keep: 3, max: 3}\n"
	sp, err := spec.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	app := &store.App{Name: "caminitos", Repo: "o/caminitos", DefaultBranch: "main", Spec: *sp}
	if err := p.Store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	for _, sha := range []string{strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("3", 40), strings.Repeat("4", 40)} {
		fake.files[sha] = fstest.MapFS{spec.FileName: {Data: []byte(yaml)}}
	}
	// The runs pass before the pull request exists: no copy is made by them.
	first := passedRun(t, p, app, sp, strings.Repeat("1", 40), "reservas")
	citas := passedRun(t, p, app, sp, strings.Repeat("2", 40), "citas")
	var pr forja.Change
	pr.Number, pr.State, pr.Title = 31, "open", "Reservas con recordatorio"
	pr.Head.Ref, pr.Base.Ref, pr.User.Login = "reservas", "main", "p0dxD"
	fake.mu.Lock()
	fake.recent = []forja.Change{pr}
	fake.mu.Unlock()
	forget := func() {
		p.branchPullsCache.mu.Lock()
		delete(p.branchPullsCache.byApp, app.ID)
		p.branchPullsCache.mu.Unlock()
	}
	forget()
	p.deployPreviewOf(ctx, app, first, *sp, p.imagesOfRun(ctx, app, first, *sp), 31, false)
	if err := p.copias().PutShort(ctx, app, "default", spec.ShortCopyOf(*sp, citas.ID), p.imagesOfRun(ctx, app, citas, *sp), citas); err != nil {
		t.Fatal(err)
	}
	if err := p.Store.CopiaMade(ctx, app.ID, 0, citas.ID, citas.ID, "citas"); err != nil {
		t.Fatal(err)
	}
	if err := p.Store.SetCopiaTests(ctx, app.ID, 31, 4, 4, nil); err != nil {
		t.Fatal(err)
	}

	v, err := p.CopiasOf(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	if v.Where != "cluster" || v.Previews == nil || v.Previews.Keep != 3 || len(v.Live) != 2 {
		t.Fatalf("the copies: %+v", v)
	}
	c, s := v.Live[0], v.Live[1]
	if c.Pull != 31 || c.Title != "Reservas con recordatorio" || c.Author != "p0dxD" || c.Branch != "reservas" || c.Run != first.ID || c.SHA != first.SHA ||
		c.URL != "https://caminitos-pr-31.example.com" || c.Tests == nil || c.Tests.Passed != 4 || c.Tests.Total != 4 || c.MemoryMiB <= 0 {
		t.Fatalf("the pull request's copy: %+v", c)
	}
	if c.DeletesOn == nil || c.DeletesOn.Sub(c.PushedAt) != 3*24*time.Hour || !c.CanExtend || c.Extended {
		t.Fatalf("its deadline: %+v", c)
	}
	if !s.Short || s.Run != citas.ID || s.Branch != "citas" || s.URL != "https://caminitos-seco-"+strconv.FormatInt(citas.ID, 10)+".example.com" {
		t.Fatalf("the short copy: %+v", s)
	}

	// Kept longer, 3 days at a time, never past 30 after its last push.
	var until time.Time
	for i := 0; i < 12; i++ {
		next, err := p.ExtendCopy(ctx, app, 31, "p0dxD")
		var no *CopiaNo
		if errors.As(err, &no) {
			break
		}
		if err != nil || !next.After(until) {
			t.Fatalf("keeping it longer (%d): %s %v", i, next, err)
		}
		until = next
	}
	if got := until.Sub(c.PushedAt); got != 30*24*time.Hour {
		t.Fatalf("kept %s after its push", got)
	}
	// The sweep keeps it past its own keep, until the day a person asked for.
	p.sweepPreviews(ctx, c.PushedAt.AddDate(0, 0, 10))
	if gone(ctx, kube, "caminitos-pr-31") {
		t.Fatal("the sweep deleted a copy kept longer")
	}

	// Made again: its branch's last run that passed is the one it runs, so
	// it restarts; then one that passed later is written to it.
	got, err := p.RemakeCopy(ctx, app, 31, "p0dxD")
	if err != nil || !got.Restarted || got.Run != first.ID {
		t.Fatalf("made again with the same run: %+v %v", got, err)
	}
	newer := passedRun(t, p, app, sp, strings.Repeat("3", 40), "reservas") // its run writes the copy too
	p.deployPreviewOf(ctx, app, first, *sp, p.imagesOfRun(ctx, app, first, *sp), 31, false)
	if got, err = p.RemakeCopy(ctx, app, 31, "p0dxD"); err != nil || got.Restarted || got.Run != newer.ID {
		t.Fatalf("made again with a newer run: %+v %v", got, err)
	}
	var cr v1alpha1.App
	if err := kube.Get(ctx, client.ObjectKey{Name: "caminitos-pr-31"}, &cr); err != nil || cr.Spec.Release != newer.ID {
		t.Fatalf("the copy runs %d (%v)", cr.Spec.Release, err)
	}

	// Deleted by a person: its comment says who, and catching up does not
	// make it again until the branch's next push.
	live, _ := p.copias().Get(ctx, "caminitos", 31)
	if err := p.DeleteCopy(ctx, app, 31, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if !gone(ctx, kube, "caminitos-pr-31") {
		t.Fatal("the copy stayed")
	}
	fake.mu.Lock()
	said := fake.said[live.CommentID]
	fake.mu.Unlock()
	if !strings.Contains(said, "**Preview of #31:** deleted, by p0dxD, from rendimiento; the next push to its branch makes it again.") {
		t.Fatalf("its comment: %q", said)
	}
	if row, err := p.Store.LastCopia(ctx, app.ID, 31); err != nil || row.GoneWhy != store.CopiaPerson || row.GoneBy != "p0dxD" || row.RunID != newer.ID {
		t.Fatalf("its row: %+v %v", row, err)
	}
	if !p.deletedByPerson(ctx, app, 31, "reservas") {
		t.Fatal("catching up would make it again before a push")
	}
	if _, err := p.RemakeCopy(ctx, app, 31, "p0dxD"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("making again a copy that is gone: %v", err)
	}

	// The next push makes it again; merged, it says so.
	forget()
	passedRun(t, p, app, sp, strings.Repeat("4", 40), "reservas")
	if p.deletedByPerson(ctx, app, 31, "reservas") {
		t.Fatal("after a push, a person's delete still holds")
	}
	if _, ok := p.copias().Get(ctx, "caminitos", 31); !ok {
		t.Fatal("the next push did not make the copy again")
	}
	p.ClosePreview(ctx, "o/caminitos", 31, true)
	if row, err := p.Store.LastCopia(ctx, app.ID, 31); err != nil || row.GoneWhy != store.CopiaMerged || row.GoneNote != "the pull request was merged" {
		t.Fatalf("merged: %+v %v", row, err)
	}
	v, err = p.CopiasOf(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Gone) != 2 || v.Gone[0].GoneWhy != store.CopiaMerged || v.Gone[1].GoneWhy != store.CopiaPerson {
		t.Fatalf("deleted lately: %+v", v.Gone)
	}
}
