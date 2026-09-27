package gc

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"
)

func checkStructLayoutAdmission(t *testing.T, desc TypeDesc, valid bool) {
	t.Helper()
	before := append([]FieldDesc(nil), desc.Fields...)
	if err := ValidateTypeDescs([]TypeDesc{desc}); (err == nil) != valid {
		t.Errorf("ValidateTypeDescs = %v, want accepted=%v", err, valid)
	}
	for _, cfg := range []Config{
		{Profile: ProfileThroughput},
		{Profile: ProfileTiny, TinyBlockBytes: 16},
	} {
		c, err := NewCollector(cfg, []TypeDesc{desc})
		if c != nil {
			c.Close()
		}
		if (err == nil) != valid {
			t.Errorf("NewCollector profile %d = %v, want accepted=%v", cfg.Profile, err, valid)
		}
		c, err = NewCollector(cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = c.AddTypes([]TypeDesc{desc})
		if (err == nil) != valid {
			t.Errorf("AddTypes profile %d = %v, want accepted=%v", cfg.Profile, err, valid)
		}
		if !valid && len(c.types) != 0 {
			t.Error("failed AddTypes changed collector descriptors")
		}
		c.Close()
	}
	if !reflect.DeepEqual(desc.Fields, before) {
		t.Fatal("validation reordered or changed the caller's fields")
	}
}

func TestStructFieldAlignmentAdmission(t *testing.T) {
	for _, kind := range []StorageKind{
		StorageI8, StorageI16, StorageI32, StorageI64, StorageF32, StorageF64, StorageV128,
		StorageRef, StorageRefNull, StorageFuncRef, StorageFuncRefNull, StorageExternRef, StorageExternRefNull,
	} {
		for _, alignment := range []uint32{1, 2, 4, 8, 16} {
			t.Run(fmt.Sprintf("kind=%d/align=%d", kind, alignment), func(t *testing.T) {
				desc, err := NewStructDesc(0, []StorageKind{kind})
				if err != nil {
					t.Fatal(err)
				}
				valid := alignment >= desc.Align
				desc.Align = alignment
				desc.Size = (desc.Size + alignment - 1) &^ (alignment - 1)
				checkStructLayoutAdmission(t, desc, valid)
			})
		}
	}
}

func TestStructLayoutSparseAndReorderedAdmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields []FieldDesc
		size   uint32
		align  uint32
		refs   bool
		valid  bool
	}{
		{"sparse reversed", []FieldDesc{{Kind: StorageI8, Offset: 262144}, {Kind: StorageI8, Offset: 0}}, 262145, 1, false, true},
		{"adjacent reversed", []FieldDesc{{Kind: StorageI64, Offset: 8}, {Kind: StorageRefNull, Offset: 4}, {Kind: StorageI32, Offset: 0}}, 16, 8, true, true},
		{"same offset", []FieldDesc{{Kind: StorageRefNull}, {Kind: StorageI32}}, 4, 4, true, false},
		{"contained range", []FieldDesc{{Kind: StorageRefNull, Offset: 4}, {Kind: StorageI64}}, 8, 8, true, false},
		{"scalar overlap", []FieldDesc{{Kind: StorageI16, Offset: 2}, {Kind: StorageI32}}, 4, 4, false, false},
		{"nonadjacent duplicate", []FieldDesc{{Kind: StorageRefNull}, {Kind: StorageI32, Offset: 16}, {Kind: StorageRefNull}}, 20, 4, true, false},
		{"misaligned offset", []FieldDesc{{Kind: StorageRefNull, Offset: 2}}, 8, 4, true, false},
		{"outside payload", []FieldDesc{{Kind: StorageI64, Offset: 8}}, 8, 8, false, false},
		{"overflowing extent", []FieldDesc{{Kind: StorageI64, Offset: ^uint32(7)}}, 8, 8, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desc := TypeDesc{ID: 0, Kind: KindStruct, Fields: tc.fields, Size: tc.size, Align: tc.align, HasRefs: tc.refs}
			checkStructLayoutAdmission(t, desc, tc.valid)
		})
	}
}

func TestStructLayoutRejectsOverlappingSubtypeExtension(t *testing.T) {
	parent, _ := NewStructDesc(0, []StorageKind{StorageRefNull})
	parent.Final = false
	child, _ := NewStructDesc(1, []StorageKind{StorageRefNull, StorageI32})
	child.HasSuper = true
	child.Fields[1].Offset = 0
	child.Size = 4
	if err := ValidateTypeDescs([]TypeDesc{parent, child}); err == nil {
		t.Fatal("accepted scalar extension overlapping an inherited reference")
	}
	for _, cfg := range []Config{{}, {Profile: ProfileTiny}} {
		c, err := NewCollector(cfg, []TypeDesc{parent, child})
		if c != nil {
			c.Close()
		}
		if err == nil {
			t.Errorf("NewCollector profile %d accepted overlapping extension", cfg.Profile)
		}
		c, err = NewCollector(cfg, []TypeDesc{parent})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddTypes([]TypeDesc{child}); err == nil {
			t.Errorf("AddTypes profile %d accepted overlapping extension", cfg.Profile)
		}
		c.Close()
	}
}

func TestStructAlignmentTinyEightByteBlocks(t *testing.T) {
	vector, _ := NewStructDesc(0, []StorageKind{StorageV128})
	for _, alignment := range []uint32{1, 8, 16} {
		vector.Align = alignment
		cfg := Config{Profile: ProfileTiny, TinyBlockBytes: 8, TinyHeapBytes: 256}
		c, err := NewCollector(cfg, []TypeDesc{vector})
		if c != nil {
			c.Close()
		}
		if err == nil {
			t.Errorf("Tiny accepted V128 with descriptor alignment %d and 8-byte blocks", alignment)
		}
		c, err = NewCollector(cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddTypes([]TypeDesc{vector}); err == nil {
			t.Errorf("Tiny appended V128 with descriptor alignment %d and 8-byte blocks", alignment)
		}
		c.Close()
	}
}

func TestStructLayoutMaximumReorderedFields(t *testing.T) {
	fields := make([]StorageKind, maxUnorderedStructFields)
	for i := range fields {
		fields[i] = StorageI32
	}
	desc, err := NewStructDesc(0, fields)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(desc.Fields)-1; i < j; i, j = i+1, j-1 {
		desc.Fields[i], desc.Fields[j] = desc.Fields[j], desc.Fields[i]
	}
	checkStructLayoutAdmission(t, desc, true)
}

func FuzzValidateStructFieldLayout(f *testing.F) {
	f.Add([]byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{4, 4, 0, 0, 0, 0, 0, 16, 0, 0, 0})
	f.Add([]byte{1, 4, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		kinds := [...]StorageKind{StorageI8, StorageI16, StorageRefNull, StorageI64, StorageV128}
		widths := [...]uint32{1, 2, 4, 8, 16}
		desc := TypeDesc{ID: 0, Kind: KindStruct, Align: 1 << (data[0] % 5)}
		var ends []uint32
		valid := true
		for at := 1; at+5 <= len(data) && len(desc.Fields) < 64; at += 5 {
			kind := data[at] % 5
			width := widths[kind]
			offset := binary.LittleEndian.Uint32(data[at+1:]) & 0x00ffffff &^ (width - 1)
			end := offset + width
			// Independent pairwise interval oracle; no sorting and no calls
			// to the production layout/overlap helpers.
			for j, prior := range desc.Fields {
				if offset < ends[j] && prior.Offset < end {
					valid = false
				}
			}
			valid = valid && desc.Align >= width
			desc.Fields = append(desc.Fields, FieldDesc{Kind: kinds[kind], Offset: offset})
			ends = append(ends, end)
			desc.HasRefs = desc.HasRefs || kind == 2
			if end > desc.Size {
				desc.Size = end
			}
		}
		desc.Size = (desc.Size + desc.Align - 1) &^ (desc.Align - 1)
		before := append([]FieldDesc(nil), desc.Fields...)
		err := ValidateTypeDescs([]TypeDesc{desc})
		if (err == nil) != valid {
			t.Fatalf("ValidateTypeDescs = %v, want accepted=%v for %+v", err, valid, desc)
		}
		if !reflect.DeepEqual(desc.Fields, before) {
			t.Fatal("validation modified fields")
		}
	})
}
