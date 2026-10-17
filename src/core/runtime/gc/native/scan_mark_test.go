//go:build amd64

package gc

import (
	"slices"
	"testing"
)

func TestDirectMarkScansMatchVisitor(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	scalar, err := NewArrayDesc(2, StorageI8)
	if err != nil {
		t.Fatal(err)
	}
	types := []TypeDesc{leaf, refs, scalar}
	for _, n := range []int{4, 8, 64} {
		fields := make([]StorageKind, n)
		for i := range fields {
			fields[i] = StorageRefNull
			if n == 64 && i%8 != 7 {
				fields[i] = StorageI64
			}
		}
		d, err := NewStructDesc(TypeID(len(types)), fields)
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, d)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20}, types)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	young, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	old, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(old); err != nil {
		t.Fatal(err)
	}
	large, err := c.NewArrayDefault(2, 65536)
	if err != nil {
		t.Fatal(err)
	}
	values := []Ref{Null(), I31New(7), young, old, large, young}
	parents := []Ref{large}
	array, err := c.NewArrayDefault(1, uint32(len(values)))
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range values {
		if err := c.ArraySet(array, uint32(i), RefValue(v)); err != nil {
			t.Fatal(err)
		}
	}
	parents = append(parents, array)
	for _, d := range types[3:] {
		parent, err := c.NewStructDefault(d.ID)
		if err != nil {
			t.Fatal(err)
		}
		slot := 0
		for i, f := range d.Fields {
			if isCollectorRefKind(f.Kind) {
				if err := c.StructSet(parent, uint32(i), RefValue(values[slot%len(values)])); err != nil {
					t.Fatal(err)
				}
				slot++
			}
		}
		parents = append(parents, parent)
	}
	for _, parent := range parents {
		for _, nursery := range []bool{false, true} {
			c.clearMarks()
			visit := c.markRef
			if nursery {
				visit = c.markNurseryRef
			}
			c.scanObjectRefs(handleOf(parent), visit)
			want := append([]uint32(nil), c.markStack...)
			c.clearMarks()
			if nursery {
				c.scanObjectNursery(handleOf(parent))
			} else {
				c.scanObjectMark(handleOf(parent))
			}
			if !slices.Equal(c.markStack, want) {
				t.Fatalf("parent%d nursery%t got%v want%v", parent, nursery, c.markStack, want)
			}
		}
	}
}
