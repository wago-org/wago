package compactgc

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestDirtyViewCertifiedStorePreservesChildren(t *testing.T) {
	c, e := NewWithTypes(256, 512, []Descriptor{{Kind: KindStruct, Align: 4, PayloadBytes: 4}, {Kind: KindArray, Align: 4, ElementSize: 4, ElementRef: true}})
	if e != nil {
		t.Fatal(e)
	}
	parent, e := c.NewArray(1, 2)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Full([]*Ref{&parent}); e != nil {
		t.Fatal(e)
	}
	v := c.ReadView()
	if v.Dirty != uintptr(unsafe.Pointer(&c.dirty[0])) || v.DirtyCount != uint32(len(c.dirty)) {
		t.Fatal("foreign dirty backing")
	}
	a, e := c.NewStruct(0)
	if e != nil {
		t.Fatal(e)
	}
	b, e := c.NewStruct(0)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.ArraySetRef(parent, 0, a); e != nil {
		t.Fatal(e)
	}
	if !c.dirty[uint32(parent)/8] || len(c.remembered) != 1 {
		t.Fatal("helper failed to register parent")
	}
	p, e := c.Payload(parent)
	if e != nil {
		t.Fatal(e)
	}
	// Native-equivalent second store consumes the existing certificate only.
	// It never writes dirty or remembered metadata.
	binary.LittleEndian.PutUint32(p[4:8], uint32(b))
	bad := Ref(8)
	if c.Minor([]*Ref{&bad}) == nil {
		t.Fatal("bad-root collection succeeded")
	}
	if !c.dirty[uint32(parent)/8] || len(c.remembered) != 1 {
		t.Fatal("failure consumed certificate")
	}
	if e = c.Minor([]*Ref{&parent}); e != nil {
		t.Fatal(e)
	}
	if c.dirty[uint32(parent)/8] || len(c.remembered) != 0 {
		t.Fatal("successful minor retained certificate")
	}
	for _, full := range []bool{false, true} {
		if full {
			if e = c.Full([]*Ref{&parent}); e != nil {
				t.Fatal(e)
			}
		}
		p, e = c.Payload(parent)
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 2; i++ {
			r := Ref(binary.LittleEndian.Uint32(p[i*4:]))
			if !c.valid(r) || c.young(r) {
				t.Fatal("child lost", i, r)
			}
		}
	}
}
