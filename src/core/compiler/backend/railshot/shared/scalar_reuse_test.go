package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func scalarAssertFreeList(t *testing.T, s *ScalarState) {
	t.Helper()
	seen := make(map[scalarID]bool)
	for id := s.nodes[0].left; id != 0; id = s.node(id).left {
		if int(id) >= len(s.nodes) || seen[id] || s.node(id).refs != 0 {
			t.Fatalf("invalid free index %d", id)
		}
		seen[id] = true
	}
	for id, n := range s.nodes {
		if id != 0 && (n.refs == 0) != seen[scalarID(id)] {
			t.Fatalf("index=%d refs=%d listed=%v", id, n.refs, seen[scalarID(id)])
		}
	}
	for _, id := range s.owners {
		if id != 0 && (s.node(id).refs == 0 || seen[id]) {
			t.Fatal("dead register owner")
		}
	}
}

func TestScalarReuseSharedChildRelease(t *testing.T) {
	var state ScalarState
	state.add(scalarNode{})
	child := state.add(scalarNode{kind: ScalarConstant, refs: 2})
	parent := state.add(scalarNode{kind: scalarDeferred, left: child, right: child, refs: 1})
	state.release(parent)
	scalarAssertFreeList(t, &state)
	one := state.add(scalarNode{refs: 1})
	two := state.add(scalarNode{refs: 1})
	if one == two || one == 0 || two == 0 || len(state.nodes) != 3 {
		t.Fatal("released parent/child not reused uniquely")
	}
	scalarAssertFreeList(t, &state)
}

func TestScalarReusePreservesSpillAge(t *testing.T) {
	var state ScalarState
	state.target = &scalarTestTarget{}
	state.regs = []uint8{1, 2}
	state.add(scalarNode{})
	low := state.add(scalarNode{refs: 1})
	older := state.add(scalarNode{kind: ScalarRegister, reg: 1, refs: 1})
	state.release(low)
	newer := state.add(scalarNode{kind: ScalarRegister, reg: 2, refs: 1})
	state.owners[1] = older
	state.owners[2] = newer
	if newer >= older || state.node(newer).order <= state.node(older).order {
		t.Fatal("fixture did not reverse index and age")
	}
	if got := state.alloc(0); got != 1 || state.node(older).kind != ScalarFrame || state.node(newer).kind != ScalarRegister {
		t.Fatalf("evicted by recycled index instead of age: reg=%d", got)
	}
	scalarAssertFreeList(t, &state)
}

func TestScalarReuseBoundsLargeThenSmall(t *testing.T) {
	var body []byte
	for i := 0; i < 3000; i++ {
		body = append(body, 0x20, 0, 0x41, 1, 0x6a, 0x21, 0)
	}
	body = append(body, 0x20, 0, 0x0b)
	ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	widths := []bool{false}
	types := []wasm.ValType{wasm.I32}
	target := &scalarTestTarget{}
	var state ScalarState
	for repeat := 0; repeat < 3; repeat++ {
		summary := AdmitScalar(body, ft, types)
		if !summary.Eligible {
			t.Fatal("large fixture rejected")
		}
		if _, err := state.CompileScalar(body, summary, widths, 1, target); err != nil {
			t.Fatal(err)
		}
		if cap(state.nodes) > 64 || state.Memory() > 2304 || len(state.nodes) > 8 || state.nodes[0].order < 6000 {
			t.Fatalf("storage followed creation count: nodes=%d capacity=%d memory=%d created=%d", len(state.nodes), cap(state.nodes), state.Memory(), state.nodes[0].order)
		}
		scalarAssertFreeList(t, &state)
		small := []byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}
		summary = AdmitScalar(small, ft, types)
		if _, err := state.CompileScalar(small, summary, widths, 1, target); err != nil {
			t.Fatal(err)
		}
		if state.nodes[0].order != 3 || len(state.nodes) != 4 {
			t.Fatalf("allocator state crossed function boundary: created=%d nodes=%d", state.nodes[0].order, len(state.nodes))
		}
		scalarAssertFreeList(t, &state)
	}
	before, discarded := state.Memory(), state.Discarded
	state.FinishWorker()
	if state.Memory() != 0 || state.Discarded != discarded+before {
		t.Fatal("worker cleanup accounting changed")
	}
}
