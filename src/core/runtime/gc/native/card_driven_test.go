package gc

import (
	"fmt"
	"testing"
	"unsafe"
)

func TestCardDrivenMinorScansDisjointArrayCards(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 2 << 20, ThroughputHeapBytes: 8 << 20, VerifyAfterCollect: true, DisableMovingNursery: true}, []TypeDesc{leaf, refs})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const length = uint32(256 << 10)
	array, err := c.NewArrayDefault(1, length)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	first, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	last, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	dead, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(array, 0, RefValue(first)); err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(array, length-1, RefValue(last)); err != nil {
		t.Fatal(err)
	}
	if got := c.dirtyObjectCardCount(); got != 2 {
		t.Fatalf("dirty cards=%d, want 2", got)
	}
	if len(c.objectCards) != 2 {
		t.Fatalf("distant cards coalesced into %d ranges: %+v", len(c.objectCards), c.objectCards)
	}
	root := Root(array)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []Ref{first, last} {
		if c.entry(ref).space != spaceOld {
			t.Fatalf("carded child %v space=%v, want old", ref, c.entry(ref).space)
		}
	}
	if c.entry(dead).space != spaceFree {
		t.Fatalf("uncarded nursery object space=%v, want free", c.entry(dead).space)
	}
}

func TestConservativeObjectBarrierCoversWholeOldParent(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentDesc, err := NewStructDesc(1, []StorageKind{StorageRefNull, StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 4096, ThroughputHeapBytes: 64 << 10, VerifyAfterCollect: true}, []TypeDesc{leaf, parentDesc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	nurseryParent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}

	c.WriteBarrierObject(Null(), child)
	c.WriteBarrierObject(parent, Null())
	c.WriteBarrierObject(nurseryParent, child)
	if c.CardCount() != 0 || c.RememberedCount() != 0 {
		t.Fatalf("invalid or young-parent barriers recorded metadata: cards=%d remembered=%d", c.CardCount(), c.RememberedCount())
	}
	c.WriteBarrierObject(parent, child)
	if c.CardCount() != 1 || c.RememberedCount() != 1 {
		t.Fatalf("whole-object barrier metadata: cards=%d remembered=%d", c.CardCount(), c.RememberedCount())
	}
	card := c.objectCards[c.entry(parent).cardSlot-1]
	if card.index != 0 || card.end != c.entry(parent).size-PayloadOffset-1 {
		t.Fatalf("whole-object card = %+v, payload bytes=%d", card, c.entry(parent).size-PayloadOffset)
	}
	if err := c.StructSet(parent, 1, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if !c.validObjectRef(child) {
		t.Fatal("whole-object barrier did not preserve nursery child")
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if c.entry(child).young() {
		t.Fatal("second minor did not tenure preserved child")
	}
	c.WriteBarrierObject(parent, child)
	if c.CardCount() != 0 {
		t.Fatalf("old-to-old barrier recorded cards: %d", c.CardCount())
	}
}

func TestMinorUsesDirtyPersistentSlotsAndInitialCards(t *testing.T) {
	c := newTestCollector(t, Config{StressNurseryBytes: 1 << 20, VerifyAfterCollect: true})
	for i := 0; i < 4096; i++ {
		c.NewGlobalSlot(Null())
	}
	initial, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	initialSlot := c.NewGlobalSlot(initial)
	stored, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetGlobalSlot(17, stored); err != nil {
		t.Fatal(err)
	}
	dead, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.slotCards) != 2 || !cardBitIsSet(c.globalCardBits, initialSlot) || !cardBitIsSet(c.globalCardBits, 17) {
		t.Fatalf("dirty persistent roots=%v", c.slotCards)
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []Ref{initial, stored} {
		if c.entry(ref).space != spaceOld {
			t.Fatalf("dirty-root child %v space=%v, want old", ref, c.entry(ref).space)
		}
	}
	if c.entry(dead).space != spaceFree {
		t.Fatalf("unrooted nursery object space=%v, want free", c.entry(dead).space)
	}
	if len(c.slotCards) != 0 || cardBitIsSet(c.globalCardBits, initialSlot) || cardBitIsSet(c.globalCardBits, 17) {
		t.Fatal("minor collection did not clear dirty persistent roots")
	}
}

func TestCardDrivenScanIsAllocationFreeAfterWarmup(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 8 << 20, DisableMovingNursery: true}, []TypeDesc{leaf, refs})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	array, err := c.NewArrayDefault(1, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(array, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(array, 1023, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	scan := func() {
		c.clearNurseryMarks()
		c.scanRememberedCards(handleOf(array))
		c.drainNurseryMarkStack()
	}
	scan()
	if got := testing.AllocsPerRun(100, scan); got != 0 {
		t.Fatalf("warmed card-driven scan allocations = %v, want 0", got)
	}
}

func TestObjectCardSlotsAreReusedAcrossSurvivorCycles(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 4 << 20, VerifyAfterCollect: true}, []TypeDesc{leaf, refs})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	parents := make([]Ref, 2)
	for i := range parents {
		parents[i], err = c.NewArrayDefault(1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ForcePromote(parents[i]); err != nil {
			t.Fatal(err)
		}
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(parents[0], 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if !c.entry(child).young() || c.CardCount() != 1 {
		t.Fatalf("initial survivor/card state: young=%v cards=%d", c.entry(child).young(), c.CardCount())
	}

	currentParent := 0
	for cycle := 0; cycle < 64; cycle++ {
		nextParent := 1 - currentParent
		nextChild, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ArraySet(parents[nextParent], 0, RefValue(nextChild)); err != nil {
			t.Fatal(err)
		}
		if err := c.ArraySet(parents[currentParent], 0, RefValue(Null())); err != nil {
			t.Fatal(err)
		}
		if err := c.CollectMinor(nil); err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
		if c.entry(child).space != spaceFree {
			t.Fatalf("cycle %d: replaced child space=%v, want free", cycle, c.entry(child).space)
		}
		if !c.entry(nextChild).young() {
			t.Fatalf("cycle %d: current child is not young", cycle)
		}
		if got := len(c.objectCards); got > 2 {
			t.Fatalf("cycle %d: card arena grew to %d slots with one live card", cycle, got)
		}
		if got := c.CardCount(); got != 1 {
			t.Fatalf("cycle %d: live cards=%d, want 1", cycle, got)
		}
		if err := c.Verify(nil); err != nil {
			t.Fatalf("cycle %d verify: %v", cycle, err)
		}
		child, currentParent = nextChild, nextParent
	}

	if err := c.ArraySet(parents[currentParent], 0, RefValue(Null())); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if len(c.objectCards) != 0 || c.freeObjectCardSlot != 0 || c.CardCount() != 0 {
		t.Fatalf("drained card arena: slots=%d free=%d live=%d", len(c.objectCards), c.freeObjectCardSlot, c.CardCount())
	}
}

func TestMalformedFreeCardHeadFallsBackAcrossLaterWrites(t *testing.T) {
	c := newTestCollector(t, Config{VerifyAfterCollect: true})
	array, err := c.NewArrayDefault(3, 65)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	first, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	c.freeObjectCardSlot = uint32(len(c.objectCards) + 1)
	if err := c.ArraySet(array, 0, RefValue(first)); err != nil {
		t.Fatal(err)
	}
	c.freeObjectCardSlot = 0
	if !c.cardFallback || c.handles[handleOf(array)].cardSlot != 0 {
		t.Fatalf("malformed free head fallback = %v/%d", c.cardFallback, c.handles[handleOf(array)].cardSlot)
	}
	if err := c.ArraySet(array, 64, RefValue(second)); err != nil {
		t.Fatal(err)
	}
	if c.handles[handleOf(array)].cardSlot == 0 || !c.cardFallback {
		t.Fatalf("later exact card lost fallback = %d/%v", c.handles[handleOf(array)].cardSlot, c.cardFallback)
	}
	root := Root(array)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(first).space != spaceOld || c.entry(second).space != spaceOld {
		t.Fatalf("fallback child spaces = %v/%v, want old/old", c.entry(first).space, c.entry(second).space)
	}
}

func TestMalformedFreeCardHeadWidensExistingRange(t *testing.T) {
	c := newTestCollector(t, Config{VerifyAfterCollect: true})
	array, err := c.NewArrayDefault(3, 65)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(array, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	c.freeObjectCardSlot = uint32(len(c.objectCards) + 1)
	c.CardMarkArray(array, 64)
	c.freeObjectCardSlot = 0
	card := c.objectCards[c.handles[handleOf(array)].cardSlot-1]
	if card.index != 0 || card.end != c.handles[handleOf(array)].size-PayloadOffset-1 || c.cardFallback {
		t.Fatalf("existing-card fallback = %+v/global:%v", card, c.cardFallback)
	}
	root := Root(array)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
}

func TestObjectCardRepeatedWriteMovesRangeToHead(t *testing.T) {
	c := newTestCollector(t, Config{})
	array, err := c.NewArrayDefault(3, 65)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	c.CardMarkArray(array, 0)
	firstSlot := c.handles[handleOf(array)].cardSlot
	c.CardMarkArray(array, 64)
	secondSlot := c.handles[handleOf(array)].cardSlot
	if firstSlot == 0 || secondSlot == 0 || firstSlot == secondSlot {
		t.Fatalf("initial card slots = %d/%d", firstSlot, secondSlot)
	}
	c.CardMarkArray(array, 0)
	if got := c.handles[handleOf(array)].cardSlot; got != secondSlot {
		t.Fatalf("repeated non-head card changed stable head slot = %d, want %d", got, secondSlot)
	}
	head, tail := c.objectCards[secondSlot-1], c.objectCards[firstSlot-1]
	if head.index != 0 || head.next != firstSlot || tail.index != 256 || tail.next != 0 || c.CardCount() != 2 {
		t.Fatalf("move-to-front intervals/links/cards = %+v/%d", c.objectCards, c.CardCount())
	}
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
	}
}

func TestObjectCardCoalescingRecyclesAbsorbedSlot(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{leaf, refs})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	array, err := c.NewArrayDefault(1, 256)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		t.Fatal(err)
	}

	c.addObjectCard(handleOf(array), 0)
	c.CardMarkArray(array, 64)
	if len(c.objectCards) != 2 {
		t.Fatalf("initial disjoint ranges = %+v", c.objectCards)
	}
	c.CardMarkArray(array, 32)
	if len(c.objectCards) != 2 || c.CardCount() != 1 || c.freeObjectCardSlot == 0 {
		t.Fatalf("bridged ranges: slots=%d live=%d free=%d cards=%+v", len(c.objectCards), c.CardCount(), c.freeObjectCardSlot, c.objectCards)
	}
	c.CardMarkArray(array, 192)
	if len(c.objectCards) != 2 || c.CardCount() != 2 || c.freeObjectCardSlot != 0 {
		t.Fatalf("reused absorbed slot: slots=%d live=%d free=%d cards=%+v", len(c.objectCards), c.CardCount(), c.freeObjectCardSlot, c.objectCards)
	}
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
	}
}

func TestCardMetadataFootprint(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("64-bit fixed-footprint assertion")
	}
	if got := unsafe.Sizeof(objectCard{}); got != 16 {
		t.Fatalf("objectCard size=%d, want 16", got)
	}
	if got := unsafe.Sizeof(Config{}); got != 72 {
		t.Fatalf("Config size=%d, want 72", got)
	}
	if got := unsafe.Sizeof(tinyScanCursor{}); got != 8 {
		t.Fatalf("tinyScanCursor size=%d, want 8", got)
	}
	if got := unsafe.Sizeof(tinyStepWork{}); got != 12 {
		t.Fatalf("tinyStepWork size=%d, want 12 with uint32 transient-root accounting", got)
	}
	if got := unsafe.Sizeof(tinyMarkState(0)); got != 1 {
		t.Fatalf("tinyMarkState size=%d, want 1", got)
	}
	if tinyStepObjectRanges > uint32(^uint16(0)) || tinyStepScanEntries > uint32(^uint16(0)) || tinyStepPayloadBytes > uint32(^uint16(0)) {
		t.Fatal("Tiny Step work bounds do not fit compact telemetry state")
	}
	if got := unsafe.Sizeof(tinyGC{}); got != 88 {
		t.Fatalf("tinyGC size=%d, want 88 with unbounded transient-root accounting", got)
	}
	wantCollector := uintptr(1128) + unsafe.Sizeof((*[]uint64)(nil))
	if got := unsafe.Sizeof(Collector{}); got != wantCollector {
		t.Fatalf("Collector size=%d, want %d", got, wantCollector)
	}
}

func TestThroughputCardSizeBoundaries(t *testing.T) {
	for _, cardBytes := range []uint32{128, 256, 512} {
		t.Run(fmt.Sprintf("bytes=%d", cardBytes), func(t *testing.T) {
			c := newTestCollector(t, Config{})
			c.cardBytes = cardBytes
			array, err := c.NewArrayDefault(3, 1024)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ForcePromote(array); err != nil {
				t.Fatal(err)
			}
			c.CardMarkArray(array, 0)
			c.CardMarkArray(array, 1023)
			if got := c.dirtyObjectCardCount(); got != 2 {
				t.Fatalf("card bytes=%d dirty cards=%d, want 2", cardBytes, got)
			}
			root := Root(array)
			if err := c.Verify(Slots{&root}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
