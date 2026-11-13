package spec

import (
	"fmt"
	"math"
	"strings"
)

// Previews asks for a live copy of the app for each of its pull requests,
// at pr-<number>.<the service's domain>, deleted when the pull request is
// closed or merged (decision 136, step 12). Off unless the block is there.
type Previews struct {
	// Data is what the copy's database starts with: "empty" (the default),
	// or "seed" (empty, then the Seed task loads sample data). A copy of the
	// real database is not offered: nothing real leaves production.
	Data string `json:"data,omitempty"`
	// Seed is the task that loads sample data, with data: seed.
	Seed string `json:"seed,omitempty"`
	// Keep is how many days a copy lives without a push (default 7, up to 30).
	Keep int `json:"keep,omitempty"`
	// Max is how many copies the app keeps at once (default 3, up to 5): a
	// new one replaces the one idle longest.
	Max int `json:"max,omitempty"`
}

// The data a preview starts with.
const (
	PreviewEmpty = "empty"
	PreviewSeed  = "seed"
)

// Preview defaults and bounds.
const (
	PreviewKeepDays = 7
	PreviewMaxKeep  = 30
	PreviewCopies   = 3
	PreviewMaxCopy  = 5
)

// KeepDays is how many days a copy lives without a push.
func (p *Previews) KeepDays() int {
	if p == nil || p.Keep == 0 {
		return PreviewKeepDays
	}
	return p.Keep
}

// Copies is how many copies the app keeps at once.
func (p *Previews) Copies() int {
	if p == nil || p.Max == 0 {
		return PreviewCopies
	}
	return p.Max
}

func (p *Previews) validate(s *Spec) []error {
	if p == nil {
		return nil
	}
	var errs []error
	switch p.Data {
	case "", PreviewEmpty:
		if p.Seed != "" {
			errs = append(errs, fmt.Errorf("previews.seed is only used with data: seed"))
		}
	case PreviewSeed:
		found := false
		for _, t := range s.Tasks {
			found = found || t.Name == p.Seed
		}
		if p.Seed == "" || !found {
			errs = append(errs, fmt.Errorf("previews.seed %q must name one of the app's tasks, which loads sample data", p.Seed))
		}
	case "copy":
		errs = append(errs, fmt.Errorf("previews.data copy is not offered: a preview never gets production's data; use empty or seed"))
	default:
		errs = append(errs, fmt.Errorf("previews.data %q must be empty or seed", p.Data))
	}
	if p.Keep != 0 && (p.Keep < 1 || p.Keep > PreviewMaxKeep) {
		errs = append(errs, fmt.Errorf("previews.keep must be 1 to %d days", PreviewMaxKeep))
	}
	if p.Max != 0 && (p.Max < 1 || p.Max > PreviewMaxCopy) {
		errs = append(errs, fmt.Errorf("previews.max must be 1 to %d copies", PreviewMaxCopy))
	}
	hosts := 0
	for _, svc := range s.Services {
		hosts += len(svc.Hosts())
	}
	if hosts == 0 {
		errs = append(errs, fmt.Errorf("previews need a service with a domain: the copy answers at pr-<number>.<domain>"))
	}
	return errs
}

// PreviewHost is a host of the copy for pull request number (a route's
// path, after the host, is kept). A domain's own host gets the prefix
// (tienda.com → pr-7.tienda.com); a subdomain gets the number in its first
// label (tienda.example.com → tienda-pr-7.example.com), so the copy stays
// one level under the zone: a proxy's wildcard certificate (Cloudflare's
// *.example.com) covers it, where pr-7.tienda.example.com would not.
func PreviewHost(number int, host string) string {
	name, path, cut := strings.Cut(host, "/")
	if cut {
		path = "/" + path
	}
	first, rest, ok := strings.Cut(name, ".")
	if !ok || !strings.Contains(rest, ".") {
		return fmt.Sprintf("pr-%d.%s%s", number, name, path)
	}
	return fmt.Sprintf("%s-pr-%d.%s%s", first, number, rest, path)
}

// PreviewOf is the spec of the copy for pull request number: every service
// once, at pr-<number>.<its domain>, asking for a quarter of the CPU and
// memory, with no scheduled jobs, no LAN ports, no GPU, no volume of
// production's, and no verification or rollback (a copy is never a
// release). It leaves s alone.
func PreviewOf(s Spec, number int) Spec {
	out := s
	out.Jobs, out.SharedNamespace, out.Previews = nil, false, nil
	out.Verify = &Verify{Disabled: true}
	out.Services = make([]Service, len(s.Services))
	for i, svc := range s.Services {
		svc.Replicas = 1
		if svc.Domain != "" {
			svc.Domain = PreviewHost(number, svc.Domain)
		}
		svc.Aliases = nil
		routes := make([]string, 0, len(svc.Routes))
		for _, r := range svc.Routes {
			routes = append(routes, PreviewHost(number, r))
		}
		svc.Routes = routes
		if svc.Ingress != nil {
			in := *svc.Ingress
			in.Name, in.TLSSecrets = "", nil
			svc.Ingress = &in
		}
		svc.TLSSecret, svc.LAN, svc.GPU = "", nil, 0
		if svc.Volume != nil {
			v := *svc.Volume
			v.ExistingClaim = ""
			if v.Size == "" {
				v.Size = "1Gi"
			}
			svc.Volume = &v
		}
		svc.Resources = quarter(ResourcesFor(svc.Size, svc.Resources), svc.Resources)
		out.Services[i] = svc
	}
	return out
}

// quarter asks for a quarter of a service's requests, keeping its limits.
func quarter(r Resources, o *ResourceOverride) *ResourceOverride {
	q := ResourceOverride{}
	if o != nil {
		q = *o
	}
	if v, ok := quantityValue(r.CPURequest); ok {
		q.CPU = fmt.Sprintf("%dm", max(10, int(math.Ceil(v*1000/4))))
	}
	if v, ok := quantityValue(r.MemRequest); ok {
		q.Memory = fmt.Sprintf("%dMi", max(16, int(math.Ceil(v/4/(1<<20)))))
	}
	if strings.TrimSpace(q.CPU+q.Memory+q.CPULimit+q.MemoryLimit) == "" {
		return nil
	}
	return &q
}
