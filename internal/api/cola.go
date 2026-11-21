package api

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The Ruta's front page («Para usted») and the coding agent's queue (la
// Ruta nueva): what waits for the person, and what the agent does, does
// next and finished.

// Tags that mark a Ruta entry as waiting for a person: a decision only a
// person makes (TagDecide), or a to-do only a person can do (TagYours, or
// the person's own login as a tag, as agents mark them).
const (
	TagDecide = "por-decidir"
	TagYours  = "para-usted"
)

// Portada is what the Ruta's front page shows: one row for each thing that
// needs the person, and the counts of what goes on by itself meanwhile.
type Portada struct {
	// Review: encargos an agent delivered, for the person to review.
	Review []store.Encargo `json:"review"`
	// Decide: entries tagged por-decidir; Yours: to-dos tagged
	// para-usted or with the person's login.
	Decide []store.RutaEntry `json:"decide"`
	Yours  []store.RutaEntry `json:"yours"`
	// Rate: known failures the diagnostician wrote that nobody rated.
	Rate []store.UnconfirmedManual `json:"rate"`
	// Unanswered: people's comments no agent has answered.
	Unanswered []store.Comentario `json:"unanswered"`
	// Unowned: pending encargos nobody has, the coding agent will not take
	// by itself (not in its queue) and the person did not leave to their
	// own agent: someone has to choose who does them.
	Unowned []store.Encargo `json:"unowned"`
	// Counts: with agents, open to-dos, done today, new stories.
	Counts store.RutaCounts `json:"counts"`
	// Latest: the entries written last (the notebook's «Lo último»).
	Latest []store.RutaEntry `json:"latest"`
}

// portada answers the Ruta's front page for the person. ?hoy= is when the
// person's day began (RFC 3339, from the browser); without it, 24 hours.
func (s *Server) portada(w http.ResponseWriter, r *http.Request, login string) {
	ctx, org, acc := r.Context(), orgOf(r), accessOf(r)
	since := time.Now().Add(-24 * time.Hour)
	if t, err := time.Parse(time.RFC3339, r.URL.Query().Get("hoy")); err == nil && time.Since(t) < 48*time.Hour && time.Since(t) >= 0 {
		since = t
	}
	out := Portada{Review: []store.Encargo{}, Decide: []store.RutaEntry{}, Yours: []store.RutaEntry{}, Rate: []store.UnconfirmedManual{},
		Unanswered: []store.Comentario{}, Unowned: []store.Encargo{}, Latest: []store.RutaEntry{}}
	encargos, err := s.Store.ListEncargos(ctx, org, true, 200)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	queue, err := s.Store.CoderQueue(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	inQueue := map[int64]bool{}
	for _, e := range queue {
		inQueue[e.ID] = true
	}
	for _, e := range encargos {
		if !acc.Sees(e.App) {
			continue
		}
		switch {
		case e.Status == store.EncargoDelivered:
			out.Review = append(out.Review, e)
		case e.Status == store.EncargoPending && e.KeyID == nil && !inQueue[e.ID] && !(e.RouteChosen && e.Route == store.RouteLocal):
			out.Unowned = append(out.Unowned, e)
		}
	}
	entries, err := s.Store.SearchRuta(ctx, org, "", "", 500)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	me := strings.ToLower(login)
	for _, e := range entries {
		if rutaAboutHidden(acc, e) || e.Kind == store.KindVivido {
			continue
		}
		if len(out.Latest) < 6 {
			out.Latest = append(out.Latest, e)
		}
		if e.Kind == store.KindPendiente && e.Done {
			continue
		}
		tags := lower(e.Tags)
		switch {
		case slices.Contains(tags, TagDecide):
			out.Decide = append(out.Decide, e)
		case e.Kind == store.KindPendiente && (slices.Contains(tags, TagYours) || (me != "" && slices.Contains(tags, me))):
			out.Yours = append(out.Yours, e)
		}
	}
	if rate, err := s.Store.UnconfirmedManuals(ctx, org, agentes.Unconfirmed, 20); err == nil {
		for _, m := range rate {
			if acc.Sees(m.App) {
				out.Rate = append(out.Rate, m)
			}
		}
	}
	if list, err := s.Store.ComentariosSinResponder(ctx, org, 20); err == nil {
		for _, c := range list {
			if acc.Sees(c.App) {
				out.Unanswered = append(out.Unanswered, c)
			}
		}
	}
	if out.Counts, err = s.Store.CountRuta(ctx, org, since); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, out)
}

func lower(l []string) []string {
	out := make([]string, len(l))
	for i, s := range l {
		out[i] = strings.ToLower(s)
	}
	return out
}

// Cola is the coding agent's queue: what it does now, what it does next
// in order, what it finished for the person to review, and what it gave
// back to them; how it works (its mode, how many at once) and what it
// spent today, against the organization's monthly cap.
type Cola struct {
	Mode        string          `json:"mode"`
	Concurrency int             `json:"concurrency"`
	Now         []store.Encargo `json:"now"`
	Next        []store.Encargo `json:"next"`
	Delivered   []store.Encargo `json:"delivered"`
	Returned    []store.Encargo `json:"returned"`
	SpentToday  int64           `json:"spentTodayMicros"`
	CapMicros   int64           `json:"capMicros"`
}

// cola answers the coding agent's queue (?hoy= as for portada).
func (s *Server) cola(w http.ResponseWriter, r *http.Request, _ string) {
	ctx, org, acc := r.Context(), orgOf(r), accessOf(r)
	set, err := s.Store.AISettingsOf(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	seen := func(l []store.Encargo) []store.Encargo {
		out := []store.Encargo{}
		for _, e := range l {
			if acc.Sees(e.App) {
				out = append(out, e)
			}
		}
		return out
	}
	now, err := s.Store.CoderOf(ctx, org, agentes.CoderName)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, err := s.Store.CoderQueue(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	delivered, returned, err := s.Store.CoderFinished(ctx, org, agentes.CoderName, 30)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	since := time.Now().Add(-24 * time.Hour)
	if t, err := time.Parse(time.RFC3339, r.URL.Query().Get("hoy")); err == nil && time.Since(t) < 48*time.Hour && time.Since(t) >= 0 {
		since = t
	}
	spent, _ := s.Store.AISpentSince(ctx, org, agentes.CoderSource, since)
	writeJSON(w, Cola{Mode: set.CodeAgent, Concurrency: max(set.CodeConcurrency, 1), Now: seen(now), Next: seen(next),
		Delivered: seen(delivered), Returned: seen(returned), SpentToday: spent, CapMicros: set.CapMicros})
}

// reorderCola puts the coding agent's queue in the order the person gave:
// the ids of the encargos, the first done first.
func (s *Server) reorderCola(w http.ResponseWriter, r *http.Request, login string) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.IDs) == 0 || len(in.IDs) > 200 {
		httpError(w, http.StatusBadRequest, "send 1 to 200 encargos")
		return
	}
	// Only encargos the person may act on keep their place; the rest go after.
	ids := in.IDs[:0:0]
	for _, id := range in.IDs {
		e, err := s.Store.Encargo(r.Context(), id)
		if err != nil || e.OrgID != orgOf(r) || !s.mayActOn(r, e.App) {
			continue
		}
		ids = append(ids, id)
	}
	n, err := s.Store.ReorderQueue(r.Context(), orgOf(r), ids)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("the coding agent's queue reordered", "n", n, "by", login)
	s.cola(w, r, login)
}

// mayActOn says whether the person is a collaborator on an app (or the
// encargo has none).
func (s *Server) mayActOn(r *http.Request, app string) bool {
	if app == "" {
		return true
	}
	a, err := s.Store.GetApp(r.Context(), app)
	return err == nil && store.RoleRank(accessOf(r).Role(a)) >= store.RoleRank(store.MemberCollaborator)
}

// setEncargoApp says which app a pending encargo is about, when it was
// asked without one (or the wrong one), before anyone takes it.
func (s *Server) setEncargoApp(w http.ResponseWriter, r *http.Request, login string) {
	id, ok := s.encargoID(w, r)
	if !ok {
		return
	}
	var in struct {
		App string `json:"app"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.App = strings.TrimSpace(in.App)
	a, err := s.Store.GetApp(r.Context(), in.App)
	if in.App == "" || err != nil || a.OrgID != orgOf(r) || accessOf(r).Role(a) == "" {
		httpError(w, http.StatusBadRequest, "no such app: "+in.App)
		return
	}
	if !s.mayActOn(r, in.App) {
		httpError(w, http.StatusForbidden, "your role here does not allow this; ask an administrator")
		return
	}
	e, err := s.Store.SetEncargoApp(r.Context(), id, login, a.Name)
	if err == nil {
		s.Log.Info("encargo app chosen", "id", id, "app", a.Name, "by", login)
	}
	s.encargoAnswer(w, e, err)
}

// stopEncargo asks the platform's coding agent to stop the encargo it
// does: it gives it back to the person with what it did.
func (s *Server) stopEncargo(w http.ResponseWriter, r *http.Request, login string) {
	id, ok := s.encargoID(w, r)
	if !ok {
		return
	}
	if e, err := s.Store.Encargo(r.Context(), id); err == nil && !s.mayActOn(r, e.App) {
		httpError(w, http.StatusForbidden, "your role here does not allow this; ask an administrator")
		return
	}
	e, err := s.Store.StopEncargo(r.Context(), id, login)
	if err == nil {
		s.Log.Info("encargo stop asked", "id", id, "by", login)
	}
	s.encargoAnswer(w, e, err)
}
