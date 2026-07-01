package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func TestCompiledCodecRoundTripsManyEmptyDataSegments(t *testing.T) {
	input := &Compiled{
		Data: []DataInit{
			{Offset: OffsetInit{Base: 0}},
			{Offset: OffsetInit{Base: 1}},
			{Offset: OffsetInit{Base: 2}},
		},
	}

	got := roundTripCompiled(t, input)
	if len(got.Data) != len(input.Data) {
		t.Fatalf("Data length = %d, want %d", len(got.Data), len(input.Data))
	}
	for i, seg := range got.Data {
		if seg.Offset != input.Data[i].Offset {
			t.Fatalf("Data[%d].Offset = %+v, want %+v", i, seg.Offset, input.Data[i].Offset)
		}
		if len(seg.Bytes) != 0 {
			t.Fatalf("Data[%d].Bytes length = %d, want 0", i, len(seg.Bytes))
		}
	}
}

func TestCompiledCodecRoundTripsManyEmptyElementSegments(t *testing.T) {
	input := &Compiled{
		HasTable:  true,
		TableSize: 0,
		Elems: []ElemInit{
			{Offset: OffsetInit{Base: 0}},
			{Offset: OffsetInit{Base: 1}},
			{Offset: OffsetInit{Base: 2}},
		},
	}

	got := roundTripCompiled(t, input)
	if len(got.Elems) != len(input.Elems) {
		t.Fatalf("Elems length = %d, want %d", len(got.Elems), len(input.Elems))
	}
	for i, seg := range got.Elems {
		if seg.Offset != input.Elems[i].Offset {
			t.Fatalf("Elems[%d].Offset = %+v, want %+v", i, seg.Offset, input.Elems[i].Offset)
		}
		if len(seg.Funcs) != 0 {
			t.Fatalf("Elems[%d].Funcs length = %d, want 0", i, len(seg.Funcs))
		}
	}
}

func TestCompiledCodecRoundTripsEmptyStrings(t *testing.T) {
	input := &Compiled{
		Code:       []byte{0},
		Entry:      []int{0},
		NumImports: 1,
		Imports:    []string{""},
		Funcs:      []FuncSig{{}},
		FuncTypeID: []uint32{0, 0},
		Exports:    map[string]int{"": 1},
	}

	got := roundTripCompiled(t, input)
	if len(got.Imports) != 1 || got.Imports[0] != "" {
		t.Fatalf("Imports = %#v, want one empty string", got.Imports)
	}
	idx, ok := got.Exports[""]
	if !ok || idx != 1 {
		t.Fatalf("Exports[empty] = %d, %t; want 1, true", idx, ok)
	}
}

func TestCompiledCodecRoundTripsMinimalGCDescriptor(t *testing.T) {
	desc, err := gc.NewStructDesc(0, nil)
	if err != nil {
		t.Fatalf("NewStructDesc: %v", err)
	}
	input := &Compiled{GCTypeDescs: []gc.TypeDesc{desc}}

	got := roundTripCompiled(t, input)
	if len(got.GCTypeDescs) != 1 {
		t.Fatalf("GCTypeDescs length = %d, want 1", len(got.GCTypeDescs))
	}
	gotDesc := got.GCTypeDescs[0]
	if gotDesc.ID != 0 || gotDesc.Kind != gc.KindStruct || gotDesc.Size != 0 || gotDesc.Align != 1 || gotDesc.HasRefs {
		t.Fatalf("GCTypeDescs[0] = %+v, want minimal zero-field struct descriptor", gotDesc)
	}
	if len(gotDesc.Fields) != 0 {
		t.Fatalf("GCTypeDescs[0].Fields length = %d, want 0", len(gotDesc.Fields))
	}
}

func roundTripCompiled(t testing.TB, input *Compiled) *Compiled {
	t.Helper()
	encoded, err := input.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	var got Compiled
	if err := got.UnmarshalBinary(encoded); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	return &got
}
