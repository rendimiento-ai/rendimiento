package agentes

import (
	"context"
	"log/slog"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

type estimateStore struct {
	auto     int64
	saved    map[int64][]any
	branches map[int64]string // each run's branch; the default is main
	encargos map[int64]store.Encargo
}

func (s *estimateStore) RunBranch(_ context.Context, run int64) (string, string, error) {
	b, ok := s.branches[run]
	if !ok {
		return "", "", store.ErrNotFound
	}
	return b, "main", nil
}

func (s *estimateStore) Encargo(_ context.Context, id int64) (store.Encargo, error) {
	e, ok := s.encargos[id]
	if !ok {
		return store.Encargo{}, store.ErrNotFound
	}
	return e, nil
}

func (s *estimateStore) EncargosToEstimate(context.Context, int) ([]store.Encargo, error) {
	return nil, nil
}

func (s *estimateStore) SaveEstimate(_ context.Context, id int64, d int, micros int64, reason, route string) error {
	s.saved[id] = []any{d, micros, reason, route}
	return nil
}

func (s *estimateStore) AISettingsOf(_ context.Context, org int64) (store.AISettings, error) {
	return store.AISettings{OrgID: org, EncargoAutoMicros: s.auto}, nil
}

type estimateModel struct{ answers map[string]string }

func (m estimateModel) Complete(_ context.Context, _ int64, source string, req ia.Request) (ia.Response, error) {
	if source != EstimateSource {
		return ia.Response{}, nil
	}
	return ia.Response{Text: m.answers[req.Messages[0].Content]}, nil
}

func (estimateModel) Price(model string, in, out int64) int64 {
	return (&ia.Gateway{}).Price(model, in, out)
}

// An encargo under the organization's amount is for the platform's AI, one
// over it for the person's own agent; an answer with no difficulty is kept
// as such, so it is not asked again.
func TestEstimate(t *testing.T) {
	st := &estimateStore{auto: 100_000, saved: map[int64][]any{}}
	m := estimateModel{answers: map[string]string{
		"Title: un texto\n":  `{"difficulty": 2, "reason": "Cambiar un \"texto\"."}`,
		"Title: la cola\n":   "Pienso... ```json\n{\"difficulty\": 7, \"reason\": \"Toca la cola.\"}\n```",
		"Title: algo vago\n": "no sé",
	}}
	e := &Estimator{Store: st, Model: m, Lang: "es", Log: slog.Default()}
	for i, title := range []string{"un texto", "la cola", "algo vago"} {
		if err := e.Estimate(context.Background(), store.Encargo{ID: int64(i + 1), OrgID: 1, Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	if got := st.saved[1]; got[0] != 2 || got[1] != int64(12_000) || got[2] != `Cambiar un "texto".` || got[3] != store.RoutePlatform {
		t.Errorf("difficulty 2 = %v, want $0.012 for the platform's AI", got)
	}
	if got := st.saved[2]; got[0] != 7 || got[1] != int64(147_000) || got[3] != store.RouteLocal {
		t.Errorf("difficulty 7 = %v, want $0.147, over the $0.10 amount: for the person's agent", got)
	}
	if got := st.saved[3]; got[0] != 0 || got[3] != "" {
		t.Errorf("no difficulty = %v, want it kept with no route", got)
	}
}

// A small fix of a failed run goes to the platform's AI only when the
// branch is not a person's: on main, or on a branch the coding agent made.
// A person's branch, or one their agent made, goes to their own agent.
func TestEstimateByWhoseBranch(t *testing.T) {
	st := &estimateStore{auto: 100_000, saved: map[int64][]any{},
		branches: map[int64]string{1: "main", 2: "propina-redonda", 3: "encargo-7-saludar", 4: "encargo-8-otra"},
		encargos: map[int64]store.Encargo{7: {ID: 7, KeyName: CoderName}, 8: {ID: 8, KeyName: "Claude main"}}}
	m := estimateModel{answers: map[string]string{"Title: arreglar\n": `{"difficulty": 2, "reason": "Un arreglo."}`}}
	e := &Estimator{Store: st, Model: m, Lang: "es", Log: slog.Default()}
	for run := int64(1); run <= 4; run++ {
		r := run
		if err := e.Estimate(context.Background(), store.Encargo{ID: r, OrgID: 1, Title: "arreglar", RunID: &r}); err != nil {
			t.Fatal(err)
		}
	}
	want := map[int64]string{1: store.RoutePlatform, 2: store.RouteLocal, 3: store.RoutePlatform, 4: store.RouteLocal}
	for id, route := range want {
		if got := st.saved[id][3]; got != route {
			t.Errorf("run on %s: route %v, want %s", st.branches[id], got, route)
		}
	}
}
