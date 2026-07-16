package gc

import "testing"

func bulkTestTypes(t *testing.T) []TypeDesc {
	t.Helper()
	obj, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	i8, err := NewArrayDesc(1, StorageI8)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(2, StorageRef)
	if err != nil {
		t.Fatal(err)
	}
	nullable, err := NewArrayDesc(3, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	return []TypeDesc{obj, i8, refs, nullable}
}

func TestArrayFillPreflightPackedTruncation(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{}, bulkTestTypes(t))
	arr, err := c.NewArrayDefault(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArrayFill(arr, 1, I32Value(0x1234), 2); err != nil {
		t.Fatal(err)
	}
	want := []uint64{0, 0x34, 0x34, 0}
	for i := range want {
		got, err := c.ArrayGet(arr, uint32(i))
		if err != nil || got.Bits != want[i] {
			t.Fatalf("array[%d]=%#x,%v want %#x", i, got.Bits, err, want[i])
		}
	}
	if err := c.ArrayFill(arr, 3, I32Value(7), 2); err != errRange {
		t.Fatalf("oob fill error=%v, want %v", err, errRange)
	}
	got, err := c.ArrayGet(arr, 3)
	if err != nil || got.Bits != 0 {
		t.Fatalf("trapping fill mutated tail: %#x,%v", got.Bits, err)
	}
}

func TestArrayInitDataPreflightWidthsAndAtomicity(t *testing.T) {
	i8, err := NewArrayDesc(0, StorageI8)
	if err != nil {
		t.Fatal(err)
	}
	i16, err := NewArrayDesc(1, StorageI16)
	if err != nil {
		t.Fatal(err)
	}
	i32, err := NewArrayDesc(2, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	i64, err := NewArrayDesc(3, StorageI64)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{}, []TypeDesc{i8, i16, i32, i64})
	data := []byte("abcdefghijkl")
	for _, tc := range []struct {
		typeID TypeID
		source uint32
		want   uint64
	}{
		{typeID: 0, source: 2, want: 0x63},
		{typeID: 1, source: 5, want: 0x6766},
		{typeID: 2, source: 0, want: 0x64636261},
		{typeID: 3, source: 0, want: 0x6867666564636261},
	} {
		arr, err := c.NewArrayDefault(tc.typeID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ArrayInitData(arr, 0, data, tc.source, 1); err != nil {
			t.Fatalf("type %d init: %v", tc.typeID, err)
		}
		got, err := c.ArrayGet(arr, 0)
		if err != nil || got.Bits != tc.want {
			t.Fatalf("type %d value=%#x,%v want %#x", tc.typeID, got.Bits, err, tc.want)
		}
	}
	arr, err := c.NewArrayDefault(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(arr, 0, I32Value(0x1122)); err != nil {
		t.Fatal(err)
	}
	if err := c.ArrayInitData(arr, 1, data[:1], 0, 1); err == nil {
		t.Fatal("short source initialized i16 array")
	}
	got, err := c.ArrayGet(arr, 0)
	if err != nil || got.Bits != 0x1122 {
		t.Fatalf("source trap changed prefix=%#x,%v", got.Bits, err)
	}
	if err := c.ArrayInitData(arr, 2, nil, 0, 0); err != nil {
		t.Fatalf("zero length at end: %v", err)
	}
	if err := c.ArrayInitData(arr, 3, nil, 0, 0); err != errRange {
		t.Fatalf("destination range error=%v, want %v", err, errRange)
	}
}

func TestArrayCopyPreflightAndOverlap(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{}, bulkTestTypes(t))
	arr, err := c.NewArrayDefault(1, 6)
	if err != nil {
		t.Fatal(err)
	}
	for i := uint32(0); i < 6; i++ {
		if err := c.ArraySet(arr, i, I32Value(int32(i+1))); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.ArrayCopy(arr, 1, arr, 0, 5); err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint64{1, 1, 2, 3, 4, 5} {
		got, _ := c.ArrayGet(arr, uint32(i))
		if got.Bits != want {
			t.Fatalf("backward overlap array[%d]=%d want %d", i, got.Bits, want)
		}
	}
	if err := c.ArrayCopy(arr, 0, arr, 1, 5); err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint64{1, 2, 3, 4, 5, 5} {
		got, _ := c.ArrayGet(arr, uint32(i))
		if got.Bits != want {
			t.Fatalf("forward overlap array[%d]=%d want %d", i, got.Bits, want)
		}
	}
	if err := c.ArrayCopy(arr, 0, arr, 0, 7); err != errRange {
		t.Fatalf("oob copy error=%v, want %v", err, errRange)
	}
	got, _ := c.ArrayGet(arr, 0)
	if got.Bits != 1 {
		t.Fatalf("trapping copy mutated prefix: %d", got.Bits)
	}
}

func TestArrayCopyReferenceCompatibilityAndBarriers(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{StressNurseryBytes: 64}, bulkTestTypes(t))
	parent, err := c.NewArrayDefault(3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	src, err := c.NewArrayWithRoots(2, 1, RefValue(child), Slots{rootSlot(child)})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArrayCopy(parent, 1, src, 0, 1); err != nil {
		t.Fatal(err)
	}
	if c.RememberedCount() != 1 || c.CardCount() == 0 {
		t.Fatalf("reference copy barriers remembered=%d cards=%d", c.RememberedCount(), c.CardCount())
	}
	got, err := c.ArrayGet(parent, 1)
	if err != nil || got.Ref != child {
		t.Fatalf("copied ref=%v,%v want %v", got.Ref, err, child)
	}

	nonNullDst, err := c.NewArrayWithRoots(2, 1, RefValue(child), Slots{rootSlot(child)})
	if err != nil {
		t.Fatal(err)
	}
	nullableSrc, err := c.NewArrayDefault(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArrayCopy(nonNullDst, 0, nullableSrc, 0, 1); err == nil {
		t.Fatal("nullable source copied into non-null destination")
	}
	got, _ = c.ArrayGet(nonNullDst, 0)
	if got.Ref != child {
		t.Fatalf("rejected reference copy mutated destination: %v", got.Ref)
	}
}

func TestArrayFillTinyRemarkBarrier(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 8, TinyStepBudget: 1}, bulkTestTypes(t))
	parent, err := c.NewArrayDefault(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(parent)
	roots := Slots{&root}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	for c.tinyGC.state == tinyMark {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyGC.state != tinyRemark || c.tinyColorOf(handleOf(parent)) != tinyBlack {
		t.Fatalf("state=%v parent=%v, want remark/black", c.tinyGC.state, c.tinyColorOf(handleOf(parent)))
	}
	if err := c.ArrayFill(parent, 0, RefValue(child), 1); err != nil {
		t.Fatal(err)
	}
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
	}
	if !c.validObjectRef(child) {
		t.Fatal("Tiny remark barrier reclaimed filled child")
	}
	got, err := c.ArrayGet(parent, 0)
	if err != nil || got.Ref != child {
		t.Fatalf("filled child=%v,%v want %v", got.Ref, err, child)
	}
}

type rootSlot Ref

func (r rootSlot) GetRef() Ref  { return Ref(r) }
func (r rootSlot) SetRef(v Ref) {}
