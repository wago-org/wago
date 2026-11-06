package gc

import "testing"

// A checked 24-byte object leaves an eight-byte alignment gap before a chunk.
// Rejected metadata and accepted type appends preserve type-independent
// identity and byte reservations. Explicit cancellation returns the alignment
// gap and unused owned tail without losing the published object.
func TestNativeReviewStage4MetadataAndAlignment(t *testing.T) {
	d, err := NewStructDesc(0, []StorageKind{StorageI64})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 256, NurseryRegionShift: 6, VerifyAfterCollect: true}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := regionStruct(t, c, 0, EmptyRoots{})
	roots := Slots{(*Root)(&r)}
	if err := c.StructSet(r, 0, I64Value(1234567)); err != nil {
		t.Fatal(err)
	}
	before := c.nurseryBump
	if before != 24 {
		t.Fatalf("fixture extent %d", before)
	}
	for i := 0; i < 100; i++ {
		if !c.PrepareNativeAllocation(16) {
			t.Fatal("reserve")
		}
		state := c.nativeStructAlloc
		epoch := c.nativeAllocEpoch
		if state.ChunkStart != 32 {
			t.Fatalf("alignment: %d", state.ChunkStart)
		}
		invalid := d
		invalid.ID = 0
		if err := c.AddTypes([]TypeDesc{invalid}); err == nil {
			t.Fatal("duplicate accepted")
		}
		if c.nativeStructAlloc != state || c.nativeAllocEpoch != epoch {
			t.Fatal("rejected append mutated reservation")
		}
		next, err := NewStructDesc(TypeID(len(c.types)), []StorageKind{StorageI64})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddTypes([]TypeDesc{next}); err != nil {
			t.Fatal(err)
		}
		if c.nativeStructAlloc != state || c.nativeAllocEpoch != epoch || c.nurseryBump != state.ChunkEnd {
			t.Fatal("accepted append disturbed type-independent byte reservation")
		}
		c.CancelNativeAllocationBatch()
		c.CancelNativeAllocationBatch()
		v, err := c.StructGet(r, 0)
		if err != nil || v.I64() != 1234567 {
			t.Fatalf("lost payload %v %v", v, err)
		}
		if err := c.Verify(roots); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 1 {
		t.Fatal("lost survivor")
	}
}
