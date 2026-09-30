package gc

import (
	"math/rand"
	"testing"
)

// Compare the represented union with a byte-level model, independent of both
// the bounds shortcut and the linked-list insertion algorithm.
func TestSparseCardBoundsRangeTransitions(t *testing.T) {
	for _, seed := range []int64{1, 7, 31, 73, 127, 1024} {
		c, parent, _ := sparseCardBoundsFixture(t, 16)
		h := handleOf(parent)
		payload := c.handles[h].size - PayloadOffset
		expected := make([]bool, payload)
		rng := rand.New(rand.NewSource(seed))
		check := func(step int) {
			t.Helper()
			actual := make([]bool, payload)
			steps := 0
			for slot := c.handles[h].cardSlot; slot != 0; slot = c.objectCards[slot-1].next {
				steps++
				if steps > len(c.objectCards) {
					t.Fatal("cyclic card list")
				}
				card := c.objectCards[slot-1]
				if card.handle != h || card.end >= payload || card.index > card.end {
					t.Fatalf("invalid card: %+v", card)
				}
				for i := card.index; i <= card.end; i++ {
					actual[i] = true
				}
			}
			for i := range expected {
				if actual[i] != expected[i] {
					t.Fatalf("seed=%d step=%d byte=%d got=%v want=%v", seed, step, i, actual[i], expected[i])
				}
			}
		}
		for step := 0; step < 300; step++ {
			if step%37 == 36 {
				if step%2 == 0 {
					c.clearCardMetadata()
				} else {
					c.removeCardsForHandle(h)
				}
				clear(expected)
				if c.lastCardBounds != (objectCardBounds{}) {
					t.Fatal("reset retained bounds")
				}
			} else {
				start := uint32(rng.Intn(int(payload)))
				end := start + uint32(rng.Intn(int(c.cardBytes)*3))
				// Include exact boundaries, payload clipping and an adjacent bridge.
				switch step % 7 {
				case 0:
					start, end = 0, 0
				case 1:
					start, end = payload-1, ^uint32(0)
				case 2:
					start, end = c.cardBytes-1, c.cardBytes
				}
				c.addObjectCardRange(h, start, end)
				start &^= c.cardBytes - 1
				if end >= payload {
					end = payload - 1
				}
				end |= c.cardBytes - 1
				if end >= payload {
					end = payload - 1
				}
				for i := start; i <= end; i++ {
					expected[i] = true
				}
			}
			check(step)
		}
	}
}

func TestSparseCardBoundsDistinctChildrenSurviveCycles(t *testing.T) {
	for _, order := range []string{"ascending", "descending", "mixed"} {
		t.Run(order, func(t *testing.T) {
			const count = 32
			c, parent, _ := sparseCardBoundsFixture(t, count)
			root := Root(parent)
			roots := Slots{&root}
			for cycle := 0; cycle < 3; cycle++ {
				children := make([]Ref, count)
				for i := range children {
					index := i
					if order == "descending" {
						index = count - 1 - i
					}
					if order == "mixed" {
						index = (i * 13) % count
					}
					child, err := c.NewStructDefault(0)
					if err != nil {
						t.Fatal(err)
					}
					children[i] = child
					if err = c.ArraySet(parent, uint32(index*128), RefValue(child)); err != nil {
						t.Fatal(err)
					}
				}
				if err := c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
				for i, child := range children {
					if !c.validObjectRef(child) {
						t.Fatalf("cycle=%d child=%d lost", cycle, i)
					}
				}
				if c.lastCardBounds != (objectCardBounds{}) {
					t.Fatal("minor retained bounds")
				}
				for i := 0; i < count; i++ {
					if err := c.ArraySet(parent, uint32(i*128), RefValue(Null())); err != nil {
						t.Fatal(err)
					}
				}
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				if c.lastCardBounds != (objectCardBounds{}) {
					t.Fatal("full collection retained bounds")
				}
			}
		})
	}
}

func TestSparseCardBoundsMovingSurvivorExtension(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 4 << 20, VerifyAfterCollect: true}, []TypeDesc{leaf, refs})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	parent, err := c.NewArrayDefault(1, 4097)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	root := Root(parent)
	roots := Slots{&root}
	children := make([]Ref, 0, 4)
	for round := 0; round < 2; round++ {
		for i := 0; i < 2; i++ {
			child, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			children = append(children, child)
			if err = c.ArraySet(parent, uint32((round*2+i)*128), RefValue(child)); err != nil {
				t.Fatal(err)
			}
		}
		if c.lastCardBounds.head == 0 {
			t.Fatal("exterior stores did not establish proof")
		}
		if err = c.CollectMinor(roots); err != nil {
			t.Fatal(err)
		}
		if !c.entry(children[len(children)-1]).young() {
			t.Fatal("test requires a retained young survivor")
		}
		for i, child := range children {
			if !c.validObjectRef(child) {
				t.Fatalf("round=%d child=%d lost", round, i)
			}
		}
	}
	if err = c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	for i, child := range children {
		if !c.validObjectRef(child) {
			t.Fatalf("full collection lost child=%d", i)
		}
	}
}
