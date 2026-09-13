package gc

import "testing"

func TestGCStructCardsRetainDistinctChildren(t *testing.T) {
	for _, unordered := range []bool{false, true} {
		for _, cards := range []int{1, 2, 4, 8, 15, 16, 32, 33} {
			c, parent, _ := structCardScanFixture(t, 8192, cards, unordered)
			children := make([]Ref, cards)
			for i := range children {
				child, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				children[i] = child
				index := 8191
				if cards > 1 {
					index = i * 8191 / (cards - 1)
				}
				if err = c.StructSet(parent, uint32(index), RefValue(child)); err != nil {
					t.Fatal(err)
				}
			}
			c.clearNurseryMarks()
			c.scanRememberedCards(handleOf(parent))
			for i, child := range children {
				if !c.mark[handleOf(child)] {
					t.Fatalf("unordered=%t range=%d/%d child was not marked", unordered, i, cards)
				}
			}
			root := Root(parent)
			if err := c.CollectMinor(Slots{&root}); err != nil {
				t.Fatal(err)
			}
			for i, child := range children {
				if !c.validObjectRef(child) {
					t.Fatalf("unordered=%t range=%d/%d child was collected", unordered, i, cards)
				}
			}
		}
	}
}

func TestGCStructRangeAdmission(t *testing.T) {
	for _, mode := range []string{"ordered", "reversed", "near"} {
		for _, cards := range []int{1, 2, 4, 8, 15, 16, 32, 33} {
			c, parent, _ := structCardScanFixtureOrder(t, 8192, cards, mode)
			c.clearNurseryMarks()
			got := c.scanStructCardRanges(handleOf(parent))
			if got != (cards >= 16 && cards <= 32) {
				t.Fatalf("mode=%s cards=%d admission=%t", mode, cards, got)
			}
		}
	}
}

func TestStructCardRangeCountGuard(t *testing.T) {
	for _, count := range []int{1, 2, 3, 4, 8, 15, 16, 32} {
		c, parent, _ := structCardScanFixture(t, 8192, count, false)
		if got := c.hasSixteenObjectCardRanges(c.handles[handleOf(parent)].cardSlot); got != (count >= 16) {
			t.Fatalf("count=%d guard=%t", count, got)
		}
	}
	c, parent, _ := structCardMixedParentFixture(t, 256, 1)
	if c.hasSixteenObjectCardRanges(c.handles[handleOf(parent)].cardSlot) {
		t.Fatal("other object ranges satisfied target guard")
	}
	if c.hasSixteenObjectCardRanges(0) || c.hasSixteenObjectCardRanges(uint32(len(c.objectCards)+1)) {
		t.Fatal("invalid prefix passed count guard")
	}
}
