package environment

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// A node «a ratos» that is off is its normal state: the nodes check stays OK
// and says so, while a usual node that is down still warns (decision 196).
func TestNodoARatosApagado(t *testing.T) {
	s := &snapshot{
		cluster: ClusterInfo{Nodes: 2, NodesReady: 2},
		nodes: []Node{
			{Name: "worker1", Ready: true, Arch: "arm64"},
			{Name: "worker2", Ready: true, Arch: "arm64"},
			{Name: "desktop-node", Ready: false, Arch: "amd64", ARatos: true},
		},
	}
	ch := checkNodes(s)
	if ch.Status != OK || len(ch.Details) != 1 {
		t.Fatalf("an off node «a ratos»: %s %v", ch.Status, ch.Details)
	}
	s.nodes[1].Ready = false
	s.cluster.NodesReady = 1
	if ch := checkNodes(s); ch.Status != Warning {
		t.Fatalf("a usual node down: %s", ch.Status)
	}
}

func TestSinDormidos(t *testing.T) {
	pods := []corev1.Pod{{Spec: corev1.PodSpec{NodeName: "desktop-node"}}, {Spec: corev1.PodSpec{NodeName: "worker1"}}}
	nodes := []Node{{Name: "desktop-node", ARatos: true}, {Name: "worker1", Ready: true}}
	if got := sinDormidos(pods, nodes); len(got) != 1 || got[0].Spec.NodeName != "worker1" {
		t.Fatalf("%+v", got)
	}
	nodes[0].Ready = true
	if got := sinDormidos(pods, nodes); len(got) != 2 {
		t.Fatal("an «a ratos» node that is on keeps its pods' problems")
	}
}
