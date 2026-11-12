package render

import (
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// The boundary between organizations: the namespace carries its
// organization, and with OrgNetwork on its policy lets in only its own pods,
// its organization's namespaces and the ones the operator lists; a `lan:`
// service's port is open to everyone. Off, nothing is rendered.
func TestRenderOrgNetwork(t *testing.T) {
	s, err := spec.Parse([]byte("services: [{name: web, port: 3000}, {name: docs, port: 8080, lan: {port: 8443}}]"))
	if err != nil {
		t.Fatal(err)
	}
	in := Input{App: "acme-site", Org: "acme", Spec: *s, Images: map[string]string{"web": "w", "docs": "d"}}
	off, err := Render(in, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if off.Namespace.Labels[LabelOrg] != "acme" {
		t.Errorf("namespace labels = %v", off.Namespace.Labels)
	}
	if len(off.NetworkPolicies) != 0 {
		t.Fatalf("off, no policy: %v", off.NetworkPolicies)
	}

	opt := DefaultOptions()
	opt.OrgNetwork = OrgNetwork{Enabled: true, Namespaces: []string{"rendimiento-system", "ingress-nginx", "ingress-nginx"}, CIDRs: []string{"192.168.1.0/24"}}
	objs, err := Render(in, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs.NetworkPolicies) != 2 {
		t.Fatalf("want the boundary and the LAN port, got %d", len(objs.NetworkPolicies))
	}
	for _, o := range objs.List() {
		if o.GetName() == NetworkPolicyName {
			t.Fatal("policies are not in List: the controller applies them only to namespaces it owns")
		}
	}
	p := objs.NetworkPolicies[0]
	if p.Name != NetworkPolicyName || p.Namespace != "acme-site" || p.Labels[LabelApp] != "acme-site" || p.Labels[LabelManagedBy] != ManagedBy {
		t.Fatalf("policy meta = %+v", p.ObjectMeta)
	}
	if len(p.Spec.PodSelector.MatchLabels) != 0 || len(p.Spec.PolicyTypes) != 1 || p.Spec.PolicyTypes[0] != "Ingress" || len(p.Spec.Egress) != 0 {
		t.Fatalf("every pod, ingress only (egress stays open): %+v", p.Spec)
	}
	from := p.Spec.Ingress[0].From
	if len(from) != 5 {
		t.Fatalf("own pods, organization, two namespaces (once each), one range: %+v", from)
	}
	if from[0].PodSelector == nil || from[0].NamespaceSelector != nil {
		t.Errorf("first: its own namespace: %+v", from[0])
	}
	if from[1].NamespaceSelector.MatchLabels[LabelOrg] != "acme" {
		t.Errorf("second: its organization: %+v", from[1])
	}
	if from[2].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "ingress-nginx" ||
		from[3].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "rendimiento-system" {
		t.Errorf("listed namespaces, by name: %+v %+v", from[2], from[3])
	}
	if from[4].IPBlock == nil || from[4].IPBlock.CIDR != "192.168.1.0/24" {
		t.Errorf("listed range: %+v", from[4])
	}
	lan := objs.NetworkPolicies[1]
	r := lan.Spec.Ingress[0]
	if lan.Name != "docs-lan" || lan.Spec.PodSelector.MatchLabels[LabelService] != "docs" || len(r.From) != 0 ||
		len(r.Ports) != 1 || r.Ports[0].Port.IntValue() != 8080 {
		t.Errorf("LAN policy: the service's pods, from anyone, on its own port: %+v", lan)
	}

	// No organization: no label and no policy.
	in.Org = ""
	none, err := Render(in, opt)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := none.Namespace.Labels[LabelOrg]; ok || len(none.NetworkPolicies) != 0 {
		t.Errorf("without an organization: labels %v, %d policies", none.Namespace.Labels, len(none.NetworkPolicies))
	}
}
