package directgc

import (
	"reflect"
	"testing"
)

func verifyFixture(t *testing.T) (*Collector, Ref) {
	t.Helper()
	c, e := NewWithTypes(128, 256, []Descriptor{{Kind: KindStruct, PayloadBytes: 8, Align: 8, RefOffsets: []uint32{0}}})
	if e != nil {
		t.Fatal(e)
	}
	r, e := c.NewStruct(0)
	if e != nil {
		t.Fatal(e)
	}
	return c, r
}
func TestVerifyCollectionAndRemembered(t *testing.T) {
	c, r := verifyFixture(t)
	roots := []*Ref{&r, &r}
	for _, full := range []bool{false, true, false, true} {
		if e := c.Verify(roots); e != nil {
			t.Fatal(e)
		}
		if e := c.Minor(roots); e != nil {
			t.Fatal(e)
		}
		child, e := c.NewStruct(0)
		if e != nil {
			t.Fatal(e)
		}
		if e = c.SetRef(r, 0, child); e != nil {
			t.Fatal(e)
		}
		if e = c.SetRef(child, 0, r); e != nil {
			t.Fatal(e)
		}
		if e = c.Verify(roots); e != nil {
			t.Fatal(e)
		}
		if full {
			e = c.Full(roots)
		} else {
			e = c.Minor(roots)
		}
		if e != nil {
			t.Fatal(e)
		}
		if e = c.Verify(roots); e != nil {
			t.Fatal(e)
		}
	}
}
func TestVerifyCorruptionReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt func(*Collector, Ref)
	}{
		{"type", func(c *Collector, r Ref) { c.put32(uint32(r), 999) }},
		{"extent", func(c *Collector, r Ref) { c.put32(uint32(r)+4, ^uint32(0)) }},
		{"flags", func(c *Collector, r Ref) { c.put32(uint32(r)+12, FlagPointerFree) }},
		{"missing start", func(c *Collector, r Ref) { c.starts[uint32(r)/8] = false }},
		{"interior start", func(c *Collector, r Ref) { c.starts[uint32(r)/8+1] = true }},
		{"edge", func(c *Collector, r Ref) { c.put32(uint32(r)+16, 0x100000) }},
		{"descriptor", func(c *Collector, r Ref) { c.types[0].RefOffsets[0] = 999 }},
		{"cursor", func(c *Collector, r Ref) { c.nurseryBump = 17 }},
		{"dirty", func(c *Collector, r Ref) { c.dirty[uint32(r)/8] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, r := verifyFixture(t)
			tc.corrupt(c, r)
			arena := append([]byte(nil), c.arena...)
			starts := append([]bool(nil), c.starts...)
			dirty := append([]bool(nil), c.dirty...)
			mark := append([]uint64(nil), c.mark...)
			queue := append([]Ref(nil), c.queue...)
			before := *c
			if e := c.Verify([]*Ref{&r}); e == nil {
				t.Fatal("accepted corruption")
			}
			if !reflect.DeepEqual(arena, c.arena) || !reflect.DeepEqual(starts, c.starts) || !reflect.DeepEqual(dirty, c.dirty) || !reflect.DeepEqual(mark, c.mark) || !reflect.DeepEqual(queue, c.queue) || !reflect.DeepEqual(before, *c) {
				t.Fatal("verification mutated heap")
			}
		})
	}
}
func TestVerifyMissingBarrierAndRoots(t *testing.T) {
	c, r := verifyFixture(t)
	if e := c.Minor([]*Ref{&r}); e != nil {
		t.Fatal(e)
	}
	child, _ := c.NewStruct(0)
	c.put32(uint32(r)+16, uint32(child))
	if e := c.Verify(nil); e == nil {
		t.Fatal("missing barrier accepted")
	}
	c.rememberParent(r)
	if e := c.Verify([]*Ref{&r}); e != nil {
		t.Fatal(e)
	}
	bad := Ref(24)
	for _, roots := range [][]*Ref{{nil}, {&bad}} {
		if e := c.Verify(roots); e == nil {
			t.Fatal("bad root accepted")
		}
	}
}
func TestVerifyOOMRetry(t *testing.T) {
	c, e := NewWithTypes(64, 32, []Descriptor{{Kind: KindStruct, PayloadBytes: 8, Align: 8}})
	if e != nil {
		t.Fatal(e)
	}
	a, _ := c.NewStruct(0)
	b, _ := c.NewStruct(0)
	if e = c.Full([]*Ref{&a, &b}); e != ErrOOM {
		t.Fatal(e)
	}
	if e = c.Verify([]*Ref{&a, &b}); e != nil {
		t.Fatal(e)
	}
	if e = c.Full([]*Ref{&a}); e != nil {
		t.Fatal(e)
	}
	if e = c.Verify([]*Ref{&a}); e != nil {
		t.Fatal(e)
	}
}

func TestVerifyRememberedOverflowAndArrays(t *testing.T) {
	c, e := NewWithTypes(32, 128<<10, []Descriptor{{Kind: KindArray, Align: 8, ElementSize: 4, ElementRef: true}, {Kind: KindStruct, Align: 8}})
	if e != nil {
		t.Fatal(e)
	}
	child, _ := c.NewStruct(1)
	for i := 0; i < rememberedParentCapacity+1; i++ {
		parent, e := c.NewArray(0, 8)
		if e != nil {
			t.Fatal(e)
		}
		if e = c.SetRef(parent, 0, child); e != nil {
			t.Fatal(e)
		}
	}
	if !c.rememberedOverflow {
		t.Fatal("overflow not exercised")
	}
	if e = c.Verify([]*Ref{&child}); e != nil {
		t.Fatal(e)
	}
	if e = c.Minor([]*Ref{&child}); e != nil {
		t.Fatal(e)
	}
	if e = c.Verify([]*Ref{&child}); e != nil {
		t.Fatal(e)
	}
}
func TestVerifyArrayExtentAndInactiveStart(t *testing.T) {
	c, e := NewWithTypes(128, 256, []Descriptor{{Kind: KindArray, Align: 16, ElementSize: 16}})
	if e != nil {
		t.Fatal(e)
	}
	r, e := c.NewArray(0, 2)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Verify([]*Ref{&r}); e != nil {
		t.Fatal(e)
	}
	c.put32(uint32(r)+8, ^uint32(0))
	if e = c.Verify(nil); e == nil {
		t.Fatal("array length overflow accepted")
	}
	c.put32(uint32(r)+8, 2)
	c.starts[c.oldStart[1]/8] = true
	if e = c.Verify(nil); e == nil {
		t.Fatal("inactive start accepted")
	}
	c.starts[c.oldStart[1]/8] = false
	i31 := Ref(1)
	null := Ref(0)
	if e = c.Verify([]*Ref{&i31, &null}); e != nil {
		t.Fatal(e)
	}
}

func TestVerifyLargeFlagMatchesExtent(t *testing.T) {
	c, r := verifyFixture(t)
	if e := c.Minor([]*Ref{&r}); e != nil {
		t.Fatal(e)
	}
	flags := c.u32(uint32(r) + 12)
	c.put32(uint32(r)+12, flags|FlagLarge)
	if e := c.Verify(nil); e == nil {
		t.Fatal("small old object marked large accepted")
	}
	c.put32(uint32(r)+12, flags)
	bigID, e := c.AddTypes([]Descriptor{{Kind: KindArray, Align: 8, ElementSize: 8}})
	if e != nil {
		t.Fatal(e)
	}
	big, e := c.NewArray(bigID, 20)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Verify(nil); e != nil {
		t.Fatal(e)
	}
	flags = c.u32(uint32(big) + 12)
	c.put32(uint32(big)+12, flags&^FlagLarge)
	if e = c.Verify(nil); e == nil {
		t.Fatal("large object without large flag accepted")
	}
}
