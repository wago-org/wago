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
	beforeCycles := c.tinyGC.cycles
	if _, err := c.NewStructDefaultWithRoots(0, tinyRootSliceSlots(roots)); err == nil || !strings.Contains(err.Error(), "bounded pacing") {
		t.Fatalf("near-exhaustion error = %v, want bounded pacing exhaustion", err)
	}
	if c.tinyGC.cycles > beforeCycles+1 {
		t.Fatalf("one allocation completed %d cycles, want at most one", c.tinyGC.cycles-beforeCycles)
	}
}

func tinyRootSliceSlots(roots []Root) Slots {
	slots := make(Slots, len(roots))
	for i := range roots {
		slots[i] = &roots[i]
	}
	return slots
}
