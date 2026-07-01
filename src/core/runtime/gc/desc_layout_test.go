package gc

import (
	"strings"
	"testing"
)

func TestDescriptorsAndLayout(t *testing.T) {
	pf, err := NewStructDesc(1, []StorageKind{StorageI32, StorageI64, StorageI8})
	if err != nil {
		t.Fatal(err)
	}
	if pf.HasRefs || !pf.PointerFree() {
		t.Fatal("pointer-free struct has refs")
	}
	if pf.Fields[0].Offset != 0 || pf.Fields[1].Offset != 8 || pf.Fields[2].Offset != 16 || pf.Size != 24 {
		t.Fatalf("bad struct offsets/size: %+v", pf)
	}
	mixed, _ := NewStructDesc(2, []StorageKind{StorageI8, StorageRef, StorageI64, StorageRefNull})
	if !mixed.HasRefs || mixed.PointerFree() {
		t.Fatal("mixed refs not detected")
	}
	off := mixed.StructRefOffsets()
	if len(off) != 2 || off[0] != 4 || off[1] != 16 {
		t.Fatalf("bad ref offsets %v", off)
	}
	arr, _ := NewArrayDesc(3, StorageI16)
	if arr.HasRefs || arr.ElemSize != 2 || arr.Align != 2 {
		t.Fatalf("bad packed array %+v", arr)
	}
	rarr, _ := NewArrayDesc(4, StorageRef)
	if !rarr.ArrayElementsAreRefs() || rarr.ElemSize != 4 {
		t.Fatalf("bad ref array %+v", rarr)
	}
	sz, _ := StructSize(pf)
	if sz != Align8(HeaderSize+pf.Size) || sz%8 != 0 {
		t.Fatalf("bad struct size %d", sz)
	}
	asz, _ := ArraySize(arr, 3)
	if asz != Align8(HeaderSize+6) || asz%8 != 0 {
		t.Fatalf("bad array size %d", asz)
	}
	if _, err := ArraySize(arr, ^uint32(0)); err == nil {
		t.Fatal("expected overflow")
	}
	overflowStruct := TypeDesc{
		ID:      0,
		Kind:    KindStruct,
		Fields:  []FieldDesc{{Kind: StorageI8, Offset: ^uint32(0) - 1}},
		Size:    ^uint32(0),
		Align:   1,
		Final:   true,
		HasRefs: false,
	}
	if _, err := StructSize(overflowStruct); err == nil || !strings.Contains(err.Error(), "struct size overflow") {
		t.Fatalf("StructSize overflow error = %v, want struct size overflow", err)
	}
	if err := ValidateTypeDescs([]TypeDesc{overflowStruct}); err == nil || !strings.Contains(err.Error(), "struct size overflow") {
		t.Fatalf("ValidateTypeDescs overflow error = %v, want struct size overflow", err)
	}
	if _, err := NewCollector(Config{}, []TypeDesc{overflowStruct}); err == nil || !strings.Contains(err.Error(), "struct size overflow") {
		t.Fatalf("NewCollector overflow error = %v, want struct size overflow", err)
	}
	if HeaderSize != 16 || PayloadOffset != 16 {
		t.Fatalf("header layout changed: %d %d", HeaderSize, PayloadOffset)
	}
}
