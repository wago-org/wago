package gc

import "testing"

func TestValidateTypeDescs(t *testing.T) {
	pf, _ := NewStructDesc(0, []StorageKind{StorageI32, StorageI64})
	pf.Final = false
	ref, _ := NewStructDesc(1, []StorageKind{StorageRef})
	ref.HasSuper = true
	ref.Super = 0
	arr, _ := NewArrayDesc(2, StorageRefNull)
	if err := ValidateTypeDescs([]TypeDesc{pf, ref, arr}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTypeDescsRejectsMalformedSuperMetadata(t *testing.T) {
	structBase, _ := NewStructDesc(0, []StorageKind{StorageI32})
	structBase.Final = false
	structChild, _ := NewStructDesc(1, []StorageKind{StorageRef})
	structChild.HasSuper = true
	structChild.Super = 0
	arrayBase, _ := NewArrayDesc(0, StorageI32)
	arrayBase.Final = false
	arrayChild, _ := NewArrayDesc(1, StorageRefNull)
	arrayChild.HasSuper = true
	arrayChild.Super = 0
	funcBase := TypeDesc{ID: 0, Kind: KindFunc}
	funcChild := TypeDesc{ID: 1, Kind: KindFunc, HasSuper: true}

	valid := [][]TypeDesc{{structBase, structChild}, {arrayBase, arrayChild}, {funcBase, funcChild}}
	for _, descs := range valid {
		if err := ValidateTypeDescs(descs); err != nil {
			t.Fatalf("valid same-kind super rejected: %v", err)
		}
	}

	finalStruct, _ := NewStructDesc(0, []StorageKind{StorageI32})
	childOfFinal, _ := NewStructDesc(1, []StorageKind{StorageRef})
	childOfFinal.HasSuper = true
	childOfFinal.Super = 0
	structSuper, _ := NewStructDesc(0, []StorageKind{StorageI32})
	structSuper.Final = false
	arrayExtendsStruct, _ := NewArrayDesc(1, StorageI32)
	arrayExtendsStruct.HasSuper = true
	arrayExtendsStruct.Super = 0
	arraySuper, _ := NewArrayDesc(0, StorageI32)
	arraySuper.Final = false
	structExtendsArray, _ := NewStructDesc(1, []StorageKind{StorageI32})
	structExtendsArray.HasSuper = true
	structExtendsArray.Super = 0
	heapExtendsFunc, _ := NewStructDesc(1, []StorageKind{StorageI32})
	heapExtendsFunc.HasSuper = true
	heapExtendsFunc.Super = 0
	funcExtendsHeap := TypeDesc{ID: 1, Kind: KindFunc, HasSuper: true}

	cases := []struct {
		name string
		desc []TypeDesc
	}{
		{"final super", []TypeDesc{finalStruct, childOfFinal}},
		{"array extends struct", []TypeDesc{structSuper, arrayExtendsStruct}},
		{"struct extends array", []TypeDesc{arraySuper, structExtendsArray}},
		{"heap extends func", []TypeDesc{{ID: 0, Kind: KindFunc}, heapExtendsFunc}},
		{"func extends heap", []TypeDesc{structSuper, funcExtendsHeap}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTypeDescs(tc.desc); err == nil {
				t.Fatal("expected malformed super metadata error")
			}
		})
	}
}

func TestValidateTypeDescsFailures(t *testing.T) {
	base, _ := NewStructDesc(0, []StorageKind{StorageI32})
	cases := []struct {
		name string
		desc []TypeDesc
	}{
		{"id mismatch", []TypeDesc{{ID: 1, Kind: KindFunc}}},
		{"invalid kind", []TypeDesc{{ID: 0, Kind: 99}}},
		{"invalid super", []TypeDesc{{ID: 0, Kind: KindFunc, HasSuper: true, Super: 2}}},
		{"self super", []TypeDesc{{ID: 0, Kind: KindFunc, HasSuper: true, Super: 0}}},
		{"indirect super cycle", []TypeDesc{{ID: 0, Kind: KindFunc, HasSuper: true, Super: 1}, {ID: 1, Kind: KindFunc, HasSuper: true, Super: 0}}},
		{"func layout", []TypeDesc{{ID: 0, Kind: KindFunc, Size: 4}}},
		{"ref offset out of bounds", []TypeDesc{{ID: 0, Kind: KindStruct, Fields: []FieldDesc{{Kind: StorageRef, Offset: 8}}, Size: 4, Align: 4, HasRefs: true}}},
		{"bad array elem", []TypeDesc{{ID: 0, Kind: KindArray, Elem: StorageRef, ElemSize: 8, Align: 4, HasRefs: true}}},
		{"has refs mismatch", []TypeDesc{{ID: 0, Kind: KindStruct, Fields: base.Fields, Size: base.Size, Align: base.Align, HasRefs: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTypeDescs(tc.desc); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
