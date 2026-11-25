package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Each subject's defaults (Ruta 184, 185): the coding agent and MCP keys
// see the platform and their copies, ask for the infrastructure, run their
// sandbox and do nothing with effect; the diagnostician's infra_read is
// always only for its steps (the app's asks); the estimator only reads the
// code and the Ruta. A mode a permission may not take is refused.
func TestPermisoDefaults(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	want := map[string]map[string]string{
		SujetoCoder:     {PermCodeRead: ModoAlways, PermPlatformRead: ModoAlways, PermInfraRead: ModoAsk, PermSandbox: ModoAlways, PermCopiesRead: ModoAlways, PermEffects: ModoOff},
		SujetoLlave(7):  {PermCodeRead: ModoAlways, PermPlatformRead: ModoAlways, PermInfraRead: ModoAsk, PermSandbox: ModoAlways, PermCopiesRead: ModoAlways, PermEffects: ModoOff},
		SujetoEstimador: {PermCodeRead: ModoAlways, PermPlatformRead: ModoOff, PermInfraRead: ModoOff, PermSandbox: ModoOff, PermCopiesRead: ModoOff, PermEffects: ModoOff},
		SujetoDiagnostico: {PermCodeRead: ModoAlways, PermPlatformRead: ModoAlways, PermInfraRead: ModoAlways, PermSandbox: ModoOff, PermCopiesRead: ModoOff,
			PermEffects: ModoOff},
	}
	for sujeto, modos := range want {
		ps, err := s.PermisosOf(ctx, DefaultOrg, sujeto)
		if err != nil || len(ps) != len(Permisos) {
			t.Fatalf("%s: %+v (%v)", sujeto, ps, err)
		}
		for _, p := range ps {
			if p.Modo != modos[p.Permiso] || p.Set || len(p.Apps) != 0 {
				t.Errorf("%s %s: %+v, want %s by default", sujeto, p.Permiso, p, modos[p.Permiso])
			}
		}
	}
	// The diagnostician's «always» is only for its steps: the app's own asks.
	if d, err := s.PermisoDecide(ctx, DefaultOrg, SujetoDiagnostico, PermInfraRead, "permdef-app", AccionPasos, 0); err != nil || !d.Puede() {
		t.Fatalf("its steps: %+v (%v)", d, err)
	}
	if d, err := s.PermisoDecide(ctx, DefaultOrg, SujetoDiagnostico, PermInfraRead, "permdef-app", "", 0); err != nil || d.Modo != ModoAsk || d.Puede() {
		t.Fatalf("the app's namespace: %+v (%v)", d, err)
	}
	for _, bad := range []Permiso{{Permiso: PermEffects, Modo: ModoAlways}, {Permiso: PermCodeRead, Modo: ModoOff}, {Permiso: "root", Modo: ModoAlways}, {Permiso: PermInfraRead, Modo: "sometimes"}} {
		if err := s.SetPermiso(ctx, DefaultOrg, SujetoCoder, bad, "p0dxD"); !errors.Is(err, ErrPermiso) {
			t.Errorf("%+v was set: %v", bad, err)
		}
	}
	if err := s.SetPermiso(ctx, DefaultOrg, "nadie", Permiso{Permiso: PermInfraRead, Modo: ModoAlways}, "p0dxD"); !errors.Is(err, ErrPermiso) {
		t.Errorf("a subject that is none was set: %v", err)
	}
}

// The run tool's old switch becomes the coding agent's sandbox: off where
// it was off (the migration, and without a row), and saving the sandbox
// keeps the switch in step for the previous version.
func TestPermisoSandboxFromCodeRun(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO ai_settings (org_id, code_run) VALUES ($1, false)`, DefaultOrg); err != nil {
		t.Fatal(err)
	}
	if p, err := s.PermisoOf(ctx, DefaultOrg, SujetoCoder, PermSandbox); err != nil || p.Modo != ModoOff || p.Set {
		t.Fatalf("without a row, off as the switch: %+v (%v)", p, err)
	}
	sql, err := migrations.ReadFile("migrations/0068_permisos_de_agentes.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("the migration again: %v", err)
	}
	if p, err := s.PermisoOf(ctx, DefaultOrg, SujetoCoder, PermSandbox); err != nil || p.Modo != ModoOff || !p.Set {
		t.Fatalf("migrated: %+v (%v)", p, err)
	}
	if err := s.SetPermiso(ctx, DefaultOrg, SujetoCoder, Permiso{Permiso: PermSandbox, Modo: ModoAsk}, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if a, err := s.AISettingsOf(ctx, DefaultOrg); err != nil || !a.CodeRun {
		t.Fatalf("asking runs commands too: the old switch %+v (%v)", a, err)
	}
	if err := s.SetCodeRun(ctx, DefaultOrg, false, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.PermisoOf(ctx, DefaultOrg, SujetoCoder, PermSandbox); p.Modo != ModoOff {
		t.Fatalf("the old switch off: %+v", p)
	}
}

// A permission limited to some apps is off for the others; ask finds the
// grant that holds: once (spent by one use), while its encargo lasts,
// while the key's cargo is open, always for its app; effects only once,
// for exactly the action given. Grants are taken back, and requests of the
// same thing join.
func TestPermisoGrants(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if err := s.SetPermiso(ctx, DefaultOrg, SujetoCoder, Permiso{Permiso: PermPlatformRead, Modo: ModoAlways, Apps: []string{"grant-tienda"}}, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, SujetoCoder, PermPlatformRead, "grant-otra", "", 0); d.Modo != ModoOff || !d.Fuera {
		t.Fatalf("another app than the list: %+v", d)
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, SujetoCoder, PermPlatformRead, "grant-tienda", "", 0); !d.Puede() {
		t.Fatalf("its app: %+v", d)
	}
	k, err := s.CreateAgentKey(ctx, DefaultOrg, "agente de código", "h-grant", RoleCollaborator, nil, "rendimiento", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.CreateEncargo(ctx, Encargo{Title: "Por qué se cayó", App: "grant-tienda", CreatedBy: "p0dxD"})
	if err != nil {
		t.Fatal(err)
	}
	enc := e.ID
	ask := func(perm, accion string, enc *int64, sujeto string, cargo *int64) PermisoPedido {
		t.Helper()
		p, err := s.PedirPermiso(ctx, PermisoPedido{OrgID: DefaultOrg, Sujeto: sujeto, Permiso: perm, App: "grant-tienda", Accion: accion, Que: "ver los eventos", EncargoID: enc, CargoID: cargo, AskedBy: "agente"})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Once: one use, then it asks again.
	p := ask(PermInfraRead, "", &enc, SujetoCoder, nil)
	if again := ask(PermInfraRead, "", &enc, SujetoCoder, nil); again.ID != p.ID {
		t.Fatalf("the same request twice: %d and %d", p.ID, again.ID)
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, SujetoCoder, PermInfraRead, "grant-tienda", "", enc); d.Modo != ModoAsk || d.Dado != nil {
		t.Fatalf("before an answer: %+v", d)
	}
	if _, dado, err := s.ResponderPermiso(ctx, p.ID, DefaultOrg, AlcanceOnce, "p0dxD"); err != nil || dado == nil || dado.Alcance != AlcanceOnce {
		t.Fatalf("once: %+v (%v)", dado, err)
	}
	if _, _, err := s.ResponderPermiso(ctx, p.ID, DefaultOrg, AlcanceAlways, "p0dxD"); !errors.Is(err, ErrPedidoRespondido) {
		t.Fatalf("answered twice: %v", err)
	}
	d, _ := s.PermisoDecide(ctx, DefaultOrg, SujetoCoder, PermInfraRead, "grant-tienda", "", enc)
	if d.Dado == nil {
		t.Fatalf("given once: %+v", d)
	}
	if ok, err := s.UsarPermisoDado(ctx, d.Dado.ID); !ok || err != nil {
		t.Fatalf("its use: %v %v", ok, err)
	}
	if ok, _ := s.UsarPermisoDado(ctx, d.Dado.ID); ok {
		t.Fatal("used twice")
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, SujetoCoder, PermInfraRead, "grant-tienda", "", enc); d.Dado != nil {
		t.Fatalf("after its use: %+v", d)
	}
	// While the encargo lasts: until it is dropped.
	p = ask(PermInfraRead, "", &enc, SujetoCoder, nil)
	if _, _, err := s.ResponderPermiso(ctx, p.ID, DefaultOrg, AlcanceEncargo, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		d, _ := s.PermisoDecide(ctx, DefaultOrg, SujetoCoder, PermInfraRead, "grant-tienda", "", enc)
		if !d.Puede() || d.Dado.Alcance != AlcanceEncargo {
			t.Fatalf("while the encargo lasts, use %d: %+v", i, d)
		}
		if ok, _ := s.UsarPermisoDado(ctx, d.Dado.ID); !ok {
			t.Fatal("an encargo's grant is not spent")
		}
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, SujetoCoder, PermInfraRead, "grant-tienda", "", enc+1000); d.Dado != nil {
		t.Fatalf("another encargo: %+v", d)
	}
	if _, err := s.CancelEncargo(ctx, enc, "p0dxD", ""); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, SujetoCoder, PermInfraRead, "grant-tienda", "", enc); d.Dado != nil {
		t.Fatalf("after the encargo ended: %+v", d)
	}
	// Effects: only once, for the action asked.
	p = ask(PermEffects, "restart:api", &enc, SujetoCoder, nil)
	for _, a := range []string{AlcanceAlways, AlcanceEncargo} {
		if _, _, err := s.ResponderPermiso(ctx, p.ID, DefaultOrg, a, "p0dxD"); !errors.Is(err, ErrPermiso) {
			t.Fatalf("effects %s: %v", a, err)
		}
	}
	if _, _, err := s.ResponderPermiso(ctx, p.ID, DefaultOrg, AlcanceOnce, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPermiso(ctx, DefaultOrg, SujetoCoder, Permiso{Permiso: PermEffects, Modo: ModoAsk}, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, SujetoCoder, PermEffects, "grant-tienda", "restart:web", enc); d.Dado != nil {
		t.Fatalf("another action: %+v", d)
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, SujetoCoder, PermEffects, "grant-tienda", "restart:api", enc); d.Dado == nil || d.Dado.Accion != "restart:api" {
		t.Fatalf("its action: %+v", d)
	}
	// An MCP key: while its cargo is open; always for its app; taken back.
	key := SujetoLlave(k.ID)
	c, err := s.TakeCargo(ctx, k.ID, "permisos")
	if err != nil {
		t.Fatal(err)
	}
	p = ask(PermInfraRead, "", nil, key, &c.ID)
	if _, dado, err := s.ResponderPermiso(ctx, p.ID, DefaultOrg, AlcanceEncargo, "p0dxD"); err != nil || dado.Alcance != AlcanceCargo {
		t.Fatalf("a key's «while it lasts»: %+v (%v)", dado, err)
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, key, PermInfraRead, "grant-tienda", "", 0); !d.Puede() {
		t.Fatalf("while its cargo is open: %+v", d)
	}
	if err := s.HandOver(ctx, c.ID, "listo", ""); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, key, PermInfraRead, "grant-tienda", "", 0); d.Puede() {
		t.Fatalf("after its cargo: %+v", d)
	}
	p = ask(PermInfraRead, "", nil, key, nil)
	if list, err := s.PermisosPedidos(ctx, DefaultOrg); err != nil || len(list) != 1 || list[0].ID != p.ID || list[0].KeyName != "agente de código" {
		t.Fatalf("open requests: %+v (%v)", list, err)
	}
	if _, _, err := s.ResponderPermiso(ctx, p.ID, 999, AlcanceAlways, "intrusa"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another organization answered: %v", err)
	}
	_, dado, err := s.ResponderPermiso(ctx, p.ID, DefaultOrg, AlcanceAlways, "p0dxD")
	if err != nil || dado.Alcance != AlcanceAlways {
		t.Fatalf("always: %+v (%v)", dado, err)
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, key, PermInfraRead, "grant-tienda", "", 0); !d.Puede() {
		t.Fatalf("always for its app: %+v", d)
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, key, PermInfraRead, "grant-otra", "", 0); d.Puede() {
		t.Fatalf("always is for its app only: %+v", d)
	}
	if list, _ := s.PermisosDados(ctx, DefaultOrg, key); len(list) != 1 || list[0].ID != dado.ID {
		t.Fatalf("the key's grants: %+v", list)
	}
	if err := s.QuitarPermisoDado(ctx, dado.ID, DefaultOrg, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.PermisoDecide(ctx, DefaultOrg, key, PermInfraRead, "grant-tienda", "", 0); d.Puede() {
		t.Fatalf("taken back: %+v", d)
	}
	if list, _ := s.PermisosPedidos(ctx, DefaultOrg); len(list) != 0 {
		t.Fatalf("answered requests still open: %+v", list)
	}
}

// A use, a request, a grant and a no go to the encargo's history with the
// permission; another kind is refused.
func TestNoteEncargoPermiso(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	e, err := s.CreateEncargo(ctx, Encargo{Title: "Ver el rollout", App: "uso-app", CreatedBy: "p0dxD"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.NoteEncargoPermiso(ctx, e.ID, "agente de código", LogPermisoUso, PermInfraRead, "leyó los eventos de uso-app"); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteEncargoPermiso(ctx, e.ID, "agente de código", "deleted", PermInfraRead, "x"); !errors.Is(err, ErrPermiso) {
		t.Fatalf("another kind: %v", err)
	}
	got, err := s.Encargo(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := got.Log[len(got.Log)-1]
	if last.What != LogPermisoUso || last.Permiso != PermInfraRead || last.Note != "leyó los eventos de uso-app" {
		t.Fatalf("the history: %+v", got.Log)
	}
	if got.Log[0].Permiso != "" {
		t.Fatalf("another kind shows a permission: %+v", got.Log[0])
	}
}
