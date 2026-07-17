package gc

import (
	"encoding/binary"
	"fmt"
	"testing"
)

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

func TestBulkArrayConstructorsReconcileLargeReferenceParents(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		c := newTestCollectorWithTypes(t, Config{
			ThroughputHeapBytes:  1 << 20,
			ThroughputPageBytes:  4096,
			ThroughputClassLimit: 256,
			LargeObjectBytes:     256,
		}, bulkTestTypes(t))
		child, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		childRoot := Root(child)
		const length = 128
		var array Ref
		if fixed {
			values := make([]Value, length)
			for i := range values {
				values[i] = RefValue(child)
			}
			array, err = c.NewArrayFixedWithRoots(2, values, Slots{&childRoot})
		} else {
			array, err = c.NewRefArrayWithRoots(2, length, &childRoot, Slots{&childRoot})
		}
		if err != nil {
			t.Fatalf("fixed=%v construct: %v", fixed, err)
		}
		if c.entry(array).space != spaceLarge || !c.entry(array).remembered || len(c.objectCards) != 1 {
			t.Fatalf("fixed=%v metadata: space=%d remembered=%v cards=%v", fixed, c.entry(array).space, c.entry(array).remembered, c.objectCards)
		}
		arrayRoot := Root(array)
		if err := c.CollectMinor(Slots{&arrayRoot}); err != nil {
			t.Fatalf("fixed=%v minor collection: %v", fixed, err)
		}
		for _, index := range []uint32{0, length / 2, length - 1} {
			got, err := c.ArrayGet(Ref(arrayRoot), index)
			if err != nil || !got.Ref.IsObj() || !c.validObjectRef(got.Ref) {
				t.Fatalf("fixed=%v array[%d]=%v,%v after minor collection", fixed, index, got.Ref, err)
			}
		}
	}
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

func TestArrayInitWordsPreflightAndAtomicity(t *testing.T) {
	i64, err := NewArrayDesc(0, StorageI64)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{}, []TypeDesc{i64})
	arr, err := c.NewArrayDefault(0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArrayInitWords(arr, 1, []uint64{0x11, 0x22}); err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint64{0, 0x11, 0x22, 0} {
		got, err := c.ArrayGet(arr, uint32(i))
		if err != nil || got.Bits != want {
			t.Fatalf("word array[%d]=%#x,%v want %#x", i, got.Bits, err, want)
		}
	}
	if err := c.ArrayInitWords(arr, 3, []uint64{0x33, 0x44}); err != errRange {
		t.Fatalf("word range error=%v, want %v", err, errRange)
	}
	got, err := c.ArrayGet(arr, 3)
	if err != nil || got.Bits != 0 {
		t.Fatalf("trapping word init mutated tail=%#x,%v", got.Bits, err)
	}
	if err := c.ArrayInitWords(arr, 4, nil); err != nil {
		t.Fatalf("zero length at end: %v", err)
	}
}

func TestArrayCopyNumericStorageWidthsAndBitPatterns(t *testing.T) {
	kinds := []StorageKind{StorageI8, StorageI16, StorageI32, StorageI64, StorageF32, StorageF64}
	types := make([]TypeDesc, len(kinds))
	for i, kind := range kinds {
		var err error
		types[i], err = NewArrayDesc(TypeID(i), kind)
		if err != nil {
			t.Fatal(err)
		}
	}
	c := newTestCollectorWithTypes(t, Config{}, types)
	values := []Value{
		I32Value(0xab), I32Value(0xabcd), I32Value(int32(0x76543210)),
		{Kind: StorageI64, Bits: 0xfedcba9876543210},
		{Kind: StorageF32, Bits: 0x7fc12345},
		{Kind: StorageF64, Bits: 0x7ff8123456789abc},
	}
	masks := []uint64{0xff, 0xffff, 0xffffffff, ^uint64(0), 0xffffffff, ^uint64(0)}
	for i, kind := range kinds {
		t.Run(fmt.Sprintf("kind-%d", kind), func(t *testing.T) {
			src, err := c.NewArrayDefault(TypeID(i), 4)
			if err != nil {
				t.Fatal(err)
			}
			dst, err := c.NewArrayDefault(TypeID(i), 4)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ArraySet(src, 3, values[i]); err != nil {
				t.Fatal(err)
			}
			if err := c.ArrayCopy(dst, 3, src, 3, 1); err != nil {
				t.Fatal(err)
			}
			got, err := c.ArrayGet(dst, 3)
			if err != nil || got.Bits != values[i].Bits&masks[i] {
				t.Fatalf("kind %d copied bits = %#x, %v; want %#x", kind, got.Bits, err, values[i].Bits&masks[i])
			}
			if err := c.ArrayCopy(dst, 4, src, 4, 0); err != nil {
				t.Fatalf("kind %d zero-length end copy: %v", kind, err)
			}
			if err := c.ArrayCopy(dst, ^uint32(0), src, 0, 2); err != errRange {
				t.Fatalf("kind %d overflowing range error = %v", kind, err)
			}
		})
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

func TestArrayCopyHardeningModesValidateCollectorPayloads(t *testing.T) {
	for _, cfg := range []Config{{VerifyAfterCollect: true}, {StressBarriers: true}} {
		c := newTestCollectorWithTypes(t, cfg, bulkTestTypes(t))
		src, err := c.NewArrayDefault(3, 1)
		if err != nil {
			t.Fatal(err)
		}
		dst, err := c.NewArrayDefault(3, 1)
		if err != nil {
			t.Fatal(err)
		}
		binary.LittleEndian.PutUint32(c.bytes(src)[PayloadOffset:], uint32(makeObjRef(0xffff)))
		if err := c.ArrayCopy(dst, 0, src, 0, 1); err == nil {
			t.Fatalf("hardening config %+v accepted forged source payload", cfg)
		}
		got, err := c.ArrayGet(dst, 0)
		if err != nil || !got.Ref.IsNull() {
			t.Fatalf("hardening rejection mutated destination: %+v, %v", got, err)
		}
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

func TestArrayFillReferenceBulkLengthsAndRememberedReconcile(t *testing.T) {
	for _, length := range []uint32{0, 1, 16, 256, 4096} {
		t.Run(benchmarkLength(length), func(t *testing.T) {
			c := newTestCollectorWithTypes(t, Config{StressNurseryBytes: 1 << 20}, bulkTestTypes(t))
			dst, err := c.NewArrayDefault(3, length)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ForcePromote(dst); err != nil {
				t.Fatal(err)
			}
			child, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ArrayFill(dst, 0, RefValue(child), length); err != nil {
				t.Fatal(err)
			}
			if length == 0 {
				if c.RememberedCount() != 0 || c.CardCount() != 0 {
					t.Fatalf("zero fill remembered/cards = %d/%d", c.RememberedCount(), c.CardCount())
				}
				return
			}
			if c.RememberedCount() != 1 || c.CardCount() != 1 {
				t.Fatalf("length %d remembered/cards = %d/%d, want 1/1", length, c.RememberedCount(), c.CardCount())
			}
			for _, index := range []uint32{0, length - 1} {
				got, err := c.ArrayGet(dst, index)
				if err != nil || got.Ref != child {
					t.Fatalf("length %d index %d = %v, %v; want child", length, index, got.Ref, err)
				}
			}
			if err := c.ArrayFill(dst, 0, Value{Kind: StorageRefNull}, length); err != nil {
				t.Fatal(err)
			}
			if c.RememberedCount() != 1 {
				t.Fatalf("length %d null replacement pruned remembered object on the bulk hot path", length)
			}
			if err := c.CollectMinor(nil); err != nil {
				t.Fatal(err)
			}
			if c.RememberedCount() != 0 || c.CardCount() != 0 {
				t.Fatalf("length %d collection retained remembered/card metadata: %d/%d", length, c.RememberedCount(), c.CardCount())
			}
		})
	}
}

func TestArrayCopyBulkReconcilePreservesNurseryEdgesOutsideRange(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{StressNurseryBytes: 1 << 20}, bulkTestTypes(t))
	dst, err := c.NewArrayDefault(3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(dst); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(dst, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	nulls, err := c.NewArrayDefault(3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArrayCopy(dst, 1, nulls, 1, 1); err != nil {
		t.Fatal(err)
	}
	if c.RememberedCount() != 1 {
		t.Fatal("bulk copy removed remembered object with nursery edge outside destination range")
	}
	if err := c.ArrayCopy(dst, 0, nulls, 0, 1); err != nil {
		t.Fatal(err)
	}
	if c.RememberedCount() != 1 {
		t.Fatal("bulk copy pruned remembered object on the hot path")
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if c.RememberedCount() != 0 || c.CardCount() != 0 {
		t.Fatal("collection retained remembered/card metadata after final nursery edge was removed")
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
