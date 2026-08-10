//go:build wagodebug

package gc

import (
	"errors"
	"reflect"
	"testing"
)

func TestObjectCardReuseDoesNotConsumeGrowthFailure(t *testing.T) {
	c := newTestCollector(t, Config{})
	arrays := make([]Ref, 4)
	for i := range arrays {
		var err error
		arrays[i], err = c.NewArrayDefault(3, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ForcePromote(arrays[i]); err != nil {
			t.Fatal(err)
		}
	}
	c.CardMarkArray(arrays[0], 0)
	c.CardMarkArray(arrays[1], 0)
	c.removeCardsForHandle(handleOf(arrays[0]))
	if c.freeObjectCardSlot == 0 {
		t.Fatal("card removal did not publish a reusable slot")
	}

	cleanup := armFailure(c, failObjectCardGrowth, 0)
	c.CardMarkArray(arrays[2], 0)
	if c.entry(arrays[2]).cardSlot == 0 || c.freeObjectCardSlot != 0 {
		t.Fatalf("reusable slot incorrectly used growth failure: slot=%d free=%d", c.entry(arrays[2]).cardSlot, c.freeObjectCardSlot)
	}
	c.CardMarkArray(arrays[3], 0)
	cleanup()
	if c.entry(arrays[3]).cardSlot != 0 || !c.entry(arrays[3]).remembered || !c.cardFallback {
		t.Fatalf("growth failure did not retain whole-object fallback: slot=%d remembered=%v fallback=%v", c.entry(arrays[3]).cardSlot, c.entry(arrays[3]).remembered, c.cardFallback)
	}
}

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

func TestInjectedPromotionFailureIsReportedByTelemetry(t *testing.T) {
	if !collectorTelemetryEnabled {
		t.Skip("collector telemetry requires wago_gcstats")
	}
	telemetry := new(Telemetry)
	c := newTestCollector(t, Config{Telemetry: telemetry, NurseryBytes: 4096, ThroughputHeapBytes: 8192, ThroughputPageBytes: 4096})
	defer c.Close()
	object, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(object)
	cleanup := armFailure(c, failPromotionPlan, 0)
	err = c.CollectMinor(Slots{&root})
	cleanup()
	if !errors.Is(err, errInjectedFailure) {
		t.Fatalf("error = %v", err)
	}
	snapshot, ok := c.TelemetrySnapshot()
	if !ok || snapshot.Minor.Cycles != 1 || snapshot.Minor.FailedCycles != 1 || snapshot.Minor.Pause.Count != 1 {
		t.Fatalf("failed-cycle telemetry = %+v, enabled=%v", snapshot.Minor, ok)
	}
}

func TestInjectedPromotionRollbackRestoresReusedFreeSpan(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		new  func(*testing.T, *Collector) Ref
	}{
		{
			name: "size class",
			cfg:  Config{},
			new: func(t *testing.T, c *Collector) Ref {
				r, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				return r
			},
		},
		{
			name: "large span",
			cfg:  Config{ThroughputClassLimit: 32},
			new: func(t *testing.T, c *Collector) Ref {
				r, err := c.NewArrayDefault(3, 8)
				if err != nil {
					t.Fatal(err)
				}
				return r
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := newTestCollector(t, test.cfg)
			freeSpan := test.new(t, c)
			if err := c.ForcePromote(freeSpan); err != nil {
				t.Fatal(err)
			}
			if err := c.CollectFull(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			roots := []Root{Root(test.new(t, c)), Root(test.new(t, c))}
			before := snapshotPromotionState(c)
			cleanup := armFailure(c, failPromotionPlan, 1)
			err := c.CollectMinor(stressRootSlots(roots))
			cleanup()
			if !errors.Is(err, errInjectedFailure) {
				t.Fatalf("error = %v", err)
			}
			assertPromotionStateEqual(t, c, before)
		})
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
		before := snapshotPromotionState(c)
		defer armFailure(&c.throughput, failBackingGrowth, 0)()
		if _, err := c.NewStructDefault(0); !errors.Is(err, errInjectedFailure) {
			t.Fatalf("error = %v", err)
		}
		assertPromotionStateEqual(t, c, before)
	})
	t.Run("collection-disabled backing growth", func(t *testing.T) {
		c := newTestCollector(t, Config{DisableCollection: true})
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
		before := snapshotPromotionState(c)
		defer armFailure(&c.throughput, failBackingGrowth, 0)()
		if err := c.ForcePromote(object); !errors.Is(err, errInjectedFailure) {
			t.Fatalf("error = %v", err)
		}
		assertPromotionStateEqual(t, c, before)
	})
	t.Run("tiny publication", func(t *testing.T) {
		c := newTinyTestCollector(t, Config{TinyHeapBytes: 32, TinyBlockBytes: 16})
		beforeHandles := append([]handleEntry(nil), c.handles...)
		defer armFailure(c, failHandlePublication, 0)()
		if _, err := c.NewStructDefault(0); !errors.Is(err, errInjectedFailure) {
			t.Fatalf("error = %v", err)
		}
		if !reflect.DeepEqual(c.handles, beforeHandles) {
			t.Fatal("tiny publication failure mutated handles")
		}
		if _, err := c.NewStructDefault(0); err != nil {
			t.Fatalf("allocation after publication failure: %v", err)
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
	if !reflect.DeepEqual(c.slotCards, beforeSlotCards) || cardBitIsSet(c.globalCardBits, global) || !c.cardFallback {
		t.Fatal("slot card failure did not preserve metadata and arm the full-root fallback")
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
	if c.nativeAllocEpoch == epoch || c.nativeStructAlloc.Count != 0 || c.nativeStructAlloc.ChunkEnd != 0 || len(c.nurseryHandles) != 0 {
		t.Fatal("collection did not atomically cancel native handle batch")
	}
	if !c.PrepareNativeStructHandles() {
		t.Fatal("prepare replacement native handles")
	}
	c.Close()
	if c.nativeStructAlloc.Count != 0 || c.nativeStructAlloc.Cursor != 0 || c.nativeStructAlloc.ChunkEnd != 0 || c.nativeStructAlloc.Epoch != c.nativeAllocEpoch {
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
	if c.nativeAllocEpoch == epoch || c.nativeStructAlloc.Count != 0 || c.nativeStructAlloc.Cursor != 0 || c.nativeStructAlloc.ChunkEnd != 0 {
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
