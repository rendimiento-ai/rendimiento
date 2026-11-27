package pipeline

import (
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// The Environment page lowers what a step asks for, never what it may use,
// and adds nodes that take no runs to BUILD_EXCLUDE_NODES.
func TestCapacityLimits(t *testing.T) {
	k := &KubeExecutor{ExcludeNodes: []string{"gpu-node"}, Limits: func() ([]string, int64) { return []string{"node-b", "gpu-node"}, 1500 }}
	res, _ := taskResources(spec.Resources{CPURequest: "2", CPULimit: "4"})
	k.capCPU(&res)
	if got := res.Requests.Cpu().MilliValue(); got != 1500 {
		t.Fatalf("request %dm, want 1500m", got)
	}
	if got := res.Limits.Cpu().MilliValue(); got != 4000 {
		t.Fatalf("limit %dm, want it left at 4000m", got)
	}
	small, _ := taskResources(spec.Resources{CPURequest: "500m"})
	k.capCPU(&small)
	if got := small.Requests.Cpu().MilliValue(); got != 500 {
		t.Fatalf("a small request went to %dm", got)
	}
	off := k.affinity().NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[1].Values
	if len(off) != 2 || off[0] != "gpu-node" || off[1] != "node-b" {
		t.Fatalf("nodes off %v", off)
	}
	if e := (&KubeExecutor{}).affinity().NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions; len(e) != 1 || e[0].Key != LabelConstruye {
		t.Fatalf("no nodes off: only the label rule, got %+v", e)
	}

	// Busy nodes are only avoided: the pod may still go there when nothing
	// else has room, instead of waiting on a node it does not fit.
	k.Busy = func() []string { return []string{"node-b", "worker1", "worker3"} }
	a := k.affinity().NodeAffinity
	if off := a.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[1].Values; len(off) != 2 {
		t.Fatalf("required off %v, want only the nodes turned off", off)
	}
	if len(a.PreferredDuringSchedulingIgnoredDuringExecution) != 1 {
		t.Fatalf("preferred %+v", a.PreferredDuringSchedulingIgnoredDuringExecution)
	}
	if avoid := a.PreferredDuringSchedulingIgnoredDuringExecution[0].Preference.MatchExpressions[0].Values; len(avoid) != 2 || avoid[0] != "worker1" || avoid[1] != "worker3" {
		t.Fatalf("avoided %v, want the busy ones not already off", avoid)
	}
	if got := k.avoided(); len(got) != 4 {
		t.Fatalf("a BuildKit daemon avoids %v", got)
	}
	only := (&KubeExecutor{Busy: func() []string { return []string{"worker1"} }}).affinity().NodeAffinity
	if len(only.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions) != 1 || len(only.PreferredDuringSchedulingIgnoredDuringExecution) != 1 {
		t.Fatalf("busy only: %+v", only)
	}
}
