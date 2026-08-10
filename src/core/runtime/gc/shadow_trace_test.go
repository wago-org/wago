package gc

import (
	"encoding/binary"
	"fmt"
	"testing"
)

type shadowRootSink struct {
	visit func(Ref) error
	err   error
}

func (s *shadowRootSink) VisitRootRef(r Ref) bool {
	s.err = s.visit(r)
	return s.err == nil
}

// shadowTraceLive is deliberately independent of the collector's mark stack
// and scanObjectRefs. It is a slow test oracle for exact root and layout
// tracing; sharing either implementation would let the same scanner bug pass
// both the collector and its verifier.
func shadowTraceLive(c *Collector, roots RootSet) ([]bool, error) {
	live := make([]bool, len(c.handles))
	stack := make([]uint32, 0, len(c.handles))
	visit := func(r Ref) error {
		if !r.IsObj() {
			return nil
		}
		h := handleOf(r)
		if h == 0 || int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
			return fmt.Errorf("shadow trace: root or field refers to invalid handle %d", h)
		}
		if !live[h] {
			live[h] = true
			stack = append(stack, h)
		}
		return nil
	}
	if roots != nil {
		var slotRefs []Ref
		var rootErr error
		roots.RangeRoots(func(slot RootSlot) bool {
			slotRefs = append(slotRefs, slot.GetRef())
			return true
		})
		rootRefs := slotRefs
		if direct, ok := roots.(DirectRootRefSet); ok {
			var directRefs []Ref
			sink := shadowRootSink{visit: func(r Ref) error {
				directRefs = append(directRefs, r)
				return nil
			}}
			direct.RangeRootRefs(&sink)
			if sink.err != nil {
				return nil, sink.err
			}
			if !sameRootMultiset(slotRefs, directRefs) {
				return nil, fmt.Errorf("shadow trace: direct and mutable root enumeration disagree: slots=%v direct=%v", slotRefs, directRefs)
			}
			rootRefs = directRefs
		}
		for _, r := range rootRefs {
			if rootErr = visit(r); rootErr != nil {
				return nil, rootErr
			}
		}
	}
	for _, r := range c.globalSlots {
		if err := visit(r); err != nil {
			return nil, err
		}
	}
	for _, r := range c.tableSlots {
		if err := visit(r); err != nil {
			return nil, err
		}
	}

	for len(stack) != 0 {
		n := len(stack) - 1
		h := stack[n]
		stack = stack[:n]
		e := c.handles[h]
		r := makeObjRef(h)
		b := c.bytes(r)
		if len(b) < int(HeaderSize) || uint32(len(b)) != e.size {
			return nil, fmt.Errorf("shadow trace: handle %d has invalid object bounds", h)
		}
		typeID := binary.LittleEndian.Uint32(b[0:4])
		if int(typeID) >= len(c.types) {
			return nil, fmt.Errorf("shadow trace: handle %d has unknown type %d", h, typeID)
		}
		d := c.types[typeID]
		if d.Kind == KindStruct {
			for _, f := range d.Fields {
				if !isCollectorRefKind(f.Kind) {
					continue
				}
				off := uint64(PayloadOffset) + uint64(f.Offset)
				if off+4 > uint64(len(b)) {
					return nil, fmt.Errorf("shadow trace: handle %d field is out of bounds", h)
				}
				if err := visit(Ref(binary.LittleEndian.Uint32(b[off : off+4]))); err != nil {
					return nil, err
				}
			}
			continue
		}
		if d.Kind != KindArray {
			return nil, fmt.Errorf("shadow trace: handle %d has non-object type %d", h, typeID)
		}
		if !isCollectorRefKind(d.Elem) {
			continue
		}
		length := binary.LittleEndian.Uint32(b[8:12])
		for i := uint32(0); i < length; i++ {
			off := uint64(PayloadOffset) + uint64(i)*uint64(d.ElemSize)
			if off+4 > uint64(len(b)) {
				return nil, fmt.Errorf("shadow trace: handle %d array element is out of bounds", h)
			}
			if err := visit(Ref(binary.LittleEndian.Uint32(b[off : off+4]))); err != nil {
				return nil, err
			}
		}
	}
	return live, nil
}

func sameRootMultiset(a, b []Ref) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[Ref]int, len(a))
	for _, r := range a {
		counts[r]++
	}
	for _, r := range b {
		if counts[r] == 0 {
			return false
		}
		counts[r]--
	}
	return true
}

func assertFullCollectionMatchesShadow(t *testing.T, c *Collector, roots RootSet) {
	t.Helper()
	want, err := shadowTraceLive(c, roots)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	for h := uint32(1); int(h) < len(c.handles); h++ {
		got := c.handles[h].space != spaceFree
		if got != want[h] {
			t.Fatalf("handle %d live after full collection = %v, shadow trace wants %v", h, got, want[h])
		}
	}
}

func TestFullCollectionMatchesIndependentShadowTrace(t *testing.T) {
	tests := []struct {
		name string
		new  func(*testing.T) *Collector
	}{
		{"throughput", func(t *testing.T) *Collector { return newTestCollector(t, Config{}) }},
		{"tiny", func(t *testing.T) *Collector { return newTinyTestCollector(t, Config{}) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := test.new(t)
			mustStruct := func(typeID TypeID) Ref {
				r, err := c.NewStructDefault(typeID)
				if err != nil {
					t.Fatal(err)
				}
				return r
			}
			mustArray := func(typeID TypeID, length uint32) Ref {
				r, err := c.NewArrayDefault(typeID, length)
				if err != nil {
					t.Fatal(err)
				}
				return r
			}
			root := mustStruct(1)
			child := mustArray(3, 2)
			globalChild := mustStruct(1)
			tableChild := mustArray(3, 1)
			unreachable := mustStruct(1)
			pointerFree := mustStruct(0)
			if err := c.StructSet(root, 0, RefValue(child)); err != nil {
				t.Fatal(err)
			}
			if err := c.ArraySet(child, 0, RefValue(I31New(7))); err != nil {
				t.Fatal(err)
			}
			if err := c.StructSet(pointerFree, 0, I32Value(int32(unreachable))); err != nil {
				t.Fatal(err)
			}
			c.NewGlobalSlot(globalChild)
			c.NewTableSlot(tableChild)
			roots := Slots{(*Root)(&root), (*Root)(&pointerFree)}
			assertFullCollectionMatchesShadow(t, c, roots)
			if c.entry(unreachable).space != spaceFree {
				t.Fatal("reference-looking bits in pointer-free payload retained garbage")
			}
		})
	}
}

func TestShadowTraceRejectsInvalidReachableReference(t *testing.T) {
	c := newTestCollector(t, Config{})
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	b := c.bytes(parent)
	binary.LittleEndian.PutUint32(b[PayloadOffset:], uint32(makeObjRef(uint32(len(c.handles)+10))))
	root := Root(parent)
	if _, err := shadowTraceLive(c, Slots{&root}); err == nil {
		t.Fatal("shadow trace accepted an invalid reachable reference")
	}
}

type mismatchedShadowRoots struct {
	slot   Root
	direct Ref
}

func (r *mismatchedShadowRoots) RangeRoots(fn func(RootSlot) bool) { fn(&r.slot) }
func (r *mismatchedShadowRoots) RangeRootRefs(sink RootRefSink) bool {
	return sink.VisitRootRef(r.direct)
}

func TestShadowTraceRejectsDivergentDirectRootEnumeration(t *testing.T) {
	c := newTestCollector(t, Config{})
	first, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	roots := &mismatchedShadowRoots{slot: Root(first), direct: second}
	if _, err := shadowTraceLive(c, roots); err == nil {
		t.Fatal("shadow trace accepted divergent direct and mutable root enumeration")
	}
}

func TestFullCollectionMatchesShadowForDirectRoots(t *testing.T) {
	c := newTestCollector(t, Config{})
	root, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	unreachable, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	roots := directTestRoots{root}
	assertFullCollectionMatchesShadow(t, c, &roots)
	if c.entry(unreachable).space != spaceFree {
		t.Fatal("direct-root shadow trace retained unreachable object")
	}
}
