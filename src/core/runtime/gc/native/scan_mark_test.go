//go:build amd64 || arm64

package gc

import (
	"encoding/binary"
	"fmt"
	"slices"
	"testing"
)

func scanObjectNurseryByValue(c *Collector, h uint32) {
	r := makeObjRef(h)
	d, err := c.refDesc(r)
	if err != nil || !d.HasRefs {
		return
	}
	hdr := c.header(r)
	b := c.bytes(r)
	if d.Kind == KindStruct {
		if nativeCollectorFastPaths && d.firstRef != 0 {
			for index := uint32(d.firstRef); index != 0; {
				field := d.Fields[index-1]
				c.markNurseryRef(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+field.Offset:])))
				index = uint32(field.nextRef)
			}
			return
		}
		for _, f := range d.Fields {
			if isCollectorRefKind(f.Kind) {
				c.markNurseryRef(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+f.Offset:])))
			}
		}
	} else if d.ArrayElementsAreRefs() {
		for i := uint32(0); i < hdr.Aux; i++ {
			off := PayloadOffset + i*d.ElemSize
			c.markNurseryRef(Ref(binary.LittleEndian.Uint32(b[off:])))
		}
	}
}

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

func TestNurseryScanPointerDescriptorMatchesValueSnapshot(t *testing.T) {
	structure, err := NewStructDesc(0, []StorageKind{StorageRefNull, StorageRefNull, StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	scalars, err := NewArrayDesc(2, StorageI8)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20}, []TypeDesc{structure, refs, scalars})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range []Ref{parent, Null(), I31New(7)} {
		if err := c.StructSet(parent, uint32(i), RefValue(value)); err != nil {
			t.Fatal(err)
		}
	}
	array, err := c.NewArrayDefault(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range []Ref{child, Null(), I31New(9)} {
		if err := c.ArraySet(array, uint32(i), RefValue(value)); err != nil {
			t.Fatal(err)
		}
	}
	scalar, err := c.NewArrayDefault(2, 3)
	if err != nil {
		t.Fatal(err)
	}
	added, err := NewStructDesc(3, []StorageKind{StorageI64, StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AddTypes([]TypeDesc{added}); err != nil {
		t.Fatal(err)
	}
	appended, err := c.NewStructDefault(3)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(appended, 1, RefValue(child)); err != nil {
		t.Fatal(err)
	}

	run := func(scan func()) (panicValue string, stack []uint32, marks []bool) {
		c.clearMarks()
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					panicValue = fmt.Sprint(recovered)
				}
			}()
			scan()
		}()
		return panicValue, slices.Clone(c.markStack), slices.Clone(c.mark)
	}
	compare := func(t *testing.T, h uint32) {
		t.Helper()
		gotPanic, gotStack, gotMarks := run(func() { c.scanObjectNursery(h) })
		wantPanic, wantStack, wantMarks := run(func() { scanObjectNurseryByValue(c, h) })
		if gotPanic != wantPanic || !slices.Equal(gotStack, wantStack) || !slices.Equal(gotMarks, wantMarks) {
			t.Fatalf("pointer panic/stack/marks = %q/%v/%v; value = %q/%v/%v", gotPanic, gotStack, gotMarks, wantPanic, wantStack, wantMarks)
		}
	}

	t.Run("struct cycle null i31", func(t *testing.T) { compare(t, handleOf(parent)) })
	t.Run("reference array", func(t *testing.T) { compare(t, handleOf(array)) })
	t.Run("non-reference array", func(t *testing.T) { compare(t, handleOf(scalar)) })
	t.Run("appended type", func(t *testing.T) { compare(t, handleOf(appended)) })
	t.Run("invalid reference", func(t *testing.T) { compare(t, uint32(len(c.handles)+10)) })

	t.Run("corrupt struct header words", func(t *testing.T) {
		b := c.bytes(parent)
		saved := slices.Clone(b[4:HeaderSize])
		for i := uint32(4); i < HeaderSize; i++ {
			b[i] = 0xff
		}
		compare(t, handleOf(parent))
		copy(b[4:HeaderSize], saved)
	})
	t.Run("unknown type", func(t *testing.T) {
		b := c.bytes(parent)
		saved := binary.LittleEndian.Uint32(b)
		binary.LittleEndian.PutUint32(b, uint32(len(c.types)))
		compare(t, handleOf(parent))
		binary.LittleEndian.PutUint32(b, saved)
	})
	t.Run("short extent", func(t *testing.T) {
		e := c.entry(parent)
		saved := e.size
		e.size = HeaderSize - 1
		compare(t, handleOf(parent))
		e.size = saved
	})
	t.Run("reference array extent panic", func(t *testing.T) {
		b := c.bytes(array)
		saved := binary.LittleEndian.Uint32(b[8:12])
		binary.LittleEndian.PutUint32(b[8:12], saved+2)
		compare(t, handleOf(array))
		binary.LittleEndian.PutUint32(b[8:12], saved)
	})
}
