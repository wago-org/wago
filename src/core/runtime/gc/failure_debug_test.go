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
			cleanup := armFailure(c, point, after)
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
		defer armFailure(c, failHandlePublication, 0)()
		if _, err := c.NewStructDefault(0); !errors.Is(err, errInjectedFailure) {
			t.Fatalf("error = %v", err)
		}
		assertPromotionStateEqual(t, c, before)
	})
	t.Run("large publication", func(t *testing.T) {
		c := newTestCollector(t, Config{LargeObjectBytes: 16})
		before := snapshotPromotionState(c)
		defer armFailure(c, failHandlePublication, 0)()
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
		defer armFailure(&c.throughput, failBackingGrowth, 0)()
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
		defer armFailure(&c.throughput, failBackingGrowth, 0)()
		if _, err := c.NewStructDefault(0); !errors.Is(err, errInjectedFailure) {
			t.Fatalf("error = %v", err)
		}
		assertPromotionStateEqual(t, c, before)
	})
	t.Run("force-promote backing growth", func(t *testing.T) {
		c := newTestCollector(t, Config{})
		object, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		c.throughput.largestFree = 256
		c.throughput.largestFreeDirty = true
		before := snapshotPromotionState(c)
		defer armFailure(&c.throughput, failBackingGrowth, 0)()
		if err := c.ForcePromote(object); !errors.Is(err, errInjectedFailure) {
			t.Fatalf("error = %v", err)
		}
		assertPromotionStateEqual(t, c, before)
	})
	t.Run("tiny publication", func(t *testing.T) {
		c := newTinyTestCollector(t, Config{})
		beforeHandles, beforeBlocks := append([]handleEntry(nil), c.handles...), append([]tinyBlock(nil), c.tiny.blocks...)
		defer armFailure(c, failHandlePublication, 0)()
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

func TestInjectedFailureIsScopedToCollector(t *testing.T) {
	first := newTestCollector(t, Config{})
	second := newTestCollector(t, Config{})
	cleanupFirst := armFailure(first, failHandlePublication, 0)
	defer cleanupFirst()
	if _, err := second.NewStructDefault(0); err != nil {
		t.Fatalf("unrelated collector consumed failure: %v", err)
	}
	cleanupSecond := armFailure(second, failHandlePublication, 0)
	defer cleanupSecond()
	if _, err := first.NewStructDefault(0); !errors.Is(err, errInjectedFailure) {
		t.Fatalf("target collector error = %v", err)
	}
	if _, err := second.NewStructDefault(0); !errors.Is(err, errInjectedFailure) {
		t.Fatalf("second target collector error = %v", err)
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
	cleanup := armFailure(c, failObjectCardGrowth, 0)
	c.addObjectCard(handleOf(parent), 0)
	cleanup()
	if !reflect.DeepEqual(c.objectCards, beforeCards) || c.entry(parent).cardSlot != 0 {
		t.Fatal("object card failure partially mutated metadata")
	}

	global := c.NewGlobalSlot(Null())
	beforeSlotCards := append([]slotCard(nil), c.slotCards...)
	cleanup = armFailure(c, failSlotCardGrowth, 0)
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

func TestNativeBatchCancellationSurvivesInjectedCollectionFailure(t *testing.T) {
	c := newTestCollector(t, Config{})
	object, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(object)
	if !c.PrepareNativeStructHandles() {
		t.Fatal("prepare native handles")
	}
	epoch := c.nativeAllocEpoch
	cleanup := armFailure(c, failPromotionPlan, 0)
	err = c.CollectMinor(Slots{&root})
	cleanup()
	if !errors.Is(err, errInjectedFailure) {
		t.Fatalf("collection error = %v", err)
	}
	if c.nativeAllocEpoch == epoch || c.nativeStructAlloc.Count != 0 || c.nativeStructAlloc.Cursor != 0 {
		t.Fatal("failed collection retained native reservations or stale epoch")
	}
	occurrences := 0
	for _, h := range c.nurseryHandles {
		if h == handleOf(object) {
			occurrences++
		}
	}
	if occurrences != 1 || len(c.nurseryHandles) != 1 {
		t.Fatalf("nursery handles after cancellation = %v; live handle occurrences %d", c.nurseryHandles, occurrences)
	}
	seenFree := make(map[uint32]bool, len(c.freeHandles))
	for _, h := range c.freeHandles {
		if seenFree[h] {
			t.Fatalf("canceled handle %d appears twice in free list", h)
		}
		seenFree[h] = true
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatalf("recovery collection: %v", err)
	}
}
