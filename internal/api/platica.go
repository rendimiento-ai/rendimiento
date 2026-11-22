package api

// Platicar una entrada: a conversation about one Ruta entry, kept with it,
// between the people who see it and the AI they choose (one of the AI
// page's, or their own agent over MCP). The AI only talks; when the idea
// is ready, it proposes encargos, which a person reviews and creates, with
// the decision saved in the Ruta.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/ruta"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// platicaRoutes are the conversations' routes, and the cap per
// conversation on the AI page.
func (s *Server) platicaRoutes(auth func(role, pattern string, h func(http.ResponseWriter, *http.Request, string))) {
	auth(store.MemberGuest, "GET /api/ruta/{id}/platica", s.getPlatica)
	auth(store.MemberCollaborator, "POST /api/ruta/{id}/platica", s.postPlatica)
	auth(store.MemberCollaborator, "PUT /api/ruta/{id}/platica/ia", s.putPlaticaIA)
	auth(store.MemberCollaborator, "PUT /api/ruta/{id}/platica/idea", s.putPlaticaIdea)
	auth(store.MemberCollaborator, "POST /api/ruta/{id}/platica/propuesta", s.proponerPlatica)
	auth(store.MemberCollaborator, "POST /api/ruta/{id}/platica/encargos", s.encargosDePlatica)
	auth(store.MemberGuest, "GET /api/ia/platica", s.getPlaticaTope)
	auth(store.MemberAdmin, "PUT /api/ia/platica", s.putPlaticaTope)
}

// PlaticaIA is one AI a conversation may choose: one of the AI page's
// providers (local, own, platform) with its model and about what one
// message costs, or the person's own agent (agente).
type PlaticaIA struct {
	ID    string `json:"id"`
	Model string `json:"model,omitempty"`
	// MensajeMicros is about what one message costs (0: free, or a model
	// whose price is not known).
	MensajeMicros int64 `json:"mensajeMicros"`
	Gratis        bool  `json:"gratis,omitempty"`
}

// PlaticaView is a conversation as its page shows it.
type PlaticaView struct {
	// Platica has ID 0 while nobody wrote in it.
	Platica  store.Platica          `json:"platica"`
	Mensajes []store.PlaticaMensaje `json:"mensajes"`
	// IAs are the ones it may choose, and IA the one answering now.
	IAs []PlaticaIA `json:"ias"`
	IA  string      `json:"ia"`
	// TopeMicros is the cap per conversation; AlTope that it was reached
	// (with what people allowed past it), so it asks before going on.
	TopeMicros int64 `json:"topeMicros"`
	AlTope     bool  `json:"alTope"`
	// Participantes are who wrote in it: people, AIs and agents.
	Participantes []string `json:"participantes"`
	FaltaDecidir  string   `json:"faltaDecidir"`
}

// About what one message of a conversation reads and writes, to price it.
const (
	platicaTokensIn  = 4000
	platicaTokensOut = 600
)

// platicaEntry is the entry {id} of the person's organization that they
// may see, to talk about; lo vivido, people's stories, is not talked over
// with an AI. It answers 400 or 404 itself.
func (s *Server) platicaEntry(w http.ResponseWriter, r *http.Request) (store.RutaEntry, bool) {
	id, ok := rutaID(w, r)
	if !ok {
		return store.RutaEntry{}, false
	}
	e, err := s.Store.Ruta(r.Context(), id)
	if err == nil && (e.OrgID != orgOf(r) || rutaAboutHidden(accessOf(r), e) || e.ArchivedAt != nil) {
		err = store.ErrNotFound
	}
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such entry")
		return e, false
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return e, false
	}
	if e.Kind == store.KindVivido {
		httpError(w, http.StatusBadRequest, i18n.M("lo vivido is people's own story: it is not talked over with an AI"))
		return e, false
	}
	return e, true
}

// platicaIAs are the AIs a conversation of the organization may choose:
// the AI page's that can answer now (the local model, the organization's
// own key, the platform's within its allowance) and the person's own agent.
func (s *Server) platicaIAs(ctx context.Context, org int64) ([]PlaticaIA, store.AISettings, error) {
	set, err := s.Store.AISettingsOf(ctx, org)
	if err != nil {
		return nil, set, err
	}
	var out []PlaticaIA
	if s.IA != nil {
		price := func(model string) int64 { return s.IA.Price(model, platicaTokensIn, platicaTokensOut) }
		if s.IA.PlatformKeyNow() != "" && min(set.AllowanceMicros, set.CapMicros) > 0 {
			model := s.IA.PlatformModel
			if model == "" {
				model = ia.DefaultAnthropicModel
			}
			out = append(out, PlaticaIA{ID: store.AIPlatform, Model: model, MensajeMicros: price(model)})
		}
		if s.IAKeys != nil {
			if hint, _ := s.IAKeys.KeyHint(ctx, org); hint != "" && (set.Kind != "openai" || set.Model != "") {
				model := set.Model
				if model == "" {
					model = ia.DefaultAnthropicModel
				}
				out = append(out, PlaticaIA{ID: store.AIOwn, Model: model, MensajeMicros: price(model)})
			}
		}
		if url, model := s.IA.LocalNow(); url != "" {
			out = append(out, PlaticaIA{ID: store.AILocal, Model: model, Gratis: true})
		}
	}
	out = append(out, PlaticaIA{ID: store.PlaticaMiAgente, Gratis: true})
	return out, set, nil
}

// platicaElegida is who answers a conversation now: the one it chose, if
// it can still answer; else the organization's provider, if it can; else
// the first that can.
func platicaElegida(p store.Platica, ias []PlaticaIA, set store.AISettings) string {
	has := func(id string) bool {
		return slices.ContainsFunc(ias, func(x PlaticaIA) bool { return x.ID == id })
	}
	switch {
	case p.IA != "" && has(p.IA):
		return p.IA
	case has(set.Provider):
		return set.Provider
	}
	return ias[0].ID
}

// platicaView reads a conversation for its page.
func (s *Server) platicaView(ctx context.Context, r *http.Request, e store.RutaEntry) (PlaticaView, error) {
	org := orgOf(r)
	v := PlaticaView{Mensajes: []store.PlaticaMensaje{}, Participantes: []string{}}
	p, err := s.Store.PlaticaDe(ctx, e.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return v, err
	}
	if errors.Is(err, store.ErrNotFound) {
		p = store.Platica{EntryID: e.ID, OrgID: org, Encargos: []int64{}}
	} else if v.Mensajes, err = s.Store.MensajesDePlatica(ctx, p.ID, 0, 0); err != nil {
		return v, err
	}
	v.Platica = p
	ias, set, err := s.platicaIAs(ctx, org)
	if err != nil {
		return v, err
	}
	v.IAs, v.IA = ias, platicaElegida(p, ias, set)
	if v.TopeMicros, err = s.Store.PlaticaTope(ctx, org); err != nil {
		return v, err
	}
	v.AlTope = store.PlaticaLlegoAlTope(p, v.TopeMicros)
	for _, m := range v.Mensajes {
		who := m.Author
		if m.Kind == store.PlaticaIA {
			who = m.Model
		}
		if m.Kind != store.PlaticaAviso && !slices.Contains(v.Participantes, who) {
			v.Participantes = append(v.Participantes, who)
		}
	}
	v.FaltaDecidir = (&agentes.Platicador{Lang: idiomaDe(r)}).FaltaDecidir()
	return v, nil
}

// getPlatica is the conversation about an entry, for its page (polled).
func (s *Server) getPlatica(w http.ResponseWriter, r *http.Request, _ string) {
	e, ok := s.platicaEntry(w, r)
	if !ok {
		return
	}
	v, err := s.platicaView(r.Context(), r, e)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, v)
}

// paga says whether an AI a conversation chose costs money.
func paga(iaChoice string) bool { return iaChoice == store.AIOwn || iaChoice == store.AIPlatform }

// postPlatica writes a person's message, signed with their login, and has
// the AI the conversation chose answer it (its own agent answers over MCP).
// Past the cap per conversation it asks first: {"continuar": true} lets
// it spend one cap more.
func (s *Server) postPlatica(w http.ResponseWriter, r *http.Request, login string) {
	e, ok := s.platicaEntry(w, r)
	if !ok {
		return
	}
	var in struct {
		Texto     string `json:"texto"`
		Continuar bool   `json:"continuar"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Texto = strings.TrimSpace(in.Texto)
	switch {
	case in.Texto == "" || len([]rune(in.Texto)) > 8000:
		httpError(w, http.StatusBadRequest, i18n.M("write a message, up to 8,000 characters"))
		return
	case ruta.LooksSecret(in.Texto):
		httpError(w, http.StatusBadRequest, "this looks like it contains a secret (a password, token or key); the Ruta keeps no secrets")
		return
	}
	ctx, org := r.Context(), orgOf(r)
	p, err := s.Store.AbrirPlatica(ctx, org, e.ID, login)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ias, set, err := s.platicaIAs(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	elegida := platicaElegida(p, ias, set)
	tope, err := s.Store.PlaticaTope(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if paga(elegida) && store.PlaticaLlegoAlTope(p, tope) {
		if !in.Continuar {
			httpError(w, http.StatusPaymentRequired, i18n.M("this conversation reached its cap (%s): say whether to go on", ia.Dollars(tope)))
			return
		}
		if err := s.Store.PermitirMasPlatica(ctx, p.ID, tope); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		anotarDetalle(r, "continuar", ia.Dollars(tope))
	}
	if _, err := s.Store.AgregarMensaje(ctx, store.PlaticaMensaje{PlaticaID: p.ID, Author: login, Kind: store.PlaticaPersona, Body: in.Texto}); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if elegida != store.PlaticaMiAgente && s.IA != nil {
		lang := idiomaDe(r)
		go s.turnoPlatica(org, p.ID, e.ID, lang)
	}
	v, err := s.platicaView(ctx, r, e)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, v)
}

// turnoPlatica has the conversation's AI answer what people wrote: one
// turn at a time per conversation (a message written meanwhile is
// answered next, up to three in a row), within its cap. When the AI
// cannot answer, rendimiento says why in the conversation.
func (s *Server) turnoPlatica(org, platicaID, entryID int64, lang string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	took, err := s.Store.TomarTurnoPlatica(ctx, platicaID)
	if err != nil || !took {
		return
	}
	defer func() { _ = s.Store.SoltarTurnoPlatica(context.Background(), platicaID) }()
	pl := &agentes.Platicador{Store: s.Store, Model: s.IA, Lang: lang}
	for range 3 {
		p, err := s.Store.PlaticaPorID(ctx, platicaID)
		if err != nil {
			return
		}
		msgs, err := s.Store.MensajesDePlatica(ctx, platicaID, 0, 0)
		if err != nil || len(msgs) == 0 || msgs[len(msgs)-1].Kind != store.PlaticaPersona {
			return
		}
		e, err := s.Store.Ruta(ctx, entryID)
		if err != nil {
			return
		}
		ias, set, err := s.platicaIAs(ctx, org)
		if err != nil {
			return
		}
		elegida := platicaElegida(p, ias, set)
		tope, err := s.Store.PlaticaTope(ctx, org)
		if err != nil || elegida == store.PlaticaMiAgente || (paga(elegida) && store.PlaticaLlegoAlTope(p, tope)) {
			return
		}
		res, err := pl.Responder(ctx, e, p, msgs, elegida)
		if err != nil {
			s.Log.Warn("conversation: the AI did not answer", "platica", platicaID, "err", err)
			_, _ = s.Store.AgregarMensaje(context.WithoutCancel(ctx), store.PlaticaMensaje{PlaticaID: platicaID, Author: "rendimiento", Kind: store.PlaticaAviso,
				Body: i18n.Translate(i18n.Parse(lang), i18n.M("The AI could not answer: %s", err.Error())), CostMicros: res.Res.CostMicros})
			return
		}
		if _, err := s.Store.AgregarMensaje(ctx, store.PlaticaMensaje{PlaticaID: platicaID, Author: res.Res.Model, Kind: store.PlaticaIA,
			Provider: res.Res.Provider, Model: res.Res.Model, Body: res.Texto, Read: res.Leido, CostMicros: res.Res.CostMicros, Note: res.Res.Note}); err != nil {
			return
		}
		if res.Idea != "" {
			_ = s.Store.GuardarIdea(ctx, platicaID, res.Idea, res.Res.Model)
		}
	}
}

// putPlaticaIA is who answers a conversation from now on: one of the AIs
// it may choose.
func (s *Server) putPlaticaIA(w http.ResponseWriter, r *http.Request, login string) {
	e, ok := s.platicaEntry(w, r)
	if !ok {
		return
	}
	var in struct {
		IA string `json:"ia"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx, org := r.Context(), orgOf(r)
	ias, _, err := s.platicaIAs(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !slices.ContainsFunc(ias, func(x PlaticaIA) bool { return x.ID == in.IA }) {
		httpError(w, http.StatusBadRequest, i18n.M("that AI cannot answer here; choose one of the list"))
		return
	}
	p, err := s.Store.AbrirPlatica(ctx, org, e.ID, login)
	if err == nil {
		err = s.Store.ElegirIAPlatica(ctx, p.ID, in.IA)
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v, err := s.platicaView(ctx, r, e)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, v)
}

// putPlaticaIdea is a person's correction of «la idea hasta ahora».
func (s *Server) putPlaticaIdea(w http.ResponseWriter, r *http.Request, login string) {
	e, ok := s.platicaEntry(w, r)
	if !ok {
		return
	}
	var in struct {
		Idea string `json:"idea"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Idea = strings.TrimSpace(in.Idea)
	switch {
	case len([]rune(in.Idea)) > 4000:
		httpError(w, http.StatusBadRequest, i18n.M("the idea must be up to 4,000 characters"))
		return
	case ruta.LooksSecret(in.Idea):
		httpError(w, http.StatusBadRequest, "this looks like it contains a secret (a password, token or key); the Ruta keeps no secrets")
		return
	}
	ctx := r.Context()
	p, err := s.Store.AbrirPlatica(ctx, orgOf(r), e.ID, login)
	if err == nil {
		err = s.Store.GuardarIdea(ctx, p.ID, in.Idea, login)
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v, err := s.platicaView(ctx, r, e)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, v)
}

// PropuestaItem is a proposed encargo, priced as the estimator prices it
// and with who should do it by the organization's amount.
type PropuestaItem struct {
	agentes.PropuestaEncargo
	CostoMicros int64  `json:"costoMicros"`
	Ruta        string `json:"ruta"`
}

// PropuestaView is the AI's proposal of encargos, for a person to change
// before creating them: with the estimator's prices for every difficulty
// (for one they add), the amount that decides the route, the apps they
// may choose and what the coding agent does.
type PropuestaView struct {
	Decidido          string          `json:"decidido"`
	Encargos          []PropuestaItem `json:"encargos"`
	Precios           []int64         `json:"precios"` // [d-1]: difficulty d
	EncargoAutoMicros int64           `json:"encargoAutoMicros"`
	Apps              []string        `json:"apps"`
	CodeAgent         string          `json:"codeAgent"`
	Model             string          `json:"model"`
	CostMicros        int64           `json:"costMicros"`
}

// appsParaEncargos are the organization's apps the person may ask
// encargos of (collaborator there).
func (s *Server) appsParaEncargos(r *http.Request) ([]string, error) {
	apps, err := s.Store.ListAppsInOrg(r.Context(), orgOf(r))
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, a := range apps {
		if store.RoleRank(accessOf(r).Role(a)) >= store.RoleRank(store.MemberCollaborator) {
			out = append(out, a.Name)
		}
	}
	return out, nil
}

// proponerPlatica asks the conversation's AI for the encargos that carry
// out what it decided (its own agent's conversation asks the organization's
// provider), priced by the estimator.
func (s *Server) proponerPlatica(w http.ResponseWriter, r *http.Request, login string) {
	e, ok := s.platicaEntry(w, r)
	if !ok {
		return
	}
	if s.IA == nil {
		httpError(w, http.StatusServiceUnavailable, "the AI gateway is not set up on this platform")
		return
	}
	ctx, org := r.Context(), orgOf(r)
	p, err := s.Store.PlaticaDe(ctx, e.ID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusConflict, i18n.M("talk it over first: the conversation is empty"))
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	msgs, err := s.Store.MensajesDePlatica(ctx, p.ID, 0, 0)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ias, set, err := s.platicaIAs(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	elegida := platicaElegida(p, ias, set)
	if elegida == store.PlaticaMiAgente {
		elegida = set.Provider // the agent talks over MCP; the platform's AI proposes
	}
	tope, err := s.Store.PlaticaTope(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if paga(elegida) && store.PlaticaLlegoAlTope(p, tope) {
		httpError(w, http.StatusPaymentRequired, i18n.M("this conversation reached its cap (%s): say whether to go on", ia.Dollars(tope)))
		return
	}
	apps, err := s.appsParaEncargos(r)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	prop, err := (&agentes.Platicador{Store: s.Store, Model: s.IA, Lang: idiomaDe(r)}).Proponer(cctx, e, p, msgs, elegida, apps)
	if prop.Res.CostMicros > 0 {
		_ = s.Store.GastoPlatica(context.WithoutCancel(ctx), p.ID, prop.Res.CostMicros)
	}
	if errors.Is(err, ia.ErrNoModel) {
		httpError(w, http.StatusServiceUnavailable, err.Error())
		return
	} else if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	est := s.estimador()
	v := PropuestaView{Decidido: prop.Decidido, Encargos: []PropuestaItem{}, EncargoAutoMicros: set.EncargoAutoMicros, Apps: apps,
		CodeAgent: set.CodeAgent, Model: prop.Res.Model, CostMicros: prop.Res.CostMicros}
	for d := 1; d <= 10; d++ {
		c, _ := est.Precio(ctx, set, d)
		v.Precios = append(v.Precios, c)
	}
	for _, x := range prop.Encargos {
		c, route := est.Precio(ctx, set, x.Dificultad)
		v.Encargos = append(v.Encargos, PropuestaItem{PropuestaEncargo: x, CostoMicros: c, Ruta: route})
	}
	writeJSON(w, v)
}

// PlaticaEncargosInput is what a person creates from the proposal: the
// encargos they kept (each with its app, difficulty, who does it and the
// earlier ones it comes after), whether to save the decision in the Ruta,
// and whether to send the platform AI's ones to the coding agent now.
type PlaticaEncargosInput struct {
	Encargos []struct {
		Titulo     string `json:"titulo"`
		App        string `json:"app"`
		Dificultad int    `json:"dificultad"`
		Detalle    string `json:"detalle"`
		Ruta       string `json:"ruta"`
		DespuesDe  []int  `json:"despuesDe"`
	} `json:"encargos"`
	Decision struct {
		Guardar bool   `json:"guardar"`
		Texto   string `json:"texto"`
	} `json:"decision"`
	Mandar bool `json:"mandar"`
}

// PlaticaEncargosView is what was made: the encargos, the decision entry,
// how many went to the coding agent, and why not, when they did not.
type PlaticaEncargosView struct {
	Encargos []store.Encargo  `json:"encargos"`
	Decision *store.RutaEntry `json:"decision,omitempty"`
	Enviados int              `json:"enviados"`
	Nota     string           `json:"note,omitempty"`
}

// palabrasDe are the words the made encargos and decision carry, in the
// person's language.
type palabrasDe struct{ es bool }

func (p palabrasDe) say(es, en string, args ...any) string {
	if p.es {
		return fmt.Sprintf(es, args...)
	}
	return fmt.Sprintf(en, args...)
}

// encargosDePlatica creates the encargos a person kept from the proposal,
// each carrying the conversation, its idea and the decision (so the agent
// knows why), priced as the estimator prices it; saves the decision as a
// Ruta entry linked to the one talked about; and sends the platform AI's
// to the coding agent when asked.
func (s *Server) encargosDePlatica(w http.ResponseWriter, r *http.Request, login string) {
	e, ok := s.platicaEntry(w, r)
	if !ok {
		return
	}
	var in PlaticaEncargosInput
	if !readJSON(w, r, &in) {
		return
	}
	ctx, org := r.Context(), orgOf(r)
	p, err := s.Store.PlaticaDe(ctx, e.ID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusConflict, i18n.M("talk it over first: the conversation is empty"))
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	in.Decision.Texto = strings.TrimSpace(in.Decision.Texto)
	if len(in.Encargos) > 10 || (len(in.Encargos) == 0 && !in.Decision.Guardar) {
		httpError(w, http.StatusBadRequest, i18n.M("create 1 to 10 encargos, or save the decision"))
		return
	}
	if in.Decision.Guardar && (in.Decision.Texto == "" || len([]rune(in.Decision.Texto)) > 4000 || ruta.LooksSecret(in.Decision.Texto)) {
		httpError(w, http.StatusBadRequest, i18n.M("say what was decided, up to 4,000 characters and with no secrets"))
		return
	}
	apps, err := s.appsParaEncargos(r)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range in.Encargos {
		x := &in.Encargos[i]
		x.Titulo, x.App, x.Detalle = strings.TrimSpace(x.Titulo), strings.TrimSpace(x.App), strings.TrimSpace(x.Detalle)
		n := i + 1
		switch {
		case x.Titulo == "" || len([]rune(x.Titulo)) > 200 || len([]rune(x.Detalle)) > 8000:
			httpError(w, http.StatusBadRequest, i18n.M("encargo %d: the title must be 1 to 200 characters, and the detail up to 8,000", n))
			return
		case !slices.Contains(apps, x.App):
			httpError(w, http.StatusBadRequest, i18n.M("encargo %d: choose its app, one you may ask encargos of", n))
			return
		case x.Dificultad < 1 || x.Dificultad > 10:
			httpError(w, http.StatusBadRequest, i18n.M("encargo %d: its difficulty is 1 to 10", n))
			return
		case x.Ruta != store.RoutePlatform && x.Ruta != store.RouteLocal:
			httpError(w, http.StatusBadRequest, i18n.M("encargo %d: it is for the platform's AI or for your own agent", n))
			return
		case ruta.LooksSecret(x.Titulo + "\n" + x.Detalle):
			httpError(w, http.StatusBadRequest, "this looks like it contains a secret; say where it lives instead of its value")
			return
		}
		for _, d := range x.DespuesDe {
			if d < 1 || d >= n {
				httpError(w, http.StatusBadRequest, i18n.M("encargo %d may come only after earlier ones", n))
				return
			}
		}
	}
	set, err := s.Store.AISettingsOf(ctx, org)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	say := palabrasDe{es: idiomaDe(r) == "es"}
	base := strings.TrimRight(s.BaseURL, "/")
	platicaURL := fmt.Sprintf("%s/ruta?tab=ruta&entrada=%d&platica=1", base, e.ID)
	out := PlaticaEncargosView{Encargos: []store.Encargo{}}

	var decisionID *int64
	if in.Decision.Guardar {
		title := []rune(say.say("Decisión: %s", "Decision: %s", e.Title))
		if len(title) > 200 {
			title = append(title[:199], '…')
		}
		body := in.Decision.Texto + "\n\n" + say.say("Salió de la plática sobre «%s» (ruta:%d): %s", "It came out of the conversation about «%s» (ruta:%d): %s", e.Title, e.ID, platicaURL)
		d, err := s.Store.AddRuta(ctx, store.RutaEntry{Kind: store.KindDecision, Title: string(title), Body: body, Tags: []string{"platica"},
			Author: login, OrgID: org, App: e.App})
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out.Decision, decisionID = &d, &d.ID
	} else if p.DecisionID != nil {
		decisionID = p.DecisionID
	}

	est := s.estimador()
	var made []store.Encargo
	for _, x := range in.Encargos {
		var b strings.Builder
		if x.Detalle != "" {
			b.WriteString(x.Detalle + "\n\n")
		}
		b.WriteString("---\n")
		b.WriteString(say.say("**Por qué:** salió de la plática sobre «%s» (ruta:%d): %s\n", "**Why:** it came out of the conversation about «%s» (ruta:%d): %s\n", e.Title, e.ID, platicaURL))
		if decisionID != nil {
			b.WriteString(say.say("**Lo decidido:** ruta:%d: %s/ruta?tab=ruta&entrada=%d\n", "**What was decided:** ruta:%d: %s/ruta?tab=ruta&entrada=%d\n", *decisionID, base, *decisionID))
		}
		if p.Idea != "" {
			b.WriteString(say.say("\n**La idea, como quedó:**\n%s\n", "\n**The idea, as it stood:**\n%s\n", p.Idea))
		}
		for _, d := range x.DespuesDe {
			prev := made[d-1]
			b.WriteString(say.say("\n**Después de:** el encargo #%d «%s»; hágalo cuando ese esté hecho.\n", "\n**After:** encargo #%d «%s»; do it once that one is done.\n", prev.ID, prev.Title))
		}
		enc, err := s.Store.CreateEncargo(ctx, store.Encargo{Title: x.Titulo, Body: b.String(), App: x.App, CreatedBy: login, OrgID: org})
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cost, route := est.Precio(ctx, set, x.Dificultad)
		if err := s.Store.SaveEstimate(ctx, enc.ID, x.Dificultad, cost, say.say("Lo estimó la plática que lo propuso.", "Estimated by the conversation that proposed it."), route); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if x.Ruta != route {
			if _, err := s.Store.RouteEncargo(ctx, enc.ID, login, x.Ruta); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if enc, err = s.Store.Encargo(ctx, enc.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		made = append(made, enc)
	}
	ids := []int64{}
	for _, enc := range made {
		ids = append(ids, enc.ID)
	}
	if in.Mandar && len(made) > 0 {
		var send []int64
		for _, enc := range made {
			if enc.Route == store.RoutePlatform {
				send = append(send, enc.ID)
			}
		}
		switch {
		case set.CodeAgent == store.CodeAgentOff:
			out.Nota = i18n.M("the coding agent is off; an administrator turns it on on the AI page")
		case len(send) == 0:
			out.Nota = i18n.M("none is for the platform's AI: they wait for your own agent")
		default:
			if out.Enviados, err = s.Store.DoEncargos(ctx, org, send, login); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	if err := s.Store.PlaticaHecha(ctx, p.ID, decisionID, ids); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The conversation says what came of it, for the others in it.
	var lista []string
	for _, enc := range made {
		lista = append(lista, fmt.Sprintf("#%d", enc.ID))
	}
	aviso := say.say("%s hizo %d encargos (%s).", "%s made %d encargos (%s).", login, len(made), strings.Join(lista, ", "))
	if len(made) == 0 {
		aviso = ""
	}
	if out.Decision != nil {
		aviso = strings.TrimSpace(aviso + " " + say.say("Guardó la decisión en la Ruta (ruta:%d).", "Saved the decision in the Ruta (ruta:%d).", out.Decision.ID))
	}
	_, _ = s.Store.AgregarMensaje(ctx, store.PlaticaMensaje{PlaticaID: p.ID, Author: login, Kind: store.PlaticaAviso, Body: aviso})
	for _, enc := range made {
		if enc, err := s.Store.Encargo(ctx, enc.ID); err == nil {
			out.Encargos = append(out.Encargos, enc)
		}
	}
	anotarDetalle(r, "encargos", ids)
	s.Log.Info("encargos made from a conversation", "entry", e.ID, "n", len(made), "sent", out.Enviados, "by", login)
	writeJSONStatus(w, http.StatusCreated, out)
}

// estimador prices encargos as the estimator does (the list prices when
// the platform has no AI gateway).
func (s *Server) estimador() *agentes.Estimator {
	g := s.IA
	if g == nil {
		g = &ia.Gateway{}
	}
	return &agentes.Estimator{Store: s.Store, Model: g}
}

// getPlaticaTope is what one conversation may spend before it asks.
func (s *Server) getPlaticaTope(w http.ResponseWriter, r *http.Request, _ string) {
	n, err := s.Store.PlaticaTope(r.Context(), orgOf(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]int64{"capMicros": n})
}

// putPlaticaTope sets what one conversation may spend before it asks
// whether to go on: $0.05 to $20.
func (s *Server) putPlaticaTope(w http.ResponseWriter, r *http.Request, login string) {
	var in struct {
		Dollars float64 `json:"dollars"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Dollars < 0.05 || in.Dollars > 20 {
		httpError(w, http.StatusBadRequest, i18n.M("the cap per conversation must be between $0.05 and $20"))
		return
	}
	if err := s.Store.SetPlaticaTope(r.Context(), orgOf(r), micros(in.Dollars), login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anotarDetalle(r, "dolares", in.Dollars)
	s.getPlaticaTope(w, r, login)
}
