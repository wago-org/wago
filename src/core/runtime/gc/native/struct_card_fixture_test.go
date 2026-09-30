package gc

import (
	"math/rand"
	"testing"
)

type structRangeFixture struct {
	c      *Collector
	parent Ref
	fields []uint32
	values []int32
}

// Padded layouts exercise the field-count boundary without reducing card size.
func newStructRangeFixture(tb testing.TB, fields, count int, order string, moving, large, padded bool, seed int64) structRangeFixture {
	tb.Helper()
	kinds := make([]StorageKind, fields)
	for i := range kinds {
		kinds[i] = StorageRefNull
		if order == "shuffled-sparse" && i%32 != 0 || order != "shuffled-sparse" && i%7 == 6 {
			kinds[i] = StorageI32
		}
	}
	leaf, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		tb.Fatal(err)
	}
	desc, err := NewStructDesc(1, kinds)
	if err != nil {
		tb.Fatal(err)
	}
	if padded {
		for i := range desc.Fields {
			desc.Fields[i].Offset = uint32(i * 16)
		}
		desc.Size = uint32((fields-1)*16 + 4)
	}
	switch order {
	case "reversed":
		for i, j := 0, len(desc.Fields)-1; i < j; i, j = i+1, j-1 {
			desc.Fields[i], desc.Fields[j] = desc.Fields[j], desc.Fields[i]
		}
	case "shuffled", "shuffled-sparse":
		rand.New(rand.NewSource(seed)).Shuffle(len(desc.Fields), func(i, j int) { desc.Fields[i], desc.Fields[j] = desc.Fields[j], desc.Fields[i] })
	}
	limit := uint32(1 << 20)
	if large {
		limit = 1024
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 4 << 20, LargeObjectBytes: limit, DisableMovingNursery: !moving}, []TypeDesc{leaf, desc})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(c.Close)
	parent, err := c.NewStructDefault(1)
	if err != nil {
		tb.Fatal(err)
	}
	if err = c.ForcePromote(parent); err != nil {
		tb.Fatal(err)
	}
	wantSpace := spaceOld
	if large {
		wantSpace = spaceLarge
	}
	if c.entry(parent).space != wantSpace {
		tb.Fatalf("parent space=%v want=%v", c.entry(parent).space, wantSpace)
	}
	f := structRangeFixture{c: c, parent: parent}
	lastCard := (c.entry(parent).size - PayloadOffset - 1) / c.cardBytes
	for n := 0; n < count; n++ {
		card := uint32(n) * lastCard / uint32(count-1)
		found := 0
		for i, field := range desc.Fields {
			if field.Offset/c.cardBytes != card || !isCollectorRefKind(field.Kind) {
				continue
			}
			f.fields = append(f.fields, uint32(i))
			f.values = append(f.values, int32(n+101))
			found++
			if found == 2 {
				break
			}
		}
		if found == 0 {
			tb.Fatalf("no reference in card %d", card)
		}
	}
	// Add an adjacent card to the first range. Other untouched reference fields
	// remain null; two fields in each selected card share one child.
	if lastCard/uint32(count-1) > 2 {
		for i, field := range desc.Fields {
			if field.Offset/c.cardBytes == 1 && isCollectorRefKind(field.Kind) {
				f.fields = append(f.fields, uint32(i))
				f.values = append(f.values, 101)
				break
			}
		}
	}
	f.populate(tb)
	if got := structRangeCount(tb, c, parent); got != count {
		tb.Fatalf("ranges=%d want=%d", got, count)
	}
	root := Root(parent)
	if err = c.Verify(Slots{&root}); err != nil {
		tb.Fatal(err)
	}
	return f
}

func (f structRangeFixture) populate(tb testing.TB) {
	tb.Helper()
	children := make(map[int32]Ref)
	for i, field := range f.fields {
		value := f.values[i]
		child, ok := children[value]
		if !ok {
			var err error
			child, err = f.c.NewStructDefault(0)
			if err == nil {
				err = f.c.StructSet(child, 0, I32Value(value))
			}
			if err != nil {
				tb.Fatal(err)
			}
			children[value] = child
		}
		if err := f.c.StructSet(f.parent, field, RefValue(child)); err != nil {
			tb.Fatal(err)
		}
	}
}
func (f structRangeFixture) check(tb testing.TB, young bool) {
	tb.Helper()
	for i, field := range f.fields {
		child, err := f.c.StructGet(f.parent, field)
		if err != nil {
			tb.Fatal(err)
		}
		value, err := f.c.StructGet(child.Ref, 0)
		if err != nil || int32(value.Bits) != f.values[i] {
			tb.Fatalf("field %d: value=%v err=%v", field, value, err)
		}
		if f.c.entry(child.Ref).young() != young {
			tb.Fatalf("field %d young=%v want=%v", field, f.c.entry(child.Ref).young(), young)
		}
	}
}
func structRangeCount(tb testing.TB, c *Collector, parent Ref) int {
	tb.Helper()
	count := 0
	for slot := c.entry(parent).cardSlot; slot != 0; slot = c.objectCards[slot-1].next {
		if count >= len(c.objectCards) || !slotIndexOK(slot-1, len(c.objectCards)) {
			tb.Fatal("invalid fixture chain")
		}
		count++
	}
	return count
}

func mustStructCardChild(tb testing.TB, f structRangeFixture) Ref {
	tb.Helper()
	v, err := f.c.StructGet(f.parent, f.fields[0])
	if err != nil {
		tb.Fatal(err)
	}
	return v.Ref
}
