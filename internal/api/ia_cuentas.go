package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// CuentasView is «How the coding agent is doing» on the AI page: what it
// did over the last Days days (0: all of it, store.CoderCuentas), and the
// factor the estimator applies today, learned from its record: the very
// numbers of its record (CoderBoleta.Learned), not computed again.
type CuentasView struct {
	store.CoderCuentas
	Days      int             `json:"days"`
	Learned   agentes.Learned `json:"learned"`
	LearnFrom int             `json:"learnFrom"`
	LearnOn   bool            `json:"learnOn"`
	RachaFrom int             `json:"rachaFrom"`
}

// cuentasDays are the periods offered (0: all of it).
var cuentasDays = map[int]bool{7: true, 30: true, 90: true, 0: true}

// iaCuentas is how the organization's coding agent is doing, over the last
// 30 days (?days=: 7, 30, 90, or 0 for all of it), for every member who
// sees the AI page.
func (s *Server) iaCuentas(w http.ResponseWriter, r *http.Request, _ string) {
	days := 30
	if q := r.URL.Query().Get("days"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || !cuentasDays[n] {
			httpError(w, http.StatusBadRequest, "the period is 7, 30 or 90 days, or 0 for all of it")
			return
		}
		days = n
	}
	ctx, org := r.Context(), orgOf(r)
	var since time.Time
	if days > 0 {
		since = time.Now().AddDate(0, 0, -days)
	}
	c, err := s.Store.CoderCuentasOf(ctx, org, agentes.CoderName, since)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v := CuentasView{Days: days, CoderCuentas: c, LearnFrom: agentes.LearnFrom, RachaFrom: store.RachaFrom,
		Learned: agentes.Learned{ByDifficulty: map[int]agentes.Factor{}, All: agentes.Factor{X: 1}}}
	if set, err := s.Store.AISettingsOf(ctx, org); err == nil {
		v.LearnOn = set.EstimateLearn
	}
	// Priced as the estimator prices it, which needs the gateway's table.
	if s.IA != nil {
		if b, err := s.coderBoleta(ctx, org); err == nil && b != nil {
			v.Learned = b.Learned
		}
	}
	writeJSON(w, v)
}
