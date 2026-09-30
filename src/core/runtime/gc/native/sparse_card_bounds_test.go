package gc

import (
	"reflect"
	"testing"
	"unsafe"
)

func TestSparseCardBoundsMatchesListSearch(t *testing.T) {
	for _, order := range []string{"ascending", "descending", "random"} {
		t.Run(order, func(t *testing.T) {
			c, parent, child := sparseCardBoundsFixture(t, 64)
			oracle, otherParent, otherChild := sparseCardBoundsFixture(t, 64)
			indexes := make([]uint32, 64)
			for i := range indexes {
				index := i
				if order == "descending" {
					index = 63 - i
				}
				if order == "random" {
					index = (i * 73) % 64
				}
				indexes[i] = uint32(index * 128)
			}
			// Bridge existing ranges, then extend again after a merge invalidates bounds.
			indexes = append(indexes, 64, 192, 64*128)
			for round := 0; round < 2; round++ {
				c.clearCardMetadata()
				oracle.clearCardMetadata()
				for _, index := range indexes {
					oracle.lastCardBounds = objectCardBounds{}
					if err := c.ArraySet(parent, index, RefValue(child)); err != nil {
						t.Fatal(err)
					}
					if err := oracle.ArraySet(otherParent, index, RefValue(otherChild)); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(c.objectCards, oracle.objectCards) {
						t.Fatalf("round=%d index=%d: bounds path differs from list search", round, index)
					}
				}
			}
			root := Root(parent)
			if err := c.CollectMinor(Slots{&root}); err != nil {
				t.Fatal(err)
			}
			if !c.validObjectRef(child) {
				t.Fatal("carded child lost")
			}
			if c.lastCardBounds != (objectCardBounds{}) {
				t.Fatal("collection retained card bounds")
			}
		})
	}
}

func TestSparseCardBoundsSwitchObjects(t *testing.T) {
	c, parent, child := sparseCardBoundsFixture(t, 8)
	other, err := c.NewArrayDefault(1, 1025)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(other); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		for _, ref := range []Ref{parent, other} {
			if err = c.ArraySet(ref, uint32(i*128), RefValue(child)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(c.objectCards) != 16 {
		t.Fatalf("got %d ranges, want 16", len(c.objectCards))
	}
	root, otherRoot := Root(parent), Root(other)
	if err = c.CollectMinor(Slots{&root, &otherRoot}); err != nil {
		t.Fatal(err)
	}
	if !c.validObjectRef(child) {
		t.Fatal("carded child lost")
	}
}

func TestSparseCardBoundsRemovedSlots(t *testing.T) {
	c, parent, child := sparseCardBoundsFixture(t, 8)
	oracle, otherParent, otherChild := sparseCardBoundsFixture(t, 8)
	for round := 0; round < 3; round++ {
		for _, index := range []uint32{0, 256, 512, 768} {
			oracle.lastCardBounds = objectCardBounds{}
			if err := c.ArraySet(parent, index, RefValue(child)); err != nil {
				t.Fatal(err)
			}
			if err := oracle.ArraySet(otherParent, index, RefValue(otherChild)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(c.objectCards, oracle.objectCards) || c.freeObjectCardSlot != oracle.freeObjectCardSlot {
				t.Fatalf("round=%d index=%d card-slot reuse differs", round, index)
			}
		}
		if c.lastCardBounds.head == 0 {
			t.Fatal("removal case must start with a live proof")
		}
		c.removeCardsForHandle(handleOf(parent))
		if c.lastCardBounds.head != 0 {
			t.Fatal("removal retained live proof")
		}
		oracle.removeCardsForHandle(handleOf(otherParent))
	}
}

func TestSparseCardBoundsOutermostHead(t *testing.T) {
	c, parent, child := sparseCardBoundsFixture(t, 16)
	for _, index := range []uint32{512, 768, 256, 1024, 0, 640} {
		if err := c.ArraySet(parent, index, RefValue(child)); err != nil {
			t.Fatal(err)
		}
		proof := c.lastCardBounds
		if index == 640 {
			if proof.head != 0 {
				t.Fatal("interior insertion retained outermost-head proof")
			}
			continue
		}
		if proof.head == 0 {
			t.Fatal("exterior insertion lost proof")
		}
		head := c.objectCards[proof.head-1]
		low, high := head.index, head.end
		for slot := head.next; slot != 0; slot = c.objectCards[slot-1].next {
			card := c.objectCards[slot-1]
			low = min(low, card.index)
			high = max(high, card.end)
		}
		if min(head.index, proof.oppositeEdge) != low || max(head.end, proof.oppositeEdge) != high {
			t.Fatal("compact proof does not cover exact list")
		}
	}
	if unsafe.Sizeof(objectCardBounds{}) != 8 {
		t.Fatal("proof must remain eight bytes")
	}
	var state Collector
	if unsafe.Offsetof(state.lastCardBounds) != unsafe.Offsetof(state.checkedHandles)+unsafe.Sizeof(state.checkedHandles) {
		t.Fatal("proof must follow existing collector fields")
	}
}
