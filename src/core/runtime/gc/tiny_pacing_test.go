package gc

import (
	"strings"
	"testing"
)

func TestTinyPacingConfigBounds(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, TinyPacingStepLimit: tinyNearExhaustionStepLimit + 1}, []TypeDesc{leaf}); err == nil {
		t.Fatal("oversized Tiny pacing limit was accepted")
	}
}

func TestTinyAllocationDebtStartsIncrementalWork(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	roots := make([]Root, 0, 80)
	for i := 0; i < 64; i++ {
		object, err := c.NewStructDefaultWithRoots(0, tinyRootSliceSlots(roots))
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, Root(object))
	}
	if c.tinyGC.state != tinyIdle {
		t.Fatalf("debt started collection before one quantum: state=%d", c.tinyGC.state)
	}
	object, err := c.NewStructDefaultWithRoots(0, tinyRootSliceSlots(roots))
	if err != nil {
		t.Fatal(err)
	}
	roots = append(roots, Root(object))
	if c.tinyGC.state == tinyIdle {
		t.Fatal("allocation debt did not purchase an incremental Step")
	}
}

func TestTinyAllocationDebtCountsCompletedCycle(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	for c.tinyGC.state != tinySweep {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyGC.sweep != c.tinyGC.sweepLimit {
		t.Fatalf("empty Tiny sweep cursor/limit = %d/%d, want completion pending", c.tinyGC.sweep, c.tinyGC.sweepLimit)
	}
	c.tinyGC.allocationDebt = tinyAllocationDebtBytes
	before := c.stats.FullCollections
	if err := c.tinyPayAllocationDebt(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.state != tinyIdle || c.stats.FullCollections != before+1 {
		t.Fatalf("ordinary debt completion state/collections = %d/%d, want idle/%d", c.tinyGC.state, c.stats.FullCollections, before+1)
	}
}

func TestTinySweepEndpointIgnoresNewHandleTail(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, []TypeDesc{leaf})
	for i := uint32(0); i < tinyStepSweepHandles; i++ {
		if _, err := c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
	}
	for c.tinyGC.state != tinySweep {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	c.tinyGC.allocationDebt = 0

	const maxAllocations = 2*tinyAllocationDebtBytes/16 + 2
	for i := uint32(0); i < maxAllocations; i++ {
		if _, err := c.NewStructDefaultWithRoots(0, EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		if c.tinyGC.state == tinyIdle {
			return
		}
	}
	t.Fatalf("Tiny sweep chased an allocation-grown handle tail: cursor=%d handles=%d", c.tinyGC.sweep, len(c.handles))
}

func TestTinyNearExhaustionAssistIsBounded(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 16 * 128, TinyBlockBytes: 16}, []TypeDesc{leaf})
	roots := make([]Root, 128)
	for i := range roots {
		object, err := c.NewStructDefaultWithRoots(0, tinyRootSliceSlots(roots[:i]))
		if err != nil {
			t.Fatal(err)
		}
		roots[i] = Root(object)
	}
	beforeCollections := c.stats.FullCollections
	if _, err := c.NewStructDefaultWithRoots(0, tinyRootSliceSlots(roots)); err == nil || !strings.Contains(err.Error(), "bounded pacing") {
		t.Fatalf("near-exhaustion error = %v, want bounded pacing exhaustion", err)
	}
	if c.stats.FullCollections > beforeCollections+1 {
		t.Fatalf("one allocation completed %d cycles, want at most one", c.stats.FullCollections-beforeCollections)
	}
}

func tinyRootSliceSlots(roots []Root) Slots {
	slots := make(Slots, len(roots))
	for i := range roots {
		slots[i] = &roots[i]
	}
	return slots
}
