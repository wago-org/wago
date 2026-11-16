package directgc

import (
	"errors"
	"testing"
)

func assertAllocations(t *testing.T, c *Collector, want uint64) {
	t.Helper()
	for i := 0; i < 3; i++ {
		got, _ := c.AllocationSnapshot()
		if got != want {
			t.Fatalf("allocations=%d want%d", got, want)
		}
	}
}
func TestDeferredAllocationAccountingRetries(t *testing.T) {
	c := heap(t, 128, 32)
	a := obj(t, c, 0, 1)
	b := obj(t, c, 0, 1)
	assertAllocations(t, c, 2)
	for _, full := range []bool{false, true} {
		var e error
		if full {
			e = c.Full([]*Ref{&a, &b})
		} else {
			e = c.Minor([]*Ref{&a, &b})
		}
		if !errors.Is(e, ErrOOM) {
			t.Fatal(e)
		}
		assertAllocations(t, c, 2)
		if c.retiredNurseryAllocations != 0 {
			t.Fatal("failed collection committed count")
		}
	}
	if e := c.Minor([]*Ref{&a}); e != nil {
		t.Fatal(e)
	}
	assertAllocations(t, c, 2) // dead b is still an allocation
	if c.retiredNurseryAllocations != 2 {
		t.Fatal("retired count", c.retiredNurseryAllocations)
	}
	b = obj(t, c, 0, 1)
	if e := c.Minor([]*Ref{&b}); !errors.Is(e, ErrOOM) {
		t.Fatal(e)
	}
	assertAllocations(t, c, 3)
	if e := c.Full([]*Ref{&b}); e != nil {
		t.Fatal(e)
	}
	assertAllocations(t, c, 3)
	if e := c.Full(nil); e != nil {
		t.Fatal(e)
	}
	assertAllocations(t, c, 3)
}
func TestDeferredAllocationAccountingOldAndZero(t *testing.T) {
	c, e := NewWithTypes(64, 512, []Descriptor{{Kind: KindArray, Align: 8, ElementSize: 8}})
	if e != nil {
		t.Fatal(e)
	}
	old, e := c.NewArray(0, 20)
	if e != nil {
		t.Fatal(e)
	}
	zero, e := c.NewArray(0, 0)
	if e != nil {
		t.Fatal(e)
	}
	assertAllocations(t, c, 2)
	if c.oldAllocations != 1 {
		t.Fatal("old count")
	}
	if e = c.Minor([]*Ref{&old, &zero}); e != nil {
		t.Fatal(e)
	}
	assertAllocations(t, c, 2)
	if e = c.Full([]*Ref{&old, &zero}); e != nil {
		t.Fatal(e)
	}
	assertAllocations(t, c, 2)
	if _, e = c.NewArray(0, 1000); !errors.Is(e, ErrOOM) {
		t.Fatal(e)
	}
	assertAllocations(t, c, 2)
}
