package gc

import "testing"

func TestValidateSubtypeLayoutRejectsIncompatibleStorage(t *testing.T) {
	refBase, _ := NewStructDesc(0, []StorageKind{StorageRefNull})
	refBase.Final = false
	numericChild, _ := NewStructDesc(1, []StorageKind{StorageI32})
	numericChild.HasSuper, numericChild.Super = true, 0

	numericBase, _ := NewStructDesc(0, []StorageKind{StorageI32})
	numericBase.Final = false
	shiftedChild := TypeDesc{
		ID: 1, Kind: KindStruct, Fields: []FieldDesc{{Kind: StorageI32, Offset: 4}},
		Size: 8, Align: 4, Final: true, HasSuper: true, Super: 0,
	}

	arrayBase, _ := NewArrayDesc(0, StorageRefNull)
	arrayBase.Final = false
	arrayChild, _ := NewArrayDesc(1, StorageI32)
	arrayChild.HasSuper, arrayChild.Super = true, 0

	for _, tc := range []struct {
		name  string
		types []TypeDesc
	}{
		{name: "hidden reference", types: []TypeDesc{refBase, numericChild}},
		{name: "shifted inherited field", types: []TypeDesc{numericBase, shiftedChild}},
		{name: "changed array element", types: []TypeDesc{arrayBase, arrayChild}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTypeDescs(tc.types); err == nil {
				t.Fatal("incompatible subtype layout was accepted")
			}
			if c, err := NewCollector(Config{}, tc.types); err == nil {
				c.Close()
				t.Fatal("collector accepted incompatible subtype layout")
			}
		})
	}
}

func TestValidateSubtypeLayoutAllowsReferenceNarrowing(t *testing.T) {
	base, _ := NewStructDesc(0, []StorageKind{StorageRefNull})
	base.Final = false
	child, _ := NewStructDesc(1, []StorageKind{StorageRef, StorageI32})
	child.HasSuper, child.Super = true, 0
	if err := ValidateTypeDescs([]TypeDesc{base, child}); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkValidateSubtypeLayouts(b *testing.B) {
	const depth = 16
	types := make([]TypeDesc, depth)
	for i := range types {
		types[i], _ = NewStructDesc(TypeID(i), []StorageKind{StorageI32})
		types[i].Final = false
		if i > 0 {
			types[i].HasSuper, types[i].Super = true, TypeID(i-1)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ValidateTypeDescs(types); err != nil {
			b.Fatal(err)
		}
	}
}
