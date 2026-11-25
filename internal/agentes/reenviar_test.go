package agentes

import (
	"context"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// givenBackBefore is a store whose encargo, once taken, was given back
// before and sent again with a note, after a person had sent it back.
type givenBackBefore struct{ *coderStore }

func (s givenBackBefore) TakeEncargo(ctx context.Context, id, key, cargo int64, who string) (store.Encargo, error) {
	e, err := s.coderStore.TakeEncargo(ctx, id, key, cargo, who)
	e.Comment = "Falta el texto en inglés."
	e.Log = []store.EncargoLog{
		{What: "created", Who: "p0dxD"},
		{What: "released", Who: CoderName, Note: "An old reason, from before."},
		{What: "told", Who: "p0dxD", Note: "An old note, from before."},
		{What: "released", Who: CoderName, Note: "The migration needs lock_timeout; I did not know if you wanted it."},
		{What: "sent", Who: "p0dxD"},
		{What: "told", Who: "p0dxD", Note: "Yes, do the lock_timeout hardening."},
		{What: "taken", Who: CoderName},
	}
	return e, err
}

// An encargo given back before starts with why it stopped last time and
// what the person answered when sending it again, besides the comment of
// a person who sent it back; older reasons and notes are left behind.
func TestCoderBriefAfterGivingBack(t *testing.T) {
	st := &coderStore{}
	m := &script{t: t}
	c := newCoder(st, &repos{st: st, files: map[string][]byte{"saludo.go": []byte("const Saludo = \"Hi\"\n")}}, m)
	c.Store = givenBackBefore{st}
	sw := &seen{m: m}
	c.Model = sw
	if err := c.Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"A person sent it back before, saying:\nFalta el texto en inglés.",
		"Last time you stopped because:\nThe migration needs lock_timeout; I did not know if you wanted it.",
		"The person answered, sending it again:\nYes, do the lock_timeout hardening.",
	} {
		if !strings.Contains(sw.brief, want) {
			t.Fatalf("the brief misses %q:\n%s", want, sw.brief)
		}
	}
	if strings.Contains(sw.brief, "from before") {
		t.Fatalf("the brief has an older reason or note:\n%s", sw.brief)
	}
}

// A delivery after a release leaves it behind: nothing given back to tell.
func TestGaveBackAfterADelivery(t *testing.T) {
	e := store.Encargo{Log: []store.EncargoLog{
		{What: "released", Note: "stopped"}, {What: "told", Note: "go on"}, {What: "delivered"}, {What: "returned", Note: "missing X"},
	}}
	if why, told := e.GaveBack(); why != "" || told != nil {
		t.Fatalf("after a delivery: %q %q", why, told)
	}
}
