package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/platform"
	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// fakeCopiasForja answers what the copies' page and actions ask of a forja:
// the recent pull requests, the copy's comment and check. The rest of
// forja.Repos is nil: a call to it fails the test loudly.
type fakeCopiasForja struct {
	forja.Repos
	mu     sync.Mutex
	recent []forja.Change
	said   map[int64]string // comment id → body
	checks int
}

func (f *fakeCopiasForja) RecentPullRequests(context.Context, int64, string) ([]forja.Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]forja.Change(nil), f.recent...), nil
}

func (f *fakeCopiasForja) Comment(_ context.Context, _ int64, _ string, _ int, id int64, body string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id == 0 {
		id = int64(len(f.said) + 1)
	}
	f.said[id] = body
	return id, nil
}

func (f *fakeCopiasForja) CreateCheck(context.Context, int64, string, forja.Check) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	return int64(f.checks), nil
}

func (f *fakeCopiasForja) FileAt(context.Context, int64, string, string, string) ([]byte, error) {
	return nil, errors.New("not found")
}

func (f *fakeCopiasForja) comment(id int64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.said[id]
}

// The Caminos tab's copies: a pull request's copy and a short one en seco,
// with what each runs, its tests, the Secret it lacks and those deleted
// lately; anyone who sees the app reads them, someone left off its list
// finds nothing. Only a person who may merge makes one again (restarting
// it when it already runs the last commit that passed), keeps it longer
// (3 days at a time, up to 30 after its last push) or deletes it (its
// comment says who, and the next push makes it again); a guest may not,
// nor an agent's key, and each action is in the bitácora.
func TestCopiasDeLaApp(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	fake := &fakeCopiasForja{said: map[int64]string{1: "the first comment"}}
	e.s.Platform.Forja = fake
	sp, err := spec.Parse([]byte("services:\n  - name: web\n    port: 3000\n    domain: copiasapi.example.com\n    secrets: [copiasapi-pago]\npreviews: {keep: 3, max: 2}\n"))
	if err != nil {
		t.Fatal(err)
	}
	app := &store.App{Name: "copiasapi", Repo: "o/copiasapi", DefaultBranch: "main", Spec: *sp}
	if err := e.store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	for login, role := range map[string]string{"invitada-cp": store.MemberGuest, "colabora-cp": store.MemberCollaborator, "fuera-cp": store.MemberCollaborator} {
		if err := e.store.SetMember(ctx, store.DefaultOrg, login, role, "p0dxD"); err != nil {
			t.Fatal(err)
		}
	}
	admin, guest, collab, fuera := e.session(t, "p0dxD"), e.session(t, "invitada-cp"), e.session(t, "colabora-cp"), e.session(t, "fuera-cp")
	passed := func(branch string, sha byte) *store.Run {
		t.Helper()
		r := &store.Run{AppID: app.ID, SHA: strings.Repeat(string(sha), 40), Branch: branch, Event: "push"}
		if err := e.store.CreateRun(ctx, r, nil); err != nil {
			t.Fatal(err)
		}
		if err := e.store.FinishRun(ctx, r.ID, store.RunSucceeded, ""); err != nil {
			t.Fatal(err)
		}
		return r
	}
	pago, citas := passed("pago", 'a'), passed("citas", 'b')
	var pr forja.Change
	pr.Number, pr.State, pr.Title = 7, "open", "Pago con tarjeta"
	pr.Head.Ref, pr.Base.Ref, pr.User.Login = "pago", "main", "p0dxD"
	fake.recent = []forja.Change{pr}

	// The copy of pull request 7, as deployPreview leaves it, with its own
	// tests' result; its namespace has none of the app's Secrets.
	pushed := time.Now().UTC().Truncate(time.Second)
	copy7 := &v1alpha1.App{}
	copy7.Name = "copiasapi-pr-7"
	copy7.Labels = map[string]string{"rendimiento.ai/preview-of": "copiasapi", render.LabelPreview: "7"}
	copy7.Annotations = map[string]string{"rendimiento.ai/pushed-at": pushed.Format(time.RFC3339), "rendimiento.ai/comment": "1"}
	copy7.Spec = v1alpha1.AppSpec{Repo: app.Repo, Preview: 7, Release: pago.ID}
	copy7.Status.Phase = v1alpha1.PhaseHealthy
	short := &v1alpha1.App{}
	short.Name = fmt.Sprintf("copiasapi-seco-%d", citas.ID)
	short.Labels = map[string]string{"rendimiento.ai/preview-of": "copiasapi", render.LabelPreview: fmt.Sprintf("seco-%d", citas.ID), "rendimiento.ai/seco-run": fmt.Sprint(citas.ID)}
	short.Annotations = map[string]string{"rendimiento.ai/pushed-at": pushed.Format(time.RFC3339)}
	short.Spec = v1alpha1.AppSpec{Repo: app.Repo, Preview: int(citas.ID), Release: citas.ID}
	ns := &corev1.Namespace{}
	ns.Name, ns.Labels = "copiasapi-pr-7", map[string]string{render.LabelApp: "copiasapi-pr-7"}
	for _, o := range []client.Object{copy7, short, ns} {
		if err := e.kube.Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.store.CopiaMade(ctx, app.ID, 7, 0, pago.ID, "pago"); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetCopiaTests(ctx, app.ID, 7, 3, 4, []string{"reserva una cita"}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.CopiaGone(ctx, app.ID, 5, 0, 0, store.CopiaMerged, "the pull request was merged", ""); err != nil {
		t.Fatal(err)
	}

	read := func(cookie *http.Cookie) platform.CopiasView {
		t.Helper()
		r, body := e.do(t, "GET", "/api/apps/copiasapi/copias", "", cookie, nil)
		if r.StatusCode != 200 {
			t.Fatalf("reading the copies: %d %s", r.StatusCode, body)
		}
		var v platform.CopiasView
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	v := read(guest)
	if v.Previews == nil || v.Previews.Keep != 3 || v.Previews.Max != 2 || v.Previews.MaxKeep != 30 || v.Previews.Extend != 3 || v.Where != "cluster" {
		t.Fatalf("settings: %+v", v)
	}
	if len(v.Live) != 2 {
		t.Fatalf("live copies: %+v", v.Live)
	}
	c := v.Live[0]
	if c.Pull != 7 || c.Short || c.Title != "Pago con tarjeta" || c.Author != "p0dxD" || c.URL != "https://copiasapi-pr-7.example.com" ||
		c.Branch != "pago" || c.SHA != pago.SHA || c.Run != pago.ID || c.Health != "Healthy" {
		t.Fatalf("the pull request's copy: %+v", c)
	}
	if c.Tests == nil || c.Tests.Passed != 3 || c.Tests.Total != 4 || strings.Join(c.Tests.Failed, ",") != "reserva una cita" {
		t.Fatalf("its tests: %+v", c.Tests)
	}
	if c.MemoryMiB <= 0 || c.MemoryFrom != platform.MemRequests || c.DeletesOn == nil || !c.DeletesOn.Equal(pushed.AddDate(0, 0, 3)) || c.Extended || !c.CanExtend {
		t.Fatalf("its memory and deadline: %+v", c)
	}
	if strings.Join(c.MissingSecrets, ",") != "copiasapi-pago" || strings.Join(v.MissingSecrets, ",") != "copiasapi-pago" {
		t.Fatalf("the Secret it lacks: %v %v", c.MissingSecrets, v.MissingSecrets)
	}
	if s := v.Live[1]; !s.Short || s.Run != citas.ID || s.Branch != "citas" || s.URL == "" || s.DeletesOn == nil {
		t.Fatalf("the short copy: %+v", s)
	}
	if len(v.Gone) != 1 || v.Gone[0].Pull != 5 || v.Gone[0].GoneWhy != store.CopiaMerged {
		t.Fatalf("deleted lately: %+v", v.Gone)
	}
	if v.MemoryMiB != c.MemoryMiB+v.Live[1].MemoryMiB {
		t.Fatalf("the memory they hold: %d", v.MemoryMiB)
	}
	// In Spanish, why each went is the page's to say; the copy's words are not touched.
	if _, body := e.do(t, "GET", "/api/apps/copiasapi/copias", "", guest, map[string]string{"Accept-Language": "es"}); !strings.Contains(body, "se integró la solicitud") {
		t.Fatalf("the note in Spanish: %s", body)
	}

	// Someone left off the app's list finds nothing of it.
	if r, _ := e.do(t, "PUT", "/api/apps/copiasapi/members/invitada-cp", `{"role":"guest"}`, admin, nil); r.StatusCode != 200 {
		t.Fatalf("putting the guest on the list: %d", r.StatusCode)
	}
	for _, p := range []struct{ method, path string }{{"GET", "/api/apps/copiasapi/copias"}, {"POST", "/api/apps/copiasapi/copias/7/extender"}, {"DELETE", "/api/apps/copiasapi/copias/7"}} {
		if r, _ := e.do(t, p.method, p.path, "", fuera, nil); r.StatusCode != 404 {
			t.Fatalf("%s %s off the list: %d", p.method, p.path, r.StatusCode)
		}
	}
	if r, _ := e.do(t, "PUT", "/api/apps/copiasapi/members/colabora-cp", `{"role":"collaborator"}`, admin, nil); r.StatusCode != 200 {
		t.Fatalf("putting the collaborator on the list: %d", r.StatusCode)
	}
	read(guest) // on the list, she still reads them

	// A guest may not act, nor an agent's key, nor nobody.
	tok := randomToken()
	if _, err := e.store.CreateAgentKey(ctx, store.DefaultOrg, "agente de copias", hashToken(tok), store.RoleCollaborator, nil, "rendimiento", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	actions := []struct{ method, path string }{
		{"POST", "/api/apps/copiasapi/copias/7/rehacer"}, {"POST", "/api/apps/copiasapi/copias/7/extender"}, {"DELETE", "/api/apps/copiasapi/copias/7"},
	}
	for _, a := range actions {
		if r, _ := e.do(t, a.method, a.path, "", guest, nil); r.StatusCode != 403 {
			t.Fatalf("%s %s by a guest: %d", a.method, a.path, r.StatusCode)
		}
		if r, _ := e.do(t, a.method, a.path, "", nil, map[string]string{"Authorization": "Bearer " + tok}); r.StatusCode != 401 {
			t.Fatalf("%s %s by an agent's key: %d", a.method, a.path, r.StatusCode)
		}
		if r, _ := e.do(t, a.method, a.path, "", nil, nil); r.StatusCode != 401 {
			t.Fatalf("%s %s with no session: %d", a.method, a.path, r.StatusCode)
		}
	}
	if r, _ := e.do(t, "POST", "/api/apps/copiasapi/copias/siete/extender", "", collab, nil); r.StatusCode != 400 {
		t.Fatalf("a bad number: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "POST", "/api/apps/copiasapi/copias/9/extender", "", collab, nil); r.StatusCode != 404 {
		t.Fatalf("a pull request with no copy: %d", r.StatusCode)
	}

	// Kept longer: 3 days at a time, up to 30 after its last push; its
	// comment says until when and who asked.
	var until time.Time
	for i := 0; ; i++ {
		r, body := e.do(t, "POST", "/api/apps/copiasapi/copias/7/extender", "", collab, nil)
		if r.StatusCode == 409 {
			if !strings.Contains(body, "already kept 30 days") {
				t.Fatalf("the bound: %s", body)
			}
			break
		}
		var got struct {
			DeletesOn time.Time `json:"deletesOn"`
		}
		if r.StatusCode != 200 || json.Unmarshal([]byte(body), &got) != nil || !got.DeletesOn.After(until) || i > 12 {
			t.Fatalf("keeping it longer (%d): %d %s", i, r.StatusCode, body)
		}
		if i == 0 && !got.DeletesOn.Equal(pushed.AddDate(0, 0, 6)) {
			t.Fatalf("the first time it goes 3 days later: %s", got.DeletesOn)
		}
		until = got.DeletesOn
	}
	if !until.Equal(pushed.AddDate(0, 0, 30)) {
		t.Fatalf("kept until %s, want 30 days after its push", until)
	}
	if said := fake.comment(1); !strings.Contains(said, "**Kept longer:** colabora-cp asked rendimiento to keep this copy until "+until.Format(time.DateOnly)) ||
		!strings.Contains(said, "**Tests:** 3 of 4 passed against this copy.") {
		t.Fatalf("its comment: %q", said)
	}
	if c := read(guest).Live[0]; !c.Extended || c.CanExtend || !c.DeletesOn.Equal(until) {
		t.Fatalf("after keeping it: %+v", c)
	}

	// Made again: with the last commit that passed it is the one it runs,
	// so it restarts; after a newer run passes, it runs that one.
	r, body := e.do(t, "POST", "/api/apps/copiasapi/copias/7/rehacer", "", collab, nil)
	if r.StatusCode != 200 || !strings.Contains(body, `"restarted":true`) || !strings.Contains(body, fmt.Sprintf(`"run":%d`, pago.ID)) {
		t.Fatalf("made again with the same commit: %d %s", r.StatusCode, body)
	}
	newer := passed("pago", 'c')
	r, body = e.do(t, "POST", "/api/apps/copiasapi/copias/7/rehacer", "", collab, nil)
	if r.StatusCode != 200 || strings.Contains(body, "restarted") || !strings.Contains(body, fmt.Sprintf(`"run":%d`, newer.ID)) {
		t.Fatalf("made again with a newer commit: %d %s", r.StatusCode, body)
	}
	var cr v1alpha1.App
	if err := e.kube.Get(ctx, client.ObjectKey{Name: "copiasapi-pr-7"}, &cr); err != nil || cr.Spec.Release != newer.ID {
		t.Fatalf("the copy runs %d (%v)", cr.Spec.Release, err)
	}
	if said := fake.comment(1); !strings.Contains(said, "run [#"+fmt.Sprint(newer.ID)+"]") {
		t.Fatalf("its comment after making it again: %q", said)
	}

	// Deleted now: its comment says who, the page lists it, and a second
	// time there is nothing to delete.
	if r, body := e.do(t, "DELETE", "/api/apps/copiasapi/copias/7", "", collab, nil); r.StatusCode != 204 {
		t.Fatalf("deleting it: %d %s", r.StatusCode, body)
	}
	if err := e.kube.Get(ctx, client.ObjectKey{Name: "copiasapi-pr-7"}, &cr); err == nil {
		t.Fatal("the copy stayed")
	}
	if said := fake.comment(1); said != "**Preview of #7:** deleted, by colabora-cp, from rendimiento; the next push to its branch makes it again." {
		t.Fatalf("its comment after deleting it: %q", said)
	}
	v = read(guest)
	if len(v.Live) != 1 || !v.Live[0].Short || len(v.Gone) != 2 || v.Gone[0].Pull != 7 || v.Gone[0].GoneWhy != store.CopiaPerson || v.Gone[0].GoneBy != "colabora-cp" {
		t.Fatalf("after deleting it: %+v", v)
	}
	if r, _ := e.do(t, "DELETE", "/api/apps/copiasapi/copias/7", "", collab, nil); r.StatusCode != 404 {
		t.Fatalf("deleting it twice: %d", r.StatusCode)
	}

	// Each action is in the bitácora, with who did it.
	rows, err := e.store.ListarBitacora(ctx, store.DefaultOrg, store.FiltroBitacora{Limite: 500})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, b := range rows {
		if b.Quien == "colabora-cp" && b.Sobre["app"] == "copiasapi" && b.Sobre["n"] == "7" && strings.HasPrefix(b.Resultado, "2") {
			seen[b.Accion] = true
		}
	}
	for _, a := range actions {
		if !seen[a.method+" "+strings.Replace(a.path, "copiasapi/copias/7", "{app}/copias/{n}", 1)] {
			t.Fatalf("no bitácora row for %s %s: %v", a.method, a.path, seen)
		}
	}
}
