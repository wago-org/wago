//go:build wago_gc_retire_adaptive && !wago_gc_retire_startmap

package directgc

import (
	"errors"
	"testing"
)

func TestAdaptiveRetirementChangingSizes(t *testing.T) {
	c := heap(t, 4096, 4096)
	if c.retireUsingMap() {
		t.Fatal("first cycle must headerwalk")
	}
	var total uint64
	for _, w := range []uint32{1, 128, 3, 128, 0, 1} {
		before := c.retireUsingMap()
		for i := 0; i < 3; i++ {
			obj(t, c, 0, w)
			total++
		}
		if e := c.Minor(nil); e != nil {
			t.Fatal(e)
		}
		dense := w <= 3
		if c.retireUsingMap() != dense {
			t.Fatal("density prediction", w, before, c.retirementDense)
		}
		assertAllocations(t, c, total)
	}
	if e := c.Minor(nil); e != nil {
		t.Fatal(e)
	}
	if c.retireUsingMap() {
		t.Fatal("empty cycle must reset")
	}
	assertAllocations(t, c, total)
}
func TestAdaptiveRetirementFailureKeepsPrediction(t *testing.T) {
	c := heap(t, 128, 32)
	obj(t, c, 0, 1)
	if e := c.Minor(nil); e != nil {
		t.Fatal(e)
	}
	if !c.retireUsingMap() {
		t.Fatal("dense prediction missing")
	}
	a := obj(t, c, 0, 1)
	b := obj(t, c, 0, 1)
	if e := c.Minor([]*Ref{&a, &b}); !errors.Is(e, ErrOOM) {
		t.Fatal(e)
	}
	if !c.retireUsingMap() {
		t.Fatal("OOM changed prediction")
	}
	assertAllocations(t, c, 3)
	if e := c.Full([]*Ref{&a}); e != nil {
		t.Fatal(e)
	}
	assertAllocations(t, c, 3)
}
