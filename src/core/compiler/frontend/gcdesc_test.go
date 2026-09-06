package frontend

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
)

func val(v wasm.ValType) wasm.StorageType     { return wasm.StorageVal(v) }
func packed(p wasm.PackType) wasm.StorageType { return wasm.StoragePacked(p) }
func ref(nullable bool, h wasm.AbsHeapType) wasm.StorageType {
	return wasm.StorageVal(wasm.RefVal(wasm.Ref(nullable, wasm.AbsHeap(h), false)))
}

func BenchmarkLowerGCTypeMetadataLarge(b *testing.B) {
	types := make([]wasm.RecType, 256)
	for i := range types {
		fields := make([]wasm.FieldType, 64)
		for j := range fields {
			fields[j] = field(val(wasm.I64))
		}
		types[i].SubTypes = []wasm.SubType{st(fields...)}
	}
	retained := uintptr(256) * unsafe.Sizeof(codegen.GCTypeLayout{})
	retained += uintptr(256*64) * unsafe.Sizeof(codegen.GCFieldLayout{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := LowerGCTypeMetadata(types); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(retained), "retained-layout-bytes")
}
func concrete(nullable bool, idx uint32) wasm.StorageType {
	return concreteType(nullable, wasm.TypeIdx{Index: idx})
}
func concreteRec(nullable bool, idx uint32) wasm.StorageType {
	return concreteType(nullable, wasm.TypeIdx{Index: idx, Rec: true})
}
func concreteType(nullable bool, idx wasm.TypeIdx) wasm.StorageType {
	return wasm.StorageVal(wasm.RefVal(wasm.Ref(nullable, wasm.IndexedHeap(idx), false)))
}
func field(s wasm.StorageType) wasm.FieldType { return wasm.NewFieldType(s, wasm.Const) }
func st(fields ...wasm.FieldType) wasm.SubType {
	return wasm.SubType{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: fields}}
}
func arr(elem wasm.StorageType) wasm.SubType {
	return wasm.SubType{Final: true, Comp: wasm.CompType{Kind: wasm.CompArray, Array: field(elem)}}
}
func fn() wasm.SubType { return wasm.SubType{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}} }

var flattenedGCTypeSink []flattenedGCType

func TestFlattenGCTypesUsesExactCompactPointerIndex(t *testing.T) {
	types := []wasm.RecType{
		{SubTypes: []wasm.SubType{fn(), st(field(val(wasm.I32)))}},
		{},
		{SubTypes: []wasm.SubType{arr(val(wasm.I64))}},
	}
	flat, hasLayouts := flattenGCTypes(types)
	if !hasLayouts {
		t.Fatal("flattened GC types lost struct/array layout presence")
	}
	if len(flat) != 3 || cap(flat) != len(flat) {
		t.Fatalf("flattened shape = len %d cap %d, want 3/3", len(flat), cap(flat))
	}
	want := []*wasm.SubType{&types[0].SubTypes[0], &types[0].SubTypes[1], &types[2].SubTypes[0]}
	for i := range flat {
		if flat[i].Source != want[i] {
			t.Fatalf("flat[%d] source = %p, want %p", i, flat[i].Source, want[i])
		}
	}
	if flat[0].RecBase != 0 || flat[0].RecLen != 2 || flat[1].RecBase != 0 || flat[1].RecLen != 2 || flat[2].RecBase != 2 || flat[2].RecLen != 1 {
		t.Fatalf("recursive group indexes = %#v", flat)
	}
	if got, want := unsafe.Sizeof(flattenedGCType{}), unsafe.Sizeof(uintptr(0))+2*unsafe.Sizeof(int(0)); got != want {
		t.Fatalf("flattened GC type size = %d, want pointer plus two indexes (%d)", got, want)
	}

	many := make([]wasm.RecType, 256)
	for i := range many {
		many[i].SubTypes = []wasm.SubType{fn()}
	}
	if _, hasLayouts := flattenGCTypes(many); hasLayouts {
		t.Fatal("function-only flattened types unexpectedly requested GC layouts")
	}
	allocs := testing.AllocsPerRun(100, func() {
		flattenedGCTypeSink, _ = flattenGCTypes(many)
	})
	if allocs > 1 {
		t.Fatalf("flattenGCTypes allocations = %.0f, want one exact backing allocation", allocs)
	}
}

func TestLowerGCTypeDescsFlattensRecGroupsAndPreservesIndexes(t *testing.T) {
	descs, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{fn(), st(field(val(wasm.I32)))}}, {SubTypes: []wasm.SubType{arr(val(wasm.I64))}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(descs) != 3 {
		t.Fatalf("len=%d", len(descs))
	}
	for i, d := range descs {
		if d.ID != gc.TypeID(i) {
			t.Fatalf("desc[%d].ID=%d", i, d.ID)
		}
	}
	if descs[0].Kind != gc.KindFunc || descs[1].Kind != gc.KindStruct || descs[2].Kind != gc.KindArray {
		t.Fatalf("bad kinds: %+v", descs)
	}
}

func TestLowerStructNumericAndPackedFields(t *testing.T) {
	descs, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{
		st(field(val(wasm.I32)), field(val(wasm.I64)), field(val(wasm.F32)), field(val(wasm.F64))),
		st(field(packed(wasm.PackI8)), field(packed(wasm.PackI16)), field(val(wasm.I32))),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	n := descs[0]
	if n.HasRefs {
		t.Fatal("numeric struct has refs")
	}
	if got := []uint32{n.Fields[0].Offset, n.Fields[1].Offset, n.Fields[2].Offset, n.Fields[3].Offset}; got[0] != 0 || got[1] != 8 || got[2] != 16 || got[3] != 24 {
		t.Fatalf("bad numeric offsets %v", got)
	}
	p := descs[1]
	if p.HasRefs {
		t.Fatal("packed struct has refs")
	}
	if p.Fields[0].Kind != gc.StorageI8 || p.Fields[1].Kind != gc.StorageI16 || p.Fields[2].Kind != gc.StorageI32 {
		t.Fatalf("bad packed kinds %+v", p.Fields)
	}
	if p.Fields[0].Offset != 0 || p.Fields[1].Offset != 2 || p.Fields[2].Offset != 4 {
		t.Fatalf("bad packed offsets %+v", p.Fields)
	}
}

func TestLowerMixedStructRefOffsets(t *testing.T) {
	descs, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{st(
		field(val(wasm.I32)),
		field(concrete(false, 0)),
		field(val(wasm.I64)),
		field(ref(true, wasm.HeapAny)),
	)}}})
	if err != nil {
		t.Fatal(err)
	}
	d := descs[0]
	if !d.HasRefs {
		t.Fatal("mixed struct should have refs")
	}
	off := d.StructRefOffsets()
	if len(off) != 2 || off[0] != 4 || off[1] != 16 {
		t.Fatalf("bad ref offsets %v", off)
	}
	if d.Fields[1].Kind != gc.StorageRef || d.Fields[3].Kind != gc.StorageRefNull {
		t.Fatalf("bad ref nullability kinds %+v", d.Fields)
	}
}

func TestLowerArraysPointerFreeAndPointerful(t *testing.T) {
	types := []wasm.StorageType{packed(wasm.PackI8), packed(wasm.PackI16), val(wasm.I32), val(wasm.I64), val(wasm.F32), val(wasm.F64), val(wasm.V128)}
	var subs []wasm.SubType
	for _, typ := range types {
		subs = append(subs, arr(typ))
	}
	subs = append(subs, arr(concrete(false, 0)), arr(ref(true, wasm.HeapEq)))
	descs, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: subs}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range types {
		if descs[i].HasRefs {
			t.Fatalf("array %d unexpectedly pointerful", i)
		}
	}
	vec := descs[len(types)-1]
	if vec.Elem != gc.StorageV128 || vec.ElemSize != 16 || vec.Align != 16 {
		t.Fatalf("v128 array descriptor = %+v", vec)
	}
	if !descs[len(types)].HasRefs || !descs[len(types)+1].HasRefs {
		t.Fatal("ref arrays should be pointerful")
	}
	if !descs[len(types)+1].ArrayElementsAreRefs() {
		t.Fatal("nullable ref array not scanned")
	}
}

func TestLowerRecursiveTypesDoNotExpandLayout(t *testing.T) {
	descs, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{
		st(field(concrete(true, 0))),
		arr(concrete(true, 0)),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if descs[0].Size != 4 || descs[1].ElemSize != 4 {
		t.Fatalf("recursive refs expanded layout: %+v %+v", descs[0], descs[1])
	}
}

func TestLowerMutuallyRecursiveTypesDoNotExpandLayout(t *testing.T) {
	descs, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{
		st(field(concrete(true, 1))),
		st(field(concrete(true, 0))),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if descs[0].Size != 4 || descs[1].Size != 4 {
		t.Fatalf("mutual refs expanded layout: %+v", descs)
	}
}

func TestLowerRecTypeIdxResolvesWithinCurrentGroup(t *testing.T) {
	base := st(field(val(wasm.I32)))
	base.Final = false
	child := st(field(concreteRec(true, 0)))
	child.Supers = []wasm.TypeIdx{{Index: 0, Rec: true}}
	descs, err := LowerGCTypeDescs([]wasm.RecType{
		{SubTypes: []wasm.SubType{fn()}},
		{SubTypes: []wasm.SubType{base, child}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(descs) != 3 {
		t.Fatalf("len=%d", len(descs))
	}
	if !descs[2].HasSuper || descs[2].Super != 1 {
		t.Fatalf("rec super lowered to %d has=%v, want flattened type 1", descs[2].Super, descs[2].HasSuper)
	}
	if descs[2].Fields[0].Kind != gc.StorageRefNull || descs[2].Fields[0].Offset != 0 {
		t.Fatalf("rec field lowered incorrectly: %+v", descs[2].Fields[0])
	}
}

func TestLowerRecSuperIndexAcrossMultiTypeGroup(t *testing.T) {
	base := st(field(val(wasm.I32)))
	base.Final = false
	mid := st(field(val(wasm.I64)))
	mid.Final = false
	child := st(field(val(wasm.I32)))
	child.Supers = []wasm.TypeIdx{{Index: 1, Rec: true}}

	descs, err := LowerGCTypeDescs([]wasm.RecType{
		{SubTypes: []wasm.SubType{fn()}},
		{SubTypes: []wasm.SubType{base, mid, child}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !descs[3].HasSuper || descs[3].Super != 2 {
		t.Fatalf("rec super lowered to %d has=%v, want flattened type 2", descs[3].Super, descs[3].HasSuper)
	}
}

func TestLowerRecRefIndexAcrossMultiTypeGroup(t *testing.T) {
	base := st(field(val(wasm.I32)))
	mid := arr(val(wasm.I64))
	child := st(field(concreteRec(false, 1)))

	descs, err := LowerGCTypeDescs([]wasm.RecType{
		{SubTypes: []wasm.SubType{fn()}},
		{SubTypes: []wasm.SubType{base, mid, child}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if descs[3].Fields[0].Kind != gc.StorageRef || descs[3].Fields[0].Offset != 0 {
		t.Fatalf("rec ref field lowered incorrectly: %+v", descs[3].Fields[0])
	}
}

func TestLowerRecSuperMetadataRejectsInvalidResolvedSuper(t *testing.T) {
	t.Run("final super", func(t *testing.T) {
		finalBase := st(field(val(wasm.I32)))
		child := st(field(val(wasm.I32)))
		child.Supers = []wasm.TypeIdx{{Index: 0, Rec: true}}
		if _, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{finalBase, child}}}); err == nil {
			t.Fatal("expected final recursive super error")
		}
	})

	t.Run("kind mismatch", func(t *testing.T) {
		arrayBase := arr(val(wasm.I32))
		arrayBase.Final = false
		child := st(field(val(wasm.I32)))
		child.Supers = []wasm.TypeIdx{{Index: 0, Rec: true}}
		if _, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{arrayBase, child}}}); err == nil {
			t.Fatal("expected recursive super kind mismatch error")
		}
	})
}

func TestLowerSubtypeSuperFinalMetadata(t *testing.T) {
	base := st(field(val(wasm.I32)))
	base.Final = false
	child := st(field(val(wasm.I32)))
	child.Final = false
	child.Supers = []wasm.TypeIdx{{Index: 0}}
	descs, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{base, child}}})
	if err != nil {
		t.Fatal(err)
	}
	if descs[0].Final || descs[1].Final {
		t.Fatal("final metadata not preserved")
	}
	if !descs[1].HasSuper || descs[1].Super != 0 {
		t.Fatalf("super metadata missing: has=%v super=%d", descs[1].HasSuper, descs[1].Super)
	}
}

func TestLowerFunctionTypesAreSentinels(t *testing.T) {
	descs, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{fn(), fn(), st(field(val(wasm.I32)))}}})
	if err != nil {
		t.Fatal(err)
	}
	if descs[0].Kind != gc.KindFunc || descs[1].Kind != gc.KindFunc || descs[2].ID != 2 || descs[2].Kind != gc.KindStruct {
		t.Fatalf("bad func sentinels: %+v", descs)
	}
}

func TestLowerV128StructField(t *testing.T) {
	descs, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{st(
		field(val(wasm.I32)), field(val(wasm.V128)), field(val(wasm.I64)),
	)}}})
	if err != nil {
		t.Fatal(err)
	}
	d := descs[0]
	if d.HasRefs || d.Align != 16 || d.Size != 48 || d.Fields[1].Kind != gc.StorageV128 || d.Fields[1].Offset != 16 || d.Fields[2].Offset != 32 {
		t.Fatalf("v128 struct descriptor = %+v", d)
	}
}

func TestLowerErrors(t *testing.T) {
	child := st(field(val(wasm.I32)))
	child.Supers = []wasm.TypeIdx{{Index: 9}}
	if _, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{child}}}); err == nil {
		t.Fatal("expected invalid super error")
	}
	if _, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{st(field(concrete(true, 9)))}}}); err == nil {
		t.Fatal("expected invalid referenced type error")
	}
	if _, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{st(field(concreteRec(true, 1)))}}}); err == nil {
		t.Fatal("expected invalid recursive referenced type error")
	}
	badRecSuper := st(field(val(wasm.I32)))
	badRecSuper.Supers = []wasm.TypeIdx{{Index: 1, Rec: true}}
	if _, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{badRecSuper}}}); err == nil {
		t.Fatal("expected invalid recursive super type error")
	}
}

func TestBuildGCTypeDescsFromModule(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{st(field(val(wasm.I32)))}}}}
	descs, err := BuildGCTypeDescs(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(descs) != 1 || descs[0].ID != 0 {
		t.Fatalf("bad descs %+v", descs)
	}
}

func TestLowerGCTypeMetadataPrecomputesCompilerLayout(t *testing.T) {
	types := []wasm.RecType{{SubTypes: []wasm.SubType{
		fn(),
		st(field(packed(wasm.PackI8)), field(val(wasm.I64)), field(val(wasm.V128)), field(ref(true, wasm.HeapAny))),
		arr(concreteRec(true, 1)),
	}}}
	metadata, err := LowerGCTypeMetadata(types)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Layouts) != 3 || metadata.Layouts[0].Type != &types[0].SubTypes[0] || metadata.Layouts[1].Type != &types[0].SubTypes[1] {
		t.Fatalf("layouts do not preserve flattened type identity: %+v", metadata.Layouts)
	}
	structLayout := metadata.Layouts[1]
	if structLayout.RecBase != 0 || structLayout.RecLen != 3 || !structLayout.Type.Final {
		t.Fatalf("struct recursive-group metadata = %+v", structLayout)
	}
	wantOffsets := []uint32{0, 8, 16, 32}
	wantSizes := []uint32{1, 8, 16, 4}
	wantSlots := []uint32{0, 1, 2, 4}
	for i, got := range structLayout.FieldLayout {
		if got.Offset != wantOffsets[i] || got.Size != wantSizes[i] || got.Slot != wantSlots[i] {
			t.Fatalf("field %d layout = %+v", i, got)
		}
		if got.Offset != metadata.Descs[1].Fields[i].Offset {
			t.Fatalf("field %d compiler/runtime offsets differ", i)
		}
	}
	if structLayout.FieldLayout[0].Align != 1 || structLayout.FieldLayout[2].Align != 16 || !structLayout.FieldLayout[3].CollectorRef {
		t.Fatalf("field layout classification = %+v", structLayout.FieldLayout)
	}
	if len(structLayout.ScanOffsets) != 1 || structLayout.ScanOffsets[0] != 32 || structLayout.FieldLayout[3].RefClass != codegen.GCRefCollector {
		t.Fatalf("struct scan metadata = %v, field=%+v", structLayout.ScanOffsets, structLayout.FieldLayout[3])
	}
	wantSize, err := gc.StructSize(metadata.Descs[1])
	if err != nil {
		t.Fatal(err)
	}
	if structLayout.ObjectSize != wantSize || structLayout.Align != metadata.Descs[1].Align || structLayout.PointerFree {
		t.Fatalf("struct aggregate layout = %+v", structLayout)
	}
	arrayLayout := metadata.Layouts[2]
	if arrayLayout.Type.Comp.Kind != wasm.CompArray || arrayLayout.ElemLayout.Size != 4 || arrayLayout.ElemLayout.Align != 4 || !arrayLayout.ElemLayout.CollectorRef || arrayLayout.PointerFree {
		t.Fatalf("array layout = %+v", arrayLayout)
	}
}

func TestGCTypeMetadataNativeConstructorSlotBoundary(t *testing.T) {
	fields := make([]wasm.FieldType, 64)
	for i := range fields {
		fields[i] = field(val(wasm.I64))
	}
	metadata, err := LowerGCTypeMetadata([]wasm.RecType{{SubTypes: []wasm.SubType{st(fields[:63]...), st(fields...)}}})
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Layouts[0].NativeAlloc {
		t.Fatal("63-field constructor should fit 64 slots including type index")
	}
	if metadata.Layouts[1].NativeAlloc {
		t.Fatal("64-field constructor exceeds 64 slots including type index")
	}
}

func TestBuildGCTypeDescsFromDecodedRecursiveTypeIndexes(t *testing.T) {
	mod := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType(nil, nil),
		[]byte{
			0x4e, 0x03, // rec group with three struct subtypes; flattened base is type 1.
			0x50, 0x00, 0x4d, 0x03, 0x5f, 0x00, // type 1: open struct, descriptor type 3.
			0x50, 0x01, 0x01, 0x5f, 0x01, 0x7f, 0x00, // type 2: open struct <: type 1, i32 field.
			0x4f, 0x01, 0x02, 0x5f, 0x02, 0x7f, 0x00, 0x63, 0x02, 0x00, // type 3: final struct <: type 2, i32 prefix plus (ref null type 2).
		},
	)))
	m, err := wasm.DecodeModule(mod)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	group := m.Types[1].SubTypes
	if idx, ok := group[0].Metadata.Descriptor.Get(); !ok || !idx.Rec || idx.Index != 2 {
		t.Fatalf("descriptor index = %#v, want rec 2", idx)
	}
	if idx := group[1].Supers[0]; !idx.Rec || idx.Index != 0 {
		t.Fatalf("middle super index = %#v, want rec 0", idx)
	}
	if idx := group[2].Supers[0]; !idx.Rec || idx.Index != 1 {
		t.Fatalf("child super index = %#v, want rec 1", idx)
	}
	fieldHeap := group[2].Comp.Fields[1].Storage().Val().Ref().Heap()
	if fieldHeap.Kind() != wasm.HeapTypeIndex || !fieldHeap.Type().Rec || fieldHeap.Type().Index != 1 {
		t.Fatalf("child field heap = %#v, want rec type 1", fieldHeap)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
	descs, err := BuildGCTypeDescs(m)
	if err != nil {
		t.Fatalf("BuildGCTypeDescs: %v", err)
	}
	if len(descs) != 4 {
		t.Fatalf("len(descs)=%d", len(descs))
	}
	if !descs[2].HasSuper || descs[2].Super != 1 {
		t.Fatalf("type 2 super = %d has=%v, want flattened type 1", descs[2].Super, descs[2].HasSuper)
	}
	if !descs[3].HasSuper || descs[3].Super != 2 {
		t.Fatalf("type 3 super = %d has=%v, want flattened type 2", descs[3].Super, descs[3].HasSuper)
	}
	if len(descs[3].Fields) != 2 || descs[3].Fields[1].Kind != gc.StorageRefNull || descs[3].Fields[1].Offset != 4 {
		t.Fatalf("type 3 fields = %+v, want i32 prefix plus nullable ref at offset 4", descs[3].Fields)
	}
}
