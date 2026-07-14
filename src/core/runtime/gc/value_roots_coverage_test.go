package gc

import (
	"math"
	"testing"
)

func TestValueAndRootSlotHelpers(t *testing.T) {
	if got := F32Value(1.5).F32(); got != 1.5 || math.IsNaN(float64(got)) {
		t.Fatalf("f32 round trip = %v", got)
	}
	if got := F64Value(-2.25).F64(); got != -2.25 {
		t.Fatalf("f64 round trip = %v", got)
	}
	r0, r1 := I31New(1), I31New(2)
	root := Root(r0)
	root.SetRef(r1)
	if root.GetRef() != r1 || RefValue(r1).Ref != r1 {
		t.Fatal("root/value reference round trip mismatch")
	}
	refs := RefSliceRoots{r0, r1}
	count := 0
	refs.RangeRoots(func(slot RootSlot) bool {
		count++
		if count == 1 {
			slot.SetRef(Null())
		}
		return true
	})
	if count != 2 || refs[0] != Null() {
		t.Fatalf("ref roots = %#v, count=%d", refs, count)
	}
	var slots Slots = []RootSlot{&root}
	if seen := 0; func() int { slots.RangeRoots(func(RootSlot) bool { seen++; return false }); return seen }() != 1 {
		t.Fatalf("slots early stop = %d", seen)
	}
	if got := withExtraRoot(nil, &root); got == nil {
		t.Fatal("extra root set is nil")
	}
	base := RefSliceRoots{r0}
	extra := Root(r1)
	seen := 0
	withExtraRoot(base, &extra).RangeRoots(func(slot RootSlot) bool {
		seen++
		return true
	})
	if seen != 2 {
		t.Fatalf("combined roots = %d, want 2", seen)
	}
	withExtraRoot(base, &extra).RangeRoots(func(RootSlot) bool { return false })
}

func TestTinyAndThroughputAllocationHelpers(t *testing.T) {
	for _, cfg := range []Config{
		{},
		{TinyHeapBytes: 64, TinyBlockBytes: 3},
		{TinyHeapBytes: 64, TinyBlockBytes: 4},
		{TinyHeapBytes: 65, TinyBlockBytes: 8},
	} {
		if err := validateTinyConfig(cfg); err == nil {
			t.Fatalf("invalid tiny config accepted: %#v", cfg)
		}
	}
	if err := validateTinyConfig(Config{TinyHeapBytes: 64, TinyBlockBytes: 8}); err != nil {
		t.Fatalf("valid tiny config rejected: %v", err)
	}
	if align64(7, 1) != 7 || align64(7, 8) != 8 {
		t.Fatal("64-bit alignment changed")
	}
	if n, err := throughputReservationLen(9, 8, 64); err != nil || n != 16 {
		t.Fatalf("throughput reservation = %d, %v", n, err)
	}
	if n, err := throughputReservationLen(65, 64, 64); err != nil || n != 65 {
		t.Fatalf("limited throughput reservation = %d, %v", n, err)
	}
	h := throughputHeap{classLimit: throughputClassSizes[1]}
	if h.classFor(throughputClassSizes[0]) != 0 || h.classFor(h.classLimit+1) != -1 {
		t.Fatal("throughput class selection changed")
	}
	c := newTestCollector(t, Config{})
	if d, err := c.desc(1); err != nil || d.ID != 1 {
		t.Fatalf("known descriptor = %#v, %v", d, err)
	}
	if _, err := c.desc(TypeID(len(c.typeIndex))); err == nil {
		t.Fatal("out-of-range descriptor accepted")
	}
	c.typeIndex[1] = -1
	if _, err := c.desc(1); err == nil {
		t.Fatal("missing descriptor index accepted")
	}
}

func TestHasHeapObjectTypes(t *testing.T) {
	if HasHeapObjectTypes([]TypeDesc{{Kind: KindFunc}}) || !HasHeapObjectTypes([]TypeDesc{{Kind: KindStruct}}) || !HasHeapObjectTypes([]TypeDesc{{Kind: KindArray}}) {
		t.Fatal("heap object type detection mismatch")
	}
}

func TestNewStructConvenienceWrapper(t *testing.T) {
	c := newTestCollector(t, Config{})
	ref, err := c.NewStruct(0)
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	if !ref.IsObj() {
		t.Fatalf("NewStruct returned %v, want object reference", ref)
	}
	if _, err := c.NewStruct(2); err == nil {
		t.Fatal("NewStruct accepted an array descriptor")
	}
}
