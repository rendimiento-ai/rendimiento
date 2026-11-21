package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// The coding agent's queue: in the order a person gave it (the rest after,
// the oldest first), with its organization's concurrency; an app chosen
// later puts an encargo in it; a stop is asked only of one a platform
// agent has; and what it finished waits for the person's review.
func TestCoderQueue(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	org, err := s.CreateOrg(ctx, "cola-org", "Cola", "p0dxD")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.SetCodeAgent(context.Background(), org.ID, CodeAgentOff, "p0dxD") })
	var made []int64
	for _, app := range []string{"tienda", "blog", "tienda"} {
		e, _ := s.CreateEncargo(ctx, Encargo{Title: "cola-" + app, App: app, CreatedBy: "p0dxD", OrgID: org.ID})
		made = append(made, e.ID)
	}
	noApp, _ := s.CreateEncargo(ctx, Encargo{Title: "cola-sin-app", CreatedBy: "p0dxD", OrgID: org.ID})
	_ = s.SetCodeAgent(ctx, org.ID, CodeAgentAsked, "p0dxD")
	if n, err := s.DoEncargos(ctx, org.ID, append(slices.Clone(made), noApp.ID), "p0dxD"); err != nil || n != 3 {
		t.Fatalf("sent %d (%v)", n, err)
	}
	queue := func() []int64 {
		l, err := s.CoderQueue(ctx, org.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := []int64{}
		for _, e := range l {
			out = append(out, e.ID)
		}
		return out
	}
	if got := queue(); !slices.Equal(got, made) {
		t.Fatalf("queue %v, want the oldest first %v", got, made)
	}
	if n, err := s.ReorderQueue(ctx, org.ID, []int64{made[2], made[0]}); err != nil || n != 2 {
		t.Fatalf("reordered %d (%v)", n, err)
	}
	if got, want := queue(), []int64{made[2], made[0], made[1]}; !slices.Equal(got, want) {
		t.Fatalf("queue %v, want %v", got, want)
	}

	// Its concurrency comes with each candidate.
	if set, _ := s.AISettingsOf(ctx, org.ID); set.CodeConcurrency != 1 {
		t.Fatalf("concurrency %d, want 1 by default", set.CodeConcurrency)
	}
	_ = s.SetCodeConcurrency(ctx, org.ID, 2, "p0dxD")
	cands, err := s.CoderCandidates(ctx, 500)
	if err != nil {
		t.Fatal(err)
	}
	var mine []int64
	for _, c := range cands {
		if c.OrgID == org.ID {
			mine = append(mine, c.ID)
			if c.Concurrency != 2 {
				t.Fatalf("candidate %d with concurrency %d", c.ID, c.Concurrency)
			}
		}
	}
	if !slices.Equal(mine, []int64{made[2], made[0], made[1]}) {
		t.Fatalf("candidates %v", mine)
	}

	// The one asked without an app: its app chosen later, it can be sent.
	if e, err := s.SetEncargoApp(ctx, noApp.ID, "p0dxD", "blog"); err != nil || e.App != "blog" {
		t.Fatalf("app %+v (%v)", e, err)
	}
	if n, _ := s.DoEncargos(ctx, org.ID, []int64{noApp.ID}, "p0dxD"); n != 1 {
		t.Fatal("not sent with its app")
	}

	// A stop only for one a platform agent has; ShouldStop sees it.
	if _, err := s.StopEncargo(ctx, made[0], "p0dxD"); !errors.Is(err, ErrEncargoState) {
		t.Fatalf("stopped a pending one: %v", err)
	}
	key, _ := s.SystemKey(ctx, org.ID, "agente de código")
	cg, _ := s.TakeCargo(ctx, key.ID, "cola")
	if _, err := s.TakeEncargo(ctx, made[2], key.ID, cg.ID, "agente de código"); err != nil {
		t.Fatal(err)
	}
	_ = s.SetEncargoDoing(ctx, made[2], "read_file:saludo.go")
	if now, _ := s.CoderOf(ctx, org.ID, "agente de código"); len(now) != 1 || now[0].Doing != "read_file:saludo.go" {
		t.Fatalf("doing now %+v", now)
	}
	if stop, _ := s.ShouldStop(ctx, made[2]); stop {
		t.Fatal("stop before anyone asked")
	}
	if _, err := s.StopEncargo(ctx, made[2], "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if stop, _ := s.ShouldStop(ctx, made[2]); !stop {
		t.Fatal("no stop after a person asked")
	}
	if _, err := s.GiveBackEncargo(ctx, made[2], key.ID, "agente de código", "Una persona lo detuvo"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TakeEncargo(ctx, made[0], key.ID, cg.ID, "agente de código"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeliverEncargo(ctx, made[0], key.ID, "agente de código", "listo", nil); err != nil {
		t.Fatal(err)
	}
	delivered, returned, err := s.CoderFinished(ctx, org.ID, "agente de código", 0)
	if err != nil || len(delivered) != 1 || delivered[0].ID != made[0] || len(returned) != 1 || returned[0].ID != made[2] || returned[0].StopAsked || returned[0].Doing != "" {
		t.Fatalf("finished %+v, returned %+v (%v)", delivered, returned, err)
	}
	if c, err := s.CountRuta(ctx, org.ID, time.Now().Add(-time.Hour)); err != nil || c.DoneToday != 1 {
		t.Fatalf("counts %+v (%v)", c, err)
	}
}
