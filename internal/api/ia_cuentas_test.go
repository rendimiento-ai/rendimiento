package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// «How the coding agent is doing» is read by every member of the
// organization who sees the AI page (a guest too), of their own
// organization only, over a period offered; nobody signed out reads it.
func TestIACuentas(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	// The coding agent's encargo in each organization: one it gave back at
	// its brake here, one it delivered in the other.
	coder := func(org int64, deliver bool) {
		t.Helper()
		k, err := e.store.SystemKey(ctx, org, agentes.CoderName)
		if err != nil {
			t.Fatal(err)
		}
		cg, err := e.store.TakeCargo(ctx, k.ID, "un encargo")
		if err != nil {
			t.Fatal(err)
		}
		enc, err := e.store.CreateEncargo(ctx, store.Encargo{Title: "Saludar en español", App: "cuentas-tienda", CreatedBy: "p0dxD", OrgID: org})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.store.TakeEncargo(ctx, enc.ID, k.ID, cg.ID, agentes.CoderName); err != nil {
			t.Fatal(err)
		}
		if deliver {
			_, err = e.store.DeliverEncargo(ctx, enc.ID, k.ID, agentes.CoderName, "listo", nil)
		} else {
			_, err = e.store.GiveBackEncargoCause(ctx, enc.ID, k.ID, agentes.CoderName, "Me detuve antes de pasar del doble.", store.CauseBudget)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	coder(store.DefaultOrg, false)
	otra, err := e.store.CreateOrg(ctx, "otra-cuentas", "Otra", "ana-cuentas")
	if err != nil {
		t.Fatal(err)
	}
	coder(otra.ID, true)
	if err := e.store.SetMember(ctx, store.DefaultOrg, "invitada-cuentas", store.MemberGuest, "p0dxD"); err != nil {
		t.Fatal(err)
	}

	if r, _ := e.do(t, "GET", "/api/ia/cuentas", "", nil, nil); r.StatusCode != 401 {
		t.Fatalf("signed out: %d", r.StatusCode)
	}
	invitada := e.session(t, "invitada-cuentas")
	read := func(path string, who string) CuentasView {
		t.Helper()
		r, body := e.do(t, "GET", path, "", e.session(t, who), nil)
		var v CuentasView
		if r.StatusCode != 200 || json.Unmarshal([]byte(body), &v) != nil {
			t.Fatalf("%s as %s: %d %s", path, who, r.StatusCode, body)
		}
		return v
	}
	v := read("/api/ia/cuentas", "invitada-cuentas")
	if v.Days != 30 || v.Since == nil || v.Encargos != 1 || v.GaveBack != 1 || v.Finished != 0 || len(v.Causes) != 1 || v.Causes[0].Cause != store.CauseBudget ||
		v.Causes[0].Approx != 0 || v.LearnFrom != agentes.LearnFrom || v.RachaFrom != store.RachaFrom || v.Learned.All.X != 1 {
		t.Fatalf("a guest reads %+v", v)
	}
	if v := read("/api/ia/cuentas?days=0", "p0dxD"); v.Days != 0 || v.Since != nil || v.Encargos != 1 {
		t.Fatalf("all of it: %+v", v)
	}
	if r, _ := e.do(t, "GET", "/api/ia/cuentas?days=3", "", invitada, nil); r.StatusCode != 400 {
		t.Fatalf("a period not offered: %d", r.StatusCode)
	}
	// The other organization's administrator sees its own coding agent.
	if v := read("/api/ia/cuentas", "ana-cuentas"); v.Encargos != 1 || v.Finished != 1 || v.FirstTry != 1 || v.ToReview != 1 || v.GaveBack != 0 || len(v.Causes) != 0 {
		t.Fatalf("the other organization reads %+v", v)
	}
}
