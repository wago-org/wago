package directgc

import (
	"bytes"
	"errors"
	"reflect"
	"slices"
	"testing"
)

func rememberedHeap(t *testing.T, old uint32) *Collector {
	t.Helper()
	c, e := NewWithTypes(32, old, []Descriptor{{Kind: KindArray, Align: 8, ElementSize: 4, ElementRef: true}, {Kind: KindStruct, Align: 8}})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func oldParent(t *testing.T, c *Collector) Ref {
	t.Helper()
	r, e := c.NewArray(0, 5)
	if e != nil {
		t.Fatal(e)
	}
	if c.IsYoung(r) {
		t.Fatal("fixture parent young")
	}
	return r
}
func TestRememberedQueueDeduplicatesAndAllocatesNothing(t *testing.T) {
	c := rememberedHeap(t, 1024)
	p := oldParent(t, c)
	child, e := c.NewStruct(1)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 20; i++ {
		edge(t, c, p, 0, child)
		if e = c.Remember(p); e != nil {
			t.Fatal(e)
		}
	}
	if len(c.remembered) != 1 || c.remembered[0] != p || c.rememberedOverflow {
		t.Fatal("not deduplicated")
	}
	if n := testing.AllocsPerRun(100, func() {
		c.resetRemembered(false)
		if e := c.Remember(p); e != nil {
			panic(e)
		}
	}); n != 0 {
		t.Fatal("barrier allocated", n)
	}
	if e = c.Minor(nil); e != nil {
		t.Fatal(e)
	}
	if len(c.remembered) != 0 || c.rememberedOverflow || c.dirty[uint32(p)/8] {
		t.Fatal("successful minor left dirty state")
	}
	moved := get(t, c, p, 0)
	if moved == child || !c.IsValid(moved) || c.IsYoung(moved) {
		t.Fatal("queue lost child")
	}
	if n := testing.AllocsPerRun(100, func() {
		if e := c.Remember(p); e != nil {
			panic(e)
		}
		if e := c.Minor(nil); e != nil {
			panic(e)
		}
	}); n != 0 {
		t.Fatal("minor allocated", n)
	}
}
func TestRememberedQueueCapacityOverflowAndReset(t *testing.T) {
	c := rememberedHeap(t, 128<<10)
	parents := make([]Ref, rememberedParentCapacity+2)
	for i := range parents {
		parents[i] = oldParent(t, c)
	}
	child, e := c.NewStruct(1)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range parents {
		edge(t, c, p, 0, child)
	}
	if len(c.remembered) != rememberedParentCapacity || !c.rememberedOverflow {
		t.Fatal("overflow not recorded")
	}
	before := c.oldBump
	if e = c.Minor(nil); e != nil {
		t.Fatal(e)
	}
	moved := get(t, c, parents[0], 0)
	if moved == child || uint32(moved) != before {
		t.Fatal("overflow child not moved")
	}
	for _, p := range parents {
		if get(t, c, p, 0) != moved || c.dirty[uint32(p)/8] {
			t.Fatal("fallback missed parent", p)
		}
	}
	if c.rememberedOverflow || len(c.remembered) != 0 {
		t.Fatal("fallback queue not reset")
	}
	// Dirty an old object with no young child then perform Full. The stale queue
	// must not be reused after parent offsets relocate to the other old semispace.
	if e = c.Remember(parents[0]); e != nil {
		t.Fatal(e)
	}
	root := parents[0]
	if e = c.Full([]*Ref{&root}); e != nil {
		t.Fatal(e)
	}
	if c.rememberedOverflow || len(c.remembered) != 0 {
		t.Fatal("full queue not reset")
	}
	for _, d := range c.dirty {
		if d {
			t.Fatal("full dirty bit retained")
		}
	}
}
func TestRememberedQueueZeroCapacityFailsOver(t *testing.T) {
	c := rememberedHeap(t, 1024)
	c.remembered = c.remembered[:0:0]
	p := oldParent(t, c)
	child, e := c.NewStruct(1)
	if e != nil {
		t.Fatal(e)
	}
	edge(t, c, p, 0, child)
	if !c.rememberedOverflow || len(c.remembered) != 0 {
		t.Fatal("zero capacity lost barrier")
	}
	if e = c.Minor(nil); e != nil {
		t.Fatal(e)
	}
	if get(t, c, p, 0) == child {
		t.Fatal("zero capacity lost child")
	}
}
func TestRememberedQueueOOMPreservesTracking(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(map[bool]string{false: "queue", true: "fallback"}[overflow], func(t *testing.T) {
			c := rememberedHeap(t, 64)
			if overflow {
				c.remembered = c.remembered[:0:0]
			}
			p := oldParent(t, c) //48/64 old bytes occupied.
			// A cyclic young object needs32bytes, exceeding remaining16bytes.
			// A reference object of32bytes fits Eden, points to itself, requiring32old.
			y, e := c.NewArray(0, 1)
			if e != nil {
				t.Fatal(e)
			}
			edge(t, c, y, 0, y)
			edge(t, c, p, 0, y)
			arena := append([]byte(nil), c.arena...)
			dirty := append([]bool(nil), c.dirty...)
			queue := append([]Ref(nil), c.remembered...)
			oldOverflow := c.rememberedOverflow
			if e = c.Minor(nil); !errors.Is(e, ErrOOM) {
				t.Fatal("expected OOM", e)
			}
			if !bytes.Equal(arena, c.arena) || !reflect.DeepEqual(dirty, c.dirty) || !slices.Equal(queue, c.remembered) || c.rememberedOverflow != oldOverflow {
				t.Fatal("failed collection consumed barrier")
			}
			if e = c.Full([]*Ref{&p}); !errors.Is(e, ErrOOM) {
				t.Fatal("expected full OOM", e)
			}
			if !bytes.Equal(arena, c.arena) || !reflect.DeepEqual(dirty, c.dirty) || !slices.Equal(queue, c.remembered) || c.rememberedOverflow != oldOverflow {
				t.Fatal("failed full consumed barrier")
			}
			edge(t, c, p, 0, 0)
			if e = c.Minor(nil); e != nil {
				t.Fatal("retry", e)
			}
			if len(c.remembered) != 0 || c.rememberedOverflow || c.dirty[uint32(p)/8] {
				t.Fatal("retry did not reset")
			}
		})
	}
}
func TestRememberedQueueVisitsOnlyDirtyParents(t *testing.T) {
	c := rememberedHeap(t, 4096)
	for i := 0; i < 30; i++ {
		oldParent(t, c)
	}
	p := oldParent(t, c)
	child, e := c.NewStruct(1)
	if e != nil {
		t.Fatal(e)
	}
	edge(t, c, p, 0, child)
	visited := 0
	if e = c.rangeRemembered(func(r Ref) error {
		visited++
		if r != p {
			t.Fatal("unrelated old object visited")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if visited != 1 {
		t.Fatal("wrong scan set", visited)
	}
	// Even without a root to the old parent, remembered edges conservatively
	// retain young children until a Full removes globally unreachable old objects.
	if e = c.Minor(nil); e != nil {
		t.Fatal(e)
	}
	if !c.IsValid(get(t, c, p, 0)) {
		t.Fatal("unrooted dirty parent lost edge")
	}
	if e = c.Full(nil); e != nil {
		t.Fatal(e)
	}
	if c.LiveObjects() != 0 {
		t.Fatal("unreachable old graph retained")
	}
}

func TestRememberedInvalidEdgePreservesTracking(t *testing.T) {
	c := rememberedHeap(t, 1024)
	p := oldParent(t, c)
	child, e := c.NewStruct(1)
	if e != nil {
		t.Fatal(e)
	}
	edge(t, c, p, 0, child)
	// Trusted-borrow corruption must be detected before consuming queue state.
	c.put32(uint32(p)+header, 2)
	q := append([]Ref(nil), c.remembered...)
	dirty := append([]bool(nil), c.dirty...)
	arena := append([]byte(nil), c.arena...)
	if e = c.Minor(nil); !errors.Is(e, ErrRef) {
		t.Fatal(e)
	}
	if !slices.Equal(q, c.remembered) || !reflect.DeepEqual(dirty, c.dirty) || !bytes.Equal(arena, c.arena) {
		t.Fatal("invalid edge consumed dirty tracking")
	}
	edge(t, c, p, 0, child)
	if e = c.Minor(nil); e != nil {
		t.Fatal(e)
	}
}
