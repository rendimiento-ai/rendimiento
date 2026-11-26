package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/agentes"
	"github.com/rendimiento-ai/rendimiento/internal/ajustes"
	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// IAKeys stores organizations' own AI keys (ia.SecretKeys).
type IAKeys interface {
	KeyHint(ctx context.Context, orgID int64) (string, error)
	SetOrgKey(ctx context.Context, orgID int64, key string) error
}

// IAView is the organization's AI page.
type IAView struct {
	Settings store.AISettings `json:"settings"`
	// KeyHint is the end of the organization's own key ("" when none).
	KeyHint string `json:"keyHint,omitempty"`
	// What it spent this month, by who paid, and what each may reach.
	SpentOwnMicros      int64  `json:"spentOwnMicros"`
	SpentPlatformMicros int64  `json:"spentPlatformMicros"`
	LocalModel          string `json:"localModel,omitempty"` // "" when there is no local model
	PlatformAvailable   bool   `json:"platformAvailable"`
	PlatformModel       string `json:"platformModel,omitempty"`
	// Escalations is how many new failures went to a stronger model this
	// month (against Settings.EscalateLimit).
	Escalations int             `json:"escalations"`
	Usage       []store.AIUsage `json:"usage"`
	// Coder is the coding agent's record (nil when it never took one).
	Coder *CoderBoleta `json:"coder,omitempty"`
	// CoderSandbox says the platform can run the coding agent's commands
	// in a sandbox (Settings.CodeRun lets it).
	CoderSandbox bool `json:"coderSandbox"`
}

// CoderBoleta is the coding agent's record: the encargos it took, how many
// it delivered at the first try, a person sent back, and it gave back; what
// it spent against what was estimated; and what the estimator learned.
type CoderBoleta struct {
	Taken          int                  `json:"taken"`
	FirstTry       int                  `json:"firstTry"`
	Returned       int                  `json:"returned"`
	GaveBack       int                  `json:"gaveBack"`
	SpentMicros    int64                `json:"spentMicros"`
	EstimateMicros int64                `json:"estimateMicros"`
	Learned        agentes.Learned      `json:"learned"`
	LearnFrom      int                  `json:"learnFrom"`
	Recent         []store.CoderEncargo `json:"recent"`
}

// coderBoleta reads the coding agent's record, and what it teaches the
// estimator, priced as the estimator prices it.
func (s *Server) coderBoleta(ctx context.Context, org int64) (*CoderBoleta, error) {
	record, err := s.Store.CoderRecord(ctx, org, agentes.CoderName, 200)
	if err != nil || len(record) == 0 {
		return nil, err
	}
	b := &CoderBoleta{Taken: len(record), LearnFrom: agentes.LearnFrom,
		Learned: agentes.Learn(record, (&agentes.Estimator{Model: s.IA}).Cost)}
	for _, r := range record {
		switch {
		case r.Returned > 0:
			b.Returned++
		case r.Delivered > 0 && (r.Status == store.EncargoDelivered || r.Status == store.EncargoReviewed):
			b.FirstTry++
		}
		if r.Released > 0 {
			b.GaveBack++
		}
		b.SpentMicros += r.SpentMicros
		if r.EstimateMicros != nil {
			b.EstimateMicros += *r.EstimateMicros
		}
	}
	b.Recent = record[:min(len(record), 12)]
	return b, nil
}

// iaView is the organization's AI settings, spending and latest calls.
func (s *Server) iaView(w http.ResponseWriter, r *http.Request, _ string) {
	if s.IA == nil {
		httpError(w, http.StatusServiceUnavailable, "the AI gateway is not set up on this platform")
		return
	}
	ctx, org := r.Context(), orgOf(r)
	v := IAView{LocalModel: "", PlatformAvailable: s.IA.PlatformKeyNow() != "", CoderSandbox: s.CoderSandbox}
	var err error
	if v.Settings, err = s.Store.AISettingsOf(ctx, org); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if url, model := s.IA.LocalNow(); url != "" {
		v.LocalModel = model
	}
	if v.PlatformAvailable {
		v.PlatformModel = s.IA.PlatformModel
	}
	if s.IAKeys != nil {
		v.KeyHint, _ = s.IAKeys.KeyHint(ctx, org)
	}
	if v.SpentOwnMicros, v.SpentPlatformMicros, err = s.Store.AISpent(ctx, org, time.Now()); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v.Escalations, err = s.Store.AICalls(ctx, org, agentes.Escalated, time.Now()); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v.Usage, err = s.Store.AIUsageOf(ctx, org, 30); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v.Usage == nil {
		v.Usage = []store.AIUsage{}
	}
	if v.Coder, err = s.coderBoleta(ctx, org); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, v)
}

// IAInput is what an organization's administrator chooses.
type IAInput struct {
	Provider  string  `json:"provider"` // local, own, platform
	Kind      string  `json:"kind"`     // own: anthropic or openai
	BaseURL   string  `json:"baseUrl"`  // openai: the API's address (https)
	Model     string  `json:"model"`
	CapDollar float64 `json:"capDollars"` // monthly, 0 to 1000
	// Key replaces the organization's own key (empty: keep it); RemoveKey
	// deletes it.
	Key       string `json:"key"`
	RemoveKey bool   `json:"removeKey"`
	// AutoDiagnose turns the diagnostician's explanations of failed runs on
	// or off (nil: unchanged).
	AutoDiagnose *bool `json:"autoDiagnose"`
	// EscalateTo is who answers new failures ("", own or platform), at most
	// EscalateLimit times a month (nil: unchanged).
	EscalateTo    *string `json:"escalateTo"`
	EscalateLimit *int    `json:"escalateLimit"`
	// EncargoAutoDollars is the most an encargo may be estimated to cost
	// for the platform's AI to take it (nil: unchanged).
	EncargoAutoDollars *float64 `json:"encargoAutoDollars"`
	// CodeAgent is what the platform's coding agent does: off, asked,
	// system or auto (nil: unchanged). It needs an Anthropic model.
	CodeAgent *string `json:"codeAgent"`
	// CodeConcurrency is how many encargos the coding agent may do at
	// once, 1 to store.MaxCodeConcurrency (nil: unchanged).
	CodeConcurrency *int `json:"codeConcurrency"`
	// CodeRun lets the coding agent run commands in a sandbox (nil:
	// unchanged).
	CodeRun *bool `json:"codeRun"`
	// EstimateLearn: the estimator corrects its table by what the coding
	// agent spent, or keeps the fixed table (nil: unchanged).
	EstimateLearn *bool `json:"estimateLearn"`
	// AutoEncargo opens an encargo for each explained failure: off,
	// branches or all (nil: unchanged).
	AutoEncargo *string `json:"autoEncargo"`
}

// micros turns dollars into millionths of a dollar.
func micros(d float64) int64 { return int64(math.Round(d * 1e6)) }

// putIA saves the organization's AI choices and, if given, its own key.
func (s *Server) putIA(w http.ResponseWriter, r *http.Request, login string) {
	if s.IA == nil {
		httpError(w, http.StatusServiceUnavailable, "the AI gateway is not set up on this platform")
		return
	}
	var in IAInput
	if !readJSON(w, r, &in) {
		return
	}
	in.BaseURL, in.Model, in.Key = strings.TrimSpace(in.BaseURL), strings.TrimSpace(in.Model), strings.TrimSpace(in.Key)
	switch {
	case in.Provider != store.AILocal && in.Provider != store.AIOwn && in.Provider != store.AIPlatform:
		httpError(w, http.StatusBadRequest, "provider must be local, own or platform")
		return
	case in.CapDollar < 0 || in.CapDollar > 1000:
		httpError(w, http.StatusBadRequest, "the monthly cap must be between $0 and $1,000")
		return
	case len(in.Model) > 100 || len(in.Key) > 500:
		httpError(w, http.StatusBadRequest, "the model or the key is too long")
		return
	}
	if in.Provider == store.AIOwn {
		switch in.Kind {
		case "anthropic":
			in.BaseURL = ""
		case "openai":
			u, err := url.Parse(in.BaseURL)
			if err != nil || u.Scheme != "https" || u.Host == "" || in.Model == "" {
				httpError(w, http.StatusBadRequest, "an OpenAI-compatible provider needs its https address and a model")
				return
			}
		default:
			httpError(w, http.StatusBadRequest, "the provider of your own key must be anthropic or openai")
			return
		}
	} else {
		in.Kind, in.BaseURL = "", ""
	}
	ctx, org := r.Context(), orgOf(r)
	if (in.Key != "" || in.RemoveKey) && s.IAKeys == nil {
		httpError(w, http.StatusServiceUnavailable, "this platform cannot keep keys")
		return
	}
	if in.RemoveKey {
		in.Key = ""
	}
	if in.Key != "" || in.RemoveKey {
		if err := s.IAKeys.SetOrgKey(ctx, org, in.Key); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.Store.SaveAISettings(ctx, store.AISettings{OrgID: org, Provider: in.Provider, Kind: in.Kind, BaseURL: in.BaseURL,
		Model: in.Model, CapMicros: micros(in.CapDollar)}, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if in.EscalateTo != nil {
		limit := 10
		if in.EscalateLimit != nil {
			limit = *in.EscalateLimit
		}
		if (*in.EscalateTo != "" && *in.EscalateTo != store.AIOwn && *in.EscalateTo != store.AIPlatform) || limit < 0 || limit > 1000 {
			httpError(w, http.StatusBadRequest, "escalate to own or platform (or nobody), at most 1,000 times a month")
			return
		}
		if err := s.Store.SetEscalation(ctx, org, *in.EscalateTo, limit, login); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if in.EncargoAutoDollars != nil {
		if *in.EncargoAutoDollars < 0 || *in.EncargoAutoDollars > 100 {
			httpError(w, http.StatusBadRequest, "the amount for encargos must be between $0 and $100")
			return
		}
		if err := s.Store.SetEncargoAuto(ctx, org, micros(*in.EncargoAutoDollars), login); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if in.CodeAgent != nil {
		mode := *in.CodeAgent
		if !store.CodeAgentMode(mode) {
			httpError(w, http.StatusBadRequest, "the coding agent is off, asked, system or auto")
			return
		}
		if mode != store.CodeAgentOff && !(in.Provider == store.AIPlatform || (in.Provider == store.AIOwn && in.Kind == "anthropic")) {
			httpError(w, http.StatusBadRequest, "the coding agent needs an Anthropic model: your own Anthropic key or the platform's")
			return
		}
		if err := s.Store.SetCodeAgent(ctx, org, mode, login); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if in.CodeConcurrency != nil {
		if n := *in.CodeConcurrency; n < 1 || n > store.MaxCodeConcurrency {
			httpError(w, http.StatusBadRequest, "the coding agent does 1 to "+strconv.Itoa(store.MaxCodeConcurrency)+" encargos at once")
			return
		}
		if err := s.Store.SetCodeConcurrency(ctx, org, *in.CodeConcurrency, login); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if in.AutoEncargo != nil {
		if !autoEncargoMode(*in.AutoEncargo, false) {
			httpError(w, http.StatusBadRequest, "an encargo opens for failures off, on branches or on all")
			return
		}
		if err := s.Store.SetAutoEncargo(ctx, org, *in.AutoEncargo, login); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if in.CodeRun != nil {
		if err := s.Store.SetCodeRun(ctx, org, *in.CodeRun, login); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if in.EstimateLearn != nil {
		if err := s.Store.SetEstimateLearn(ctx, org, *in.EstimateLearn, login); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if in.AutoDiagnose != nil {
		if err := s.Store.SetAutoDiagnose(ctx, org, *in.AutoDiagnose, login); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.Log.Info("AI settings saved", "org", org, "provider", in.Provider, "kind", in.Kind, "keyChanged", in.Key != "" || in.RemoveKey, "by", login)
	s.iaView(w, r, login)
}

// testIA asks the organization's model one question, to try the settings
// (measured like any call).
func (s *Server) testIA(w http.ResponseWriter, r *http.Request, login string) {
	if s.IA == nil {
		httpError(w, http.StatusServiceUnavailable, "the AI gateway is not set up on this platform")
		return
	}
	var in struct {
		Prompt string `json:"prompt"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Prompt == "" || len(in.Prompt) > 2000 {
		httpError(w, http.StatusBadRequest, "ask something, up to 2,000 characters")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	res, err := s.IA.Complete(ctx, orgOf(r), "try: "+login, ia.Request{MaxTokens: 400,
		System:   "Answer briefly, in the language of the question.",
		Messages: []ia.Message{{Role: "user", Content: in.Prompt}}})
	if errors.Is(err, ia.ErrNoModel) {
		httpError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, res)
}

// IAOrg is one organization on the operator's AI page.
type IAOrg struct {
	Org                 store.Org        `json:"org"`
	Settings            store.AISettings `json:"settings"`
	SpentPlatformMicros int64            `json:"spentPlatformMicros"`
}

// iaOrgs is the operator's view: every organization's allowance of the
// platform's AI and what it spent of it this month, and the platform's bill.
func (s *Server) iaOrgs(w http.ResponseWriter, r *http.Request, _ string) {
	if s.IA == nil {
		httpError(w, http.StatusServiceUnavailable, "the AI gateway is not set up on this platform")
		return
	}
	ctx, now := r.Context(), time.Now()
	orgs, err := s.Store.AllOrgs(ctx)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := struct {
		Orgs        []IAOrg `json:"orgs"`
		SpentMicros int64   `json:"spentMicros"`
		// CapMicros is what the platform's key may spend this month in all
		// (AI_PLATFORM_CAP); CapSaved says it was saved on the AI page
		// rather than read from the environment; AllowancesMicros is every
		// organization's allowance added up.
		CapMicros        int64 `json:"capMicros"`
		CapSaved         bool  `json:"capSaved"`
		AllowancesMicros int64 `json:"allowancesMicros"`
		PlatformKeySet   bool  `json:"platformKeySet"`
	}{Orgs: []IAOrg{}, CapMicros: s.IA.PlatformCapNow(), PlatformKeySet: s.IA.PlatformKeyNow() != ""}
	if s.Settings != nil {
		_, out.CapSaved = s.Settings.SavedValues()[ajustes.AIPlatformCap]
	}
	if out.SpentMicros, err = s.Store.AIPlatformSpent(ctx, now); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, o := range orgs {
		set, err := s.Store.AISettingsOf(ctx, o.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_, spent, err := s.Store.AISpent(ctx, o.ID, now)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out.Orgs = append(out.Orgs, IAOrg{Org: o, Settings: set, SpentPlatformMicros: spent})
		out.AllowancesMicros += set.AllowanceMicros
	}
	writeJSON(w, out)
}

// IACapView is the platform's total after a change on the AI page: what
// its key may spend a month in all, and whether saving an allowance just
// raised it.
type IACapView struct {
	CapMicros int64 `json:"capMicros"`
	Raised    bool  `json:"raised,omitempty"`
}

// saveIACap saves what the platform's key may spend a month in all as the
// live setting AI_PLATFORM_CAP: it wins over the environment's in every
// process within 15 seconds, this one at once.
func (s *Server) saveIACap(r *http.Request, micros int64, login string) error {
	eff := s.Settings.Effective()
	before := ajustes.Dollars(ajustes.AIPlatformCapMicros(eff))
	eff[ajustes.AIPlatformCap] = ajustes.Dollars(micros)
	if _, err := ajustes.Read(eff, s.Settings.PlatformNS, s.Settings.IngressClass); err != nil {
		return fmt.Errorf("%w: %w", errSettingBad, err)
	}
	if err := s.Store.SaveAjuste(r.Context(), ajustes.AIPlatformCap, ajustes.Dollars(micros), login); err != nil {
		return err
	}
	anotarDetalle(r, "tope_antes", before)
	anotarDetalle(r, "tope_despues", ajustes.Dollars(micros))
	_ = s.Settings.Refresh(r.Context())
	return nil
}

// errSettingBad is a value that does not check out with the other live
// settings.
var errSettingBad = errors.New("the setting does not check out")

// putIACap is what the platform's key may spend a month across every
// organization (AI_PLATFORM_CAP), changed on the AI page by its operators.
func (s *Server) putIACap(w http.ResponseWriter, r *http.Request, login string) {
	if s.IA == nil {
		httpError(w, http.StatusServiceUnavailable, "the AI gateway is not set up on this platform")
		return
	}
	if s.Settings == nil {
		httpError(w, http.StatusServiceUnavailable, "this process does not keep live settings")
		return
	}
	var in struct {
		Dollars float64 `json:"dollars"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Dollars < 0 || in.Dollars > maxIACap {
		httpError(w, http.StatusBadRequest, "the platform's total must be between $0 and $100,000 a month")
		return
	}
	if err := s.saveIACap(r, micros(in.Dollars), login); errors.Is(err, errSettingBad) {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("AI platform cap set", "dollars", in.Dollars, "by", login)
	writeJSON(w, IACapView{CapMicros: s.IA.PlatformCapNow()})
}

// maxIACap is the most, in dollars a month, the AI page lets the
// platform's key spend in all.
const maxIACap = 100_000

// putIAAllowance is what the operator lets an organization spend of the
// platform's AI each month.
func (s *Server) putIAAllowance(w http.ResponseWriter, r *http.Request, login string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad organization id")
		return
	}
	var in struct {
		Dollars float64 `json:"dollars"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Dollars < 0 || in.Dollars > 1000 {
		httpError(w, http.StatusBadRequest, "the allowance must be between $0 and $1,000")
		return
	}
	if _, err := s.Store.GetOrg(r.Context(), id); err != nil {
		httpError(w, http.StatusNotFound, "no such organization")
		return
	}
	if err := s.Store.SetAIAllowance(r.Context(), id, micros(in.Dollars), login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("AI allowance set", "org", id, "dollars", in.Dollars, "by", login)
	// An allowance is what the operator wants that organization to be able
	// to spend: when the allowances add up to more than the platform's
	// total, the total goes up with them, or the allowance would never be
	// reached.
	out := IACapView{}
	if s.IA != nil {
		out.CapMicros = s.IA.PlatformCapNow()
		total, err := s.Store.AIAllowancesTotal(r.Context())
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if total > out.CapMicros && s.Settings != nil {
			if err := s.saveIACap(r, total, login); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.Log.Info("AI platform cap raised to the allowances", "dollars", ajustes.Dollars(total), "by", login)
			out.CapMicros, out.Raised = s.IA.PlatformCapNow(), true
		}
	}
	writeJSON(w, out)
}

// autoEncargoMode says whether mode is one an organization (or, with
// empty allowed, an app following it) may choose.
func autoEncargoMode(mode string, empty bool) bool {
	return mode == store.AutoEncargoOff || mode == store.AutoEncargoBranches || mode == store.AutoEncargoAll || (empty && mode == "")
}

// putAppAutoEncargo is an app's own choice of when an encargo opens for its
// failures ("" follows the organization's AI page).
func (s *Server) putAppAutoEncargo(w http.ResponseWriter, r *http.Request, login string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	var in struct {
		Mode string `json:"mode"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !autoEncargoMode(in.Mode, true) {
		httpError(w, http.StatusBadRequest, "an encargo opens for failures off, on branches or on all, or as the organization says")
		return
	}
	if err := s.Store.SetAppAutoEncargo(r.Context(), a.ID, in.Mode); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("app auto encargo set", "app", a.Name, "mode", in.Mode, "by", login)
	w.WriteHeader(http.StatusNoContent)
}

// putAppAutoMerge is whether rendimiento merges the app's open pull
// requests once their branch passes its run: the ones a person approved
// in rendimiento, or any.
func (s *Server) putAppAutoMerge(w http.ResponseWriter, r *http.Request, login string) {
	a := s.app(w, r)
	if a == nil {
		return
	}
	var in struct {
		On  bool `json:"on"`
		Any bool `json:"any"` // every open pull request, not only what a person approved here
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.Store.SetAppAutoMerge(r.Context(), a.ID, in.On, in.Any); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Log.Info("app auto merge set", "app", a.Name, "on", in.On, "any", in.Any, "by", login)
	w.WriteHeader(http.StatusNoContent)
}

// LocalProbe is what an Ollama said when asked before connecting it.
type LocalProbe struct {
	Models   []string `json:"models"`
	HasModel bool     `json:"hasModel"` // it holds the model asked for
	Error    string   `json:"error,omitempty"`
}

// probeLocal asks an Ollama which models it holds, so the AI page can say
// whether it answers before its address is saved (AI_LOCAL_URL). Only the
// platform's administrators, who may save it, may ask.
func (s *Server) probeLocal(w http.ResponseWriter, r *http.Request, _ string) {
	var in struct {
		URL   string `json:"url"`
		Model string `json:"model"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.URL, in.Model = strings.TrimSpace(in.URL), strings.TrimSpace(in.Model)
	if _, err := ajustes.Read(ajustes.Values{ajustes.AILocalURL: in.URL, ajustes.AILocalModel: in.Model}, "", ""); err != nil || in.URL == "" {
		httpError(w, http.StatusBadRequest, "give an Ollama's address, such as http://192.168.1.20:11434")
		return
	}
	model := in.Model
	if model == "" {
		model = ia.DefaultLocalModel
	}
	out := LocalProbe{Models: []string{}}
	models, err := ia.ProbeLocal(r.Context(), &http.Client{}, in.URL)
	if err != nil {
		out.Error = err.Error()
		writeJSON(w, out)
		return
	}
	out.Models = models
	for _, m := range models {
		// Ollama names a model with its tag; "qwen3.5" means "qwen3.5:latest".
		if m == model || (!strings.Contains(model, ":") && m == model+":latest") {
			out.HasModel = true
		}
	}
	writeJSON(w, out)
}

// LocalModelView is the local model as the AI page shows it to the
// platform's administrators: its address and model now, and whether they
// were saved there (or come from the environment).
type LocalModelView struct {
	URL   string `json:"url"`
	Model string `json:"model"`
	Saved bool   `json:"saved"`
}

func (s *Server) localModelView(w http.ResponseWriter) {
	if s.Settings == nil {
		httpError(w, http.StatusServiceUnavailable, "this process does not keep live settings")
		return
	}
	eff, saved := s.Settings.Effective(), s.Settings.SavedValues()
	_, a := saved[ajustes.AILocalURL]
	_, b := saved[ajustes.AILocalModel]
	writeJSON(w, LocalModelView{URL: eff[ajustes.AILocalURL], Model: eff[ajustes.AILocalModel], Saved: a || b})
}

// getLocalModel is the local model the gateway uses now.
func (s *Server) getLocalModel(w http.ResponseWriter, _ *http.Request, _ string) { s.localModelView(w) }

// putLocalModel connects the local model (an empty address disconnects it):
// saved as the live settings AI_LOCAL_URL and AI_LOCAL_MODEL, it wins over
// the environment's in every process within 15 seconds, this one at once.
// On a cluster and on a computer alike, so it lives here and not under the
// Environment page's settings.
func (s *Server) putLocalModel(w http.ResponseWriter, r *http.Request, login string) {
	if s.Settings == nil {
		httpError(w, http.StatusServiceUnavailable, "this process does not keep live settings")
		return
	}
	var in struct {
		URL   string `json:"url"`
		Model string `json:"model"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.URL, in.Model = strings.TrimSpace(in.URL), strings.TrimSpace(in.Model)
	if in.URL == "" {
		in.Model = ""
	}
	eff := s.Settings.Effective()
	before := eff[ajustes.AILocalURL] + " " + eff[ajustes.AILocalModel]
	eff[ajustes.AILocalURL], eff[ajustes.AILocalModel] = in.URL, in.Model
	if _, err := ajustes.Read(eff, s.Settings.PlatformNS, s.Settings.IngressClass); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	for key, value := range map[string]string{ajustes.AILocalURL: in.URL, ajustes.AILocalModel: in.Model} {
		if err := s.Store.SaveAjuste(r.Context(), key, value, login); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	anotarDetalle(r, "antes", strings.TrimSpace(before))
	anotarDetalle(r, "despues", strings.TrimSpace(in.URL+" "+in.Model))
	_ = s.Settings.Refresh(r.Context())
	s.Log.Info("local model saved", "url", in.URL, "model", in.Model, "by", login)
	s.localModelView(w)
}
