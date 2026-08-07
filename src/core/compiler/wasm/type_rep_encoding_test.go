package wasm

import (
	"math"
	"testing"
)

func TestPackedTypeRepresentationPreservesFullIndexes(t *testing.T) {
	for _, rec := range []bool{false, true} {
		want := TypeIdx{Index: math.MaxUint32, Rec: rec}
		heap := IndexedHeap(want)
		if heap.Kind() != HeapTypeIndex || heap.Type() != want {
			t.Fatalf("indexed heap round trip = kind %d index %+v, want %+v", heap.Kind(), heap.Type(), want)
		}
	}

	def := &DefType{GroupIndex: math.MaxUint32, Index: math.MaxUint32}
	heap := DefinedHeap(def)
	group, member, _, present := heap.Def()
	if !present || group != math.MaxUint32 || member != math.MaxUint32 {
		t.Fatalf("defined heap coordinates = %d.%d present=%t", group, member, present)
	}
	if _, valid := heap.DefCompKind(); valid {
		t.Fatal("out-of-range definition unexpectedly has a component kind")
	}
}

func TestPackedTypeRepresentationSemanticSurface(t *testing.T) {
	def := &DefType{GroupIndex: 7, Index: 1, Rec: RecType{SubTypes: []SubType{
		{Comp: CompType{Kind: CompFunc}},
		{Comp: CompType{Kind: CompStruct}},
	}}}
	defHeap := DefinedHeap(def)
	if group, member, _, present := defHeap.Def(); !present || group != 7 || member != 1 {
		t.Fatalf("definition = %d.%d present=%t", group, member, present)
	}
	if kind, valid := defHeap.DefCompKind(); !valid || kind != CompStruct {
		t.Fatalf("definition component = %d valid=%t", kind, valid)
	}

	for _, tc := range []struct {
		name     string
		value    ValType
		kind     ValTypeKind
		num      NumType
		nullable bool
		exact    bool
		heap     HeapType
	}{
		{"i32", I32, ValNum, NumI32, false, false, HeapType{}},
		{"i64", I64, ValNum, NumI64, false, false, HeapType{}},
		{"f32", F32, ValNum, NumF32, false, false, HeapType{}},
		{"f64", F64, ValNum, NumF64, false, false, HeapType{}},
		{"v128", V128, ValVec, 0, false, false, HeapType{}},
		{"bottom", Bot, ValBot, 0, false, false, HeapType{}},
		{"abstract", RefVal(Ref(true, AbsHeap(HeapAny), false)), ValRef, 0, true, false, AbsHeap(HeapAny)},
		{"indexed exact", RefVal(Ref(false, IndexedHeap(TypeIdx{Index: math.MaxUint32, Rec: true}), true)), ValRef, 0, false, true, IndexedHeap(TypeIdx{Index: math.MaxUint32, Rec: true})},
		{"defined", RefVal(Ref(false, defHeap, true)), ValRef, 0, false, true, defHeap},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value.Kind() != tc.kind || tc.value.Num() != tc.num {
				t.Fatalf("kind/num = %d/%d, want %d/%d", tc.value.Kind(), tc.value.Num(), tc.kind, tc.num)
			}
			if tc.kind == ValRef {
				rt := tc.value.Ref()
				if rt.Nullable() != tc.nullable || rt.Exact() != tc.exact || !equalHeapType(rt.Heap(), tc.heap) {
					t.Fatalf("reference round trip = %s", tc.value)
				}
			}
		})
	}
}

func TestPackedFieldRepresentationRoundTrip(t *testing.T) {
	storages := []StorageType{
		StorageVal(I32),
		StorageVal(V128),
		StorageVal(RefVal(Ref(true, AbsHeap(HeapEq), true))),
		StoragePacked(PackI8),
		StoragePacked(PackI16),
	}
	for _, storage := range storages {
		for _, mut := range []Mut{Const, Var} {
			field := NewFieldType(storage, mut)
			if field.Mut() != mut || !equalStorageType(field.Storage(), storage) {
				t.Fatalf("field round trip = storage %v mut %d", field.Storage(), field.Mut())
			}
		}
	}
}

func TestPackedReferenceBareFlagIsNotSemanticIdentity(t *testing.T) {
	bare := FuncRef
	expanded := RefVal(Ref(true, AbsHeap(HeapFunc), false))
	if bare == expanded {
		t.Fatal("binary spelling flag was not retained")
	}
	if !EqualValType(bare, expanded) {
		t.Fatal("equivalent bare and expanded funcref compared unequal")
	}
	if !bare.Ref().Bare() || expanded.Ref().Bare() {
		t.Fatal("bare spelling flag round trip failed")
	}
}

func TestPackedTypeAccessorsDoNotAllocate(t *testing.T) {
	field := NewFieldType(StorageVal(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: math.MaxUint32, Rec: true}), true))), Var)
	if got := testing.AllocsPerRun(1000, func() {
		storage := field.Storage()
		_ = storage.Packed()
		_ = storage.Val().Ref().Heap().Type()
		_ = field.Mut()
	}); got != 0 {
		t.Fatalf("accessors allocate %g times/run", got)
	}
}
