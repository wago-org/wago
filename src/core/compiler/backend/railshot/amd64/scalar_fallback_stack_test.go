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
	if got := cap(f.s.chunks[0]); got != compileStackFirstChunkCap {
		t.Fatalf("first fallback chunk cap = %d, want %d", got, compileStackFirstChunkCap)
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

func TestSmallerFallbackChunkPreservesRecycleThreshold(t *testing.T) {
	for _, legacyCap := range []int{minStackArenaCap, 64, 65, 127, 128, 255, defaultStackArenaCap} {
		main := newStackWithCap(legacyCap)
		var candidate stack
		candidate.initWithFirstChunk(legacyCap, compileStackFirstChunkCap)
		for range 2 {
			// Compare with the old driver predicate after every allocation,
			// including early physical growth and reuse across functions.
			for used := 1; used <= defaultStackArenaCap+2; used++ {
				want := main.cur != 0 || len(main.chunks[0]) >= defaultStackArenaCap
				if got := candidate.recycleThresholdReached(); got != want {
					t.Fatalf("legacy cap=%d used=%d chunk=%d: recycle=%v, want %v", legacyCap, used, candidate.cur, got, want)
				}
				main.alloc()
				candidate.alloc()
			}
			main.reset()
			candidate.reset()
		}
	}
}
