package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// What was seen of a failed release: flagged on its release in the list,
// served whole (in Spanish too, untranslated: it is what the cluster said),
// and 404 for a release with nothing kept.
func TestReleaseEvidenceAPI(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	sp := spec.Spec{Services: []spec.Service{{Name: "web", Port: 8080}}}
	sp.Default()
	app := &store.App{Name: "evidencia", Repo: "p0dxD/evidencia", DefaultBranch: "main", Spec: sp}
	if err := e.store.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	var rels []*store.Release
	for n := int64(1); n <= 2; n++ {
		r := &store.Release{AppID: app.ID, Number: n, SHA: fmt.Sprintf("sha%d", n), Images: map[string]string{"web": "web@sha256:1"}, Spec: sp}
		if err := e.store.CreateRelease(ctx, r); err != nil {
			t.Fatal(err)
		}
		rels = append(rels, r)
	}
	code := int32(1)
	ev := &store.ReleaseEvidence{CapturedAt: time.Now().UTC(), Where: "kubernetes", Services: []store.ServiceEvidence{{Name: "web", ReplicaSet: "web-7c9d",
		Pods: []store.PodEvidence{{Name: "web-7c9d-abcde", Phase: "Running", Containers: []store.ContainerEvidence{{Name: "web", State: "waiting",
			Reason: "CrashLoopBackOff", Message: "back-off restarting failed container", LastExitCode: &code, PreviousLog: "panic: no config"}}}}}},
		Events: []store.EvidenceEvent{{At: time.Now().UTC(), Type: "Warning", Reason: "BackOff", Object: "Pod/web-7c9d-abcde", Message: "Back-off restarting failed container"}}}
	if err := e.store.SaveReleaseEvidence(ctx, rels[1].ID, ev); err != nil {
		t.Fatal(err)
	}
	me := e.session(t, "p0dxD")

	r, body := e.do(t, "GET", "/api/apps/evidencia/releases", "", me, nil)
	var list []store.Release
	if err := json.Unmarshal([]byte(body), &list); r.StatusCode != 200 || err != nil || len(list) != 2 {
		t.Fatalf("releases: %d %v %s", r.StatusCode, err, body)
	}
	for _, rel := range list {
		if rel.Evidence != (rel.Number == 2) {
			t.Fatalf("release %d: evidence %v", rel.Number, rel.Evidence)
		}
	}

	for _, lang := range []string{"en", "es-MX"} {
		r, body = e.do(t, "GET", "/api/apps/evidencia/releases/2/evidence", "", me, map[string]string{"Accept-Language": lang})
		var got store.ReleaseEvidence
		if err := json.Unmarshal([]byte(body), &got); r.StatusCode != 200 || err != nil {
			t.Fatalf("%s: %d %v %s", lang, r.StatusCode, err, body)
		}
		c := got.Services[0].Pods[0].Containers[0]
		if c.Reason != "CrashLoopBackOff" || c.Message != "back-off restarting failed container" || c.PreviousLog != "panic: no config" ||
			got.Events[0].Message != "Back-off restarting failed container" || got.Services[0].Name != "web" {
			t.Fatalf("%s: %s", lang, body)
		}
	}
	if r, _ := e.do(t, "GET", "/api/apps/evidencia/releases/1/evidence", "", me, nil); r.StatusCode != 404 {
		t.Fatalf("a release with nothing kept: %d", r.StatusCode)
	}
	if r, _ := e.do(t, "GET", "/api/apps/evidencia/releases/dos/evidence", "", me, nil); r.StatusCode != 400 {
		t.Fatalf("a bad number: %d", r.StatusCode)
	}
}
