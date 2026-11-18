package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/rendimiento-ai/rendimiento/internal/fresco"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// deliveryDays is the dashboard's period; the previous one, just as long,
// gives each number its trend.
const deliveryDays = 30

// DeliveryReport is the dashboard's delivery health: the last 30 days, the
// 30 before them, and twelve weeks of history.
type DeliveryReport struct {
	Days     int                  `json:"days"`
	TimeZone string               `json:"timeZone"`
	Current  *store.DeliveryStats `json:"current"`
	Previous *store.DeliveryStats `json:"previous"`
	Weekly   []store.DeliveryWeek `json:"weekly"` // oldest first
}

// deliveryTTL keeps the delivery report as it is: its numbers are of
// days, and reading them is several queries over every run. An older
// report (up to deliveryStale) is the answer while a new one is read.
const (
	deliveryTTL   = time.Minute
	deliveryStale = 30 * time.Minute
)

// deliveryCache keeps delivery reports by time zone.
type deliveryCache struct {
	once sync.Once
	fresco.Cache[string, *DeliveryReport]
}

// delivery is the dashboard's delivery health: deploys, builds and their
// times, verifications, rollbacks and outages of the last 30 days next to
// the 30 before, and twelve weeks of history.
func (s *Server) delivery(w http.ResponseWriter, r *http.Request, _ string) {
	tzName := s.PublicTimeZone
	if tzName == "" {
		tzName = "UTC"
	}
	tz, err := time.LoadLocation(tzName)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.deliveries.once.Do(func() {
		s.deliveries.TTL, s.deliveries.Stale, s.deliveries.Build, s.deliveries.Parallel = deliveryTTL, deliveryStale, time.Minute, 1
	})
	out, err := s.deliveries.Get(r.Context(), tzName, func(ctx context.Context) (*DeliveryReport, error) {
		return s.deliveryReport(ctx, tzName, tz)
	})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, out)
}

// deliveryReport reads the delivery report's three parts at once.
func (s *Server) deliveryReport(ctx context.Context, tzName string, tz *time.Location) (*DeliveryReport, error) {
	now := time.Now()
	out := &DeliveryReport{Days: deliveryDays, TimeZone: tzName}
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		out.Current, err = s.Store.Delivery(ctx, deliveryDays, tzName)
		return err
	})
	g.Go(func() (err error) {
		out.Previous, err = s.Store.DeliveryBetween(ctx, now.AddDate(0, 0, -2*deliveryDays), now.AddDate(0, 0, -deliveryDays))
		return err
	})
	g.Go(func() (err error) {
		out.Weekly, err = s.Store.DeliveryWeeks(ctx, 12, now, tz)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}
