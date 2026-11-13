// Package marcador is the scoreboard of a day's summary: the day's totals
// from each app's numbers, the runs by hour, and the day's lotería card,
// the one most notable thing. Build is pure; the API hands it the numbers.
package marcador

import (
	"sort"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The cards, as the lotería names them.
const (
	CardNube     = "nube"     // problems or rollbacks
	CardTropiezo = "tropiezo" // many runs failed
	CardFlor     = "flor"     // releases, all healthy
	CardLuna     = "luna"     // releases at night
	CardColibri  = "colibri"  // agents' work waits for a review
)

// Card is the day's lotería card: which one, its number, and the app it
// is about ("" for the organization).
type Card struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	Of    int    `json:"of,omitempty"` // for tropiezo: of how many runs
	App   string `json:"app,omitempty"`
}

// Board is a day's scoreboard. The totals and the hours are sums of Apps,
// so the numbers and the bars always agree.
type Board struct {
	Releases  int            `json:"releases"`
	Healthy   int            `json:"healthy"`
	Rollbacks int            `json:"rollbacks"`
	Runs      int            `json:"runs"`
	Failed    int            `json:"failed"`
	Merges    int            `json:"merges"`
	Problems  int            `json:"problems"`
	ToReview  int            `json:"toReview"`
	Hours     [24][2]int     `json:"hours"`
	Apps      []store.AppDay `json:"apps"`
	Card      *Card          `json:"card,omitempty"`
}

// Build adds up the apps (those the reader may see) and picks the card.
func Build(apps []store.AppDay, toReview int) Board {
	b := Board{ToReview: toReview, Apps: append([]store.AppDay{}, apps...)}
	sort.Slice(b.Apps, func(i, j int) bool {
		if b.Apps[i].Runs+b.Apps[i].Releases != b.Apps[j].Runs+b.Apps[j].Releases {
			return b.Apps[i].Runs+b.Apps[i].Releases > b.Apps[j].Runs+b.Apps[j].Releases
		}
		return b.Apps[i].App < b.Apps[j].App
	})
	for _, a := range b.Apps {
		b.Releases += a.Releases
		b.Healthy += a.Healthy
		b.Rollbacks += a.Rollbacks
		b.Runs += a.Runs
		b.Failed += a.Failed
		b.Merges += a.Merges
		b.Problems += a.Problems
		for h := range a.Hours {
			b.Hours[h][0] += a.Hours[h][0]
			b.Hours[h][1] += a.Hours[h][1]
		}
	}
	b.Card = b.card()
	return b
}

// card is the most notable thing of the day, in this order: what went
// wrong in production, a day of many failures, releases all healthy,
// releases at night, and work waiting for a review. None on a quiet day.
func (b Board) card() *Card {
	most := func(v func(store.AppDay) int) string {
		best, app := 0, ""
		for _, a := range b.Apps {
			if n := v(a); n > best {
				best, app = n, a.App
			}
		}
		return app
	}
	night := 0
	for _, a := range b.Apps {
		night += a.Night
	}
	switch {
	case b.Problems+b.Rollbacks > 0:
		return &Card{Kind: CardNube, Count: b.Problems + b.Rollbacks, App: most(func(a store.AppDay) int { return a.Problems + a.Rollbacks })}
	case b.Failed >= 3 && b.Failed*100 >= b.Runs*15: // 15% or more of the runs
		return &Card{Kind: CardTropiezo, Count: b.Failed, Of: b.Runs, App: most(func(a store.AppDay) int { return a.Failed })}
	case b.Releases > 0 && b.Healthy == b.Releases:
		return &Card{Kind: CardFlor, Count: b.Releases, App: most(func(a store.AppDay) int { return a.Releases })}
	case night > 0:
		return &Card{Kind: CardLuna, Count: night, App: most(func(a store.AppDay) int { return a.Night })}
	case b.ToReview > 0:
		return &Card{Kind: CardColibri, Count: b.ToReview}
	}
	return nil
}
