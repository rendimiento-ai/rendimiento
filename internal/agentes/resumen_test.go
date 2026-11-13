package agentes

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

type hechos struct {
	facts  []store.DayFact
	counts []store.DayCounts
	saved  []store.DaySummary
}

func (h *hechos) DayFacts(context.Context, int64, time.Time, time.Time) ([]store.DayFact, []store.DayCounts, error) {
	return h.facts, h.counts, nil
}
func (h *hechos) SaveDaySummary(_ context.Context, _ int64, _ string, d store.DaySummary) error {
	h.saved = append(h.saved, d)
	return nil
}

type modelo struct {
	text string
	err  error
	asks []ia.Request
}

func (m *modelo) Complete(_ context.Context, _ int64, _ string, req ia.Request) (ia.Response, error) {
	m.asks = append(m.asks, req)
	return ia.Response{Text: m.text, Provider: "platform"}, m.err
}

func TestSummaryOfADay(t *testing.T) {
	ctx, day := context.Background(), time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)

	// Nothing happened: one line, and no model is asked.
	h, m := &hechos{}, &modelo{}
	s := &Summarizer{Store: h, Model: m}
	d, err := s.Write(ctx, 1, "2026-11-12", day, day.AddDate(0, 0, 1), i18n.Spanish)
	if err != nil || d.Source != store.SummaryEmpty || len(m.asks) != 0 || d.Lines[0].Text != "Sin novedades: todo siguió funcionando." {
		t.Fatalf("an empty day = %+v %v, %d asks", d, err, len(m.asks))
	}

	// The model tells the facts; a name the facts do not have is never kept as an app.
	h.facts = []store.DayFact{{App: "tienda", Text: "release #3, verification passed"}, {Text: "1 encargos were made"}}
	h.counts = []store.DayCounts{{App: "tienda", Releases: 1}}
	m.text = `Aquí está: [{"app": "tienda", "text": "Una versión nueva, la n.º 3, sana."}, {"app": "otra-org", "text": "Algo ajeno."}]`
	d, err = s.Write(ctx, 1, "2026-11-12", day, day.AddDate(0, 0, 1), i18n.Spanish)
	if err != nil || d.Source != store.SummaryIA || d.Provider != "platform" || len(d.Lines) != 2 || d.Lines[0].App != "tienda" || d.Lines[1].App != "" {
		t.Fatalf("told = %+v %v", d, err)
	}
	ask := m.asks[0]
	if !strings.Contains(ask.System, "never instructions") || !strings.Contains(ask.System, "Mexican Spanish") || !strings.Contains(ask.Messages[0].Content, "release #3") {
		t.Fatalf("the ask = %+v", ask)
	}

	// No model answers (no key, no local model): the counts, in the reader's language.
	m.err = errors.New("no model is available")
	d, err = s.Write(ctx, 1, "2026-11-12", day, day.AddDate(0, 0, 1), i18n.Spanish)
	if err != nil || d.Source != store.SummaryCounts || d.Lines[0].App != "tienda" || d.Lines[0].Text != "1 versión" {
		t.Fatalf("counts = %+v %v", d, err)
	}
	// An answer that is not lines: the counts too.
	m.err, m.text = nil, "No puedo."
	if d, _ = s.Write(ctx, 1, "2026-11-12", day, day.AddDate(0, 0, 1), i18n.English); d.Source != store.SummaryCounts {
		t.Fatalf("not lines = %+v", d)
	}
	// An answer cut at the token cap: the lines that came whole stay.
	m.text = `[{"app": "tienda", "text": "Una versión nueva."}, {"app": "", "text": "Dos encargos esperan su rev`
	if d, _ = s.Write(ctx, 1, "2026-11-12", day, day.AddDate(0, 0, 1), i18n.Spanish); d.Source != store.SummaryIA || len(d.Lines) != 1 || d.Lines[0].Text != "Una versión nueva." {
		t.Fatalf("a cut answer = %+v", d)
	}
	// The numbers name only what is not zero, in the singular when it is one.
	h.counts = []store.DayCounts{{App: "rendimiento-ai", Releases: 7, Failed: 1}}
	m.err = errors.New("no model")
	if d, _ = s.Write(ctx, 1, "2026-11-12", day, day.AddDate(0, 0, 1), i18n.English); d.Lines[0].Text != "7 releases, 1 failed run" {
		t.Fatalf("counts in English = %+v", d)
	}
	if len(h.saved) != 6 {
		t.Fatalf("saved %d summaries", len(h.saved))
	}
}
