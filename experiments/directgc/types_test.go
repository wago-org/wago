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

func TestAdapterObjectViewAndLiveCount(t *testing.T) {
	c := typedHeap(t, []Descriptor{{Kind: KindArray, Align: 8, ElementSize: 1}})
	r, e := c.NewArray(0, 1)
	if e != nil {
		t.Fatal(e)
	}
	b, e := c.ObjectBytes(r)
	if e != nil || len(b) != 24 || cap(b) != 24 {
		t.Fatal("object extent", len(b), e)
	}
	if binary.LittleEndian.Uint32(b[4:]) != 24 || binary.LittleEndian.Uint32(b[8:]) != 1 {
		t.Fatal("native header")
	}
	if !c.IsYoung(r) || c.IsYoung(I31New(1)) || c.LiveObjects() != 1 {
		t.Fatal("allocation state")
	}
	if e = c.Minor([]*Ref{&r}); e != nil {
		t.Fatal(e)
	}
	if c.IsYoung(r) || c.LiveObjects() != 1 {
		t.Fatal("promoted state")
	}
	if e = c.Full(nil); e != nil {
		t.Fatal(e)
	}
	if c.LiveObjects() != 0 {
		t.Fatal("dead count")
	}
	if _, e = c.ObjectBytes(r); !errors.Is(e, ErrRef) {
		t.Fatal("stale bytes accepted", e)
	}
}

func TestOversizedOldAllocationAndBarrier(t *testing.T) {
	c, e := NewWithTypes(64, 512, []Descriptor{{Kind: KindArray, Align: 8, ElementSize: 4, ElementRef: true}, {Kind: KindStruct, Align: 8}})
	if e != nil {
		t.Fatal(e)
	}
	p, e := c.NewArray(0, 20)
	if e != nil {
		t.Fatal(e)
	}
	if c.IsYoung(p) || c.nurseryBump != c.nurseryStart {
		t.Fatal("oversized allocation used Eden")
	}
	h, e := c.Header(p)
	if e != nil || h.Flags&(FlagOld|FlagLarge) != (FlagOld|FlagLarge) {
		t.Fatal(h, e)
	}
	child, e := c.NewStruct(1)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.ArraySetRef(p, 19, child); e != nil {
		t.Fatal(e)
	}
	if e = c.Minor([]*Ref{&p}); e != nil {
		t.Fatal(e)
	}
	moved := get(t, c, p, 19)
	if moved == child || c.IsYoung(moved) || !c.IsValid(moved) {
		t.Fatal("oversized parent lost young child")
	}
	if e = c.Full([]*Ref{&p}); e != nil {
		t.Fatal(e)
	}
	if n, e := c.ArrayLen(p); e != nil || n != 20 {
		t.Fatal(n, e)
	}
}
func TestOversizedOOMAtomicAndNoNurseryBypass(t *testing.T) {
	c, e := NewWithTypes(32, 64, []Descriptor{{Kind: KindArray, Align: 8, ElementSize: 8}, {Kind: KindStruct, Align: 8, PayloadBytes: 16}})
	if e != nil {
		t.Fatal(e)
	}
	before := append([]byte(nil), c.arena...)
	n, o := c.nurseryBump, c.oldBump
	if _, e = c.NewArray(0, 100); !errors.Is(e, ErrOOM) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, c.arena) || n != c.nurseryBump || o != c.oldBump || c.LiveObjects() != 0 {
		t.Fatal("oversized failure mutated heap")
	}
	if _, e = c.NewStruct(1); e != nil {
		t.Fatal(e)
	}
	o = c.oldBump
	if _, e = c.NewStruct(1); !errors.Is(e, ErrOOM) {
		t.Fatal("full Eden bypassed", e)
	}
	if c.oldBump != o {
		t.Fatal("normal allocation spilled old")
	}
}

func TestForwardingScratchMemoryAccounting(t *testing.T) {
	c := heap(t, 1<<20, 4<<20)
	want := uint64(len(c.arena)) + 15 + uint64(len(c.starts)+len(c.dirty)) + 8*uint64(len(c.mark)) + 4*uint64(len(c.queue)) + 4*uint64(cap(c.remembered))
	if c.FixedBytes() != want {
		t.Fatal("backing accounting", c.FixedBytes(), want)
	}
	former := uint64(len(c.arena)) + 15 + uint64(len(c.starts)+len(c.dirty)) + 4*uint64(len(c.arena)/8) + 4*uint64(len(c.queue)) + 4*uint64(cap(c.remembered))
	saved := former - c.FixedBytes()
	if saved != 4*uint64(len(c.arena)/8)-8*uint64(len(c.mark)) || saved < 4<<20 {
		t.Fatal("forwarding memory not removed", saved)
	}
}
func TestForwardingPreservesPackedAuxAndAliasRoots(t *testing.T) {
	c := typedHeap(t, []Descriptor{{Kind: KindArray, Align: 8, ElementSize: 1}, {Kind: KindArray, Align: 8, ElementSize: 4, ElementRef: true}})
	packed, e := c.NewArray(0, 3)
	if e != nil {
		t.Fatal(e)
	}
	parent, e := c.NewArray(1, 2)
	if e != nil {
		t.Fatal(e)
	}
	edge(t, c, parent, 0, packed)
	edge(t, c, parent, 1, parent)
	if e = c.WriteBytes(packed, 0, []byte{9, 8, 7}); e != nil {
		t.Fatal(e)
	}
	for cycle := 0; cycle < 12; cycle++ {
		if e = c.Full([]*Ref{&packed, &packed, &parent, &parent}); e != nil {
			t.Fatal(e)
		}
		h, e := c.Header(packed)
		if e != nil || h.Aux != 3 || h.Size != 24 || h.Flags&FlagForwarded != 0 {
			t.Fatal("copied source forwarding into live header", h, e)
		}
		if get(t, c, parent, 0) != packed || get(t, c, parent, 1) != parent {
			t.Fatal("graph rewrite mismatch")
		}
		p, e := c.Payload(packed)
		if e != nil || !reflect.DeepEqual(p, []byte{9, 8, 7}) {
			t.Fatal("packed payload", p, e)
		}
	}
}

func TestPointerFreeTracingFlagsSurviveCollections(t *testing.T) {
	c := typedHeap(t, []Descriptor{
		{Kind: KindStruct, Align: 8, PayloadBytes: 8},
		{Kind: KindStruct, Align: 8, PayloadBytes: 8, RefOffsets: []uint32{0}},
		{Kind: KindArray, Align: 8, ElementSize: 4},
		{Kind: KindArray, Align: 8, ElementSize: 4, ElementRef: true},
	})
	var refs []Ref
	for _, id := range []TypeID{0, 1} {
		r, err := c.NewStruct(id)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, r)
	}
	for _, pair := range [][2]uint32{{2, 3}, {3, 3}, {3, 0}} {
		r, err := c.NewArray(TypeID(pair[0]), pair[1])
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, r)
	}
	edge(t, c, refs[1], 0, refs[0])
	edge(t, c, refs[3], 0, refs[0])
	roots := make([]*Ref, len(refs))
	for i := range refs {
		roots[i] = &refs[i]
	}
	wantFree := []bool{true, false, true, false, false}
	wantSlots := []int{0, 1, 0, 3, 0}
	for phase := 0; phase < 3; phase++ {
		for i, r := range refs {
			h, err := c.Header(r)
			if err != nil {
				t.Fatal(err)
			}
			if (h.Flags&FlagPointerFree != 0) != wantFree[i] {
				t.Fatalf("phase %d object %d: flags %x", phase, i, h.Flags)
			}
			slots := 0
			if err := c.eachRef(uint32(r), func(uint32) error { slots++; return nil }); err != nil {
				t.Fatal(err)
			}
			if slots != wantSlots[i] {
				t.Fatalf("phase %d object %d: slots %d", phase, i, slots)
			}
		}
		if get(t, c, refs[1], 0) != refs[0] || get(t, c, refs[3], 0) != refs[0] {
			t.Fatal("lost reference")
		}
		if phase == 0 {
			if err := c.Minor(roots); err != nil {
				t.Fatal(err)
			}
		}
		if phase == 1 {
			if err := c.Full(roots); err != nil {
				t.Fatal(err)
			}
		}
	}
}
