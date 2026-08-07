package gc

import "testing"

func opaqueReferenceTypes(t *testing.T) []TypeDesc {
	t.Helper()
	kinds := []StorageKind{
		StorageFuncRef, StorageFuncRefNull,
		StorageExternRef, StorageExternRefNull,
		StorageRef, StorageRefNull,
	}
	types := make([]TypeDesc, len(kinds)+1)
	obj, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	types[0] = obj
	for i, kind := range kinds {
		types[i+1], err = NewArrayDesc(TypeID(i+1), kind)
		if err != nil {
			t.Fatal(err)
		}
	}
	return types
}

func TestReferenceStorageCompatibilityClasses(t *testing.T) {
	for _, kind := range []StorageKind{
		StorageRef, StorageRefNull,
		StorageFuncRef, StorageFuncRefNull,
		StorageExternRef, StorageExternRefNull,
	} {
		if !referenceStorageCompatible(kind, kind) {
			t.Fatalf("exact storage %d is incompatible with itself", kind)
		}
	}
	for _, pair := range [][2]StorageKind{
		{StorageRefNull, StorageRef},
		{StorageFuncRefNull, StorageFuncRef},
		{StorageExternRefNull, StorageExternRef},
	} {
		if !referenceStorageCompatible(pair[0], pair[1]) {
			t.Fatalf("non-null to nullable widening %d <- %d rejected", pair[0], pair[1])
		}
		if referenceStorageCompatible(pair[1], pair[0]) {
			t.Fatalf("nullable to non-null narrowing %d <- %d accepted", pair[1], pair[0])
		}
	}
	if referenceStorageCompatible(StorageFuncRefNull, StorageExternRef) || referenceStorageCompatible(StorageRefNull, StorageFuncRef) {
		t.Fatal("unrelated reference storage classes were compatible")
	}
}

func TestOpaqueReferenceArrayCopyWideningAndAtomicity(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{}, opaqueReferenceTypes(t))
	for _, tc := range []struct {
		name              string
		nonNull, nullable TypeID
		value             Value
	}{
		{name: "func", nonNull: 1, nullable: 2, value: Value{Kind: StorageFuncRef, Bits: 0x1234}},
		{name: "extern", nonNull: 3, nullable: 4, value: Value{Kind: StorageExternRef, Bits: 0x5678}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, err := c.NewArray(tc.nonNull, 2, tc.value)
			if err != nil {
				t.Fatal(err)
			}
			dst, err := c.NewArrayDefault(tc.nullable, 2)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ArrayCopy(dst, 0, src, 0, 2); err != nil {
				t.Fatalf("widening copy: %v", err)
			}
			for i := uint32(0); i < 2; i++ {
				got, err := c.ArrayGet(dst, i)
				if err != nil || got.Bits != tc.value.Bits {
					t.Fatalf("dst[%d]=%#x,%v want %#x", i, got.Bits, err, tc.value.Bits)
				}
			}

			narrowDst, err := c.NewArray(tc.nonNull, 2, tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ArrayCopy(narrowDst, 0, dst, 0, 2); err == nil {
				t.Fatal("nullable-to-non-null copy succeeded")
			}
			for i := uint32(0); i < 2; i++ {
				got, err := c.ArrayGet(narrowDst, i)
				if err != nil || got.Bits != tc.value.Bits {
					t.Fatalf("rejected copy mutated dst[%d]=%#x,%v", i, got.Bits, err)
				}
			}
		})
	}
}

func TestArrayInitDataRejectsEveryReferenceStorageClass(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{}, opaqueReferenceTypes(t))
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		typeID TypeID
		init   Value
	}{
		{1, Value{Kind: StorageFuncRef, Bits: 1}},
		{2, Value{Kind: StorageFuncRefNull}},
		{3, Value{Kind: StorageExternRef, Bits: 2}},
		{4, Value{Kind: StorageExternRefNull}},
		{5, RefValue(child)},
		{6, Value{Kind: StorageRefNull}},
	}
	for _, tc := range cases {
		arr, err := c.NewArray(tc.typeID, 1, tc.init)
		if err != nil {
			t.Fatalf("type %d construct: %v", tc.typeID, err)
		}
		before, err := c.ArrayGet(arr, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ArrayInitData(arr, 0, []byte{1, 2, 3, 4, 5, 6, 7, 8}, 0, 1); err == nil {
			t.Fatalf("type %d accepted raw data initialization", tc.typeID)
		}
		after, err := c.ArrayGet(arr, 0)
		if err != nil || after != before {
			t.Fatalf("type %d rejected init mutated value: before=%+v after=%+v err=%v", tc.typeID, before, after, err)
		}
	}
}
