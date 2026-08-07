//go:build wagodebug

package gc

import (
	"errors"
	"reflect"
	"testing"
)

func TestInjectedPromotionFailuresAreTransactional(t *testing.T) {
	points := []failurePoint{failPromotionPlan, failPromotionDestination, failPromotionCommit}
	for _, point := range points {
		for after := 0; after < 4; after++ {
			c := newTestCollector(t, Config{NurseryBytes: 4096, ThroughputHeapBytes: 8192, ThroughputPageBytes: 4096})
			roots := make([]Root, 4)
			for i := range roots {
				r, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				roots[i] = Root(r)
			}
			before := snapshotPromotionState(c)
			cleanup := armFailure(point, after)
			err := c.CollectMinor(stressRootSlots(roots))
			cleanup()
			if !errors.Is(err, errInjectedFailure) {
				t.Fatalf("point %d after %d: error = %v", point, after, err)
			}
			assertPromotionStateEqual(t, c, before)
			c.Close()
		}
	}
}

func TestInjectedPublicationAndBackingFailuresAreTransactional(t *testing.T) {
	t.Run("nursery publication", func(t *testing.T) {
		c := newTestCollector(t, Config{})
		before := snapshotPromotionState(c)
		defer armFailure(failHandlePublication, 0)()
		if _, err := c.NewStructDefault(0); !errors.Is(err, errInjectedFailure) {
			t.Fatalf("error = %v", err)
		}
		assertPromotionStateEqual(t, c, before)
	})
	t.Run("large publication", func(t *testing.T) {
		c := newTestCollector(t, Config{LargeObjectBytes: 16})
		before := snapshotPromotionState(c)
		defer armFailure(failHandlePublication, 0)()
		if _, err := c.NewStructDefault(0); !errors.Is(err, errInjectedFailure) {
			t.Fatalf("error = %v", err)
		}
		assertPromotionStateEqual(t, c, before)
	})
	t.Run("backing growth", func(t *testing.T) {
		c := newTestCollector(t, Config{LargeObjectBytes: 16})
		c.throughput.largestFree = 256
		c.throughput.largestFreeDirty = true
		before := snapshotPromotionState(c)
		defer armFailure(failBackingGrowth, 0)()
		if _, err := c.NewStructDefault(0); !errors.Is(err, errInjectedFailure) {
			t.Fatalf("error = %v", err)
		}
		assertPromotionStateEqual(t, c, before)
	})
	t.Run("collection-disabled backing growth", func(t *testing.T) {
		c := newTestCollector(t, Config{DisableCollection: true})
		c.throughput.largestFree = 256
		c.throughput.largestFreeDirty = true
		before := snapshotPromotionState(c)
		defer armFailure(failBackingGrowth, 0)()
		if _, err := c.NewStructDefault(0); !errors.Is(err, errInjectedFailure) {
			t.Fatalf("error = %v", err)
		}
		assertPromotionStateEqual(t, c, before)
	})
	t.Run("tiny publication", func(t *testing.T) {
		c := newTinyTestCollector(t, Config{})
		beforeHandles, beforeBlocks := append([]handleEntry(nil), c.handles...), append([]tinyBlock(nil), c.tiny.blocks...)
		defer armFailure(failHandlePublication, 0)()
		if _, err := c.NewStructDefault(0); !errors.Is(err, errInjectedFailure) {
			t.Fatalf("error = %v", err)
		}
		if !reflect.DeepEqual(c.handles, beforeHandles) || !reflect.DeepEqual(c.tiny.blocks, beforeBlocks) {
			t.Fatal("tiny publication failure mutated allocator or handles")
		}
	})
}

func TestStressBuildRandomizesMinorAndFullCollections(t *testing.T) {
	c := newTestCollector(t, Config{CollectEveryAlloc: true, NurseryBytes: 4096, ThroughputHeapBytes: 8192, ThroughputPageBytes: 4096})
	for i := 0; i < 64; i++ {
		if _, err := c.NewStructDefaultWithRoots(0, EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
	}
	stats := c.Stats()
	if stats.MinorCollections == 0 || stats.FullCollections == 0 {
		t.Fatalf("stress sequence produced minor/full = %d/%d", stats.MinorCollections, stats.FullCollections)
	}
}

func TestInjectedCardGrowthLeavesMetadataUnchanged(t *testing.T) {
	c := newTestCollector(t, Config{})
	parent, err := c.NewArrayDefault(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	beforeCards := append([]objectCard(nil), c.objectCards...)
	cleanup := armFailure(failObjectCardGrowth, 0)
	c.addObjectCard(handleOf(parent), 0)
	cleanup()
	if !reflect.DeepEqual(c.objectCards, beforeCards) || c.entry(parent).cardSlot != 0 {
		t.Fatal("object card failure partially mutated metadata")
	}

	global := c.NewGlobalSlot(Null())
	beforeSlotCards := append([]slotCard(nil), c.slotCards...)
	cleanup = armFailure(failSlotCardGrowth, 0)
	c.addSlotCard(SlotGlobal, global)
	cleanup()
	if !reflect.DeepEqual(c.slotCards, beforeSlotCards) || len(c.slotCardSlot) != 0 {
		t.Fatal("slot card failure partially mutated metadata")
	}
}

func TestNativeBatchCancellationAcrossCollectionAndClose(t *testing.T) {
	c := newTestCollector(t, Config{})
	if !c.PrepareNativeStructHandles() {
		t.Fatal("prepare native handles")
	}
	epoch := c.nativeAllocEpoch
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.nativeAllocEpoch == epoch || c.nativeStructAlloc.Count != 0 || len(c.nurseryHandles) != 0 {
		t.Fatal("collection did not atomically cancel native handle batch")
	}
	if !c.PrepareNativeStructHandles() {
		t.Fatal("prepare replacement native handles")
	}
	c.Close()
	if c.nativeStructAlloc.Count != 0 || c.nativeStructAlloc.Cursor != 0 || c.nativeStructAlloc.Epoch != c.nativeAllocEpoch {
		t.Fatal("Close left native handle reservations or a stale epoch")
	}
}
