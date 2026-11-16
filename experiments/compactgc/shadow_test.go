//go:build wago_gc_compact_shadow

package compactgc

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestShadowReachabilityCyclesAliasesAndI31(t *testing.T) {
	c := heap(t, 128, 512, node)
	a, b := obj(t, c, 0), obj(t, c, 0)
	set(t, c, a, 0, b)
	set(t, c, b, 0, a)
	set(t, c, a, 1, I31New(7))
	if e := c.Minor([]*Ref{&a, &a}); e != nil {
		t.Fatal(e)
	}
	if s := c.ShadowDiagnostics(); s.UncoveredCycles != 1 {
		t.Fatal(s)
	}
	arena := append([]byte(nil), c.arena...)
	starts := append([]bool(nil), c.starts...)
	marks := append([]uint64(nil), c.mark...)
	original := a
	immediate := I31New(9)
	d := c.beginShadowCycle([]*Ref{&a, &a, &immediate}, false)
	if !d.observation.Valid || d.observation.PromotedObjects != 2 || d.observation.ReachableObjects != 2 {
		t.Fatal(d)
	}
	if a != original || immediate != I31New(9) || !bytes.Equal(arena, c.arena) || !reflect.DeepEqual(starts, c.starts) || !reflect.DeepEqual(marks, c.mark) {
		t.Fatal("shadow mutated authoritative state")
	}
	for _, v := range c.shadowMark {
		if v != 0 {
			t.Fatal("dirty shadow scratch")
		}
	}
	for _, v := range c.shadowQueue {
		if v != 0 {
			t.Fatal("retained shadow references")
		}
	}
	if e := c.Minor([]*Ref{&a}); e != nil {
		t.Fatal(e)
	}
	if s := c.ShadowDiagnostics(); s.CoveredCycles != 1 || s.Last.ReachableObjects != 2 {
		t.Fatal(s)
	}
	// A newly promoted cohort can become wholly unreachable before the next minor.
	fresh := obj(t, c, 0)
	if e := c.Minor([]*Ref{&fresh}); e != nil {
		t.Fatal(e)
	}
	if e := c.Minor(nil); e != nil {
		t.Fatal(e)
	}
	if s := c.ShadowDiagnostics(); !s.Last.Valid || s.Last.PromotedObjects != 1 || s.Last.ReachableObjects != 0 {
		t.Fatal(s)
	}
}

func TestShadowFailuresAndFullInvalidate(t *testing.T) {
	c := heap(t, 64, 16, node)
	live := obj(t, c, 0)
	if e := c.Minor([]*Ref{&live}); e != nil {
		t.Fatal(e)
	}
	last := c.ShadowDiagnostics().Last
	fresh := obj(t, c, 0)
	if e := c.Minor([]*Ref{&fresh}); !errors.Is(e, ErrOOM) {
		t.Fatal(e)
	}
	if s := c.ShadowDiagnostics(); s.MinorFailures != 1 || s.Last != last || !c.shadowKnown {
		t.Fatal(s)
	}
	bad := Ref(3 << 4)
	if e := c.Full([]*Ref{&bad}); e == nil {
		t.Fatal("expected invalid full root")
	}
	if c.shadowKnown || c.ShadowDiagnostics().FullFailures != 1 {
		t.Fatal(c.ShadowDiagnostics())
	}
	if e := c.Full(nil); e != nil {
		t.Fatal(e)
	}
	if e := c.Minor(nil); e != nil {
		t.Fatal(e)
	}
	if s := c.ShadowDiagnostics(); s.Last.Covered || s.FullSuccesses != 1 {
		t.Fatal(s)
	}
}

func TestShadowInvalidGraphDoesNotChangeMinorResult(t *testing.T) {
	c := heap(t, 64, 256, node)
	parent := obj(t, c, 0)
	if e := c.Minor([]*Ref{&parent}); e != nil {
		t.Fatal(e)
	}
	// Deliberately bypass the barrier with an invalid old edge. Ordinary minor
	// ignores this clean old object, whereas full-root shadow traversal must fail.
	p, e := c.Payload(parent)
	if e != nil {
		t.Fatal(e)
	}
	p[0] = 0xf0
	p[1] = 0xff
	p[2] = 0xff
	p[3] = 0xff
	before := append([]byte(nil), c.arena...)
	original := parent
	if e = c.Minor([]*Ref{&parent}); e != nil {
		t.Fatalf("diagnostic changed minor outcome: %v", e)
	}
	if s := c.ShadowDiagnostics(); s.InvalidCycles != 1 || s.CoveredCycles != 0 || s.Last.Valid {
		t.Fatal(s)
	}
	if parent != original || !bytes.Equal(before, c.arena) {
		t.Fatal("minor or shadow mutated clean old graph")
	}
}
