package platform

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A release counts its services' requests times their replicas, and the
// database its needs run; the quota refuses what goes past it.
func TestOrgQuotaRequests(t *testing.T) {
	sp, err := spec.Parse([]byte(`
services:
  - name: web
    replicas: 2
    size: medium
  - name: api
    resources: { cpu: 300m, memory: 100Mi }
    needs: [postgres]
`))
	if err != nil {
		t.Fatal(err)
	}
	cpu, mem := specRequests(*sp)
	// web 2 × (100m, 256Mi); api 300m, 100Mi; postgres 100m, 256Mi.
	if cpu.MilliValue() != 600 || mem.Value() != (512+100+256)<<20 {
		t.Fatalf("requests = %s CPU, %s memory", cpu.String(), mem.String())
	}
	q := OrgQuota{CPU: resource.MustParse("500m")}
	if why := q.overQuota(cpu, mem); !strings.Contains(why, "CPU") {
		t.Errorf("600m over 500m: %q", why)
	}
	q = OrgQuota{CPU: resource.MustParse("1"), Memory: resource.MustParse("1Gi")}
	if why := q.overQuota(cpu, mem); why != "" {
		t.Errorf("within the quota: %q", why)
	}
	q.Memory = resource.MustParse("512Mi")
	if why := q.overQuota(cpu, mem); !strings.Contains(why, "memory") {
		t.Errorf("868Mi over 512Mi: %q", why)
	}
	if why := (OrgQuota{}).overQuota(cpu, mem); why != "" {
		t.Errorf("no quota, no limit: %q", why)
	}

	// The platform's organization, and no quota at all, are never counted
	// (no store is needed to say so).
	p := &Platform{Config: Config{OrgQuota: OrgQuota{CPU: resource.MustParse("1m")}}}
	if err := p.checkOrgQuota(context.Background(), &store.App{OrgID: store.DefaultOrg}, *sp); err != nil {
		t.Errorf("the platform's organization has no quota: %v", err)
	}
	p.Config.OrgQuota = OrgQuota{}
	if err := p.checkOrgQuota(context.Background(), &store.App{OrgID: 2}, *sp); err != nil {
		t.Errorf("no quota set: %v", err)
	}
}
