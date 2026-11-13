package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

type modeloResumen struct{ text string }

func (m modeloResumen) Complete(context.Context, int64, string, ia.Request) (ia.Response, error) {
	return ia.Response{Text: m.text, Provider: "local"}, nil
}

func TestTheDaysSummary(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	var apps []*store.App
	for _, name := range []string{"resumen-a", "resumen-b"} {
		a := &store.App{Name: name, Repo: "p0dxD/" + name, DefaultBranch: "main", Spec: sp}
		if err := e.store.CreateApp(ctx, a); err != nil {
			t.Fatal(err)
		}
		if err := e.store.CreateRelease(ctx, &store.Release{AppID: a.ID, Number: 1, SHA: "abcabcabc", Images: map[string]string{"web": "web@sha256:1"}, Spec: sp}); err != nil {
			t.Fatal(err)
		}
		apps = append(apps, a)
	}
	// resumen-b has its own list, without fuera: she does not see it.
	if err := e.store.SetMember(ctx, store.DefaultOrg, "fuera", store.MemberCollaborator, "p0dxD"); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetAppList(ctx, apps[1].ID, true); err != nil {
		t.Fatal(err)
	}

	// The facts of today, written as yesterday's summary.
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	facts, _, err := e.store.DayFacts(ctx, store.DefaultOrg, from, from.AddDate(0, 0, 1))
	if err != nil || len(facts) < 2 {
		t.Fatalf("facts = %+v %v", facts, err)
	}
	// The scoreboard of today counts each app's release; the view has one
	// board per day shown, with only the apps the reader sees.
	board, err := e.store.DayBoard(ctx, store.DefaultOrg, from, from.AddDate(0, 0, 1))
	if err != nil || len(board) != 2 || board[0].Releases != 1 || board[1].Releases != 1 {
		t.Fatalf("board = %+v %v", board, err)
	}
	yesterday := from.AddDate(0, 0, -1).Format(time.DateOnly)
	s := &agentes.Summarizer{Store: e.store, Model: modeloResumen{`[{"app": "resumen-a", "text": "a: release #1"}, {"app": "resumen-b", "text": "b: release #1"}, {"app": "", "text": "all good"}]`}}
	if _, err := s.Write(ctx, store.DefaultOrg, yesterday, from, from.AddDate(0, 0, 1), i18n.English); err != nil {
		t.Fatal(err)
	}
	e.s.Summarizer = s

	read := func(login string) SummaryView {
		t.Helper()
		r, body := e.do(t, "GET", "/api/summary", "", e.session(t, login), map[string]string{"Accept-Language": "en"})
		var v SummaryView
		if r.StatusCode != 200 || json.Unmarshal([]byte(body), &v) != nil {
			t.Fatalf("%s: %d %s", login, r.StatusCode, body)
		}
		return v
	}
	v := read("p0dxD")
	if len(v.Days) != 1 || v.Days[0].Day != yesterday || len(v.Days[0].Lines) != 3 || v.Days[0].Source != store.SummaryIA {
		t.Fatalf("admin = %+v", v)
	}
	if _, ok := v.Boards[yesterday]; !ok {
		t.Fatalf("no scoreboard for %s: %+v", yesterday, v.Boards)
	}
	v = read("fuera")
	if len(v.Days) != 1 || len(v.Days[0].Lines) != 2 || v.Days[0].Lines[0].App != "resumen-a" || v.Days[0].Lines[1].App != "" {
		t.Fatalf("fuera sees = %+v", v.Days)
	}
	// Spanish was not written yet: it is pending, and starts.
	r, body := e.do(t, "GET", "/api/summary", "", e.session(t, "p0dxD"), map[string]string{"Accept-Language": "es"})
	if r.StatusCode != 200 || !json.Valid([]byte(body)) {
		t.Fatalf("es: %d %s", r.StatusCode, body)
	}
	var es SummaryView
	_ = json.Unmarshal([]byte(body), &es)
	if !es.Pending || len(es.Days) != 0 {
		t.Fatalf("es = %+v", es)
	}
	if r, _ := e.do(t, "POST", "/api/summary/redo", `{"day":"`+yesterday+`"}`, e.session(t, "fuera"), nil); r.StatusCode != 202 {
		t.Fatalf("redo: %d", r.StatusCode)
	}
}
