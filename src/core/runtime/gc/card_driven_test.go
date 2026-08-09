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
	wantCollector := uintptr(1120)
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
