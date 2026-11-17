package store

import (
	"context"
	"testing"
	"time"
)

func TestLogLines(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	run := int64(7)
	var lines []LogLine
	for i := range 5 {
		lines = append(lines, LogLine{At: base.Add(time.Duration(i) * time.Minute), Level: "INFO", Process: "worker", Message: "step done", App: "tienda", Run: &run, Attrs: map[string]any{"step": "test"}})
	}
	lines = append(lines,
		LogLine{At: base.Add(10 * time.Minute), Level: "WARN", Process: "api", Message: "request failed", Attrs: map[string]any{"route": "GET /api/apps/{app}", "status": 500}},
		LogLine{At: base.Add(11 * time.Minute), Level: "ERROR", Process: "control", Message: "sync failed", App: "caja"},
		LogLine{At: base.Add(-40 * 24 * time.Hour), Level: "INFO", Process: "api", Message: "very old"},
	)
	if err := s.AddLogLines(ctx, lines); err != nil {
		t.Fatal(err)
	}

	all, next, err := s.LogLines(ctx, LogFilter{})
	if err != nil || len(all) != 8 || next != "" {
		t.Fatalf("all = %d lines, next %q, %v", len(all), next, err)
	}
	if all[0].Message != "sync failed" || all[7].Message != "very old" {
		t.Fatalf("not newest first: %q … %q", all[0].Message, all[7].Message)
	}
	if all[1].Attrs["route"] != "GET /api/apps/{app}" || all[2].Run == nil || *all[2].Run != 7 {
		t.Fatalf("attributes or run lost: %+v %+v", all[1], all[2])
	}

	// Paging by cursor: three pages of three, two and nothing more.
	page, next, err := s.LogLines(ctx, LogFilter{App: "tienda", Limit: 3})
	if err != nil || len(page) != 3 || next == "" {
		t.Fatalf("first page = %d, next %q, %v", len(page), next, err)
	}
	rest, next2, err := s.LogLines(ctx, LogFilter{App: "tienda", Limit: 3, Before: next})
	if err != nil || len(rest) != 2 || next2 != "" || !rest[0].At.Before(page[2].At) {
		t.Fatalf("second page = %d, next %q, %v", len(rest), next2, err)
	}
	if _, _, err := s.LogLines(ctx, LogFilter{Before: "nonsense"}); err != ErrBadCursor {
		t.Fatalf("bad cursor: %v", err)
	}

	for _, c := range []struct {
		f    LogFilter
		want int
	}{
		{LogFilter{Process: "api"}, 2},
		{LogFilter{Levels: []string{"WARN", "ERROR"}}, 2},
		{LogFilter{Text: "REQUEST"}, 1},
		{LogFilter{Text: "%"}, 0},
		{LogFilter{Since: base.Add(9 * time.Minute)}, 2},
		{LogFilter{Since: base, Until: base.Add(2 * time.Minute)}, 2},
	} {
		got, _, err := s.LogLines(ctx, c.f)
		if err != nil || len(got) != c.want {
			t.Errorf("%+v: %d lines, want %d (%v)", c.f, len(got), c.want, err)
		}
	}

	if n, err := s.PruneLog(ctx, time.Now().Add(-30*24*time.Hour)); err != nil || n != 1 {
		t.Fatalf("pruned %d, %v", n, err)
	}
}
