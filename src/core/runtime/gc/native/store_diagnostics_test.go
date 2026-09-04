//go:build wago_gcstats

package gc

import "testing"

func TestStoreDiagnosticsReportSpacesAndFailClosedCards(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := NewArrayDesc(2, StorageV128)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 4096, ThroughputHeapBytes: 64 << 10, LargeObjectBytes: 1024}, []TypeDesc{leaf, refs, vectors})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	parent, err := c.NewArrayDefault(1, 64)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	parentSpace, childSpace, remembered := c.DiagnosticObjectStore(parent, child)
	if parentSpace != DiagnosticSpaceNursery || childSpace != DiagnosticSpaceNursery || remembered {
		t.Fatalf("nursery diagnostic = %d/%d/%v", parentSpace, childSpace, remembered)
	}
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	parentSpace, childSpace, remembered = c.DiagnosticObjectStore(parent, child)
	if parentSpace != DiagnosticSpaceOld || childSpace != DiagnosticSpaceNursery || !remembered {
		t.Fatalf("old-to-young diagnostic = %d/%d/%v", parentSpace, childSpace, remembered)
	}
	if got, _, _ := c.DiagnosticObjectStore(parent, Null()); got != DiagnosticSpaceOld {
		t.Fatalf("old parent space = %d", got)
	}
	if _, got, _ := c.DiagnosticObjectStore(parent, I31New(1)); got != DiagnosticSpaceImmediate {
		t.Fatalf("i31 child space = %d", got)
	}
	if _, got, _ := c.DiagnosticObjectStore(parent, Ref(0xfffe)); got != DiagnosticSpaceInvalid {
		t.Fatalf("forged child space = %d", got)
	}
	var nilCollector *Collector
	if got, childGot, rememberedGot := nilCollector.DiagnosticObjectStore(parent, child); got != DiagnosticSpaceInvalid || childGot != DiagnosticSpaceInvalid || rememberedGot {
		t.Fatalf("nil collector diagnostic = %d/%d/%v", got, childGot, rememberedGot)
	}

	if present, covers := c.DiagnosticArrayCard(parent, 0); !present || !covers {
		t.Fatalf("first card = %v/%v", present, covers)
	}
	if present, covers := c.DiagnosticArrayCard(parent, 63); !present || covers {
		t.Fatalf("clean distant card = %v/%v", present, covers)
	}
	if present, covers := c.DiagnosticArrayCard(Null(), 0); present || covers {
		t.Fatalf("null parent card = %v/%v", present, covers)
	}
	if present, covers := nilCollector.DiagnosticArrayCard(parent, 0); present || covers {
		t.Fatalf("nil collector card = %v/%v", present, covers)
	}

	h := handleOf(parent)
	slot := c.handles[h].cardSlot
	c.handles[h].cardSlot = uint32(len(c.objectCards) + 1)
	if present, covers := c.DiagnosticArrayCard(parent, 0); present || covers {
		t.Fatalf("stale card head = %v/%v", present, covers)
	}
	c.handles[h].cardSlot = slot
	card := c.objectCards[slot-1]
	c.objectCards[slot-1].handle = handleOf(child)
	if present, covers := c.DiagnosticArrayCard(parent, 0); present || covers {
		t.Fatalf("wrong-owner card = %v/%v", present, covers)
	}
	c.objectCards[slot-1] = card
	c.objectCards[slot-1].index, c.objectCards[slot-1].end = 1, 0
	if present, covers := c.DiagnosticArrayCard(parent, 0); present || covers {
		t.Fatalf("reversed card range = %v/%v", present, covers)
	}
	c.objectCards[slot-1] = card

	vectorArray, err := c.NewArrayDefault(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(vectorArray); err != nil {
		t.Fatal(err)
	}
	c.addObjectCard(handleOf(vectorArray), 0)
	if present, covers := c.DiagnosticArrayCard(vectorArray, ^uint32(0)); !present || covers {
		t.Fatalf("overflowing diagnostic index = %v/%v", present, covers)
	}
	if got := diagnosticSpace(spaceFree); got != DiagnosticSpaceInvalid {
		t.Fatalf("free diagnostic space = %d", got)
	}
}

func TestStoreDiagnosticsReportLargeAndTinySpaces(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	largeDesc, err := NewArrayDesc(1, StorageI64)
	if err != nil {
		t.Fatal(err)
	}
	throughput, err := NewCollector(Config{NurseryBytes: 4096, ThroughputHeapBytes: 64 << 10, LargeObjectBytes: 64}, []TypeDesc{leaf, largeDesc})
	if err != nil {
		t.Fatal(err)
	}
	defer throughput.Close()
	large, err := throughput.NewArrayDefault(1, 16)
	if err != nil {
		t.Fatal(err)
	}
	if _, childSpace, _ := throughput.DiagnosticObjectStore(large, large); childSpace != DiagnosticSpaceLarge {
		t.Fatalf("large object diagnostic space = %d", childSpace)
	}

	tiny, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	if err != nil {
		t.Fatal(err)
	}
	defer tiny.Close()
	object, err := tiny.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	parentSpace, childSpace, remembered := tiny.DiagnosticObjectStore(object, object)
	if parentSpace != DiagnosticSpaceTiny || childSpace != DiagnosticSpaceTiny || remembered {
		t.Fatalf("tiny diagnostic = %d/%d/%v", parentSpace, childSpace, remembered)
	}
}
