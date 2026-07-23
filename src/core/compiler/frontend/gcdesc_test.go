package frontend

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func val(v wasm.ValType) wasm.StorageType     { return wasm.StorageType{Val: v} }
func packed(p wasm.PackType) wasm.StorageType { return wasm.StorageType{Packed: true, Pack: p} }
func ref(nullable bool, h wasm.AbsHeapType) wasm.StorageType {
	return wasm.StorageType{Val: wasm.RefVal(wasm.Ref(nullable, wasm.AbsHeap(h), false))}
}
func concrete(nullable bool, idx uint32) wasm.StorageType {
	return concreteType(nullable, wasm.TypeIdx{Index: idx})
}
func concreteRec(nullable bool, idx uint32) wasm.StorageType {
	return concreteType(nullable, wasm.TypeIdx{Index: idx, Rec: true})
}
func concreteType(nullable bool, idx wasm.TypeIdx) wasm.StorageType {
	return wasm.StorageType{Val: wasm.RefVal(wasm.Ref(nullable, wasm.IndexedHeap(idx), false))}
}
func field(s wasm.StorageType) wasm.FieldType { return wasm.FieldType{Storage: s} }
func st(fields ...wasm.FieldType) wasm.SubType {
	return wasm.SubType{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: fields}}
}
func arr(elem wasm.StorageType) wasm.SubType {
	return wasm.SubType{Final: true, Comp: wasm.CompType{Kind: wasm.CompArray, Array: field(elem)}}
}
func fn() wasm.SubType { return wasm.SubType{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}} }

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
	types := []wasm.StorageType{packed(wasm.PackI8), packed(wasm.PackI16), val(wasm.I32), val(wasm.I64), val(wasm.F32), val(wasm.F64)}
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

func TestLowerErrors(t *testing.T) {
	if _, err := LowerGCTypeDescs([]wasm.RecType{{SubTypes: []wasm.SubType{st(field(val(wasm.V128)))}}}); err == nil {
		t.Fatal("expected v128 error")
	}
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
	if idx := *group[0].Metadata.Descriptor; !idx.Rec || idx.Index != 2 {
		t.Fatalf("descriptor index = %#v, want rec 2", idx)
	}
	if idx := group[1].Supers[0]; !idx.Rec || idx.Index != 0 {
		t.Fatalf("middle super index = %#v, want rec 0", idx)
	}
	if idx := group[2].Supers[0]; !idx.Rec || idx.Index != 1 {
		t.Fatalf("child super index = %#v, want rec 1", idx)
	}
	fieldHeap := group[2].Comp.Fields[1].Storage.Val.Ref.Heap
	if fieldHeap.Kind != wasm.HeapTypeIndex || !fieldHeap.Type.Rec || fieldHeap.Type.Index != 1 {
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
