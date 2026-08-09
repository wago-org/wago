package gc

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestAllocationMinorRetriesAfterFullOldSpaceReclamation(t *testing.T) {
	c := newTestCollector(t, Config{
		StressNurseryBytes:  8192,
		ThroughputHeapBytes: 4096,
		ThroughputPageBytes: 4096,
		VerifyAfterCollect:  true,
	})
	var survivor Ref
	for {
		ref, err := c.NewStructDefault(1)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ForcePromote(ref); err != nil {
			if !errors.Is(err, errThroughputHeapExhausted) {
				t.Fatal(err)
			}
			survivor = ref
			break
		}
	}
	root := Root(survivor)
	before := c.Stats()
	promotionErr := c.CollectMinor(Slots{&root})
	if !errors.Is(promotionErr, errThroughputHeapExhausted) {
		t.Fatalf("initial promotion error = %v, want throughput exhaustion", promotionErr)
	}
	if err := c.retryAllocationMinorAfterPromotionFailure(Slots{&root}, promotionErr); err != nil {
		t.Fatalf("allocation minor fallback: %v", err)
	}
	after := c.Stats()
	if after.FullCollections != before.FullCollections+1 {
		t.Fatalf("full collections = %d, want %d", after.FullCollections, before.FullCollections+1)
	}
	if got := c.entry(Ref(root)).space; got != spaceOld {
		t.Fatalf("survivor space = %v, want old", got)
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
}

func TestCollectMinorPromotionFailureLeavesNurserySurvivorsUnmoved(t *testing.T) {
	c := newTestCollector(t, Config{
		StressNurseryBytes:  8192,
		ThroughputHeapBytes: 4096,
		ThroughputPageBytes: 4096,
		VerifyAfterCollect:  true,
	})

	roots := make([]Root, 0, 160)
	for i := 0; i < 160; i++ {
		r, err := c.NewStructDefault(1)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			if err := c.StructSet(Ref(roots[i-1]), 0, RefValue(r)); err != nil {
				t.Fatal(err)
			}
		}
		roots = append(roots, Root(r))
	}
	parent, err := c.NewArrayDefault(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(parent, 0, RefValue(Ref(roots[0]))); err != nil {
		t.Fatal(err)
	}
	global := c.NewGlobalSlot(Null())
	if err := c.SetGlobalSlot(global, Ref(roots[1])); err != nil {
		t.Fatal(err)
	}
	slots := stressRootSlots(roots)
	bumpBefore := c.nurseryBump
	handlesBefore := slices.Clone(c.handles)
	nurseryHandlesBefore := slices.Clone(c.nurseryHandles)
	rememberedBefore := slices.Clone(c.remembered)
	objectCardsBefore := slices.Clone(c.objectCards)
	freeCardSlotBefore := c.freeObjectCardSlot
	slotCardsBefore := slices.Clone(c.slotCards)
	globalCardBitsBefore := slices.Clone(c.globalCardBits)
	tableCardBitsBefore := slices.Clone(c.tableCardBits)
	cardFallbackBefore := c.cardFallback

	err = c.CollectMinor(slots)
	if err == nil || !strings.Contains(err.Error(), "throughput heap exhausted") {
		t.Fatalf("CollectMinor error = %v, want throughput exhaustion", err)
	}
	if c.nurseryBump != bumpBefore {
		t.Fatalf("nursery bump changed after failed promotion: got %d want %d", c.nurseryBump, bumpBefore)
	}
	if !slices.Equal(c.handles, handlesBefore) {
		t.Fatal("handle metadata changed after failed promotion")
	}
	if !slices.Equal(c.nurseryHandles, nurseryHandlesBefore) {
		t.Fatal("nursery handle set changed after failed promotion")
	}
	if !slices.Equal(c.remembered, rememberedBefore) || !slices.Equal(c.objectCards, objectCardsBefore) || c.freeObjectCardSlot != freeCardSlotBefore ||
		!slices.Equal(c.slotCards, slotCardsBefore) ||
		!slices.Equal(c.globalCardBits, globalCardBitsBefore) || !slices.Equal(c.tableCardBits, tableCardBitsBefore) || c.cardFallback != cardFallbackBefore {
		t.Fatal("remembered or card metadata changed after failed promotion")
	}
	if len(c.promotionScratch) != 0 || cap(c.promotionScratch) == 0 {
		t.Fatalf("promotion scratch after rollback len/cap=%d/%d", len(c.promotionScratch), cap(c.promotionScratch))
	}
	for i, plan := range c.promotionScratch[:cap(c.promotionScratch)] {
		if plan != (plannedPromotion{}) {
			t.Fatalf("promotion scratch %d retained stale plan %+v", i, plan)
		}
	}
	for i, root := range roots {
		if !Ref(root).IsObj() || !c.validObjectRef(Ref(root)) {
			t.Fatalf("root %d no longer points to a valid object: %v", i, root)
		}
		if got := c.entry(Ref(root)).space; got != spaceNursery {
			t.Fatalf("root %d moved to %v after failed promotion, want nursery", i, got)
		}
	}
	if err := c.Verify(slots); err != nil {
		t.Fatalf("heap inconsistent after failed promotion: %v", err)
	}

	for _, root := range roots {
		if err := c.StructSet(Ref(root), 0, RefValue(Null())); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.ArraySet(parent, 0, RefValue(Null())); err != nil {
		t.Fatal(err)
	}
	if err := c.SetGlobalSlot(global, Null()); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(roots); i++ {
		roots[i] = Root(Null())
	}
	slots = stressRootSlots(roots)
	if err := c.CollectFull(slots); err != nil {
		t.Fatalf("full collection after failed promotion: %v", err)
	}
	if err := c.Verify(slots); err != nil {
		t.Fatalf("heap inconsistent after recovery full collection: %v", err)
	}
	if err := c.CollectMinor(slots); err != nil {
		t.Fatalf("minor collection after freeing survivors: %v", err)
	}
	if got := c.entry(Ref(roots[0])).space; got != spaceOld {
		t.Fatalf("remaining survivor space=%v, want old", got)
	}
	if len(c.promotionScratch) != 0 {
		t.Fatalf("promotion scratch retained live length %d", len(c.promotionScratch))
	}
	if err := c.Verify(slots); err != nil {
		t.Fatalf("heap inconsistent after recovery minor collection: %v", err)
	}
}
