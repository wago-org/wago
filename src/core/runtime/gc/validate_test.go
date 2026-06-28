package gc

import "testing"

func TestValidateTypeDescs(t *testing.T) {
	pf, _ := NewStructDesc(0, []StorageKind{StorageI32, StorageI64})
	ref, _ := NewStructDesc(1, []StorageKind{StorageRef})
	arr, _ := NewArrayDesc(2, StorageRefNull)
	arr.HasSuper = true
	arr.Super = 1
	if err := ValidateTypeDescs([]TypeDesc{pf, ref, arr}); err != nil {
		t.Fatal(err)
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
