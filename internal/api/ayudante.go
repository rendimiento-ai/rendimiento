package api

// El ayudante (decision 188): a person asks rendimiento anything about it
// (where a setting is, what a page does, why a run failed) and its helper
// answers with links to where to go. It only reads and points. Each
// person's conversation is theirs alone and is kept for 7 days.

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ayudanteRoutes are the helper's routes: anyone signed in, for their own
// conversation.
func (s *Server) ayudanteRoutes(auth func(role, pattern string, h func(http.ResponseWriter, *http.Request, string))) {
	auth(store.MemberGuest, "GET /api/ayudante", s.getAyudante)
	auth(store.MemberGuest, "POST /api/ayudante", s.postAyudante)
}

// AyudanteView is a person's conversation with the helper and the AIs it
// may answer with: the local model first, free, when there is one.
type AyudanteView struct {
	Mensajes []store.AyudanteMensaje `json:"mensajes"`
	IAs      []PlaticaIA             `json:"ias"`
	IA       string                  `json:"ia"`
	Libro    string                  `json:"libro"`
}

// ayudanteIAs are the AIs the helper may answer with (the conversations'
// ones but the person's own agent), and the one it uses unless asked:
// the local model when there is one, it costs nothing.
func (s *Server) ayudanteIAs(ctx context.Context, org int64) ([]PlaticaIA, string, error) {
	all, set, err := s.platicaIAs(ctx, org)
	if err != nil {
		return nil, "", err
	}
	out := []PlaticaIA{}
	for _, x := range all {
		if x.ID != store.PlaticaMiAgente {
			out = append(out, x)
		}
	}
	has := func(id string) bool { return slices.ContainsFunc(out, func(x PlaticaIA) bool { return x.ID == id }) }
	switch {
	case has(store.AILocal):
		return out, store.AILocal, nil
	case has(set.Provider):
		return out, set.Provider, nil
	case len(out) > 0:
		return out, out[0].ID, nil
	}
	return out, "", nil
}

func (s *Server) getAyudante(w http.ResponseWriter, r *http.Request, login string) {
	ctx := r.Context()
	org := orgOf(r)
	msgs, err := s.Store.AyudanteDe(ctx, org, login, time.Now().Add(-store.AyudanteGuarda))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ias, elegida, err := s.ayudanteIAs(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, AyudanteView{Mensajes: msgs, IAs: ias, IA: elegida, Libro: agentes.LibroURL(idiomaDe(r))})
}

// AyudanteAnswer is a question and the helper's answer to it, as kept.
type AyudanteAnswer struct {
	Pregunta  store.AyudanteMensaje `json:"pregunta"`
	Respuesta store.AyudanteMensaje `json:"respuesta"`
}

func (s *Server) postAyudante(w http.ResponseWriter, r *http.Request, login string) {
	var in struct {
		Texto  string `json:"texto"`
		Pagina string `json:"pagina"`
		IA     string `json:"ia"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Texto, in.Pagina = strings.TrimSpace(in.Texto), strings.TrimSpace(in.Pagina)
	switch {
	case in.Texto == "":
		httpError(w, http.StatusBadRequest, i18n.M("write a question"))
		return
	case len([]rune(in.Texto)) > 2000:
		httpError(w, http.StatusBadRequest, i18n.M("a question is at most 2,000 characters"))
		return
	case len(in.Pagina) > 300 || (in.Pagina != "" && !strings.HasPrefix(in.Pagina, "/")):
		httpError(w, http.StatusBadRequest, i18n.M("the page is an address of rendimiento, such as /apps/web"))
		return
	}
	if s.IA == nil {
		httpError(w, http.StatusServiceUnavailable, i18n.M("no AI is set up: the helper needs the local model or a key (page AI)"))
		return
	}
	ctx := r.Context()
	org := orgOf(r)
	ias, elegida, err := s.ayudanteIAs(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if in.IA != "" {
		if !slices.ContainsFunc(ias, func(x PlaticaIA) bool { return x.ID == in.IA }) {
			httpError(w, http.StatusBadRequest, i18n.M("that AI cannot answer now"))
			return
		}
		elegida = in.IA
	}
	if elegida == "" {
		httpError(w, http.StatusServiceUnavailable, i18n.M("no AI is set up: the helper needs the local model or a key (page AI)"))
		return
	}
	visibles, err := s.orgApps(r)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	apps := make([]string, 0, len(visibles))
	for a := range visibles {
		apps = append(apps, a)
	}
	slices.Sort(apps)
	historia, err := s.Store.AyudanteDe(ctx, org, login, time.Now().Add(-store.AyudanteGuarda))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pregunta, err := s.Store.AgregarAyudante(ctx, store.AyudanteMensaje{OrgID: org, Who: login, Kind: store.AyudantePersona, Body: in.Texto, Pagina: in.Pagina})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = s.Store.PodarAyudante(ctx, time.Now().Add(-store.AyudanteGuarda))
	ay := &agentes.Ayudante{Model: s.IA, Lang: idiomaDe(r)}
	res, err := ay.Responder(ctx, org, elegida, agentes.AyudantePregunta{Texto: in.Texto, Pagina: in.Pagina,
		Datos: s.datosDePagina(ctx, in.Pagina, visibles), Apps: apps, Historia: historia})
	if err != nil {
		s.Log.Warn("helper: the AI did not answer", "ia", elegida, "err", err)
		httpError(w, http.StatusBadGateway, i18n.M("the AI could not answer: %s", err.Error()))
		return
	}
	respuesta, err := s.Store.AgregarAyudante(ctx, store.AyudanteMensaje{OrgID: org, Who: login, Kind: store.AyudanteRespuesta, Body: res.Texto,
		Pagina: in.Pagina, Enlaces: res.Enlaces, Leido: res.Leido, Provider: res.Res.Provider, Model: res.Res.Model, CostMicros: res.Res.CostMicros})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, AyudanteAnswer{Pregunta: pregunta, Respuesta: respuesta})
}

var paginaDeEjecucion = regexp.MustCompile(`^/apps/([a-z0-9][a-z0-9-]{0,62})/runs/([0-9]{1,12})(?:[/?#].*)?$`)

// datosDePagina is what the platform knows of the page a person asks from,
// for the helper to read: today, a run of an app they may see (its branch,
// status, steps that did not pass and the diagnostician's explanation).
// Anything else, or a page they may not see, gives nothing.
func (s *Server) datosDePagina(ctx context.Context, pagina string, visibles map[string]bool) string {
	m := paginaDeEjecucion.FindStringSubmatch(pagina)
	if m == nil || !visibles[m[1]] {
		return ""
	}
	id, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil {
		return ""
	}
	app, err := s.Store.GetApp(ctx, m[1])
	if err != nil {
		return ""
	}
	run, err := s.Store.GetRun(ctx, id)
	if err != nil || run.AppID != app.ID {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Run %d of the app %s: branch %s, commit %.7s, status %s", run.ID, app.Name, run.Branch, run.SHA, run.Status)
	if run.Message != "" {
		fmt.Fprintf(&b, " (%s)", run.Message)
	}
	b.WriteString(".\n")
	for _, st := range run.Steps {
		if st.Status == "succeeded" {
			continue
		}
		fmt.Fprintf(&b, "- step %s: %s", st.ID, st.Status)
		if st.Message != "" {
			fmt.Fprintf(&b, " — %s", st.Message)
		}
		if st.Tries > 1 {
			fmt.Fprintf(&b, " (ran %d times)", st.Tries)
		}
		b.WriteString("\n")
	}
	if d, err := s.Store.GetDiagnosis(ctx, run.ID); err == nil && d.Text != "" {
		fmt.Fprintf(&b, "The diagnostician explained it: %s\n", d.Text)
	}
	return b.String()
}
