package platform

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// OrgQuota is the most CPU and memory the apps of one organization other
// than the platform's may ask for together (their requests, as the
// scheduler counts them); a zero value has no limit. Kubernetes has no
// quota for a group of namespaces, so the platform counts at release time:
// a release that would take its organization past the quota is not made.
type OrgQuota struct {
	CPU, Memory resource.Quantity
}

// specRequests is what a release asks the cluster for while it runs: the
// requests of each of its services and of the database and cache its needs
// run, times their replicas. Cron jobs and deploy tasks come and go and are
// not counted.
func specRequests(sp spec.Spec) (cpu, mem resource.Quantity) {
	cpu, mem = resource.Quantity{Format: resource.DecimalSI}, resource.Quantity{Format: resource.BinarySI}
	add := func(to *resource.Quantity, v string, n int) {
		if q, err := resource.ParseQuantity(v); err == nil {
			for range n {
				to.Add(q)
			}
		}
	}
	for _, svc := range append(append([]spec.Service{}, sp.Services...), render.NeedServices(sp)...) {
		if svc.Replicas <= 0 {
			continue
		}
		r := spec.ResourcesFor(svc.Size, svc.Resources)
		add(&cpu, r.CPURequest, svc.Replicas)
		add(&mem, r.MemRequest, svc.Replicas)
	}
	return cpu, mem
}

// overQuota says why requests of cpu and mem exceed the quota ("" when
// they do not).
func (q OrgQuota) overQuota(cpu, mem resource.Quantity) string {
	switch {
	case !q.CPU.IsZero() && cpu.Cmp(q.CPU) > 0:
		return M("its organization's apps would ask for %s of CPU, more than the organization's quota of %s", cpu.String(), q.CPU.String())
	case !q.Memory.IsZero() && mem.Cmp(q.Memory) > 0:
		return M("its organization's apps would ask for %s of memory, more than the organization's quota of %s", mem.String(), q.Memory.String())
	}
	return ""
}

// checkOrgQuota refuses a release of an app of an organization other than
// the platform's when, with the latest release of each of the
// organization's other apps, it would ask for more than Config.OrgQuota.
func (p *Platform) checkOrgQuota(ctx context.Context, app *store.App, sp spec.Spec) error {
	q := p.Config.OrgQuota
	if (q.CPU.IsZero() && q.Memory.IsZero()) || app.OrgID == 0 || app.OrgID == store.DefaultOrg || !sp.Runs() {
		return nil
	}
	cpu, mem := specRequests(sp)
	apps, err := p.Store.ListAppsInOrg(ctx, app.OrgID)
	if err != nil {
		return err
	}
	for _, a := range apps {
		if a.ID == app.ID {
			continue
		}
		rel, err := p.Store.LatestRelease(ctx, a.ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		c, m := specRequests(rel.Spec)
		cpu.Add(c)
		mem.Add(m)
	}
	if why := q.overQuota(cpu, mem); why != "" {
		return errors.New(why)
	}
	return nil
}
