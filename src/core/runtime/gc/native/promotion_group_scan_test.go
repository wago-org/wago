//go:build wago_gcplanscancheck

package gc

import (
	"fmt"
	"testing"
)

func TestPromotionEqualSizeGroupScanIsLinear(t *testing.T) {
	const objects = 64
	for _, immediate := range []bool{false, true} {
		for _, groups := range []int{1, 2} {
			name := "survivor"
			cfg := Config{NurseryBytes: 1 << 20, SurvivorBytes: 1 << 20,
				ThroughputHeapBytes: 1 << 20, ThroughputPageBytes: 4096}
			if immediate {
				name = "immediate"
				cfg.SurvivorBytes = 0
				cfg.DisableMovingNursery = true
			}
			t.Run(fmt.Sprintf("%s/groups-%d", name, groups), func(t *testing.T) {
				c := newTestCollector(t, cfg)
				newObject := func(i int) Ref {
					ref, err := c.NewArray(2, uint32(3+16*(i%groups)), I32Value(int32(i)))
					if err != nil {
						t.Fatal(err)
					}
					return ref
				}
				// Keep every other old object live, leaving isolated slots
				// for each size group through normal collector operations.
				oldRoots := make([]Root, objects)
				for i := 0; i < objects*2; i++ {
					ref := newObject(i / 2)
					if err := c.ForcePromote(ref); err != nil {
						t.Fatal(err)
					}
					if i%2 != 0 {
						oldRoots[i/2] = Root(ref)
					}
				}
				if err := c.CollectFull(stressRootSlots(oldRoots)); err != nil {
					t.Fatal(err)
				}
				roots := make([]Root, objects)
				for i := range roots {
					roots[i] = Root(newObject(i))
				}
				allRoots := append(oldRoots, roots...)
				slots := stressRootSlots(allRoots)
				if err := c.Verify(slots); err != nil {
					t.Fatal(err)
				}
				c.tenuringThreshold = 1
				resetPromotionGroupScanCount()
				if err := c.CollectMinor(slots); err != nil {
					t.Fatal(err)
				}
				if got := currentPromotionGroupScanCount(); got != uint64(objects-groups) {
					t.Fatalf("equal-size group scan comparisons = %d, want %d", got, objects-groups)
				}
				t.Logf("equal-size group scan comparisons = %d", currentPromotionGroupScanCount())
				for i, root := range roots {
					ref := Ref(root)
					if got := c.entry(ref).space; got != spaceOld {
						t.Fatalf("root %d space = %d, want old", i, got)
					}
					length, err := c.ArrayLen(ref)
					if err != nil || length != uint32(3+16*(i%groups)) {
						t.Fatalf("root %d length = %d, err %v", i, length, err)
					}
					value, err := c.ArrayGet(ref, length-1)
					if err != nil || value.I32() != int32(i) {
						t.Fatalf("root %d last value = %d, err %v", i, value.I32(), err)
					}
				}
				if err := c.Verify(slots); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
