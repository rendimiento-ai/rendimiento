package ajustes

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

func TestRead(t *testing.T) {
	b, err := Read(Values{OrgNetworkPolicy: "true", OrgNetworkNamespaces: "monitoring, umami", OrgNetworkCIDRs: "192.168.50.0/24", OrgQuotaCPU: "4", OrgQuotaMemory: "8Gi"},
		"rendimiento-system", "nginx")
	if err != nil {
		t.Fatal(err)
	}
	if !b.Network.Enabled || !slices.Equal(b.Network.Namespaces, []string{"rendimiento-system", "ingress-nginx", "monitoring", "umami"}) ||
		!slices.Equal(b.Network.CIDRs, []string{"192.168.50.0/24"}) || b.QuotaCPU.String() != "4" || b.QuotaMemory.String() != "8Gi" {
		t.Fatalf("boundary = %+v", b)
	}
	if b, err := Read(Values{}, "rendimiento-system", "nginx"); err != nil || b.Network.Enabled || !b.QuotaCPU.IsZero() {
		t.Fatalf("nothing set: %+v %v", b, err)
	}
	for _, c := range []struct {
		v     Values
		class string
		want  string
	}{
		{Values{OrgNetworkPolicy: "yes"}, "nginx", "must be true or false"},
		{Values{OrgNetworkPolicy: "true"}, "other", "needs INGRESS_NAMESPACE"},
		{Values{IngressNamespace: "Ingress_NGINX"}, "nginx", "must be a namespace's name"},
		{Values{OrgNetworkNamespaces: "monitoring,no espacios!"}, "nginx", "must be namespaces' names"},
		{Values{OrgNetworkCIDRs: "192.168.50.0"}, "nginx", "must be address ranges"},
		{Values{OrgQuotaCPU: "-1"}, "nginx", "must be a quantity"},
		{Values{OrgQuotaMemory: "mucho"}, "nginx", "must be a quantity"},
		{Values{OrgNetworkNamespaces: strings.Repeat("a,", MaxListed+1)}, "nginx", "at most"},
	} {
		if _, err := Read(c.v, "rendimiento-system", c.class); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Read(%v) = %v, want %q", c.v, err, c.want)
		}
	}
}

type saved struct {
	m   map[string]store.Ajuste
	err error
}

func (s *saved) GetAjustes(context.Context) (map[string]store.Ajuste, error) { return s.m, s.err }

func TestHolderSavedWins(t *testing.T) {
	st := &saved{m: map[string]store.Ajuste{}}
	changes := 0
	h := &Holder{Store: st, Boot: Values{OrgNetworkPolicy: "false", OrgQuotaCPU: "2"}, PlatformNS: "rendimiento-system", IngressClass: "nginx",
		OnChange: func(Boundary) { changes++ }}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	if h.Network().Enabled || h.Boundary().QuotaCPU.String() != "2" {
		t.Fatalf("at boot: %+v", h.Boundary())
	}
	ctx := context.Background()
	// A saved value wins over the environment's, setting by setting.
	st.m = map[string]store.Ajuste{OrgNetworkPolicy: {Key: OrgNetworkPolicy, Value: "true", UpdatedBy: "p0dxD"}, "NOT_LIVE": {Value: "x"}}
	if err := h.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.Network().Enabled || h.Boundary().QuotaCPU.String() != "2" || h.Effective()[OrgNetworkPolicy] != "true" || changes != 1 {
		t.Fatalf("saved: %+v, %d changes", h.Boundary(), changes)
	}
	// The same values again: nothing changed.
	_ = h.Refresh(ctx)
	if changes != 1 {
		t.Fatalf("changes = %d", changes)
	}
	// A saved value that does not check out: the environment's hold.
	st.m = map[string]store.Ajuste{OrgNetworkNamespaces: {Value: "Mal Nombre"}}
	_ = h.Refresh(ctx)
	if h.Invalid() == "" || h.Network().Enabled || h.Effective()[OrgNetworkNamespaces] != "" || changes != 2 {
		t.Fatalf("invalid: %q %+v, %d changes", h.Invalid(), h.Boundary(), changes)
	}
	// The database cannot be read: the last values stay.
	st.m = map[string]store.Ajuste{OrgNetworkPolicy: {Value: "true"}}
	_ = h.Refresh(ctx)
	st.err = errors.New("down")
	if err := h.Refresh(ctx); err == nil || !h.Network().Enabled {
		t.Fatalf("database down: %v %+v", err, h.Boundary())
	}
	// Forgotten: the environment's value again.
	st.err, st.m = nil, map[string]store.Ajuste{}
	_ = h.Refresh(ctx)
	if h.Network().Enabled || len(h.SavedValues()) != 0 {
		t.Fatalf("forgotten: %+v", h.Boundary())
	}
}

func TestHolderRefusesABadEnvironment(t *testing.T) {
	h := &Holder{Store: &saved{}, Boot: Values{OrgNetworkPolicy: "si"}, PlatformNS: "rendimiento-system", IngressClass: "nginx"}
	if err := h.Start(); err == nil {
		t.Fatal("a bad environment value must stop the platform, as before")
	}
}
