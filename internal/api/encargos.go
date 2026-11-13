package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/ruta"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// NewEncargo asks agents for something: what, the detail, and the app or
// run it is about.
type NewEncargo struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	App   string `json:"app"`
	RunID *int64 `json:"runId"`
}

// listEncargos lists the open encargos (?all=1: also reviewed and cancelled).
func (s *Server) listEncargos(w http.ResponseWriter, r *http.Request, _ string) {
	list, err := s.Store.ListEncargos(r.Context(), orgOf(r), r.URL.Query().Get("all") != "1", 100)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []store.Encargo{}
	for _, e := range list {
		if accessOf(r).Sees(e.App) { // an app whose list leaves the person out
			out = append(out, e)
		}
	}
	writeJSON(w, out)
}

// createEncargo leaves work for agents, pending until one takes it.
func (s *Server) createEncargo(w http.ResponseWriter, r *http.Request, login string) {
	var in NewEncargo
	if !readJSON(w, r, &in) {
		return
	}
	in.Title, in.App = strings.TrimSpace(in.Title), strings.TrimSpace(in.App)
	if in.Title == "" || len([]rune(in.Title)) > 200 || len([]rune(in.Body)) > 20000 {
		httpError(w, http.StatusBadRequest, "the title must be 1 to 200 characters, and the detail up to 20,000")
		return
	}
	if ruta.LooksSecret(in.Title) || ruta.LooksSecret(in.Body) {
		httpError(w, http.StatusBadRequest, "this looks like it contains a secret; say where it lives instead of its value")
		return
	}
	var run *store.Run
	if in.RunID != nil {
		var err error
		run, err = s.Store.GetRun(r.Context(), *in.RunID)
		if err != nil {
			httpError(w, http.StatusBadRequest, "no such run")
			return
		}
		a, err := s.Store.GetAppByID(r.Context(), run.AppID)
		if err != nil || accessOf(r).Role(a) == "" {
			httpError(w, http.StatusBadRequest, "no such run")
			return
		}
		if in.App == "" {
			in.App = a.Name
		}
	}
	if in.App != "" {
		a, err := s.Store.GetApp(r.Context(), in.App)
		if err != nil || accessOf(r).Role(a) == "" {
			httpError(w, http.StatusBadRequest, "no such app: "+in.App)
			return
		}
		if store.RoleRank(accessOf(r).Role(a)) < store.RoleRank(store.MemberCollaborator) {
			httpError(w, http.StatusForbidden, "your role here does not allow this; ask an administrator")
			return
		}
	}
	// One open encargo per branch: a run of a branch that already has one
	// (the encargo it was made for, or one about another of its runs) is
	// added to that one, for whoever has it to read.
	if run != nil {
		if a, err := s.Store.GetAppByID(r.Context(), run.AppID); err == nil && run.Branch != a.DefaultBranch {
			if open, err := s.Store.OpenEncargoOnBranch(r.Context(), a, run.Branch); err == nil {
				note := in.Title
				if strings.TrimSpace(in.Body) != "" {
					note += "\n\n" + in.Body
				}
				e, err := s.Store.NoteEncargo(r.Context(), open.ID, login, note)
				if err != nil {
					httpError(w, http.StatusInternalServerError, err.Error())
					return
				}
				s.Log.Info("encargo noted", "id", e.ID, "app", e.App, "run", run.ID, "by", login)
				writeJSON(w, e)
				return
			}
		}
	}
	e, err := s.Store.CreateEncargo(r.Context(), store.Encargo{Title: in.Title, Body: in.Body, App: in.App, RunID: in.RunID, CreatedBy: login, OrgID: orgOf(r)})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("encargo created", "id", e.ID, "app", e.App, "by", login)
	writeJSONStatus(w, http.StatusCreated, e)
}

// encargoID reads {id} of an encargo of the person's organization, or
// answers 400 or 404.
func (s *Server) encargoID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad encargo id")
		return 0, false
	}
	if e, err := s.Store.Encargo(r.Context(), id); err == nil && (e.OrgID != orgOf(r) || !accessOf(r).Sees(e.App)) {
		httpError(w, http.StatusNotFound, "no such encargo")
		return 0, false
	}
	return id, true
}

// getEncargo is one encargo with its history.
func (s *Server) getEncargo(w http.ResponseWriter, r *http.Request, _ string) {
	id, ok := s.encargoID(w, r)
	if !ok {
		return
	}
	e, err := s.Store.Encargo(r.Context(), id)
	s.encargoAnswer(w, e, err)
}

// reviewEncargo is the person's answer to a delivered encargo: accepted,
// or sent back with a comment for the next agent.
func (s *Server) reviewEncargo(w http.ResponseWriter, r *http.Request, login string) {
	id, ok := s.encargoID(w, r)
	if !ok {
		return
	}
	var in struct {
		Accept  bool   `json:"accept"`
		Comment string `json:"comment"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Comment = strings.TrimSpace(in.Comment)
	if !in.Accept && in.Comment == "" {
		httpError(w, http.StatusBadRequest, "say what is missing, for the agent that takes it next")
		return
	}
	e, err := s.Store.ReviewEncargo(r.Context(), id, login, in.Accept, in.Comment)
	if err == nil && in.Accept && s.Platform != nil {
		// Accepting the work is the yes to merging the branch it delivered.
		s.Platform.ApproveLinks(r.Context(), e.Links, login)
	}
	s.encargoAnswer(w, e, err)
}

// routeEncargo is who does a pending encargo: the platform's AI, or the
// person's own agent.
func (s *Server) routeEncargo(w http.ResponseWriter, r *http.Request, login string) {
	id, ok := s.encargoID(w, r)
	if !ok {
		return
	}
	var in struct {
		Route string `json:"route"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Route != store.RoutePlatform && in.Route != store.RouteLocal {
		httpError(w, http.StatusBadRequest, "route must be platform or local")
		return
	}
	e, err := s.Store.RouteEncargo(r.Context(), id, login, in.Route)
	s.encargoAnswer(w, e, err)
}

// doEncargos sends pending encargos to the platform's coding agent.
func (s *Server) doEncargos(w http.ResponseWriter, r *http.Request, login string) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.IDs) == 0 || len(in.IDs) > 100 {
		httpError(w, http.StatusBadRequest, "send 1 to 100 encargos")
		return
	}
	set, err := s.Store.AISettingsOf(r.Context(), orgOf(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if set.CodeAgent == store.CodeAgentOff {
		httpError(w, http.StatusConflict, "the coding agent is off; an administrator turns it on on the AI page")
		return
	}
	// Only encargos of apps the person may act on (collaborator there).
	ids := in.IDs[:0:0]
	for _, id := range in.IDs {
		e, err := s.Store.Encargo(r.Context(), id)
		if err != nil || e.OrgID != orgOf(r) {
			continue
		}
		if e.App != "" {
			if a, err := s.Store.GetApp(r.Context(), e.App); err != nil ||
				store.RoleRank(accessOf(r).Role(a)) < store.RoleRank(store.MemberCollaborator) {
				continue
			}
		}
		ids = append(ids, id)
	}
	n, err := s.Store.DoEncargos(r.Context(), orgOf(r), ids, login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("encargos sent to the coding agent", "n", n, "by", login)
	writeJSON(w, map[string]int{"sent": n})
}

// reestimateEncargo asks the estimator to rate an encargo again.
func (s *Server) reestimateEncargo(w http.ResponseWriter, r *http.Request, login string) {
	id, ok := s.encargoID(w, r)
	if !ok {
		return
	}
	e, err := s.Store.ReestimateEncargo(r.Context(), id, login)
	s.encargoAnswer(w, e, err)
}

// cancelEncargo cancels an encargo that is not finished.
func (s *Server) cancelEncargo(w http.ResponseWriter, r *http.Request, login string) {
	id, ok := s.encargoID(w, r)
	if !ok {
		return
	}
	e, err := s.Store.CancelEncargo(r.Context(), id, login, "")
	s.encargoAnswer(w, e, err)
}

func (s *Server) encargoAnswer(w http.ResponseWriter, e store.Encargo, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "no such encargo")
	case errors.Is(err, store.ErrEncargoState):
		httpError(w, http.StatusConflict, "the encargo is not in a state that allows this")
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, e)
	}
}
