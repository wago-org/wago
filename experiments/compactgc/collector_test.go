package compactgc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"unsafe"
)

var node = Descriptor{Kind: KindStruct, Align: 4, PayloadBytes: 12, RefOffsets: []uint32{0, 4}}

func heap(t *testing.T, n, o uint32, types ...Descriptor) *Collector {
	t.Helper()
	c, e := NewWithTypes(n, o, types)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func obj(t *testing.T, c *Collector, id TypeID) Ref {
	t.Helper()
	r, e := c.NewStruct(id)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func arr(t *testing.T, c *Collector, id TypeID, n uint32) Ref {
	t.Helper()
	r, e := c.NewArray(id, n)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func set(t *testing.T, c *Collector, r Ref, i uint32, v Ref) {
	t.Helper()
	if e := c.SetRef(r, i, v); e != nil {
		t.Fatal(e)
	}
}
func edge(t *testing.T, c *Collector, r Ref, i uint32) Ref {
	t.Helper()
	v, e := c.RefField(r, i)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func allocations(t *testing.T, c *Collector, want uint64) {
	t.Helper()
	for i := 0; i < 3; i++ {
		n, _ := c.AllocationSnapshot()
		if n != want {
			t.Fatalf("allocations%d want%d", n, want)
		}
	}
}

func TestCompactLayoutAndMixedMoves(t *testing.T) {
	types := []Descriptor{
		{Kind: KindStruct, Align: 4, PayloadBytes: 4}, {Kind: KindStruct, Align: 8, PayloadBytes: 8},
		{Kind: KindStruct, Align: 16, PayloadBytes: 16}, {Kind: KindStruct, Align: 1},
		{Kind: KindArray, Align: 1, ElementSize: 1}, {Kind: KindArray, Align: 16, ElementSize: 16},
		{Kind: KindArray, Align: 4, ElementSize: 4, ElementRef: true}, node,
	}
	c := heap(t, 1024, 2048, types...)
	refs := []Ref{obj(t, c, 0), obj(t, c, 1), obj(t, c, 2), obj(t, c, 3), arr(t, c, 4, 3), arr(t, c, 5, 2), arr(t, c, 6, 3), obj(t, c, 7)}
	offsets := []uint32{4, 8, 16, 4, 8, 16, 8, 4}
	strides := []uint32{16, 16, 32, 16, 16, 48, 32, 16}
	for i, r := range refs {
		h, e := c.Header(r)
		if e != nil {
			t.Fatal(e)
		}
		if h.PayloadOffset != offsets[i] || h.Stride != strides[i] {
			t.Fatalf("layout%d %+v", i, h)
		}
		if i < 6 {
			p, _ := c.Payload(r)
			for j := range p {
				p[j] = byte(i + j)
			}
			if cap(p) != len(p) {
				t.Fatal("borrow extends past payload")
			}
			if len(p) > 0 && uintptr(unsafe.Pointer(&p[0]))%uintptr(types[i].Align) != 0 {
				t.Fatal("unaligned payload")
			}
		}
	}
	set(t, c, refs[6], 0, refs[7])
	set(t, c, refs[6], 1, I31New(-5))
	set(t, c, refs[7], 0, refs[6])
	set(t, c, refs[7], 1, refs[0])
	roots := make([]*Ref, len(refs))
	for i := range refs {
		roots[i] = &refs[i]
	}
	roots = append(roots, &refs[6], &refs[6])
	for round := 0; round < 4; round++ {
		var e error
		if round == 0 {
			e = c.Minor(roots)
		} else {
			e = c.Full(roots)
		}
		if e != nil {
			t.Fatal(e)
		}
		for i, r := range refs {
			h, e := c.Header(r)
			if e != nil || h.TypeID != TypeID(i) || h.PayloadOffset != offsets[i] || h.Stride != strides[i] {
				t.Fatalf("movedlayout%d %+v %v", i, h, e)
			}
			if i < 6 {
				p, _ := c.Payload(r)
				for j, v := range p {
					if v != byte(i+j) {
						t.Fatal("payload changed")
					}
				}
			}
		}
		if edge(t, c, refs[6], 0) != refs[7] || edge(t, c, refs[7], 0) != refs[6] || edge(t, c, refs[6], 1) != I31New(-5) {
			t.Fatal("cycle/sharing/i31 changed")
		}
		allocations(t, c, 8)
	}
	if e := c.Full(nil); e != nil {
		t.Fatal(e)
	}
	allocations(t, c, 8)
	if c.LiveObjects() != 0 {
		t.Fatal("unrooted objects retained")
	}
}

func TestCompactBarrierAndOversized(t *testing.T) {
	c := heap(t, 32, 512, node, Descriptor{Kind: KindArray, Align: 4, ElementSize: 4, ElementRef: true})
	parent := arr(t, c, 1, 20) // intrinsically oversized, allocated old
	child := obj(t, c, 0)
	set(t, c, parent, 0, child)
	set(t, c, parent, 1, child)
	if c.IsYoung(parent) || !c.IsYoung(child) || len(c.remembered) != 1 {
		t.Fatal("wrong generations or barrier dedup")
	}
	root := parent
	if e := c.Minor([]*Ref{&root}); e != nil {
		t.Fatal(e)
	}
	child = edge(t, c, root, 0)
	if child != edge(t, c, root, 1) || c.IsYoung(child) {
		t.Fatal("young edge not promoted")
	}
	young := obj(t, c, 0)
	set(t, c, root, 2, young)
	if e := c.Full([]*Ref{&root, &root}); e != nil {
		t.Fatal(e)
	}
	if edge(t, c, root, 0) != edge(t, c, root, 1) || edge(t, c, root, 2) == 0 {
		t.Fatal("full lost edges")
	}
	allocations(t, c, 3)
}

type snapshot struct {
	arena             []byte
	starts, dirty     []bool
	remembered        []Ref
	overflow          bool
	nursery, old      uint32
	active            int
	retired, oldCount uint64
}

func snap(c *Collector) snapshot {
	return snapshot{append([]byte{}, c.arena...), append([]bool{}, c.starts...), append([]bool{}, c.dirty...), append([]Ref{}, c.remembered...), c.rememberedOverflow, c.nurseryBump, c.oldBump, c.active, c.retiredNurseryAllocations, c.oldAllocations}
}
func TestCompactPreflightAtomic(t *testing.T) {
	c := heap(t, 64, 16, node)
	a, b := obj(t, c, 0), obj(t, c, 0)
	set(t, c, a, 0, b)
	before := snap(c)
	oldA := a
	for _, full := range []bool{false, true} {
		var e error
		if full {
			e = c.Full([]*Ref{&a, &a})
		} else {
			e = c.Minor([]*Ref{&a, &a})
		}
		if !errors.Is(e, ErrOOM) {
			t.Fatal(e)
		}
		if a != oldA || !reflect.DeepEqual(before, snap(c)) {
			t.Fatal("OOM mutated authoritative state")
		}
		allocations(t, c, 2)
	}
	bad := Ref(8)
	if e := c.Full([]*Ref{&bad}); !errors.Is(e, ErrRef) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, snap(c)) {
		t.Fatal("invalid root mutated state")
	}
	set(t, c, a, 0, 0)
	if e := c.Full([]*Ref{&a}); e != nil {
		t.Fatal(e)
	}
	allocations(t, c, 2)
	before = snap(c)
	if _, e := c.NewArray(0, 1); !errors.Is(e, ErrType) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, snap(c)) {
		t.Fatal("bad allocation mutated heap")
	}
}

func TestCompactRandomGraphOracle(t *testing.T) {
	for seed := int64(0); seed < 40; seed++ {
		c := heap(t, 4096, 8192, node)
		rng := rand.New(rand.NewSource(seed))
		const n = 80
		refs := make([]Ref, n)
		graph := make([][2]int, n)
		for i := range refs {
			refs[i] = obj(t, c, 0)
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], uint32(i))
			if e := c.WriteBytes(refs[i], 8, b[:]); e != nil {
				t.Fatal(e)
			}
		}
		for i, r := range refs {
			for j := 0; j < 2; j++ {
				target := rng.Intn(n+1) - 1
				graph[i][j] = target
				if target >= 0 {
					set(t, c, r, uint32(j), refs[target])
				}
			}
		}
		root := refs[0]
		for round := 0; round < 4; round++ {
			var e error
			if round == 0 {
				e = c.Minor([]*Ref{&root, &root})
			} else {
				e = c.Full([]*Ref{&root, &root})
			}
			if e != nil {
				t.Fatal(e)
			}
			seen := map[int]Ref{}
			var visit func(Ref, int)
			visit = func(r Ref, id int) {
				if old, ok := seen[id]; ok {
					if old != r {
						t.Fatal("sharing changed")
					}
					return
				}
				seen[id] = r
				p, e := c.Payload(r)
				if e != nil || binary.LittleEndian.Uint32(p[8:]) != uint32(id) {
					t.Fatal("identity changed")
				}
				for j, want := range graph[id] {
					v := edge(t, c, r, uint32(j))
					if want < 0 {
						if v != 0 {
							t.Fatal("null changed")
						}
					} else {
						visit(v, want)
					}
				}
			}
			visit(root, 0)
			allocations(t, c, n)
		}
	}
}

func TestCompactDescriptorsAndBounds(t *testing.T) {
	d := node
	d.RefOffsets = append([]uint32{}, d.RefOffsets...)
	c := heap(t, 128, 256, d)
	d.RefOffsets[0] = 8 // deep copy must isolate descriptor
	r := obj(t, c, 0)
	set(t, c, r, 0, I31New(7))
	if edge(t, c, r, 0) != I31New(7) {
		t.Fatal("descriptor alias")
	}
	d.RefOffsets[0] = 0
	before := len(c.types)
	if _, e := c.AddTypes([]Descriptor{{Kind: KindArray, Align: 1, ElementSize: 3}}); !errors.Is(e, ErrType) || len(c.types) != before {
		t.Fatal("invalid batch published")
	}
	if e := c.WriteBytes(r, 0, []byte{1}); !errors.Is(e, ErrField) {
		t.Fatal("overwrote reference")
	}
	if e := c.WriteBytes(r, 12, []byte{1}); !errors.Is(e, ErrField) {
		t.Fatal("overwrote end")
	}
	if e := c.SetRef(r, 0, Ref(8)); !errors.Is(e, ErrRef) {
		t.Fatal("accepted interior")
	}
	c2 := heap(t, 32, 64, Descriptor{Kind: KindArray, Align: 1, ElementSize: 1})
	z := arr(t, c2, 0, 0)
	p, _ := c2.Payload(z)
	if len(p) != 0 || cap(p) != 0 {
		t.Fatal("empty payload")
	}
	beforeArena := append([]byte{}, c2.arena...)
	if _, e := c2.NewArray(0, ^uint32(0)); !errors.Is(e, ErrOOM) || !bytes.Equal(beforeArena, c2.arena) {
		t.Fatal("overflow mutated arena")
	}
}

func TestCompactMemoryAndNoAllocCollection(t *testing.T) {
	c := heap(t, 128, 512, node)
	m := c.MemoryUsage()
	t.Logf("memory %+v total=%d fixed=%d", m, m.Total(), c.FixedBytes())
	if m.Arena != uint64(16+128+1024+15) || m.Starts != uint64((16+128+1024)/8) || m.Queue != uint64((16+128+1024)/16*4) {
		t.Fatalf("backing %+v", m)
	}
	if m.Total() != c.FixedBytes()+m.Collector+m.Descriptors+m.ReferenceOffsets {
		t.Fatal("memory accounting")
	}
	r := obj(t, c, 0)
	roots := []*Ref{&r}
	if a := testing.AllocsPerRun(20, func() {
		if e := c.Full(roots); e != nil {
			panic(e)
		}
	}); a != 0 {
		t.Fatalf("collection allocates %v", a)
	}
}
