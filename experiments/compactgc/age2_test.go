//go:build wago_gc_compact_age2

package compactgc

import (
	"bytes"
	"reflect"
	"testing"
)

func ageHeap(t *testing.T, n, o, cap uint32, ds ...Descriptor) *Collector {
	t.Helper()
	c, e := NewAge2WithTypes(n, o, cap, ds)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestAge2EqualBudgetAndDefault(t *testing.T) {
	c := ageHeap(t, 128, 256, 32, node)
	base := heap(t, 128, 256, node)
	if len(c.arena) != len(base.arena) || c.FixedBytes() != base.FixedBytes() {
		t.Fatal("young budget doubled")
	}
	n, a, cap := c.YoungBudget()
	if n != 128 || a != 64 || cap != 32 {
		t.Fatal(n, a, cap)
	}
	if base.Age2Enabled() {
		t.Fatal("default policy changed")
	}
	for _, v := range [][3]uint32{{32, 256, 0}, {96, 256, 32}, {128, 256, 48}, {128, 256, 1}} {
		if _, e := NewAge2WithTypes(v[0], v[1], v[2], nil); e == nil {
			t.Fatal("bad config", v)
		}
	}
}
func TestAge2CyclesAliasesAndCounts(t *testing.T) {
	c := ageHeap(t, 128, 256, 32, node)
	a, b := obj(t, c, 0), obj(t, c, 0)
	set(t, c, a, 0, b)
	set(t, c, b, 0, a)
	set(t, c, a, 1, I31New(-3))
	roots := []*Ref{&a, &a}
	if e := c.Minor(roots); e != nil {
		t.Fatal(e)
	}
	if !c.young(a) || !c.young(edge(t, c, a, 0)) {
		t.Fatal("first survivors not young")
	}
	allocations(t, c, 2)
	if e := c.Minor(roots); e != nil {
		t.Fatal(e)
	}
	if c.young(a) || edge(t, c, edge(t, c, a, 0), 0) != a || edge(t, c, a, 1) != I31New(-3) {
		t.Fatal("promotion graph")
	}
	allocations(t, c, 2)
	for i := 0; i < 8; i++ {
		if e := c.Full(roots); e != nil {
			t.Fatal(e)
		}
		allocations(t, c, 2)
	}
}
func TestAge2PromotedParentRetainsYoungerChild(t *testing.T) {
	c := ageHeap(t, 128, 256, 32, node)
	p := obj(t, c, 0)
	if e := c.Minor([]*Ref{&p}); e != nil {
		t.Fatal(e)
	}
	child := obj(t, c, 0)
	set(t, c, p, 0, child)
	if e := c.Minor([]*Ref{&p}); e != nil {
		t.Fatal(e)
	}
	child = edge(t, c, p, 0)
	if c.young(p) || !c.young(child) || !c.dirty[uint32(p)/8] {
		t.Fatal("promoted parent barrier missing")
	}
	if e := c.Minor([]*Ref{&p}); e != nil {
		t.Fatal(e)
	}
	if c.young(edge(t, c, p, 0)) || c.dirty[uint32(p)/8] {
		t.Fatal("retained child promotion/barrier cleanup")
	}
	allocations(t, c, 2)
}
func TestAge2RetentionOverflowAndOldOOMAtomic(t *testing.T) {
	c := ageHeap(t, 128, 128, 16, node)
	roots := []Ref{obj(t, c, 0), obj(t, c, 0), obj(t, c, 0)}
	if e := c.Minor([]*Ref{&roots[0], &roots[1], &roots[2]}); e != nil {
		t.Fatal(e)
	}
	if !c.young(roots[0]) || c.young(roots[1]) || c.young(roots[2]) {
		t.Fatal("retention cap")
	}
	allocations(t, c, 3)
	d := ageHeap(t, 64, 32, 16, Descriptor{Kind: KindStruct, Align: 4, PayloadBytes: 20})
	old := obj(t, d, 0)
	if e := d.Minor([]*Ref{&old}); e != nil {
		t.Fatal(e)
	}
	fresh := obj(t, d, 0)
	rootBefore := fresh
	arena := append([]byte(nil), d.arena...)
	starts := append([]bool(nil), d.starts...)
	dirty := append([]bool(nil), d.dirty...)
	count, _ := d.AllocationSnapshot()
	if e := d.Minor([]*Ref{&fresh}); e != ErrOOM {
		t.Fatal("expected OOM", e)
	}
	if fresh != rootBefore || !bytes.Equal(arena, d.arena) || !reflect.DeepEqual(starts, d.starts) || !reflect.DeepEqual(dirty, d.dirty) {
		t.Fatal("failed preflight mutated heap")
	}
	if got, _ := d.AllocationSnapshot(); got != count {
		t.Fatal("failed accounting")
	}
	if e := d.Full([]*Ref{&fresh}); e != nil {
		t.Fatal("full retry", e)
	}
	allocations(t, d, 2)
}
func TestAge2RememberedOverflow(t *testing.T) {
	c := ageHeap(t, 128<<10, 256<<10, 32<<10, node)
	parents := make([]Ref, 1025)
	roots := make([]*Ref, len(parents))
	for i := range parents {
		parents[i] = obj(t, c, 0)
		roots[i] = &parents[i]
	}
	if e := c.Minor(roots); e != nil {
		t.Fatal(e)
	}
	if e := c.Minor(roots); e != nil {
		t.Fatal(e)
	}
	for _, p := range parents {
		set(t, c, p, 0, obj(t, c, 0))
	}
	if !c.rememberedOverflow {
		t.Fatal("expected overflow")
	}
	if e := c.Minor(roots); e != nil {
		t.Fatal(e)
	}
	if !c.rememberedOverflow {
		t.Fatal("survivor overflow tracking lost")
	}
	for _, p := range parents {
		if !c.young(edge(t, c, p, 0)) {
			t.Fatal("child lost")
		}
	}
	if e := c.Minor(roots); e != nil {
		t.Fatal(e)
	}
	if c.rememberedOverflow || len(c.remembered) != 0 {
		t.Fatal("promoted children still remembered")
	}
	allocations(t, c, 2050)
}
func TestAge2OversizedAndZeroAllocationCollection(t *testing.T) {
	c := ageHeap(t, 128, 256, 32, Descriptor{Kind: KindStruct, Align: 4, PayloadBytes: 68})
	r := obj(t, c, 0)
	if c.young(r) || c.oldAllocations != 1 {
		t.Fatal("active-half oversized threshold")
	}
	roots := []*Ref{&r}
	if n := testing.AllocsPerRun(10, func() {
		if e := c.Full(roots); e != nil {
			panic(e)
		}
	}); n != 0 {
		t.Fatal("full allocates", n)
	}
	if n := testing.AllocsPerRun(10, func() {
		if e := c.Minor(roots); e != nil {
			panic(e)
		}
	}); n != 0 {
		t.Fatal("minor allocates", n)
	}
	allocations(t, c, 1)
}

func TestAge2MixedFullFailureAndCapZero(t *testing.T) {
	var nilHeap *Collector
	if n, a, cap := nilHeap.YoungBudget(); n != 0 || a != 0 || cap != 0 {
		t.Fatal("nil budget")
	}
	c := ageHeap(t, 128, 32, 32, node)
	a, b, p := obj(t, c, 0), obj(t, c, 0), obj(t, c, 0)
	if e := c.Minor([]*Ref{&a, &b, &p}); e != nil {
		t.Fatal(e)
	}
	set(t, c, a, 0, b)
	set(t, c, p, 0, a)
	roots := []*Ref{&p}
	rootBefore := p
	arena := append([]byte(nil), c.arena...)
	dirty := append([]bool(nil), c.dirty...)
	remembered := append([]Ref(nil), c.remembered...)
	if e := c.Full(roots); e != ErrOOM {
		t.Fatal("expected mixed full OOM", e)
	}
	if p != rootBefore || !bytes.Equal(arena, c.arena) || !reflect.DeepEqual(dirty, c.dirty) || !reflect.DeepEqual(remembered, c.remembered) {
		t.Fatal("full failure changed graph or barriers")
	}
	set(t, c, a, 0, 0)
	if e := c.Full(roots); e != nil {
		t.Fatal("retry", e)
	}
	if c.young(p) || c.young(edge(t, c, p, 0)) {
		t.Fatal("full retained young")
	}
	allocations(t, c, 3)
	z := ageHeap(t, 64, 128, 0, node)
	r := obj(t, z, 0)
	if e := z.Minor([]*Ref{&r}); e != nil {
		t.Fatal(e)
	}
	if z.young(r) {
		t.Fatal("cap0 retained survivor")
	}
	allocations(t, z, 1)
}
