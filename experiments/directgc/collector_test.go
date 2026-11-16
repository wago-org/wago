package directgc

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func heap(t *testing.T, n, o uint32) *Collector {
	t.Helper()
	c, e := New(n, o)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func obj(t *testing.T, c *Collector, refs, words uint32) Ref {
	t.Helper()
	r, e := c.NewObject(refs, words)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func edge(t *testing.T, c *Collector, r Ref, i uint32, v Ref) {
	t.Helper()
	if e := c.SetRef(r, i, v); e != nil {
		t.Fatal(e)
	}
}
func get(t *testing.T, c *Collector, r Ref, i uint32) Ref {
	t.Helper()
	v, e := c.RefField(r, i)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestCyclesSharingAndRepeatedMajor(t *testing.T) {
	c := heap(t, 1024, 1024)
	a := obj(t, c, 2, 1)
	b := obj(t, c, 1, 1)
	edge(t, c, a, 0, b)
	edge(t, c, a, 1, b)
	edge(t, c, b, 0, a)
	if e := c.SetWord(b, 0, 42); e != nil {
		t.Fatal(e)
	}
	stale := a
	if e := c.Minor([]*Ref{&a, &a, &b}); e != nil {
		t.Fatal(e)
	}
	if a == stale || c.valid(stale) {
		t.Fatal("nursery source not invalidated")
	}
	for i := 0; i < 8; i++ {
		if get(t, c, a, 0) != b || get(t, c, a, 1) != b || get(t, c, b, 0) != a {
			t.Fatal("graph changed")
		}
		if v, e := c.Word(b, 0); e != nil || v != 42 {
			t.Fatal("payload changed", v, e)
		}
		if e := c.Full([]*Ref{&a, &a, &b}); e != nil {
			t.Fatal(e)
		}
	}
}
func TestBarrierRetainsYoungAndOverwriteReclaims(t *testing.T) {
	c := heap(t, 256, 512)
	parent := obj(t, c, 1, 0)
	if e := c.Minor([]*Ref{&parent}); e != nil {
		t.Fatal(e)
	}
	child := obj(t, c, 1, 1)
	edge(t, c, child, 0, child)
	edge(t, c, parent, 0, child)
	if e := c.Minor(nil); e != nil {
		t.Fatal(e)
	} // remembered old is a conservative root.
	moved := get(t, c, parent, 0)
	if moved == child || !c.valid(moved) || get(t, c, moved, 0) != moved {
		t.Fatal("barrier lost cycle")
	}
	edge(t, c, parent, 0, 0)
	if e := c.Full([]*Ref{&parent}); e != nil {
		t.Fatal(e)
	}
	if c.oldBump-c.oldStart[c.active] != 24 {
		t.Fatal("unreachable child retained")
	}
	// A dirty old object whose edge was overwritten with null must not retain a
	// later nursery allocation merely because its remembered bit remains set.
	doomed := obj(t, c, 0, 0)
	edge(t, c, parent, 0, doomed)
	edge(t, c, parent, 0, 0)
	if e := c.Minor([]*Ref{&parent}); e != nil {
		t.Fatal(e)
	}
	if c.valid(doomed) {
		t.Fatal("overwritten young edge retained")
	}
}
func TestTransactionalOOM(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "minor", true: "full"}[full], func(t *testing.T) {
			c := heap(t, 256, 32)
			a := obj(t, c, 1, 0)
			b := obj(t, c, 1, 0)
			edge(t, c, a, 0, b)
			edge(t, c, b, 0, a)
			before := append([]byte(nil), c.arena...)
			starts := append([]bool(nil), c.starts...)
			dirty := append([]bool(nil), c.dirty...)
			saved := a
			nb, ob, active := c.nurseryBump, c.oldBump, c.active
			if e := c.collect([]*Ref{&a}, full); !errors.Is(e, ErrOOM) {
				t.Fatal("want OOM", e)
			}
			if !bytes.Equal(before, c.arena) || !reflect.DeepEqual(starts, c.starts) || !reflect.DeepEqual(dirty, c.dirty) || a != saved || nb != c.nurseryBump || ob != c.oldBump || active != c.active {
				t.Fatal("failed collection mutated heap")
			}
			for _, f := range c.forward {
				if f != 0 {
					t.Fatal("failed plan leaked forwarding")
				}
			}
			edge(t, c, a, 0, 0)
			if e := c.collect([]*Ref{&a}, full); e != nil {
				t.Fatal("retry", e)
			}
		})
	}
}
func TestRejectBadRootsAndDirtyEdgesAtomically(t *testing.T) {
	c := heap(t, 256, 256)
	p := obj(t, c, 1, 0)
	if e := c.Minor([]*Ref{&p}); e != nil {
		t.Fatal(e)
	}
	child := obj(t, c, 0, 0)
	edge(t, c, p, 0, child)
	// Internal corruption tests preflight; no public API exposes arena bytes.
	c.put32(uint32(p)+header, uint32(child)+8)
	before := append([]byte(nil), c.arena...)
	if e := c.Minor(nil); !errors.Is(e, ErrRef) {
		t.Fatal("bad dirty edge accepted", e)
	}
	if !bytes.Equal(before, c.arena) {
		t.Fatal("failed plan mutated heap")
	}
	bad := Ref(1)
	if e := c.Full([]*Ref{&bad}); !errors.Is(e, ErrRef) {
		t.Fatal(e)
	}
	if e := c.Full([]*Ref{nil}); !errors.Is(e, ErrRef) {
		t.Fatal(e)
	}
	if e := c.SetRef(p, 0, Ref(uint32(child)+8)); !errors.Is(e, ErrRef) {
		t.Fatal(e)
	}
}
func TestBoundsNullAndCapacity(t *testing.T) {
	c := heap(t, 32, 32)
	r := obj(t, c, 1, 1)
	if uint32(r)%8 != 0 || r == 0 {
		t.Fatal("invalid address")
	}
	if _, e := c.NewObject(0, 0); !errors.Is(e, ErrOOM) {
		t.Fatal(e)
	}
	if _, e := c.NewObject(^uint32(0), ^uint32(0)); !errors.Is(e, ErrOOM) {
		t.Fatal(e)
	}
	if e := c.SetRef(r, 1, 0); !errors.Is(e, ErrField) {
		t.Fatal(e)
	}
	if _, e := c.Word(r, 1); !errors.Is(e, ErrField) {
		t.Fatal(e)
	}
	if e := c.SetWord(0, 0, 1); !errors.Is(e, ErrRef) {
		t.Fatal(e)
	}
	null := Ref(0)
	if e := c.Full([]*Ref{&null}); e != nil {
		t.Fatal(e)
	}
	if c.oldBump != c.oldStart[c.active] {
		t.Fatal("null retained object")
	}
	for _, cfg := range [][2]uint32{{0, 32}, {32, 0}, {17, 32}, {32, ^uint32(0)}} {
		if _, e := New(cfg[0], cfg[1]); !errors.Is(e, ErrConfig) {
			t.Fatal("bad config accepted", cfg)
		}
	}
}
func TestExactRootSetDropsUnregisteredCopies(t *testing.T) {
	c := heap(t, 128, 128)
	a := obj(t, c, 0, 1)
	b := obj(t, c, 0, 1)
	oldB := b
	if e := c.Full([]*Ref{&a}); e != nil {
		t.Fatal(e)
	}
	if c.valid(oldB) {
		t.Fatal("unregistered object retained")
	}
}
func TestFullCanRecoverOldSpaceExhaustion(t *testing.T) {
	c := heap(t, 128, 64)
	dead := obj(t, c, 0, 4)
	if e := c.Minor([]*Ref{&dead}); e != nil {
		t.Fatal(e)
	}
	live := obj(t, c, 1, 0)
	edge(t, c, live, 0, live)
	if e := c.Minor([]*Ref{&live}); !errors.Is(e, ErrOOM) {
		t.Fatal(e)
	}
	if e := c.Full([]*Ref{&live}); e != nil {
		t.Fatal(e)
	}
	if get(t, c, live, 0) != live {
		t.Fatal("retry lost root cycle")
	}
}

func TestRandomGraphOracle(t *testing.T) {
	// Deterministic xorshift supplies reproducible cycles, nulls and shared edges.
	seed := uint32(0x13a47)
	next := func() uint32 { seed ^= seed << 13; seed ^= seed >> 17; seed ^= seed << 5; return seed }
	for trial := 0; trial < 50; trial++ {
		c := heap(t, 4096, 4096)
		refs := make([]Ref, 64)
		edges := make([][3]int, 64)
		for i := range refs {
			refs[i] = obj(t, c, 3, 1)
			if e := c.SetWord(refs[i], 0, uint64(i+1)); e != nil {
				t.Fatal(e)
			}
		}
		for i := range refs {
			for j := range edges[i] {
				n := int(next()%65) - 1
				edges[i][j] = n
				if n >= 0 {
					edge(t, c, refs[i], uint32(j), refs[n])
				}
			}
		}
		root := refs[0]
		for cycle := 0; cycle < 4; cycle++ {
			var e error
			if cycle == 0 {
				e = c.Minor([]*Ref{&root})
			} else {
				e = c.Full([]*Ref{&root})
			}
			if e != nil {
				t.Fatal(e)
			}
			seen := map[int]Ref{}
			queue := []Ref{root}
			for len(queue) > 0 {
				r := queue[0]
				queue = queue[1:]
				id, e := c.Word(r, 0)
				if e != nil || id < 1 || id > 64 {
					t.Fatal("bad identity", id, e)
				}
				i := int(id) - 1
				if old, ok := seen[i]; ok {
					if old != r {
						t.Fatal("sharing split")
					}
					continue
				}
				seen[i] = r
				for j, n := range edges[i] {
					v := get(t, c, r, uint32(j))
					if n < 0 {
						if v != 0 {
							t.Fatal("null changed")
						}
						continue
					}
					id, e := c.Word(v, 0)
					if e != nil || id != uint64(n+1) {
						t.Fatal("edge mismatch", trial, cycle, i, j, id, e)
					}
					queue = append(queue, v)
				}
			}
		}
	}
}

func TestCollectionUsesPreallocatedScratch(t *testing.T) {
	c := heap(t, 128, 128)
	r := obj(t, c, 1, 0)
	edge(t, c, r, 0, r)
	roots := []*Ref{&r}
	if n := testing.AllocsPerRun(100, func() {
		if e := c.Full(roots); e != nil {
			panic(e)
		}
	}); n != 0 {
		t.Fatalf("collection allocated %.1f Go objects", n)
	}
}
