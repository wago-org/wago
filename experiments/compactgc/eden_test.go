//go:build wago_gc_compact_eden && !wago_gc_compact_cohort && !wago_gc_compact_shadow && !wago_gc_compact_age2_metrics && !wago_gc_compact_age2_queue_reuse && !wago_gc_compact8

package compactgc

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func edenHeap(t *testing.T, e, s, o uint32) *Collector {
	t.Helper()
	c, err := NewEdenWithTypes(e, s, o, []Descriptor{node})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func edenCount(t *testing.T, c *Collector, want uint64) {
	t.Helper()
	got, _ := c.AllocationSnapshot()
	if got != want {
		t.Fatal(got, want)
	}
}
func TestEdenGeometryAndNoNativeLease(t *testing.T) {
	c := edenHeap(t, 128, 32, 256)
	base := heap(t, 192, 256, node)
	if len(c.arena) != len(base.arena) || c.FixedBytes() != base.FixedBytes() {
		t.Fatal("backing budget")
	}
	if total, fresh, survivor := c.EdenBudget(); total != 192 || fresh != 128 || survivor != 32 {
		t.Fatal(total, fresh, survivor)
	}
	if c.ReadView().Arena != 0 || c.AllocationWords().Bump != nil || base.EdenEnabled() || c.Age2Enabled() {
		t.Fatal("policy/native exposure")
	}
	for _, x := range [][3]uint32{{0, 16, 256}, {16, 0, 256}, {17, 16, 256}, {16, 17, 256}, {0xfffffff0, 16, 256}} {
		if _, err := NewEdenWithTypes(x[0], x[1], x[2], nil); err == nil {
			t.Fatal(x)
		}
	}
}
func TestEdenCyclesTwoAgesAndBarriers(t *testing.T) {
	c := edenHeap(t, 128, 64, 512)
	a, b := obj(t, c, 0), obj(t, c, 0)
	set(t, c, a, 0, b)
	set(t, c, b, 0, a)
	set(t, c, a, 1, I31New(-3))
	roots := []*Ref{&a, &a}
	oldA := a
	if err := c.Minor(roots); err != nil {
		t.Fatal(err)
	}
	if !c.edenYoung(a) || c.valid(oldA) || c.nurseryBump != c.nurseryStart {
		t.Fatal("first retention")
	}
	child := obj(t, c, 0)
	set(t, c, a, 1, child)
	if err := c.Minor(roots); err != nil {
		t.Fatal(err)
	}
	if c.young(a) || !c.edenYoung(edge(t, c, a, 1)) || edge(t, c, edge(t, c, a, 0), 0) != a {
		t.Fatal("age graph")
	}
	if !c.dirty[uint32(a)/8] {
		t.Fatal("promoted parent not remembered")
	}
	younger := obj(t, c, 0)
	set(t, c, a, 0, younger)
	if err := c.Minor(roots); err != nil {
		t.Fatal(err)
	}
	if !c.edenYoung(edge(t, c, a, 0)) || c.young(edge(t, c, a, 1)) {
		t.Fatal("old barrier")
	}
	edenCount(t, c, 4)
}
func TestEdenOverflowAndFullBothYoungRegions(t *testing.T) {
	c := edenHeap(t, 128, 16, 256)
	a, b := obj(t, c, 0), obj(t, c, 0)
	set(t, c, a, 0, b)
	set(t, c, b, 0, a)
	if err := c.Minor([]*Ref{&a}); err != nil {
		t.Fatal(err)
	}
	if !c.edenYoung(a) || c.young(edge(t, c, a, 0)) {
		t.Fatal("overflow destination")
	}
	fresh := obj(t, c, 0)
	set(t, c, fresh, 0, a)
	roots := []*Ref{&fresh, &a}
	if err := c.Full(roots); err != nil {
		t.Fatal(err)
	}
	if c.young(a) || c.young(fresh) || edge(t, c, fresh, 0) != a || edge(t, c, edge(t, c, a, 0), 0) != a {
		t.Fatal("full graph")
	}
	edenCount(t, c, 3)
	for i := 0; i < 4; i++ {
		if err := c.Full(nil); err != nil {
			t.Fatal(err)
		}
		if err := c.Minor(nil); err != nil {
			t.Fatal(err)
		}
	}
	edenCount(t, c, 3)
}
func TestEdenFailedPreflightAtomicAndRetry(t *testing.T) {
	for _, full := range []bool{false, true} {
		c := edenHeap(t, 128, 16, 16)
		a, b, d := obj(t, c, 0), obj(t, c, 0), obj(t, c, 0)
		set(t, c, a, 0, b)
		set(t, c, b, 0, d)
		before := append([]byte(nil), c.arena...)
		starts := append([]bool(nil), c.starts...)
		dirty := append([]bool(nil), c.dirty...)
		bump := c.nurseryBump
		root := a
		collect := c.Minor
		if full {
			collect = c.Full
		}
		if err := collect([]*Ref{&a}); !errors.Is(err, ErrOOM) {
			t.Fatal(err)
		}
		if a != root || c.nurseryBump != bump || !bytes.Equal(before, c.arena) || !reflect.DeepEqual(starts, c.starts) || !reflect.DeepEqual(dirty, c.dirty) {
			t.Fatal("failed preflight mutated")
		}
		for _, m := range c.mark {
			if m != 0 {
				t.Fatal("dirty scratch")
			}
		}
		set(t, c, a, 0, 0)
		if err := collect([]*Ref{&a}); err != nil {
			t.Fatal(err)
		}
		edenCount(t, c, 3)
	}
}
func TestEdenInvalidChildRepair(t *testing.T) {
	c := edenHeap(t, 128, 32, 128)
	a, b := obj(t, c, 0), obj(t, c, 0)
	set(t, c, a, 0, b)
	at, err := c.refOffset(b, 0)
	if err != nil {
		t.Fatal(err)
	}
	c.put32(at, 0xfffffff0)
	before := append([]byte(nil), c.arena...)
	if err := c.Minor([]*Ref{&a}); !errors.Is(err, ErrRef) {
		t.Fatal(err)
	}
	if !bytes.Equal(before, c.arena) {
		t.Fatal("invalid child mutated arena")
	}
	c.put32(at, 0)
	if err := c.Minor([]*Ref{&a}); err != nil {
		t.Fatal(err)
	}
	if edge(t, c, a, 0) == 0 {
		t.Fatal("lost child")
	}
	edenCount(t, c, 2)
}

func TestEdenRememberedOverflowAndZeroAllocation(t *testing.T) {
	c := edenHeap(t, 32768, 32768, 131072)
	parents := make([]Ref, rememberedParentCapacity+1)
	roots := make([]*Ref, len(parents))
	for i := range parents {
		parents[i] = obj(t, c, 0)
		roots[i] = &parents[i]
	}
	if err := c.Full(roots); err != nil {
		t.Fatal(err)
	}
	for _, p := range parents {
		child := obj(t, c, 0)
		set(t, c, p, 0, child)
	}
	if !c.rememberedOverflow {
		t.Fatal("queue did not overflow")
	}
	if err := c.Minor(roots); err != nil {
		t.Fatal(err)
	}
	for _, p := range parents {
		if !c.edenYoung(edge(t, c, p, 0)) {
			t.Fatal("lost first survivor")
		}
	}
	if !c.rememberedOverflow {
		t.Fatal("survivor barriers not retained")
	}
	if err := c.Minor(roots); err != nil {
		t.Fatal(err)
	}
	for _, p := range parents {
		if c.young(edge(t, c, p, 0)) {
			t.Fatal("child not promoted")
		}
	}
	edenCount(t, c, uint64(2*len(parents)))
	if n := testing.AllocsPerRun(5, func() {
		if err := c.Full(roots); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("collection allocated", n)
	}
}

func TestEdenFullFailureWithBothYoungSources(t *testing.T) {
	c := edenHeap(t, 64, 32, 16)
	survivor := obj(t, c, 0)
	if err := c.Minor([]*Ref{&survivor}); err != nil {
		t.Fatal(err)
	}
	fresh := obj(t, c, 0)
	set(t, c, fresh, 0, survivor)
	before := append([]byte(nil), c.arena...)
	starts := append([]bool(nil), c.starts...)
	active, bump := c.edenSurvivorActive, c.edenSurvivorBump
	if err := c.Full([]*Ref{&fresh}); !errors.Is(err, ErrOOM) {
		t.Fatal(err)
	}
	if !bytes.Equal(before, c.arena) || !reflect.DeepEqual(starts, c.starts) || active != c.edenSurvivorActive || bump != c.edenSurvivorBump {
		t.Fatal("failed full altered young sources")
	}
	set(t, c, fresh, 0, 0)
	if err := c.Full([]*Ref{&fresh}); err != nil {
		t.Fatal(err)
	}
	if c.young(fresh) || c.valid(survivor) {
		t.Fatal("full retry source retirement")
	}
	edenCount(t, c, 2)
}
