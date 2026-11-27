package wago

import "testing"

func FuzzDefinedTypeFingerprintEquivalence(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Add([]byte{0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		if len(data) > 16 {
			data = data[:16]
		}
		n := len(data)
		types := make([]DefinedTypeDescriptor, n*2)
		for copyIndex := 0; copyIndex < 2; copyIndex++ {
			for i, v := range data {
				d := DefinedTypeDescriptor{RecGroup: uint32(copyIndex), Final: v&8 != 0, Kind: CompositeTypeKind(v % 3)}
				value := ValueTypeDescriptor{Kind: ValueTypeI32}
				if v&1 != 0 {
					value = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: uint32(copyIndex*n + (i+1)%n)}}}
				}
				field := FieldTypeDescriptor{Mutable: v&4 != 0, Storage: StorageTypeDescriptor{Value: value}}
				switch d.Kind {
				case CompositeTypeFunction:
					d.Params = []ValueTypeDescriptor{value}
					d.Results = []ValueTypeDescriptor{{Kind: ValueTypeI32}}
				case CompositeTypeStruct:
					d.Fields = []FieldTypeDescriptor{field}
				case CompositeTypeArray:
					d.Array = field
				}
				types[copyIndex*n+i] = d
			}
		}
		keys, ok := definedTypeFingerprints(types)
		if !ok {
			t.Fatal("valid graph rejected")
		}
		for i := 0; i < n; i++ {
			if !definedTypeEquivalent(uint32(i), types, uint32(n+i), types) {
				t.Fatal("duplicate group differs")
			}
			if keys[i] != keys[n+i] {
				t.Fatalf("equal members %d and %d have different fingerprints", i, n+i)
			}
		}
		// Mutating the late member invalidates all projections of its group.
		types[len(types)-1].Final = !types[len(types)-1].Final
		for i := 0; i < n; i++ {
			if definedTypeEquivalent(uint32(i), types, uint32(n+i), types) {
				t.Fatal("late difference ignored")
			}
		}
	})
}
