package main

import (
	"fmt"
	"net"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/rendimiento-ai/rendimiento/internal/platform"
	"github.com/rendimiento-ai/rendimiento/internal/render"
)

// ingressNamespaces are where the usual ingress controllers run, by
// ingress class, when INGRESS_NAMESPACE does not say.
var ingressNamespaces = map[string]string{"nginx": "ingress-nginx", "traefik": "kube-system"}

// orgNetwork reads the network boundary between organizations: off unless
// ORG_NETWORK_POLICY is true. On, each app namespace lets in its
// organization's namespaces, the platform's own namespace (its uptime and
// release checks), the ingress controller's (INGRESS_NAMESPACE, or the
// usual one for the ingress class), ORG_NETWORK_POLICY_NAMESPACES and the
// address ranges in ORG_NETWORK_POLICY_CIDRS.
func orgNetwork(platformNS, ingressClass string) (render.OrgNetwork, error) {
	var o render.OrgNetwork
	switch v := env("ORG_NETWORK_POLICY", "false"); v {
	case "true":
		o.Enabled = true
	case "false":
	default:
		return o, fmt.Errorf("ORG_NETWORK_POLICY must be true or false, got %q", v)
	}
	ingressNS := env("INGRESS_NAMESPACE", ingressNamespaces[ingressClass])
	if o.Enabled && ingressNS == "" {
		return o, fmt.Errorf("ORG_NETWORK_POLICY needs INGRESS_NAMESPACE, the namespace the ingress controller of class %q runs in", ingressClass)
	}
	o.Namespaces = append([]string{platformNS, ingressNS}, splitList(env("ORG_NETWORK_POLICY_NAMESPACES", ""))...)
	o.CIDRs = splitList(env("ORG_NETWORK_POLICY_CIDRS", ""))
	for _, c := range o.CIDRs {
		if _, _, err := net.ParseCIDR(c); err != nil {
			return o, fmt.Errorf("ORG_NETWORK_POLICY_CIDRS must be address ranges such as 192.168.1.0/24, got %q", c)
		}
	}
	return o, nil
}

// orgQuota reads the most CPU and memory an organization other than the
// platform's may ask for across its apps (ORG_QUOTA_CPU, ORG_QUOTA_MEMORY;
// unset: no limit).
func orgQuota() (platform.OrgQuota, error) {
	var q platform.OrgQuota
	for _, s := range []struct {
		key string
		to  *resource.Quantity
	}{{"ORG_QUOTA_CPU", &q.CPU}, {"ORG_QUOTA_MEMORY", &q.Memory}} {
		v := env(s.key, "")
		if v == "" {
			continue
		}
		n, err := resource.ParseQuantity(v)
		if err != nil || n.Sign() <= 0 {
			return q, fmt.Errorf("%s must be a quantity such as 4 or 8Gi, got %q", s.key, v)
		}
		*s.to = n
	}
	return q, nil
}
