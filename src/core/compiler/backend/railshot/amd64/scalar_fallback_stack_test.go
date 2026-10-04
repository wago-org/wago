//go:build amd64

package amd64

import "testing"

func TestSharedFallbackStackInitializedInPlace(t *testing.T) {
	var initial stack
	sc := scratch{stack: &initial, stackCap: defaultStackArenaCap}
	f := fn{s: sc.stack}
	allocs := testing.AllocsPerRun(10, func() {
		initial = stack{}
		sc.ensureTargetStack()
	})
	if allocs > 2 {
		t.Fatalf("first fallback stack initialization allocates %g times, budget 2", allocs)
	}
	if f.s != sc.stack || f.s != &initial || f.s.head == nil {
		t.Fatal("function lost initialized stack alias")
	}
	if f.s.head.prev != f.s.head || f.s.head.next != f.s.head {
		t.Fatal("invalid sentinel links")
	}
	_, reserved := f.s.nodeMemory()
	if reserved == 0 || sc.nodeScratchReserved != reserved || sc.nodeScratchPeak != reserved {
		t.Fatal("missing initial storage accounting")
	}
	head := f.s.head
	if allocs := testing.AllocsPerRun(10, func() { sc.ensureTargetStack() }); allocs != 0 {
		t.Fatalf("reuse allocated %g times", allocs)
	}
	if f.s.head != head {
		t.Fatal("reuse replaced live sentinel")
	}
}
