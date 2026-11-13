package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// githubDeCuentas is a fake GitHub with the App on four accounts: the
// platform's (p0dxD, 1), ana's own (2), a GitHub organization (acme, 3) and
// bea's own (4). Each installation sees one repository of its account,
// carrying a rendimiento.yaml.
func githubDeCuentas(t *testing.T) *httptest.Server {
	t.Helper()
	cuentas := map[string]string{"1": "p0dxD", "2": "ana", "3": "acme", "4": "bea"}
	mux := http.NewServeMux()
	jsonOK := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /app/installations", func(w http.ResponseWriter, _ *http.Request) {
		var all []map[string]any
		for _, id := range []string{"1", "2", "3", "4"} {
			all = append(all, map[string]any{"id": json.Number(id), "account": map[string]string{"login": cuentas[id]}})
		}
		jsonOK(w, all)
	})
	mux.HandleFunc("POST /app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"token": "tok-" + r.PathValue("id"), "expires_at": "2099-01-01T00:00:00Z"})
	})
	cuenta := func(r *http.Request) string {
		return cuentas[strings.TrimPrefix(r.Header.Get("Authorization"), "token tok-")]
	}
	mux.HandleFunc("GET /installation/repositories", func(w http.ResponseWriter, r *http.Request) {
		c := cuenta(r)
		jsonOK(w, map[string]any{"repositories": []map[string]any{{"id": len(c), "full_name": c + "/huerta", "name": "huerta", "default_branch": "main"}}})
	})
	mux.HandleFunc("GET /repos/{owner}/{name}/commits/{ref}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("owner") != cuenta(r) {
			http.NotFound(w, r)
			return
		}
		jsonOK(w, map[string]string{"sha": strings.Repeat("a", 40)})
	})
	mux.HandleFunc("GET /repos/{owner}/{name}/contents/{path...}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("owner") != cuenta(r) || r.PathValue("path") != "rendimiento.yaml" {
			http.NotFound(w, r)
			return
		}
		jsonOK(w, map[string]string{"content": base64.StdEncoding.EncodeToString([]byte("services:\n  - name: web\n    port: 8080\n"))})
	})
	mux.HandleFunc("/", http.NotFound)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// An organization's administrator creates apps from the GitHub accounts
// that are provably theirs (the App on their own account, connected by
// them) or that an operator gave it, and from no other: not the platform's,
// not another organization's, not one nobody gave it. The refusal is the
// same for all of them. A member of lower role does none of it.
func TestAppsFromAnOrganizationsOwnGitHub(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	app, err := e.s.GitHub.Get()
	if err != nil {
		t.Fatal(err)
	}
	app.BaseURL = githubDeCuentas(t).URL
	otra, err := e.store.CreateOrg(ctx, "otra", "Otra", "ana")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetMember(ctx, otra.ID, "lector", store.MemberCollaborator, "ana"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.CreateOrg(ctx, "tercera", "Tercera", "bea"); err != nil {
		t.Fatal(err)
	}
	me, ana, bea, lector := e.session(t, "p0dxD"), e.session(t, "ana"), e.session(t, "bea"), e.session(t, "lector")

	// Bea connects the App on her own account to her organization.
	if r, body := e.do(t, "POST", "/api/installations/4/connect", "", bea, nil); r.StatusCode != 200 {
		t.Fatalf("bea connects her own: %d %s", r.StatusCode, body)
	}

	// Ana sees nothing she may use yet, and what she could connect: hers.
	r, body := e.do(t, "GET", "/api/installations", "", ana, nil)
	var list struct {
		Installations []struct{ ID int64 } `json:"installations"`
		Connectable   []struct{ ID int64 } `json:"connectable"`
		InstallURL    string               `json:"installURL"`
	}
	if r.StatusCode != 200 || json.Unmarshal([]byte(body), &list) != nil || len(list.Installations) != 0 ||
		len(list.Connectable) != 1 || list.Connectable[0].ID != 2 || list.InstallURL == "" {
		t.Fatalf("ana's installations before connecting: %d %s", r.StatusCode, body)
	}
	for _, leak := range []string{"p0dxD", "acme", "bea"} {
		if strings.Contains(body, leak) {
			t.Fatalf("ana's installations show %q: %s", leak, body)
		}
	}

	// Nothing of the others', and one answer for all of them: the
	// platform's (1), a GitHub organization nobody gave her (3), bea's
	// organization's (4) and one that does not exist (99).
	refusal := ""
	for _, id := range []int{1, 3, 4, 99} {
		checks := map[string][3]string{
			"repos":   {"GET", fmt.Sprintf("/api/installations/%d/repos", id), ""},
			"propose": {"POST", "/api/propose", fmt.Sprintf(`{"installation":%d,"repo":"bea/huerta","branch":"main"}`, id)},
			"create":  {"POST", "/api/apps", fmt.Sprintf(`{"installation":%d,"repo":"bea/huerta","defaultBranch":"main","name":"huerta","spec":{"services":[{"name":"web","port":8080}]},"existing":true}`, id)},
		}
		for what, c := range checks {
			r, body := e.do(t, c[0], c[1], c[2], ana, nil)
			if r.StatusCode != 403 {
				t.Errorf("%s with installation %d: %d %s, want 403", what, id, r.StatusCode, body)
			}
			if refusal == "" {
				refusal = body
			} else if body != refusal {
				t.Errorf("%s with installation %d answers %s, unlike %s: it tells them apart", what, id, body, refusal)
			}
		}
	}
	for _, id := range []int{1, 3, 4, 99} {
		if r, body := e.do(t, "POST", fmt.Sprintf("/api/installations/%d/connect", id), "", ana, nil); r.StatusCode != 403 {
			t.Errorf("ana connects installation %d, not on her account: %d %s", id, r.StatusCode, body)
		}
	}
	if apps, _ := e.store.ListAppsInOrg(ctx, otra.ID); len(apps) != 0 {
		t.Fatalf("refused, and yet apps: %+v", apps)
	}

	// A collaborator of her organization does none of it, not even with hers.
	for _, c := range [][3]string{
		{"GET", "/api/installations", ""},
		{"POST", "/api/installations/2/connect", ""},
		{"GET", "/api/installations/2/repos", ""},
		{"POST", "/api/propose", `{"installation":2,"repo":"ana/huerta","branch":"main"}`},
		{"POST", "/api/apps", `{"installation":2,"repo":"ana/huerta","defaultBranch":"main","name":"huerta","spec":{"services":[{"name":"web","port":8080}]},"existing":true}`},
		{"GET", "/api/zones", ""},
	} {
		if r, _ := e.do(t, c[0], c[1], c[2], lector, nil); r.StatusCode != 403 {
			t.Errorf("a collaborator: %s %s: %d, want 403", c[0], c[1], r.StatusCode)
		}
	}

	// Ana connects hers, and creates an app from it.
	if r, body := e.do(t, "POST", "/api/installations/2/connect", "", ana, nil); r.StatusCode != 200 {
		t.Fatalf("ana connects her own: %d %s", r.StatusCode, body)
	}
	if r, body := e.do(t, "GET", "/api/installations/2/repos", "", ana, nil); r.StatusCode != 200 || !strings.Contains(body, "ana/huerta") {
		t.Fatalf("her repos: %d %s", r.StatusCode, body)
	}
	if r, body := e.do(t, "POST", "/api/propose", `{"installation":2,"repo":"p0dxD/huerta","branch":"main"}`, ana, nil); r.StatusCode != 403 {
		t.Fatalf("her installation with another account's repository: %d %s", r.StatusCode, body)
	}
	if r, body := e.do(t, "POST", "/api/apps", `{"installation":2,"repo":"ana/huerta","defaultBranch":"main","name":"huerta","spec":{"services":[{"name":"web","port":8080}]},"existing":true}`, ana, nil); r.StatusCode != 201 || !strings.Contains(body, `"otra-huerta"`) {
		t.Fatalf("ana creates an app from hers: %d %s", r.StatusCode, body)
	}
	if a, err := e.store.GetApp(ctx, "otra-huerta"); err != nil || a.OrgID != otra.ID || a.InstallationID != 2 {
		t.Fatalf("her app: %+v %v", a, err)
	}

	// An operator gives her organization the GitHub organization's, and
	// takes it away; she cannot do either.
	if r, _ := e.do(t, "PUT", "/api/org-installations/3", fmt.Sprintf(`{"org":%d}`, otra.ID), ana, nil); r.StatusCode != 403 {
		t.Fatalf("ana gives herself one: %d", r.StatusCode)
	}
	if r, body := e.do(t, "PUT", "/api/org-installations/3", fmt.Sprintf(`{"org":%d}`, otra.ID), me, nil); r.StatusCode != 204 {
		t.Fatalf("the operator gives acme's: %d %s", r.StatusCode, body)
	}
	if r, body := e.do(t, "PUT", "/api/org-installations/3", fmt.Sprintf(`{"org":%d}`, store.DefaultOrg), me, nil); r.StatusCode != 409 {
		t.Fatalf("giving one to the platform's organization: %d %s", r.StatusCode, body)
	}
	if r, body := e.do(t, "GET", "/api/org-installations", "", me, nil); r.StatusCode != 200 || !strings.Contains(body, `"account":"acme"`) || !strings.Contains(body, `"account":"bea"`) {
		t.Fatalf("the operator's list: %d %s", r.StatusCode, body)
	}
	if _, body := e.do(t, "GET", "/api/installations", "", ana, nil); !strings.Contains(body, "acme") || strings.Contains(body, "bea") || strings.Contains(body, "p0dxD") {
		t.Fatalf("ana's after the operator gave acme's: %s", body)
	}
	if r, _ := e.do(t, "GET", "/api/installations/3/repos", "", ana, nil); r.StatusCode != 200 {
		t.Fatalf("acme's repos, given: %d", r.StatusCode)
	}
	if r, body := e.do(t, "DELETE", fmt.Sprintf("/api/org-installations/3?org=%d", otra.ID), "", me, nil); r.StatusCode != 204 {
		t.Fatalf("the operator takes it away: %d %s", r.StatusCode, body)
	}
	if r, _ := e.do(t, "GET", "/api/installations/3/repos", "", ana, nil); r.StatusCode != 403 {
		t.Fatalf("acme's repos, taken away: %d", r.StatusCode)
	}

	// The platform's organization keeps every installation.
	if _, body := e.do(t, "GET", "/api/installations", "", me, nil); !strings.Contains(body, "acme") || !strings.Contains(body, `"login":"ana"`) || !strings.Contains(body, `"connectable":[]`) {
		t.Fatalf("the platform's installations: %s", body)
	}
	// And bea's organization does not see ana's app, nor ana's installation.
	if _, body := e.do(t, "GET", "/api/apps", "", bea, nil); strings.Contains(body, "otra-huerta") {
		t.Fatalf("bea sees ana's app: %s", body)
	}
	if _, body := e.do(t, "GET", "/api/installations", "", bea, nil); strings.Contains(body, `"login":"ana"`) {
		t.Fatalf("bea sees ana's installation: %s", body)
	}
}
