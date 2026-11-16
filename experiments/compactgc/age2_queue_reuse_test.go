//go:build wago_gc_compact_age2 && wago_gc_compact_age2_queue_reuse && !wago_gc_compact_cohort && !wago_gc_compact_shadow

package compactgc

import (
	"bytes"
	"reflect"
	"testing"
)

func TestAge2QueueReuseRejectsMalformedQueuedAndRememberedObjects(t *testing.T) {
	for _, old := range []bool{false, true} {
		c := ageHeap(t, 128, 256, 32, node)
		r := obj(t, c, 0)
		if old {
			for i := 0; i < 2; i++ {
				if e := c.Minor([]*Ref{&r}); e != nil {
					t.Fatal(e)
				}
			}
			child := obj(t, c, 0)
			set(t, c, r, 0, child)
		}
		c.put32(uint32(r), ^uint32(0))
		arena := append([]byte(nil), c.arena...)
		starts := append([]bool(nil), c.starts...)
		dirty := append([]bool(nil), c.dirty...)
		before := r
		if e := c.Minor([]*Ref{&r, &r}); e == nil {
			t.Fatal("malformed type admitted", old)
		}
		if r != before || !bytes.Equal(arena, c.arena) || !reflect.DeepEqual(starts, c.starts) || !reflect.DeepEqual(dirty, c.dirty) {
			t.Fatal("invalid scan mutated state", old)
		}
	}
}
func TestAge2QueueReuseBadChildAtomic(t *testing.T) {
	c := ageHeap(t, 128, 256, 32, node)
	a, b := obj(t, c, 0), obj(t, c, 0)
	set(t, c, a, 0, b)
	set(t, c, b, 0, a)
	at, e := c.refOffset(b, 1)
	if e != nil {
		t.Fatal(e)
	}
	c.put32(at, 48)
	arena := append([]byte(nil), c.arena...)
	before := a
	if e := c.Minor([]*Ref{&a, &a}); e == nil {
		t.Fatal("invalid child accepted")
	}
	if a != before || !bytes.Equal(arena, c.arena) {
		t.Fatal("failed queued edge changed heap")
	}
	c.put32(at, uint32(I31New(-1)))
	if e := c.Minor([]*Ref{&a, &a}); e != nil {
		t.Fatal(e)
	}
	if edge(t, c, edge(t, c, a, 0), 0) != a {
		t.Fatal("cycle alias lost")
	}
}
