package platform

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// What the Environment page sets wins over the environment's values, up to
// MaxWorkers; a value it leaves at zero keeps the environment's.
func TestLimitsFollowTheEnvironmentPage(t *testing.T) {
	h := &CapacityHolder{}
	p := &Platform{Workers: 3, PerApp: 2, Capacity: h.Runs}
	if l := p.limits(); l.workers != 3 || l.perApp != 2 {
		t.Fatalf("unset: %+v", l)
	}
	h.Set(store.Capacity{ParallelRuns: 20, RunsPerApp: 1})
	if l := p.limits(); l.workers != MaxWorkers || l.perApp != 1 {
		t.Fatalf("set: %+v", l)
	}
	h.Set(store.Capacity{RunsPerApp: 3})
	if l := p.limits(); l.workers != 3 || l.perApp != 3 {
		t.Fatalf("only per app: %+v", l)
	}
	if nodes, cpu := h.Limits(); len(nodes) != 0 || cpu != 0 {
		t.Fatalf("limits %v %d", nodes, cpu)
	}
}

// A node reserved or used at the limit or more is busy; when every node is,
// the least full one still takes work; at 100 nothing is ever busy.
func TestBusyNodes(t *testing.T) {
	loads := []NodeLoad{{Name: "colorpi", Reserved: 96, Used: 60}, {Name: "worker1", Reserved: 50, Used: 90}, {Name: "worker2", Reserved: 48, Used: 15}, {Name: "worker3", Reserved: 50, Used: -1}}
	if got := BusyNodes(loads, 0); len(got) != 2 || got[0] != "colorpi" || got[1] != "worker1" {
		t.Fatalf("busy at 85: %v", got)
	}
	if got := BusyNodes(loads, 95); len(got) != 1 || got[0] != "colorpi" {
		t.Fatalf("busy at 95: %v", got)
	}
	if got := BusyNodes(loads, 100); len(got) != 0 {
		t.Fatalf("busy at 100: %v", got)
	}
	all := []NodeLoad{{Name: "a", Reserved: 90}, {Name: "b", Reserved: 99}, {Name: "c", Used: 87}}
	if got := BusyNodes(all, 85); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("all busy: %v, want c left free", got)
	}
	// Memory counts as much as CPU: idle cores with the memory taken (a node
	// with a language model loaded) is busy too, reserved or in use.
	mem := []NodeLoad{{Name: "gpu", Reserved: 30, Used: 5, MemReserved: 97, MemUsed: 80}, {Name: "pi", Reserved: 40, Used: 20, MemReserved: 50, MemUsed: 88}, {Name: "libre", Reserved: 40, Used: 20, MemReserved: 50, MemUsed: -1}}
	if got := BusyNodes(mem, 85); len(got) != 2 || got[0] != "gpu" || got[1] != "pi" {
		t.Fatalf("busy by memory: %v", got)
	}
	h := &CapacityHolder{}
	h.Set(store.Capacity{NodesOff: []string{"colorpi"}})
	busy := []string{"colorpi", "worker1"}
	h.busy.Store(&busy)
	if off, _ := h.Limits(); len(off) != 1 || off[0] != "colorpi" {
		t.Fatalf("limits %v: only the node turned off, the busy ones are only avoided", off)
	}
	if b := h.Busy(); len(b) != 2 {
		t.Fatalf("busy %v", b)
	}
}

type marcas struct{ vistas []string }

func (m *marcas) MarcarEncendido(_ context.Context, nodo string, on bool) error {
	m.vistas = append(m.vistas, fmt.Sprintf("%s=%v", nodo, on))
	return nil
}

// A node «a ratos» is written when it comes on and goes off, not at every
// measure; the drawer's switches come out by its name.
func TestEncendidosARatos(t *testing.T) {
	nodo := func(ready bool) corev1.Node {
		st := corev1.ConditionFalse
		if ready {
			st = corev1.ConditionTrue
		}
		return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "desktop-node", Labels: map[string]string{pipeline.TaintARatos: "true"}},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: st}}}}
	}
	otro := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "nodo-b"}}
	m := &marcas{}
	h := &CapacityHolder{Encendidos: m}
	ctx := context.Background()
	for _, ready := range []bool{true, true, false, false, true} {
		h.marcar(ctx, []corev1.Node{otro, nodo(ready)})
	}
	if got := strings.Join(m.vistas, " "); got != "desktop-node=true desktop-node=false desktop-node=true" {
		t.Fatalf("written: %s", got)
	}
	h.Set(store.Capacity{ARatos: map[string]store.ARatosNodo{"desktop-node": {SinPruebas: true, Esperar: true}}})
	if a := h.ARatos("desktop-node"); !a.SinPruebas || !a.Esperar || a.SinOtraMitad || h.ARatos("nodo-b") != (pipeline.AjustesARatos{}) {
		t.Fatalf("switches: %+v", a)
	}
}
