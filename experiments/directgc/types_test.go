package directgc

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
	"unsafe"
)

func typedHeap(t *testing.T, ds []Descriptor) *Collector {
	t.Helper()
	c, e := NewWithTypes(4096, 4096, ds)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestNativeHeaderAndTypedMixedPayload(t *testing.T) {
	if unsafe.Sizeof(ObjHeader{}) != 16 || unsafe.Offsetof(ObjHeader{}.Size) != 4 || unsafe.Offsetof(ObjHeader{}.Aux) != 8 || unsafe.Offsetof(ObjHeader{}.Flags) != 12 {
		t.Fatal("header ABI")
	}
	ds := []Descriptor{{Kind: KindStruct, PayloadBytes: 48, Align: 16, RefOffsets: []uint32{40, 4}}}
	c := typedHeap(t, ds)
	ds[0].RefOffsets[0] = 0 // immutable deep copy, sorted privately.
	p, e := c.NewStruct(0)
	if e != nil {
		t.Fatal(e)
	}
	child, e := c.NewStruct(0)
	if e != nil {
		t.Fatal(e)
	}
	edge(t, c, p, 0, child)
	imm := I31New(-12345)
	edge(t, c, p, 1, imm)
	data := make([]byte, 16)
	for i := range data {
		data[i] = byte(i + 1)
	}
	if e = c.WriteBytes(p, 16, data); e != nil {
		t.Fatal(e)
	}
	if e = c.WriteBytes(p, 2, []byte{1, 2, 3}); !errors.Is(e, ErrField) {
		t.Fatal("reference overwrite accepted", e)
	}
	if e = c.WriteBytes(p, 48, []byte{1}); !errors.Is(e, ErrField) {
		t.Fatal(e)
	}
	h, e := c.Header(p)
	if e != nil || h != (ObjHeader{TypeID: 0, Size: 64}) {
		t.Fatal(h, e)
	}
	if e = c.Minor([]*Ref{&p, &imm}); e != nil {
		t.Fatal(e)
	}
	if p%16 != 0 || imm != I31New(-12345) || get(t, c, p, 1) != imm {
		t.Fatal("alignment/immediate changed")
	}
	b, e := c.Payload(p)
	if e != nil || !reflect.DeepEqual(b[16:32], data) || uintptr(unsafe.Pointer(&b[0]))%16 != 0 {
		t.Fatal("vector bytes changed", e)
	}
	h, e = c.Header(p)
	if e != nil || h.Flags&FlagOld == 0 {
		t.Fatal("old flag", h, e)
	}
	if id, e := c.ObjectType(p); e != nil || id != 0 {
		t.Fatal(id, e)
	}
	if _, e = c.ArrayLen(p); !errors.Is(e, ErrType) {
		t.Fatal(e)
	}
}
func TestPackedAndNumericArrayLengthsSurviveCopy(t *testing.T) {
	for _, width := range []uint32{1, 2, 4, 8, 16} {
		t.Run(string(rune('a'+width)), func(t *testing.T) {
			align := uint32(8)
			if width == 16 {
				align = 16
			}
			c := typedHeap(t, []Descriptor{{Kind: KindArray, Align: align, ElementSize: width}})
			r, e := c.NewArray(0, 3)
			if e != nil {
				t.Fatal(e)
			}
			payload, e := c.Payload(r)
			if e != nil {
				t.Fatal(e)
			}
			for i := range payload {
				payload[i] = byte(i + 1)
			}
			expected := append([]byte(nil), payload...)
			// Numeric bytes can resemble references and must never be traced.
			if width >= 4 {
				binary.LittleEndian.PutUint32(payload, 0xfffffffe)
				copy(expected, payload)
			}
			for cycle := 0; cycle < 4; cycle++ {
				if e = c.Full([]*Ref{&r}); e != nil {
					t.Fatal(e)
				}
				if n, e := c.ArrayLen(r); e != nil || n != 3 {
					t.Fatal("packed length lost", n, e)
				}
				got, e := c.Payload(r)
				if e != nil || !reflect.DeepEqual(got, expected) {
					t.Fatal("numeric bytes changed", e)
				}
				h, e := c.Header(r)
				want := (uint32(16) + 3*width + 7) &^ 7
				if e != nil || h.Size != want || h.Aux != 3 || h.Flags&(FlagOld|FlagPointerFree) != (FlagOld|FlagPointerFree) {
					t.Fatal(h, e)
				}
			}
			if e = c.ArraySetRef(r, 0, 0); !errors.Is(e, ErrField) {
				t.Fatal("numeric array accepted reference", e)
			}
		})
	}
}
func TestReferenceArraysI31AndRawRemember(t *testing.T) {
	c := typedHeap(t, []Descriptor{{Kind: KindArray, Align: 8, ElementSize: 4, ElementRef: true}, {Kind: KindStruct, Align: 8}})
	a, e := c.NewArray(0, 3)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Minor([]*Ref{&a}); e != nil {
		t.Fatal(e)
	}
	child, e := c.NewStruct(1)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := c.Payload(a)
	if e != nil {
		t.Fatal(e)
	}
	binary.LittleEndian.PutUint32(raw, uint32(child))
	if e = c.Remember(a); e != nil {
		t.Fatal(e)
	}
	imm := I31New(0x3fffffff)
	if e = c.ArraySetRef(a, 1, imm); e != nil {
		t.Fatal(e)
	}
	if e = c.ArraySetRef(a, 2, a); e != nil {
		t.Fatal(e)
	}
	if e = c.Minor([]*Ref{&a, &imm}); e != nil {
		t.Fatal(e)
	}
	moved := get(t, c, a, 0)
	if moved == child || !c.IsValid(moved) {
		t.Fatal("raw barrier lost child")
	}
	if e = c.Full([]*Ref{&a, &imm}); e != nil {
		t.Fatal(e)
	}
	if get(t, c, a, 1) != imm || get(t, c, a, 2) != a {
		t.Fatal("i31/cycle changed")
	}
	for _, v := range []int32{-1, -1073741824, 0, 1, 1073741823} {
		r := I31New(v)
		if !r.IsI31() || r.IsObj() || r.I31GetS() != v || !c.IsValid(r) {
			t.Fatal("i31 encoding", v, r)
		}
	}
	if _, e = c.Payload(imm); !errors.Is(e, ErrRef) {
		t.Fatal(e)
	}
	if e = c.ArraySetRef(a, 3, 0); !errors.Is(e, ErrField) {
		t.Fatal(e)
	}
}
func TestDescriptorValidationAndAtomicAppend(t *testing.T) {
	c := typedHeap(t, nil)
	good := Descriptor{Kind: KindStruct, Align: 8, PayloadBytes: 8, RefOffsets: []uint32{0}}
	bad := []Descriptor{
		{Kind: 99, Align: 8}, {Kind: KindStruct, Align: 4},
		{Kind: KindStruct, Align: 8, PayloadBytes: 4, RefOffsets: []uint32{1}},
		{Kind: KindStruct, Align: 8, PayloadBytes: 4, RefOffsets: []uint32{4}},
		{Kind: KindStruct, Align: 8, PayloadBytes: 4, RefOffsets: []uint32{0, 0}},
		{Kind: KindArray, Align: 8, ElementSize: 0},
		{Kind: KindArray, Align: 8, ElementSize: 8, ElementRef: true},
		{Kind: KindArray, Align: 8, ElementSize: 16},
	}
	for _, d := range bad {
		n := len(c.types)
		if _, e := c.AddTypes([]Descriptor{good, d}); !errors.Is(e, ErrType) {
			t.Fatal("descriptor accepted", d, e)
		}
		if len(c.types) != n {
			t.Fatal("partial type batch committed")
		}
	}
	id, e := c.AddTypes([]Descriptor{good})
	if e != nil || id != 0 {
		t.Fatal(id, e)
	}
	good.RefOffsets[0] = 4
	if c.types[0].RefOffsets[0] != 0 {
		t.Fatal("descriptor alias")
	}
	if _, e = c.NewArray(0, 1); !errors.Is(e, ErrType) {
		t.Fatal(e)
	}
	if _, e = c.NewStruct(100); !errors.Is(e, ErrType) {
		t.Fatal(e)
	}
	_, e = c.AddTypes([]Descriptor{{Kind: KindArray, Align: 16, ElementSize: 16}})
	if e != nil {
		t.Fatal(e)
	}
	before := c.nurseryBump
	if _, e = c.NewArray(1, ^uint32(0)); !errors.Is(e, ErrOOM) {
		t.Fatal(e)
	}
	if c.nurseryBump != before {
		t.Fatal("overflow mutated bump")
	}
}
