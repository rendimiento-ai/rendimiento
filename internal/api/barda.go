package api

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/ajustes"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// LabelNoPolicy marks a node whose network does not enforce network
// policies (its kernel or plugin cannot): apps running there are outside
// the boundary whatever it says. The operator sets it with kubectl label.
const LabelNoPolicy = "rendimiento.ai/network-policy"

// Why an app is outside the boundary.
const (
	OutsideShared = "shared" // a shared namespace: someone else's, never walled
	OutsideNode   = "node"   // a pod runs on a node that does not enforce it
)

// Barda is the boundary between organizations as one organization's apps
// see it: whether it is on, who is let in (the ingress, rendimiento, the
// listed namespaces and ranges), which apps it leaves out and why, the
// ports open to the LAN, and who calls in from outside.
type Barda struct {
	// State is "on" or "off".
	State      string   `json:"state"`
	Org        string   `json:"org"`
	Ingress    string   `json:"ingress"`
	Platform   string   `json:"platform"`
	Namespaces []string `json:"namespaces"`
	CIDRs      []string `json:"cidrs"`
	// Outside are the apps the boundary does not cover: app -> why
	// (OutsideShared, OutsideNode), and Nodes the nodes behind OutsideNode.
	Outside map[string]string   `json:"outside"`
	Nodes   map[string][]string `json:"nodes"`
	// LAN are each app's services open to the local network.
	LAN map[string][]string `json:"lan"`
	// Callers point at the apps from outside the organization: with the
	// boundary on, those not In are stopped.
	Callers []BoundaryCaller `json:"callers"`
}

// barda works out the boundary for an organization's apps (nil when there
// is no cluster or no settings: nothing to draw).
func (s *Server) barda(ctx context.Context, orgID int64, apps []*store.App) (*Barda, error) {
	if s.Settings == nil || s.Kube == nil {
		return nil, nil
	}
	org, err := s.Store.GetOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	eff, net := s.Settings.Effective(), s.Settings.Network()
	b := &Barda{State: "off", Org: org.Slug, Ingress: ajustes.IngressNamespaceOf(eff, s.Settings.IngressClass), Platform: s.Settings.PlatformNS,
		Namespaces: ajustes.Split(eff[ajustes.OrgNetworkNamespaces]), CIDRs: net.CIDRs,
		Outside: map[string]string{}, Nodes: map[string][]string{}, LAN: map[string][]string{}}
	if net.Enabled {
		b.State = "on"
	}
	if b.Namespaces == nil {
		b.Namespaces = []string{}
	}
	if b.CIDRs == nil {
		b.CIDRs = []string{}
	}
	var nodes corev1.NodeList
	if err := s.Kube.List(ctx, &nodes, client.MatchingLabels{LabelNoPolicy: "no"}); err != nil {
		return nil, err
	}
	open := map[string]bool{}
	for _, n := range nodes.Items {
		open[n.Name] = true
	}
	for _, a := range apps {
		for _, svc := range a.Spec.Services {
			if svc.LAN != nil {
				b.LAN[a.Name] = append(b.LAN[a.Name], svc.Name)
			}
		}
		if a.Spec.SharedNamespace {
			b.Outside[a.Name] = OutsideShared
			continue
		}
		if len(open) == 0 {
			continue
		}
		var pods corev1.PodList
		if err := s.Kube.List(ctx, &pods, client.InNamespace(a.Name)); err != nil {
			return nil, err
		}
		for _, p := range pods.Items {
			if n := p.Spec.NodeName; open[n] && !slices.Contains(b.Nodes[a.Name], n) {
				b.Nodes[a.Name] = append(b.Nodes[a.Name], n)
				b.Outside[a.Name] = OutsideNode
			}
		}
		sort.Strings(b.Nodes[a.Name])
	}
	all, err := s.Store.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	if b.Callers, err = s.boundaryCallers(ctx, all, orgID); err != nil {
		return nil, err
	}
	return b, nil
}

// AppBarda is one app's place in the boundary, for its page.
type AppBarda struct {
	Barda
	// Inside is false when the boundary does not cover the app (or is off).
	Inside bool `json:"inside"`
	// Why it is outside: OutsideShared, OutsideNode, or "off".
	Why string `json:"why,omitempty"`
}

// appBarda is the boundary as one app sees it (204 with no cluster).
func (s *Server) appBarda(w http.ResponseWriter, r *http.Request, _ string) {
	app := s.app(w, r)
	if app == nil {
		return
	}
	b, err := s.barda(r.Context(), app.OrgID, []*store.App{app})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if b == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	out := AppBarda{Barda: *b, Why: b.Outside[app.Name]}
	out.Callers = callersOf(b.Callers, []*store.App{app})
	switch {
	case out.Why != "":
	case b.State != "on":
		out.Why = "off"
	default:
		out.Inside = true
	}
	writeJSON(w, out)
}

// callersOf keeps the calls into the given apps, and the callers left with
// any: a person sees nothing of an app they cannot see.
func callersOf(callers []BoundaryCaller, apps []*store.App) []BoundaryCaller {
	out := []BoundaryCaller{}
	for _, c := range callers {
		var calls []string
		for _, call := range c.Calls {
			if slices.ContainsFunc(apps, func(a *store.App) bool { return strings.HasPrefix(call, a.Name+"/") }) {
				calls = append(calls, call)
			}
		}
		if len(calls) > 0 {
			c.Calls = calls
			out = append(out, c)
		}
	}
	return out
}
