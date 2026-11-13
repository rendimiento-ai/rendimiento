package api

import (
	"strings"
	"testing"
	"time"
)

func TestSummaryDays(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	for last, want := range map[string]string{
		"":           "2026-11-12",                       // the first visit: yesterday
		"2026-11-13": "2026-11-12",                       // already here today: still yesterday
		"2026-11-11": "2026-11-12,2026-11-11",            // back after a day away
		"2026-11-01": "2026-11-12,2026-11-11,2026-11-10", // after long: at most three days
	} {
		if got := strings.Join(summaryDays(now, last), ","); got != want {
			t.Errorf("summaryDays(last %q) = %s, want %s", last, got, want)
		}
	}
}
