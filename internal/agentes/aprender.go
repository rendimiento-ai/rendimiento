package agentes

import (
	"math"
	"sort"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The estimator learns what the coding agent really spends (Ruta 77): for
// each difficulty, the middle of what it spent over what the price table
// said, once it finished enough encargos of that difficulty to trust it.
const (
	// LearnFrom is how many finished encargos a factor needs.
	LearnFrom = 5
	// The factor stays between these: a few odd encargos never make an
	// estimate absurd.
	minFactor = 0.5
	maxFactor = 4.0
)

// Factor is what the estimates are multiplied by, and from how many
// encargos it was learned (X is 1 and N under LearnFrom when it has not
// learned yet).
type Factor struct {
	X float64 `json:"x"`
	N int     `json:"n"`
}

// Learned is the factors, by difficulty and for all of them together.
type Learned struct {
	ByDifficulty map[int]Factor `json:"byDifficulty"`
	All          Factor         `json:"all"`
}

// For is the factor for an encargo of difficulty d: that difficulty's once
// it has LearnFrom encargos, else all of them together once those are
// LearnFrom, else 1 (the table as it is). ok says whether it learned one.
func (l Learned) For(d int) (Factor, bool) {
	if f, ok := l.ByDifficulty[d]; ok && f.N >= LearnFrom {
		return f, true
	}
	if l.All.N >= LearnFrom {
		return l.All, true
	}
	return Factor{X: 1, N: l.All.N}, false
}

// Learn reads the coding agent's record: each encargo it delivered (the
// work finished) against what the table prices its difficulty at, cost(d).
// Ones it gave back are left out, since what it spent says only when it
// stopped.
func Learn(record []store.CoderEncargo, cost func(d int) int64) Learned {
	by := map[int][]float64{}
	var all []float64
	for _, r := range record {
		if r.Delivered == 0 || r.Difficulty == nil || r.SpentMicros <= 0 {
			continue
		}
		base := cost(*r.Difficulty)
		if base <= 0 {
			continue
		}
		x := float64(r.SpentMicros) / float64(base)
		by[*r.Difficulty] = append(by[*r.Difficulty], x)
		all = append(all, x)
	}
	l := Learned{ByDifficulty: map[int]Factor{}, All: factorOf(all)}
	for d, xs := range by {
		l.ByDifficulty[d] = factorOf(xs)
	}
	return l
}

// factorOf is the middle of the ratios (the median: one encargo that went
// badly does not move it much), kept between minFactor and maxFactor and
// rounded to a tenth.
func factorOf(xs []float64) Factor {
	if len(xs) == 0 {
		return Factor{X: 1}
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := s[len(s)/2]
	if len(s)%2 == 0 {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	m = math.Round(min(max(m, minFactor), maxFactor)*10) / 10
	return Factor{X: m, N: len(s)}
}

// Scale is an estimate multiplied by a factor.
func (f Factor) Scale(micros int64) int64 { return int64(math.Round(float64(micros) * f.X)) }
