package api

import (
	"net/http"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// SummaryDays is how many past days the summary covers at most.
const SummaryDays = 3

// SummaryView is the home page's summary: a block per day since the
// reader's last visit (at most SummaryDays, at least yesterday), newest
// first, with only the lines of apps the reader may see. Pending says some
// day is still being written.
type SummaryView struct {
	Days     []store.DaySummary `json:"days"`
	Pending  bool               `json:"pending"`
	TimeZone string             `json:"timeZone"`
}

// summaryTZ is where days begin and end: the public stats' time zone, or UTC.
func (s *Server) summaryTZ() *time.Location {
	if tz, err := time.LoadLocation(s.PublicTimeZone); err == nil && s.PublicTimeZone != "" {
		return tz
	}
	return time.UTC
}

// summaryDays are the days to show to someone who last came on last
// ("" never): yesterday, and the days before it since that visit, at most
// SummaryDays, newest first.
func summaryDays(now time.Time, last string) []string {
	yesterday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
	days := []string{yesterday.Format(time.DateOnly)}
	for i := 1; i < SummaryDays; i++ {
		d := yesterday.AddDate(0, 0, -i).Format(time.DateOnly)
		if last == "" || d < last {
			break
		}
		days = append(days, d)
	}
	return days
}

// getSummary is the home page's summary. A day not written yet in the
// reader's language is started in the background; the page asks again.
func (s *Server) getSummary(w http.ResponseWriter, r *http.Request, login string) {
	ctx, org, tz := r.Context(), orgOf(r), s.summaryTZ()
	lang := i18n.FromRequest(r)
	now := time.Now().In(tz)
	last, err := s.Store.SummaryVisit(ctx, org, login, now.Format(time.DateOnly))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	apps, err := s.Store.ListAppsInOrg(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sees := map[string]bool{}
	for _, a := range apps {
		sees[a.Name] = accessOf(r).Role(a) != ""
	}
	out := SummaryView{Days: []store.DaySummary{}, TimeZone: tz.String()}
	for _, day := range summaryDays(now, last) {
		d, err := s.Store.GetDaySummary(ctx, org, day, string(lang))
		if err != nil {
			out.Pending = true
			if s.Summarizer != nil {
				from, _ := time.ParseInLocation(time.DateOnly, day, tz)
				s.Summarizer.Start(org, day, from, from.AddDate(0, 0, 1), lang)
			}
			continue
		}
		var lines []store.SummaryLine
		for _, l := range d.Lines {
			if l.App == "" || sees[l.App] {
				lines = append(lines, l)
			}
		}
		d.Lines = lines
		out.Days = append(out.Days, d)
	}
	writeJSON(w, out)
}

// redoSummary writes a day's summary again, in the background (Rehacer).
func (s *Server) redoSummary(w http.ResponseWriter, r *http.Request, _ string) {
	var in struct {
		Day string `json:"day"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	tz := s.summaryTZ()
	from, err := time.ParseInLocation(time.DateOnly, in.Day, tz)
	if err != nil || !from.Before(time.Now().In(tz).Add(-time.Hour)) {
		httpError(w, http.StatusBadRequest, "a past day, as 2006-01-02")
		return
	}
	if s.Summarizer == nil {
		httpError(w, http.StatusServiceUnavailable, "the summary is not written in this process")
		return
	}
	s.Summarizer.Start(orgOf(r), in.Day, from, from.AddDate(0, 0, 1), i18n.FromRequest(r))
	w.WriteHeader(http.StatusAccepted)
}
