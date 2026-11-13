package api

import (
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/ajustes"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// SettingView is one live setting on the Environment page: the value in
// force, the environment's (the ConfigMap's), and the saved one that wins
// over it.
type SettingView struct {
	ajustes.Def
	Value string        `json:"value"`
	Env   string        `json:"env"`
	Saved *store.Ajuste `json:"saved,omitempty"`
	// Applies is false for a cluster setting on one computer.
	Applies bool `json:"applies"`
}

// FixedSetting is a setting the page shows but does not change: the
// platform needs it to start, or it changes what already exists. Secrets
// show only whether they are set.
type FixedSetting struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Secret bool   `json:"secret,omitempty"`
	Set    bool   `json:"set"`
}

// SettingsView is every setting the Environment page shows.
type SettingsView struct {
	Live []SettingView `json:"live"`
	// Invalid is why the saved values are set aside, if they are.
	Invalid string         `json:"invalid,omitempty"`
	Fixed   []FixedSetting `json:"fixed"`
	// IngressNamespace is where the boundary lets the ingress in from now.
	IngressNamespace string `json:"ingressNamespace"`
	PlatformNS       string `json:"platformNamespace"`
}

// fixedSettings are shown with their values; fixedSecrets only as set or not.
var (
	fixedSettings = []string{"BASE_URL", "MODE", "ROLE", "INGRESS_CLASS", "STORAGE_CLASS", "REGISTRY", "CLUSTER_ISSUER", "DNS_ZONE", "DNS_TARGET"}
	fixedSecrets  = []string{"DATABASE_URL", "CLOUDFLARE_API_TOKEN", "SECRETS_KEY_FILE", "AI_PLATFORM_KEY", "SMTP_PASSWORD"}
)

func (s *Server) settingsView(w http.ResponseWriter) {
	if s.Settings == nil {
		httpError(w, http.StatusServiceUnavailable, "the platform's settings are not read in this process")
		return
	}
	v := SettingsView{Live: []SettingView{}, Fixed: []FixedSetting{}, Invalid: s.Settings.Invalid(), PlatformNS: s.Settings.PlatformNS}
	eff, saved := s.Settings.Effective(), s.Settings.SavedValues()
	for _, d := range ajustes.Live {
		sv := SettingView{Def: d, Value: eff[d.Key], Env: s.Settings.Boot[d.Key], Applies: !d.Cluster || s.Kube != nil}
		if a, ok := saved[d.Key]; ok {
			sv.Saved = &a
		}
		v.Live = append(v.Live, sv)
	}
	for _, k := range fixedSettings {
		val := os.Getenv(k)
		v.Fixed = append(v.Fixed, FixedSetting{Key: k, Value: val, Set: val != ""})
	}
	for _, k := range fixedSecrets {
		v.Fixed = append(v.Fixed, FixedSetting{Key: k, Secret: true, Set: os.Getenv(k) != ""})
	}
	v.IngressNamespace = ajustes.IngressNamespaceOf(eff, s.Settings.IngressClass)
	writeJSON(w, v)
}

// getSettings is every setting, live and fixed, with where its value comes from.
func (s *Server) getSettings(w http.ResponseWriter, _ *http.Request, _ string) { s.settingsView(w) }

// putSetting saves a live setting: it wins over the environment's value in
// every process within 15 seconds, this one at once. It is checked with
// the others first, as at boot, and the namespaces it names must exist.
func (s *Server) putSetting(w http.ResponseWriter, r *http.Request, login string) {
	key := r.PathValue("key")
	d, ok := s.liveSetting(w, key)
	if !ok {
		return
	}
	var in struct {
		Value string `json:"value"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	value := strings.TrimSpace(in.Value)
	if key == ajustes.OrgNetworkNamespaces || key == ajustes.OrgNetworkCIDRs {
		value = strings.Join(ajustes.Split(value), ",")
	}
	eff := s.Settings.Effective()
	before := eff[key]
	eff[key] = value
	if _, err := ajustes.Read(eff, s.Settings.PlatformNS, s.Settings.IngressClass); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Group == "boundary" {
		if missing := s.missingNamespaces(r, eff, key); missing != "" {
			httpError(w, http.StatusBadRequest, "namespace "+missing+" does not exist")
			return
		}
	}
	if err := s.Store.SaveAjuste(r.Context(), key, value, login); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anotarDetalle(r, "antes", before)
	anotarDetalle(r, "despues", value)
	_ = s.Settings.Refresh(r.Context())
	s.Log.Info("platform setting saved", "setting", key, "value", value, "by", login)
	s.settingsView(w)
}

// deleteSetting forgets a saved setting: the environment's value holds again.
func (s *Server) deleteSetting(w http.ResponseWriter, r *http.Request, login string) {
	key := r.PathValue("key")
	if _, ok := s.liveSetting(w, key); !ok {
		return
	}
	eff := s.Settings.Effective()
	if env, ok := s.Settings.Boot[key]; ok {
		eff[key] = env
	} else {
		delete(eff, key)
	}
	if _, err := ajustes.Read(eff, s.Settings.PlatformNS, s.Settings.IngressClass); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	before := s.Settings.Effective()[key]
	if _, err := s.Store.DeleteAjuste(r.Context(), key); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	anotarDetalle(r, "antes", before)
	anotarDetalle(r, "despues", s.Settings.Boot[key])
	_ = s.Settings.Refresh(r.Context())
	s.Log.Info("platform setting back to the environment's value", "setting", key, "by", login)
	s.settingsView(w)
}

// liveSetting finds a live setting that can be changed in this process.
func (s *Server) liveSetting(w http.ResponseWriter, key string) (ajustes.Def, bool) {
	if s.Settings == nil {
		httpError(w, http.StatusServiceUnavailable, "the platform's settings are not read in this process")
		return ajustes.Def{}, false
	}
	i := slices.IndexFunc(ajustes.Live, func(d ajustes.Def) bool { return d.Key == key })
	if i < 0 {
		httpError(w, http.StatusNotFound, "no setting "+key+" can be changed here")
		return ajustes.Def{}, false
	}
	d := ajustes.Live[i]
	if d.Cluster && s.Kube == nil {
		httpError(w, http.StatusConflict, key+" does not apply on one computer")
		return d, false
	}
	return d, true
}

// missingNamespaces is the first namespace the changed setting names that
// the cluster does not have ("" when they all exist, or with no cluster).
func (s *Server) missingNamespaces(r *http.Request, eff ajustes.Values, key string) string {
	if s.Kube == nil {
		return ""
	}
	var names []string
	switch key {
	case ajustes.IngressNamespace:
		names = []string{eff[key]}
	case ajustes.OrgNetworkNamespaces:
		names = ajustes.Split(eff[key])
	}
	for _, n := range names {
		if n == "" {
			continue
		}
		var ns corev1.Namespace
		if err := s.Kube.Get(r.Context(), client.ObjectKey{Name: n}, &ns); apierrors.IsNotFound(err) {
			return n
		}
	}
	return ""
}

// BoundaryCaller is a namespace whose workloads call apps across the
// boundary: with it on, they are stopped unless it is let in.
type BoundaryCaller struct {
	Namespace string `json:"namespace"`
	// Calls are the services it calls (app/service) and the variable that
	// points at each.
	Calls []string `json:"calls"`
	// In is true when the boundary lets it in already.
	In bool `json:"in"`
}

// BoundaryCheck is what the page shows before the boundary is turned on.
type BoundaryCheck struct {
	Callers []BoundaryCaller `json:"callers"`
	// IngressNamespace is where the ingress is let in from, and IngressFound
	// whether that namespace exists.
	IngressNamespace string `json:"ingressNamespace"`
	IngressFound     bool   `json:"ingressFound"`
}

// boundaryCheck lists the likely callers of apps from outside their
// organization: workloads whose environment points at an app's service
// (the services catalog). rendimiento sees no traffic, so it is a guide,
// not a proof; the page says so.
func (s *Server) boundaryCheck(w http.ResponseWriter, r *http.Request, _ string) {
	if s.Settings == nil || s.Kube == nil || s.Catalog == nil {
		httpError(w, http.StatusConflict, "the boundary needs a cluster")
		return
	}
	ctx := r.Context()
	eff := s.Settings.Effective()
	out := BoundaryCheck{Callers: []BoundaryCaller{}, IngressNamespace: ajustes.IngressNamespaceOf(eff, s.Settings.IngressClass)}
	if out.IngressNamespace != "" {
		var ns corev1.Namespace
		out.IngressFound = s.Kube.Get(ctx, client.ObjectKey{Name: out.IngressNamespace}, &ns) == nil
	}
	apps, err := s.Store.ListApps(ctx)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The namespaces with the boundary, by their organization.
	orgOfNS := map[string]int64{}
	for _, a := range apps {
		if !a.Spec.SharedNamespace {
			orgOfNS[a.Name] = a.OrgID
		}
	}
	allowed := s.Settings.Network().Namespaces
	cat, err := s.Catalog.Get(ctx, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byNS := map[string]*BoundaryCaller{}
	for _, e := range cat.Entries {
		org, ok := orgOfNS[e.Namespace]
		if !ok {
			continue
		}
		for _, c := range e.UsedBy {
			if c.Namespace == e.Namespace {
				continue
			}
			if o, mine := orgOfNS[c.Namespace]; mine && o == org {
				continue // its own organization: always in
			}
			bc := byNS[c.Namespace]
			if bc == nil {
				bc = &BoundaryCaller{Namespace: c.Namespace, Calls: []string{}, In: slices.Contains(allowed, c.Namespace)}
				byNS[c.Namespace] = bc
			}
			call := e.Namespace + "/" + e.Name
			if c.Env != "" {
				call += " (" + c.Env + ")"
			}
			if !slices.Contains(bc.Calls, call) {
				bc.Calls = append(bc.Calls, call)
			}
		}
	}
	for _, bc := range byNS {
		sort.Strings(bc.Calls)
		out.Callers = append(out.Callers, *bc)
	}
	sort.Slice(out.Callers, func(i, j int) bool { return out.Callers[i].Namespace < out.Callers[j].Namespace })
	writeJSON(w, out)
}
