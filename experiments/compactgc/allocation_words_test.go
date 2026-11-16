package compactgc

import "testing"

func TestAllocationWordsPublicationMixedAccounting(t *testing.T) {
	c, e := NewWithTypes(128, 256, []Descriptor{{Kind: KindStruct, Align: 4, PayloadBytes: 4}})
	if e != nil {
		t.Fatal(e)
	}
	w := c.AllocationWords()
	native := func(value uint32) Ref {
		off := *w.Bump
		if off%16 != 0 || off > w.End || 16 > w.End-off {
			t.Fatal("no room")
		}
		// Same transaction as native code: all guards first, complete initialization,
		// start publication, authoritative bump. No allocation helper/counter.
		clear(c.arena[off : off+16])
		c.put32(off, 0)
		c.put32(off+4, value)
		c.starts[off/8] = true
		*w.Bump = off + 16
		return Ref(off)
	}
	a := native(77)
	b, e := c.NewStruct(0)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		n, _ := c.AllocationSnapshot()
		if n != 2 {
			t.Fatal("pending double count", n)
		}
	}
	if e = c.Minor([]*Ref{&a, &b}); e != nil {
		t.Fatal(e)
	}
	if *w.Bump != 16 {
		t.Fatal("cursor did not track retirement")
	}
	d := native(88)
	if e = c.Full([]*Ref{&a, &d}); e != nil {
		t.Fatal(e)
	}
	n, _ := c.AllocationSnapshot()
	if n != 3 {
		t.Fatal("retirement count", n)
	}
	p, e := c.Payload(a)
	if e != nil || len(p) != 4 || c.u32(uint32(a)+4) != 77 {
		t.Fatal("native payload lost", e)
	}
	p, e = c.Payload(d)
	if e != nil || len(p) != 4 || c.u32(uint32(d)+4) != 88 {
		t.Fatal("second native payload lost", e)
	}
}
