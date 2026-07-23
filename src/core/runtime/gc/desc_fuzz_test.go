package gc

import "testing"

func FuzzTypeDescLayoutAndValidation(f *testing.F) {
	seeds := [][]byte{
		{},
		{byte(StorageI8), byte(StorageI16), byte(StorageI32), byte(StorageI64)},
		{byte(StorageRef), byte(StorageRefNull)},
		{0, byte(StorageRef), 255},
		{byte(StorageF32), byte(StorageF64), byte(StorageI8)},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32 {
			data = data[:32]
		}
		fields := make([]StorageKind, len(data))
		valid := true
		hasRefs := false
		for i, b := range data {
			k := StorageKind(b % 10)
			if b == 255 {
				k = StorageKind(255)
			}
			fields[i] = k
			if _, _, err := storageLayout(k); err != nil {
				valid = false
			}
			if isCollectorRefKind(k) {
				hasRefs = true
			}
		}

		d, err := NewStructDesc(0, fields)
		if !valid {
			if err == nil {
				t.Fatalf("NewStructDesc accepted invalid fields %#v as %+v", fields, d)
			}
			for _, k := range fields {
				if _, _, layoutErr := storageLayout(k); layoutErr != nil {
					if _, err := NewArrayDesc(0, k); err == nil {
						t.Fatalf("NewArrayDesc accepted invalid element %v", k)
					}
				}
			}
			return
		}
		if err != nil {
			t.Fatalf("NewStructDesc rejected valid fields %#v: %v", fields, err)
		}
		if d.HasRefs != hasRefs {
			t.Fatalf("HasRefs=%v, want %v for %#v", d.HasRefs, hasRefs, fields)
		}
		if err := ValidateTypeDescs([]TypeDesc{d}); err != nil {
			t.Fatalf("ValidateTypeDescs(struct): %v", err)
		}
		if _, err := StructSize(d); err != nil {
			t.Fatalf("StructSize: %v", err)
		}
		for _, off := range d.StructRefOffsets() {
			if off+4 > d.Size {
				t.Fatalf("ref offset %d out of bounds for size %d", off, d.Size)
			}
			if off%4 != 0 {
				t.Fatalf("ref offset %d is not 4-byte aligned", off)
			}
		}

		for _, k := range fields {
			ad, err := NewArrayDesc(0, k)
			if err != nil {
				t.Fatalf("NewArrayDesc(%v): %v", k, err)
			}
			if ad.HasRefs != isCollectorRefKind(k) {
				t.Fatalf("array HasRefs=%v, want %v", ad.HasRefs, isCollectorRefKind(k))
			}
			if err := ValidateTypeDescs([]TypeDesc{ad}); err != nil {
				t.Fatalf("ValidateTypeDescs(array): %v", err)
			}
			if _, err := ArraySize(ad, uint32(len(data))); err != nil {
				t.Fatalf("ArraySize: %v", err)
			}
		}
	})
}
