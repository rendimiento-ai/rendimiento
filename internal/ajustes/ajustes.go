// Package ajustes is the platform's settings that change while it runs:
// the boundary between organizations, their quota, and the platform's AI
// (the local model, and what the platform's key may spend). Each one starts
// from its environment variable (the ConfigMap) and, once someone saves it
// on the Environment page, follows the saved value, in every process,
// without a restart. The checks are the same at boot and on the page.
package ajustes

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// The settings that change live, by their environment variable.
const (
	OrgNetworkPolicy     = "ORG_NETWORK_POLICY"
	IngressNamespace     = "INGRESS_NAMESPACE"
	OrgNetworkNamespaces = "ORG_NETWORK_POLICY_NAMESPACES"
	OrgNetworkCIDRs      = "ORG_NETWORK_POLICY_CIDRS"
	OrgQuotaCPU          = "ORG_QUOTA_CPU"
	OrgQuotaMemory       = "ORG_QUOTA_MEMORY"
	AILocalURL           = "AI_LOCAL_URL"
	AILocalModel         = "AI_LOCAL_MODEL"
	AIPlatformCap        = "AI_PLATFORM_CAP"
)

// DefaultAIPlatformCap is what the platform's key may spend a month, in
// dollars, across every organization, when AI_PLATFORM_CAP says nothing.
const DefaultAIPlatformCap = 5

// Def is one live setting: its variable, the group it is shown in, and
// whether it only means something on a cluster.
type Def struct {
	Key     string `json:"key"`
	Group   string `json:"group"`
	Cluster bool   `json:"cluster"`
}

// Live are the settings the Environment page changes. A setting becomes
// live by adding it here (and reading it through the Holder).
var Live = []Def{
	{OrgNetworkPolicy, "boundary", true},
	{IngressNamespace, "boundary", true},
	{OrgNetworkNamespaces, "boundary", true},
	{OrgNetworkCIDRs, "boundary", true},
	{OrgQuotaCPU, "quota", false},
	{OrgQuotaMemory, "quota", false},
	{AILocalURL, "ai", false},
	{AILocalModel, "ai", false},
	{AIPlatformCap, "ai", false},
}

// Known says whether key is a live setting.
func Known(key string) bool {
	return slices.ContainsFunc(Live, func(d Def) bool { return d.Key == key })
}

// MaxListed is the most namespaces or address ranges a list may hold.
const MaxListed = 50

// ingressNamespaces are where the usual ingress controllers run, by
// ingress class, when INGRESS_NAMESPACE does not say.
var ingressNamespaces = map[string]string{"nginx": "ingress-nginx", "traefik": "kube-system"}

// Values are settings by their variable; a missing or empty one is unset.
type Values map[string]string

// FromEnv reads the live settings from the environment (the ConfigMap).
func FromEnv(getenv func(string) string) Values {
	v := Values{}
	for _, d := range Live {
		if s := getenv(d.Key); s != "" {
			v[d.Key] = s
		}
	}
	return v
}

// Boundary is what the settings make: the network boundary between
// organizations and the quota of each one (zero: no limit).
type Boundary struct {
	Network     render.OrgNetwork
	QuotaCPU    resource.Quantity
	QuotaMemory resource.Quantity
}

// Read checks the settings together and works out the boundary. The
// platform's own namespace is always let in; the ingress controller's is
// INGRESS_NAMESPACE, or the usual one for the ingress class.
func Read(v Values, platformNS, ingressClass string) (Boundary, error) {
	var b Boundary
	switch s := v[OrgNetworkPolicy]; s {
	case "true":
		b.Network.Enabled = true
	case "false", "":
	default:
		return b, fmt.Errorf("%s must be true or false, got %q", OrgNetworkPolicy, s)
	}
	ingressNS := v[IngressNamespace]
	if ingressNS == "" {
		ingressNS = ingressNamespaces[ingressClass]
	}
	if ingressNS != "" && !isLabel(ingressNS) {
		return b, fmt.Errorf("%s must be a namespace's name, got %q", IngressNamespace, ingressNS)
	}
	if b.Network.Enabled && ingressNS == "" {
		return b, fmt.Errorf("%s needs %s, the namespace the ingress controller of class %q runs in", OrgNetworkPolicy, IngressNamespace, ingressClass)
	}
	listed := Split(v[OrgNetworkNamespaces])
	if len(listed) > MaxListed {
		return b, fmt.Errorf("%s holds at most %d namespaces", OrgNetworkNamespaces, MaxListed)
	}
	for _, ns := range listed {
		if !isLabel(ns) {
			return b, fmt.Errorf("%s must be namespaces' names, got %q", OrgNetworkNamespaces, ns)
		}
	}
	b.Network.Namespaces = []string{platformNS}
	if ingressNS != "" {
		b.Network.Namespaces = append(b.Network.Namespaces, ingressNS)
	}
	b.Network.Namespaces = append(b.Network.Namespaces, listed...)
	b.Network.CIDRs = Split(v[OrgNetworkCIDRs])
	if len(b.Network.CIDRs) > MaxListed {
		return b, fmt.Errorf("%s holds at most %d address ranges", OrgNetworkCIDRs, MaxListed)
	}
	for _, c := range b.Network.CIDRs {
		if _, _, err := net.ParseCIDR(c); err != nil {
			return b, fmt.Errorf("%s must be address ranges such as 192.168.1.0/24, got %q", OrgNetworkCIDRs, c)
		}
	}
	for _, q := range []struct {
		key string
		to  *resource.Quantity
	}{{OrgQuotaCPU, &b.QuotaCPU}, {OrgQuotaMemory, &b.QuotaMemory}} {
		s := v[q.key]
		if s == "" {
			continue
		}
		n, err := resource.ParseQuantity(s)
		if err != nil || n.Sign() <= 0 {
			return b, fmt.Errorf("%s must be a quantity such as 4 or 8Gi, got %q", q.key, s)
		}
		*q.to = n
	}
	if s := v[AILocalURL]; s != "" {
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return b, fmt.Errorf("%s must be an Ollama's address such as http://192.168.1.20:11434, got %q", AILocalURL, s)
		}
	}
	if s := v[AILocalModel]; len(s) > 200 || strings.ContainsAny(s, " \t\n") {
		return b, fmt.Errorf("%s must be a model's name such as qwen3.5:4b, got %q", AILocalModel, s)
	}
	if s := v[AIPlatformCap]; s != "" {
		if _, err := capDollars(s); err != nil {
			return b, err
		}
	}
	return b, nil
}

// capDollars reads AI_PLATFORM_CAP: dollars a month, none or more.
func capDollars(s string) (float64, error) {
	d, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || d < 0 || math.IsInf(d, 0) || math.IsNaN(d) {
		return 0, fmt.Errorf("%s must be dollars a month, such as 5, got %q", AIPlatformCap, s)
	}
	return d, nil
}

// AIPlatformCapMicros is what the platform's key may spend a month across
// every organization, in millionths of a dollar: AI_PLATFORM_CAP, or
// DefaultAIPlatformCap when it is unset (or does not check out, which
// Read stops first).
func AIPlatformCapMicros(v Values) int64 {
	d, err := capDollars(v[AIPlatformCap])
	if v[AIPlatformCap] == "" || err != nil {
		d = DefaultAIPlatformCap
	}
	return int64(math.Round(d * 1e6))
}

// Dollars writes millionths of a dollar as a setting's value: 16, 2.5.
func Dollars(micros int64) string {
	return strconv.FormatFloat(float64(micros)/1e6, 'f', -1, 64)
}

// IngressNamespaceOf is the namespace the boundary lets the ingress
// controller in from: the setting, or the usual one for the class.
func IngressNamespaceOf(v Values, ingressClass string) string {
	if s := v[IngressNamespace]; s != "" {
		return s
	}
	return ingressNamespaces[ingressClass]
}

// Split reads a list written with commas or spaces.
func Split(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' })
}

func isLabel(s string) bool { return len(validation.IsDNS1123Label(s)) == 0 }

// Saved are the settings someone saved on the Environment page.
type Saved interface {
	GetAjustes(ctx context.Context) (map[string]store.Ajuste, error)
}

// Holder keeps the live settings of one process: the environment's at
// boot, then each saved value over it, read again every 15 seconds. A
// saved value that no longer checks out (with the others) is set aside and
// the environment's is used, with the reason in Invalid.
type Holder struct {
	Store Saved
	// Boot are the environment's values, checked at boot.
	Boot                     Values
	PlatformNS, IngressClass string
	Log                      *slog.Logger
	// OnChange is called when the boundary changes (the control service
	// then reconciles every app, so policies follow within a minute).
	OnChange func(Boundary)

	cur atomic.Pointer[state]
}

type state struct {
	saved     map[string]store.Ajuste
	effective Values
	boundary  Boundary
	invalid   string
}

// Start checks the environment's values (an error stops the platform, as a
// bad variable always has) and makes them the current ones.
func (h *Holder) Start() error {
	b, err := Read(h.Boot, h.PlatformNS, h.IngressClass)
	if err != nil {
		return err
	}
	h.cur.Store(&state{saved: map[string]store.Ajuste{}, effective: copyValues(h.Boot), boundary: b})
	return nil
}

// Run reads the saved values now and every 15 seconds until ctx ends.
func (h *Holder) Run(ctx context.Context) {
	for {
		if err := h.Refresh(ctx); err != nil && h.Log != nil {
			h.Log.Warn("could not read the platform's settings; keeping the last ones", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
	}
}

// Refresh reads the saved values and follows them; when the database
// cannot be read, the last values stay.
func (h *Holder) Refresh(ctx context.Context) error {
	saved, err := h.Store.GetAjustes(ctx)
	if err != nil {
		return err
	}
	h.apply(saved)
	return nil
}

func (h *Holder) apply(saved map[string]store.Ajuste) {
	eff := copyValues(h.Boot)
	for k, a := range saved {
		if Known(k) {
			eff[k] = a.Value
		}
	}
	st := &state{saved: saved, effective: eff}
	b, err := Read(eff, h.PlatformNS, h.IngressClass)
	if err != nil {
		// A saved value that does not check out (a namespace deleted since,
		// say): the environment's values hold until it is fixed.
		st.invalid, st.effective = err.Error(), copyValues(h.Boot)
		b, _ = Read(h.Boot, h.PlatformNS, h.IngressClass)
		if h.Log != nil {
			h.Log.Warn("a saved setting does not check out; using the environment's", "err", err)
		}
	}
	st.boundary = b
	prev := h.cur.Swap(st)
	if h.OnChange != nil && (prev == nil || !sameNetwork(prev.boundary.Network, b.Network)) {
		h.OnChange(b)
	}
}

// Boundary is the boundary and quota now.
func (h *Holder) Boundary() Boundary {
	if s := h.cur.Load(); s != nil {
		return s.boundary
	}
	b, _ := Read(h.Boot, h.PlatformNS, h.IngressClass)
	return b
}

// Network is the network boundary now, for the controller's render.
func (h *Holder) Network() render.OrgNetwork { return h.Boundary().Network }

// Effective are the values in force now.
func (h *Holder) Effective() Values {
	if s := h.cur.Load(); s != nil {
		return copyValues(s.effective)
	}
	return copyValues(h.Boot)
}

// SavedValues are the values saved on the page, by variable.
func (h *Holder) SavedValues() map[string]store.Ajuste {
	if s := h.cur.Load(); s != nil {
		return s.saved
	}
	return map[string]store.Ajuste{}
}

// Invalid is why the saved values are set aside ("" when they are not).
func (h *Holder) Invalid() string {
	if s := h.cur.Load(); s != nil {
		return s.invalid
	}
	return ""
}

func copyValues(v Values) Values {
	out := Values{}
	for k, s := range v {
		out[k] = s
	}
	return out
}

func sameNetwork(a, b render.OrgNetwork) bool {
	return a.Enabled == b.Enabled && slices.Equal(a.Namespaces, b.Namespaces) && slices.Equal(a.CIDRs, b.CIDRs)
}
