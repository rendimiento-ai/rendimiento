package render

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// LabelOrg is the address (slug) of the organization an app belongs
	// to: on its App object, written by the platform, and on the namespace
	// rendimiento makes for it. An App without it is the platform's.
	LabelOrg = "rendimiento.ai/org"
	// LabelPreview marks the namespace of a pull request's copy (its
	// number), so a sweep finds and deletes the copies whose pull request
	// is closed or gone.
	LabelPreview = "rendimiento.ai/preview"
	// PlatformOrg is the address of the platform's own organization (the
	// one store's migration 0018 makes, store.DefaultOrg).
	PlatformOrg = "principal"
	// NetworkPolicyName is the policy that keeps an app's namespace to its
	// organization (Options.OrgNetwork).
	NetworkPolicyName = "rendimiento-org"
	// namespaceNameLabel is the label Kubernetes gives every namespace with
	// its own name.
	namespaceNameLabel = "kubernetes.io/metadata.name"
)

// OrgNetwork is the network boundary between organizations. With it on,
// each app namespace rendimiento owns gets a NetworkPolicy that lets in
// only its own pods, the namespaces of its organization, the namespaces and
// address ranges listed here, and anyone on the ports of its `lan:`
// services. Egress stays open. It only holds where the cluster's network
// plugin enforces NetworkPolicy.
type OrgNetwork struct {
	Enabled bool
	// Namespaces are always let in: the ingress controller's, the
	// platform's own (its uptime and release checks), and any the operator
	// adds (monitoring, say).
	Namespaces []string
	// CIDRs are address ranges always let in, e.g. the nodes' network
	// when the ingress controller runs on the host network.
	CIDRs []string
}

// lanPeer is a `lan:` service's pods and port: the LAN reaches them through
// the load balancer, from addresses no namespace selector matches.
type lanPeer struct {
	name     string
	selector map[string]string
	port     int
}

// networkPolicies are the policies of an app's namespace: the organization's
// boundary, and one per `lan:` service opening its port to everyone.
func networkPolicies(ns, app, org string, lan []lanPeer, opt OrgNetwork) []*networkingv1.NetworkPolicy {
	if !opt.Enabled || org == "" {
		return nil
	}
	ingressOnly := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	from := []networkingv1.NetworkPolicyPeer{
		{PodSelector: &metav1.LabelSelector{}}, // its own namespace, even without its label
		{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{LabelOrg: org}}},
	}
	names := append([]string{}, opt.Namespaces...)
	sort.Strings(names)
	for i, n := range names {
		if n == "" || (i > 0 && names[i-1] == n) {
			continue
		}
		from = append(from, networkingv1.NetworkPolicyPeer{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{namespaceNameLabel: n}}})
	}
	for _, c := range opt.CIDRs {
		from = append(from, networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: c}})
	}
	meta := func(name string) metav1.ObjectMeta {
		l := appLabels(app)
		l[LabelOrg] = org
		return metav1.ObjectMeta{Name: name, Namespace: ns, Labels: l}
	}
	out := []*networkingv1.NetworkPolicy{{
		TypeMeta:   metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: meta(NetworkPolicyName),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: ingressOnly,
			Ingress:     []networkingv1.NetworkPolicyIngressRule{{From: from}},
		},
	}}
	tcp := corev1.ProtocolTCP
	for _, l := range lan {
		port := intstr.FromInt32(int32(l.port))
		out = append(out, &networkingv1.NetworkPolicy{
			TypeMeta:   metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
			ObjectMeta: meta(l.name),
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: l.selector},
				PolicyTypes: ingressOnly,
				// No from: anyone, on the service's own port only.
				Ingress: []networkingv1.NetworkPolicyIngressRule{{Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &port}}}},
			},
		})
	}
	return out
}
