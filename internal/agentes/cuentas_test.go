package agentes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The coding agent says why it gave an encargo back: its brake, the cause
// the model gave with give_up (not one it may not choose), a person's stop.
func TestCoderRecordsWhyItGaveBack(t *testing.T) {
	files := func() map[string][]byte { return map[string][]byte{"saludo.go": []byte("const Saludo = \"Hi\"\n")} }

	// Its brake.
	st := &coderStore{}
	est := int64(300_000)
	m := &script{t: t, cost: 250_000, turns: [][]ia.ToolCall{
		{call("read_file", map[string]string{"path": "saludo.go"})},
		{call("read_file", map[string]string{"path": "saludo.go"})},
		{call("read_file", map[string]string{"path": "saludo.go"})},
	}}
	if err := newCoder(st, &repos{st: st, files: files()}, m).Do(context.Background(), store.Encargo{ID: 12, OrgID: 1, App: "tienda", EstimateMicros: &est}); err != nil {
		t.Fatal(err)
	}
	if st.gaveBack == "" || st.cause != store.CauseBudget {
		t.Fatalf("the brake: gave back %q with cause %q", st.gaveBack, st.cause)
	}

	// The model's, with give_up.
	for given, want := range map[string]string{"needs_decision": store.CauseNeedsDecision, " Conflict ": store.CauseConflict, "stopped": "", "restart": "", "a guess": "", "": ""} {
		st := &coderStore{}
		m := &script{t: t, cost: 1000, turns: [][]ia.ToolCall{
			{call("give_up", map[string]string{"reason": "Necesito que usted elija entre dos diseños", "cause": given})},
		}}
		if err := newCoder(st, &repos{st: st, files: files()}, m).Do(context.Background(), store.Encargo{ID: 13, OrgID: 1, App: "tienda"}); err != nil {
			t.Fatal(err)
		}
		if st.gaveBack == "" || st.cause != want {
			t.Errorf("give_up with %q: gave back %q with cause %q, want %q", given, st.gaveBack, st.cause, want)
		}
	}

	// A person stops it.
	st = &coderStore{stop: true}
	c := &Coder{Store: st, Repos: &repos{st: st, files: files()}, Model: waiter{}, Lang: "es", Log: slog.Default(), Poll: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Do(ctx, store.Encargo{ID: 14, OrgID: 1, App: "tienda"}); err != nil {
		t.Fatal(err)
	}
	if st.cause != store.CauseStopped {
		t.Fatalf("a person stopped it: cause %q", st.cause)
	}
}

// An error that ends its work: the forja's before it called the model, the
// model's after, neither when it ran out of turns or time.
func TestErrCause(t *testing.T) {
	for _, c := range []struct {
		calls int
		err   error
		want  string
	}{
		{0, errors.New(`github GET /repos/o/tienda/branches/main: 502`), store.CauseForgeError},
		{0, fmt.Errorf("no app %q", "tienda"), store.CauseOther},
		{1, fmt.Errorf("calling the model: %w", ia.ErrNoToolModel), store.CauseAIError},
		{3, errors.New("connection reset by peer"), store.CauseAIError},
		{3, context.DeadlineExceeded, store.CauseOther},
		{maxTurns, errors.New("se acabaron los turnos"), store.CauseOther},
	} {
		j := &job{costs: make([]int64, c.calls)}
		if got := j.errCause(c.err); got != c.want {
			t.Errorf("%d calls, %v: %q, want %q", c.calls, c.err, got, c.want)
		}
	}
}
