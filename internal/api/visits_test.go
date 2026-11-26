package api

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/analytics"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A Visits request's window: calendar days run midnight to midnight in the
// visits' time zone (today until now), rolling ranges end now.
func TestVisitPeriod(t *testing.T) {
	tz, _ := time.LoadLocation("America/Mexico_City")
	now := time.Date(2026, 10, 26, 15, 30, 0, 0, tz)
	cases := []struct {
		q          string
		name, unit string
		from, to   time.Time
	}{
		{"range=today", "today", "hour", time.Date(2026, 10, 26, 0, 0, 0, 0, tz), now},
		{"range=yesterday", "yesterday", "hour", time.Date(2026, 10, 25, 0, 0, 0, 0, tz), time.Date(2026, 10, 25, 23, 59, 59, 0, tz)},
		{"day=2026-10-20", "day", "hour", time.Date(2026, 10, 20, 0, 0, 0, 0, tz), time.Date(2026, 10, 20, 23, 59, 59, 0, tz)},
		{"day=2026-10-26", "day", "hour", time.Date(2026, 10, 26, 0, 0, 0, 0, tz), now},
		{"", "7d", "day", now.Add(-7 * 24 * time.Hour), now},
		{"range=30d", "30d", "day", now.Add(-30 * 24 * time.Hour), now},
	}
	for _, c := range cases {
		q, _ := url.ParseQuery(c.q)
		name, p, err := visitPeriod(q, tz, now)
		if err != nil || name != c.name || p.Unit != c.unit || !p.From.Equal(c.from) || !p.To.Equal(c.to) {
			t.Errorf("%q: %s %s %v–%v, %v; want %s %s %v–%v", c.q, name, p.Unit, p.From, p.To, err, c.name, c.unit, c.from, c.to)
		}
	}
	for _, bad := range []string{"range=1y", "day=2026-10-27", "day=2025-01-01", "day=yesterday"} {
		q, _ := url.ParseQuery(bad)
		if _, _, err := visitPeriod(q, tz, now); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// countingAnalytics is fakeAnalytics that notes each summary it is asked
// for: the website, the unit and the length of the period.
type countingAnalytics struct {
	fakeAnalytics
	mu    sync.Mutex
	asked []string
}

func (c *countingAnalytics) Summary(ctx context.Context, w analytics.Website, p analytics.Period) (*analytics.Report, error) {
	c.mu.Lock()
	c.asked = append(c.asked, fmt.Sprintf("%s %s %s", w.ID, p.Unit, p.To.Sub(p.From)))
	c.mu.Unlock()
	return c.fakeAnalytics.Summary(ctx, w, p)
}

// What an app's page shows of its visits (the last 24 hours and 7 days, in
// brief) is read ahead for every app Umami counts, each website once
// however many apps share it, so the page finds it kept even after a
// restart. Websites no app answers on are not read.
func TestWarmVisits(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	for _, a := range [][2]string{{"tienda-visitas", "shop.example.com"}, {"tienda-otra-puerta", "www.shop.example.com"}, {"sin-visitas", "nadie.example.com"}} {
		sp := spec.Spec{Services: []spec.Service{{Name: "web", Domain: a[1]}}}
		sp.Default()
		if err := e.store.CreateApp(ctx, &store.App{Name: a[0], Repo: "p0dxD/" + a[0], DefaultBranch: "main", Spec: sp}); err != nil {
			t.Fatal(err)
		}
	}
	c := &countingAnalytics{}
	e.s.Analytics = c
	if err := e.s.warmVisits(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"w1 hour 24h0m0s", "w1 day 168h0m0s"}; !slices.Equal(c.asked, want) {
		t.Fatalf("read ahead %v, want %v", c.asked, want)
	}
	// Without Umami there is nothing to read, and it returns at once.
	e.s.Analytics = nil
	e.s.WarmVisits(ctx)
}
