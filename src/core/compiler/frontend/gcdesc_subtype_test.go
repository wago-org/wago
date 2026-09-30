package frontend

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestLowerValidatedGCSubtypes(t *testing.T) {
	for _, tc := range []struct {
		name          string
		parent, child wasm.StorageType
	}{
		{"i8", packed(wasm.PackI8), packed(wasm.PackI8)},
		{"i16", packed(wasm.PackI16), packed(wasm.PackI16)},
		{"i32", val(wasm.I32), val(wasm.I32)},
		{"i64", val(wasm.I64), val(wasm.I64)},
		{"f32", val(wasm.F32), val(wasm.F32)},
		{"f64", val(wasm.F64), val(wasm.F64)},
		{"v128", val(wasm.V128), val(wasm.V128)},
		{"collector narrowing", ref(true, wasm.HeapAny), ref(false, wasm.HeapEq)},
		{"function narrowing", ref(true, wasm.HeapFunc), concrete(false, 0)},
		{"extern narrowing", ref(true, wasm.HeapExtern), ref(false, wasm.HeapExtern)},
		{"abstract bottom", ref(true, wasm.HeapStruct), ref(false, wasm.HeapNone)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, array := range []bool{false, true} {
				parent, child := st(field(tc.parent)), st(field(tc.child), field(val(wasm.V128)))
				if array {
					parent, child = arr(tc.parent), arr(tc.child)
				}
				parent.Final = false
				child.Supers = []wasm.TypeIdx{{Index: 1}}
				m := &wasm.Module{Types: []wasm.RecType{
					{SubTypes: []wasm.SubType{fn()}},
					{SubTypes: []wasm.SubType{parent}},
					{SubTypes: []wasm.SubType{child}},
				}}
				if err := wasm.ValidateModule(m); err != nil {
					t.Fatalf("array=%v: Wasm validation: %v", array, err)
				}
				metadata, err := BuildGCTypeMetadata(m)
				if err != nil {
					t.Fatalf("array=%v: lowering: %v", array, err)
				}
				if metadata.Descs[2].Super != 1 {
					t.Fatal("lost direct supertype")
				}
				if array {
					if metadata.Layouts[1].ElemLayout.Size != metadata.Layouts[2].ElemLayout.Size ||
						metadata.Layouts[1].ElemLayout.Align != metadata.Layouts[2].ElemLayout.Align ||
						metadata.Layouts[1].ElemLayout.RefClass != metadata.Layouts[2].ElemLayout.RefClass {
						t.Fatal("inherited array representation changed")
					}
				} else if metadata.Descs[1].Fields[0].Offset != metadata.Descs[2].Fields[0].Offset {
					t.Fatal("inherited field moved")
				}
			}
		})
	}
}

func TestLowerGCSubtypeMutabilityBoundary(t *testing.T) {
	for _, array := range []bool{false, true} {
		for _, mutable := range []bool{false, true} {
			for _, narrow := range []bool{false, true} {
				mut := wasm.Const
				if mutable {
					mut = wasm.Var
				}
				parentField := wasm.NewFieldType(ref(true, wasm.HeapAny), mut)
				childField := wasm.NewFieldType(ref(!narrow, wasm.HeapAny), mut)
				parent, child := st(parentField), st(childField)
				if array {
					parent.Comp = wasm.CompType{Kind: wasm.CompArray, Array: parentField}
					child.Comp = wasm.CompType{Kind: wasm.CompArray, Array: childField}
				}
				parent.Final = false
				child.Supers = []wasm.TypeIdx{{Index: 0, Rec: true}}
				m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{parent, child}}}}
				wantValid := !mutable || !narrow
				if err := wasm.ValidateModule(m); (err == nil) != wantValid {
					t.Fatalf("array=%v mutable=%v narrow=%v: Wasm validation = %v, want valid=%v", array, mutable, narrow, err, wantValid)
				}
				// Mutability is intentionally absent from TypeDesc. Lowering is
				// not a replacement for semantic Wasm module validation.
				if _, err := BuildGCTypeDescs(m); err != nil {
					t.Fatalf("physically compatible storage rejected: %v", err)
				}
			}
		}
	}
}

func TestLowerValidatedRecursiveSubtypeChain(t *testing.T) {
	base := st(field(ref(true, wasm.HeapAny)))
	mid := st(field(concreteRec(false, 2)))                              // forward reference to the leaf
	leaf := st(field(concreteRec(false, 2)), field(packed(wasm.PackI8))) // self reference
	base.Final, mid.Final = false, false
	mid.Supers = []wasm.TypeIdx{{Index: 0, Rec: true}}
	leaf.Supers = []wasm.TypeIdx{{Index: 1, Rec: true}}
	m := &wasm.Module{Types: []wasm.RecType{
		{SubTypes: []wasm.SubType{fn()}},
		{SubTypes: []wasm.SubType{base, mid, leaf}},
	}}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	descs, err := BuildGCTypeDescs(m)
	if err != nil {
		t.Fatal(err)
	}
	if descs[2].Super != 1 || descs[3].Super != 2 || descs[3].Fields[0].Kind != gc.StorageRef {
		t.Fatalf("incorrect recursive subtype layout: %+v", descs)
	}
}

func TestLowerRejectsIncompatibleGCSubtypes(t *testing.T) {
	for _, tc := range []struct {
		name          string
		parent, child wasm.SubType
	}{
		// The original recursive-index fixtures changed i32 to a reference,
		// and i64 to i32. Neither is a valid Wasm inherited field subtype.
		{"scalar to reference", st(field(val(wasm.I32))), st(field(concreteRec(true, 0)))},
		{"i64 to i32", st(field(val(wasm.I64))), st(field(val(wasm.I32)))},
		{"missing field", st(field(ref(true, wasm.HeapAny))), st()},
		{"array reference to scalar", arr(ref(true, wasm.HeapAny)), arr(val(wasm.I32))},
		{"array reference widening", arr(ref(false, wasm.HeapAny)), arr(ref(true, wasm.HeapAny))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.parent.Final = false
			tc.child.Supers = []wasm.TypeIdx{{Index: 0, Rec: true}}
			m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{tc.parent, tc.child}}}}
			if err := wasm.ValidateModule(m); err == nil {
				t.Fatal("accepted invalid Wasm subtype")
			}
			if _, err := BuildGCTypeDescs(m); err == nil {
				t.Fatal("lowering accepted incompatible storage")
			}
		})
	}
}
