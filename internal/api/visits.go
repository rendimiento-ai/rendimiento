package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/rendimiento-ai/rendimiento/internal/analytics"
	"github.com/rendimiento-ai/rendimiento/internal/fresco"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// visitRanges are the Visits tab's rolling ranges and how their charts are
// bucketed; "today", "yesterday" and a chosen day are calendar days, by hour.
var visitRanges = map[string]struct {
	span time.Duration
	unit string
}{
	"24h": {24 * time.Hour, "hour"},
	"7d":  {7 * 24 * time.Hour, "day"},
	"30d": {30 * 24 * time.Hour, "day"},
}

// visitDaysBack is how far back a chosen day may be (Umami keeps more, but
// the tab is about recent visits).
const visitDaysBack = 90

// visitPeriod is the window a Visits request asks for: range=24h|7d|30d|today|yesterday,
// or day=2026-10-20 (a calendar day in the visits' time zone).
func visitPeriod(q url.Values, tz *time.Location, now time.Time) (string, analytics.Period, error) {
	midnight := func(t time.Time) time.Time {
		t = t.In(tz)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, tz)
	}
	if d := q.Get("day"); d != "" {
		day, err := time.ParseInLocation("2006-01-02", d, tz)
		if err != nil || day.After(now) || now.Sub(day) > visitDaysBack*24*time.Hour {
			return "", analytics.Period{}, errors.New("day must be a date in the last 90 days, like 2026-10-20")
		}
		return "day", analytics.Period{From: day, To: minTime(day.AddDate(0, 0, 1).Add(-time.Second), now), Unit: "hour", TZ: tz}, nil
	}
	name := q.Get("range")
	switch name {
	case "":
		name = "7d"
	case "today":
		return name, analytics.Period{From: midnight(now), To: now, Unit: "hour", TZ: tz}, nil
	case "yesterday":
		today := midnight(now)
		return name, analytics.Period{From: today.AddDate(0, 0, -1), To: today.Add(-time.Second), Unit: "hour", TZ: tz}, nil
	}
	rg, ok := visitRanges[name]
	if !ok {
		return "", analytics.Period{}, errors.New("range must be today, yesterday, 24h, 7d or 30d")
	}
	return name, analytics.Period{From: now.Add(-rg.span), To: now, Unit: rg.unit, TZ: tz}, nil
}

// minTime is the earlier of a and b.
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// visitDays is the dashboard summary's period.
const visitDays = 7

// VisitsView is an app's Visits tab: one report per Umami website whose
// domain is one of the app's hostnames.
type VisitsView struct {
	Configured bool                `json:"configured"` // rendimiento can read Umami
	Range      string              `json:"range"`      // today, yesterday, 24h, 7d, 30d, or day
	From       time.Time           `json:"from"`
	To         time.Time           `json:"to"`
	Unit       string              `json:"unit"` // hour | day: how the chart is bucketed
	TimeZone   string              `json:"timeZone"`
	Hosts      []string            `json:"hosts"`              // the app's hostnames
	UmamiURL   string              `json:"umamiURL,omitempty"` // Umami's public address, for links
	Brief      bool                `json:"brief,omitempty"`    // ?brief=1: each report's totals and series only, for an app's page
	Reports    []*analytics.Report `json:"reports"`
}

// appHosts are every hostname an app's services answer on.
func appHosts(a *store.App) []string {
	hosts := []string{}
	for _, svc := range a.Spec.Services {
		hosts = append(hosts, svc.Hosts()...)
	}
	return hosts
}

// visitTZ is the time zone visits are counted in (PUBLIC_STATS_TZ, else UTC).
func (s *Server) visitTZ() *time.Location {
	if tz, err := time.LoadLocation(s.PublicTimeZone); err == nil && s.PublicTimeZone != "" {
		return tz
	}
	return time.UTC
}

// umamiParallel is how many of Umami's websites one page reads at once.
const umamiParallel = 6

// umamiError answers a failed Umami call: a refused login is a setup
// problem, a slow first read is still going on, anything else that Umami
// is unreachable.
func umamiError(w http.ResponseWriter, err error) {
	if errors.Is(err, analytics.ErrAuth) {
		httpError(w, http.StatusBadGateway, "Umami refused rendimiento's user name or password (UMAMI_USERNAME, UMAMI_PASSWORD)")
		return
	}
	if errors.Is(err, fresco.ErrSlow) {
		httpError(w, http.StatusServiceUnavailable, "Umami is slow to answer; rendimiento keeps reading its numbers, try again in a moment")
		return
	}
	httpError(w, http.StatusBadGateway, "could not read Umami: "+err.Error())
}

// visits is an app's Visits tab: Umami's numbers for each of its addresses
// today, yesterday, on a chosen day, or over the last 24 hours, 7 or 30
// days, with the latest visitors of that time. With brief=1 (an app's page,
// above its tabs and on its overview) only each address's totals and
// series, two of Umami's queries instead of eight.
func (s *Server) visits(w http.ResponseWriter, r *http.Request, _ string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	tz := s.visitTZ()
	name, period, err := visitPeriod(r.URL.Query(), tz, time.Now())
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	view := VisitsView{Configured: s.Analytics != nil, Range: name, From: period.From, To: period.To, Unit: period.Unit, TimeZone: tz.String(),
		Hosts: appHosts(a), UmamiURL: s.AnalyticsURL, Brief: r.URL.Query().Get("brief") == "1", Reports: []*analytics.Report{}}
	if s.Analytics == nil {
		writeJSON(w, view)
		return
	}
	sites, err := s.Analytics.Websites(r.Context())
	if err != nil {
		umamiError(w, err)
		return
	}
	read := s.Analytics.Report
	if view.Brief {
		read = s.Analytics.Summary
	}
	matched := analytics.Match(sites, view.Hosts)
	reports := make([]*analytics.Report, len(matched))
	g, ctx := errgroup.WithContext(r.Context())
	g.SetLimit(umamiParallel)
	for i, site := range matched {
		g.Go(func() (err error) {
			reports[i], err = read(ctx, site, period)
			return err
		})
	}
	if err := g.Wait(); err != nil {
		umamiError(w, err)
		return
	}
	view.Reports = append(view.Reports, reports...)
	writeJSON(w, view)
}

// visitActivity is what one visitor of an app did, page by page: the
// website must be one of the app's, and the window at most 31 days.
func (s *Server) visitActivity(w http.ResponseWriter, r *http.Request, _ string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	if s.Analytics == nil {
		httpError(w, http.StatusNotFound, "visits are off: rendimiento has no Umami account")
		return
	}
	ms := func(k string) (time.Time, bool) {
		n, err := strconv.ParseInt(r.URL.Query().Get(k), 10, 64)
		return time.UnixMilli(n), err == nil && n > 0
	}
	from, ok1 := ms("from")
	to, ok2 := ms("to")
	if !ok1 || !ok2 || !to.After(from) || to.Sub(from) > 31*24*time.Hour {
		httpError(w, http.StatusBadRequest, "from and to must be times in milliseconds, at most 31 days apart")
		return
	}
	sites, err := s.Analytics.Websites(r.Context())
	if err != nil {
		umamiError(w, err)
		return
	}
	for _, site := range analytics.Match(sites, appHosts(a)) {
		if site.ID != r.PathValue("site") {
			continue
		}
		steps, err := s.Analytics.Activity(r.Context(), site, r.PathValue("session"), from, to)
		if err != nil {
			umamiError(w, err)
			return
		}
		writeJSON(w, steps)
		return
	}
	httpError(w, http.StatusNotFound, "that website is not one of this app's")
}

// visitsAhead are the brief ranges an app's page asks for: the last 24
// hours above its tabs, the last 7 days on its overview.
var visitsAhead = []string{"24h", "7d"}

// visitsAheadEvery is how often WarmVisits reads them again: within the
// hour Umami's numbers are still shown while new ones are read, so an app's
// page always finds them kept.
const visitsAheadEvery = 30 * time.Minute

// WarmVisits reads ahead, for every app Umami counts, what its page shows
// of its visits: when rendimiento starts and then every visitsAheadEvery.
// Without it, the first look at each app's page after an update (a restart
// forgets what was kept) or after an idle hour waited for Umami, often
// several seconds. Until ctx ends.
func (s *Server) WarmVisits(ctx context.Context) {
	if s.Analytics == nil || s.Store == nil {
		return
	}
	for {
		wait := visitsAheadEvery
		if err := s.warmVisits(ctx); errors.Is(err, fresco.ErrSlow) {
			wait = time.Minute // Umami's list of websites is still coming
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// warmVisits reads each website of an app once, however many apps share
// it, and one at a time, so Umami is not crowded. A summary still being
// read when its wait ends goes on behind and is kept.
func (s *Server) warmVisits(ctx context.Context) error {
	sites, err := s.Analytics.Websites(ctx)
	if err != nil {
		return err
	}
	apps, err := s.Store.ListApps(ctx)
	if err != nil {
		return err
	}
	tz := s.visitTZ()
	seen := map[string]bool{}
	for _, a := range apps {
		for _, site := range analytics.Match(sites, appHosts(a)) {
			if seen[site.ID] {
				continue
			}
			seen[site.ID] = true
			for _, name := range visitsAhead {
				// Each period ends now, as the page's will: they share the
				// kept answer.
				_, p, err := visitPeriod(url.Values{"range": {name}}, tz, time.Now())
				if err != nil {
					return err
				}
				if _, err := s.Analytics.Summary(ctx, site, p); ctx.Err() != nil {
					return ctx.Err()
				} else if errors.Is(err, analytics.ErrAuth) {
					return err
				}
			}
		}
	}
	return nil
}

// AppVisits is one app's line in the dashboard summary.
type AppVisits struct {
	App      string          `json:"app"`
	Current  analytics.Stats `json:"current"`
	Previous analytics.Stats `json:"previous"`
}

// VisitsSummary is the dashboard's visitors panel: the last 7 days of
// every app that Umami counts, and the 7 before them.
type VisitsSummary struct {
	Configured bool            `json:"configured"`
	Days       int             `json:"days"`
	Current    analytics.Stats `json:"current"`
	Previous   analytics.Stats `json:"previous"`
	Apps       []AppVisits     `json:"apps"` // most visitors first; only apps Umami counts
	// Platform is rendimiento's own pages (UMAMI_WEBSITE_ID): only for an
	// administrator of the platform's organization, and not in the totals.
	Platform *PlatformVisits `json:"platform,omitempty"`
}

// PlatformVisits is the dashboard's line of rendimiento's own visits.
type PlatformVisits struct {
	Domain   string          `json:"domain"`
	URL      string          `json:"url,omitempty"` // the website in Umami
	Current  analytics.Stats `json:"current"`
	Previous analytics.Stats `json:"previous"`
}

// addStats adds b's numbers to a.
func addStats(a *analytics.Stats, b analytics.Stats) {
	a.Pageviews += b.Pageviews
	a.Visitors += b.Visitors
	a.Visits += b.Visits
	a.Bounces += b.Bounces
	a.TotalTime += b.TotalTime
}

// visitsSummary is the dashboard's visitors: each app's total of the last
// days, next to the days before.
func (s *Server) visitsSummary(w http.ResponseWriter, r *http.Request, _ string) {
	out := VisitsSummary{Configured: s.Analytics != nil, Days: visitDays, Apps: []AppVisits{}}
	if s.Analytics == nil {
		writeJSON(w, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	sites, err := s.Analytics.Websites(ctx)
	if err != nil {
		umamiError(w, err)
		return
	}
	apps, err := s.Store.ListAppsInOrg(ctx, orgOf(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	to := time.Now()
	from := to.AddDate(0, 0, -visitDays)
	// Each website's totals are read once, at most umamiParallel at once,
	// however many apps share it.
	type pair struct{ cur, prev analytics.Stats }
	totals := map[string]*pair{}
	var lines [][]analytics.Website
	var names []string
	for _, a := range apps {
		if accessOf(r).Role(a) == "" {
			continue // its list leaves the person out
		}
		matched := analytics.Match(sites, appHosts(a))
		if len(matched) == 0 {
			continue
		}
		lines, names = append(lines, matched), append(names, a.Name)
		for _, site := range matched {
			totals[site.ID] = &pair{}
		}
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(umamiParallel)
	for _, site := range sites {
		t, ok := totals[site.ID]
		if !ok {
			continue
		}
		g.Go(func() (err error) {
			t.cur, t.prev, err = s.Analytics.Totals(gctx, site, from, to)
			return err
		})
	}
	if err := g.Wait(); err != nil {
		umamiError(w, err)
		return
	}
	for i, matched := range lines {
		line := AppVisits{App: names[i]}
		for _, site := range matched {
			addStats(&line.Current, totals[site.ID].cur)
			addStats(&line.Previous, totals[site.ID].prev)
		}
		addStats(&out.Current, line.Current)
		addStats(&out.Previous, line.Previous)
		out.Apps = append(out.Apps, line)
	}
	sort.SliceStable(out.Apps, func(i, j int) bool { return out.Apps[i].Current.Visitors > out.Apps[j].Current.Visitors })
	// Who visits rendimiento itself is the platform's business: its
	// administrators see it, and no other organization does.
	if s.TrackerSite != "" && orgOf(r) == store.DefaultOrg && roleOf(r) == store.MemberAdmin {
		for _, site := range sites {
			if !strings.EqualFold(site.ID, s.TrackerSite) {
				continue
			}
			cur, prev, err := s.Analytics.Totals(ctx, site, from, to)
			if err != nil {
				umamiError(w, err)
				return
			}
			out.Platform = &PlatformVisits{Domain: site.Domain, Current: cur, Previous: prev}
			if s.AnalyticsURL != "" {
				out.Platform.URL = s.AnalyticsURL + "/websites/" + url.PathEscape(site.ID)
			}
		}
	}
	writeJSON(w, out)
}
