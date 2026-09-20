package gc

import (
	"fmt"
	"reflect"
	"testing"
)

func TestStructCardLifecycle(t *testing.T) {
	for _, moving := range []bool{false, true} {
		for _, large := range []bool{false, true} {
			for _, order := range []string{"ordered", "reversed", "shuffled", "shuffled-sparse"} {
				for _, count := range []int{15, 16, 32, 33} {
					t.Run(fmt.Sprintf("moving=%t/large=%t/%s/ranges=%d", moving, large, order, count), func(t *testing.T) {
						f := newStructRangeFixture(t, 4097, count, order, moving, large, false, 17)
						c := f.c
						root := Root(f.parent)
						roots := Slots{&root}
						for reuse := 0; reuse < 2; reuse++ {
							if reuse > 0 {
								f.populate(t)
							}
							if got := structRangeCount(t, c, f.parent); got != count {
								t.Fatalf("reused ranges=%d", got)
							}
							for minor := 0; minor < 3; minor++ {
								wantYoung := moving && minor+1 < int(c.tenuringThreshold)
								if err := c.CollectMinor(roots); err != nil {
									t.Fatal(err)
								}
								f.parent = Ref(root)
								f.check(t, wantYoung)
								if err := c.Verify(roots); err != nil {
									t.Fatal(err)
								}
							}
							if c.entry(f.parent).cardSlot != 0 || c.cardFallback || c.RememberedCount() != 0 {
								t.Fatal("cards not cleared after promotion")
							}
							for _, field := range f.fields {
								if err := c.StructSet(f.parent, field, RefValue(Null())); err != nil {
									t.Fatal(err)
								}
							}
							if err := c.CollectFull(roots); err != nil {
								t.Fatal(err)
							}
							if c.Stats().LiveObjects != 1 {
								t.Fatalf("unreachable children remain: %d", c.Stats().LiveObjects)
							}
						}
					})
				}
			}
		}
	}
}

func TestStructCardBoundaryPaths(t *testing.T) {
	for _, fields := range []int{255, 256, 257, 4097} {
		counts := []int{15, 16}
		if fields == 4097 {
			counts = []int{15, 16, 32, 33}
		}
		for _, count := range counts {
			f := newStructRangeFixture(t, fields, count, "shuffled", true, false, fields < 4097, 29)
			f.c.clearNurseryMarks()
			got := f.c.hasSixteenObjectCardRanges(f.c.entry(f.parent).cardSlot) && f.c.scanStructCardRanges(handleOf(f.parent))
			if want := fields >= 256 && count >= 16 && count <= 32; got != want {
				t.Fatalf("fields=%d ranges=%d fast=%v want=%v", fields, count, got, want)
			}
			f.c.clearNurseryMarks()
			f.c.scanRememberedCards(handleOf(f.parent))
			if f.c.cardFallback {
				t.Fatal("valid layout enabled global fallback")
			}
		}
	}
}

func TestStructCardRejectedPrefix(t *testing.T) {
	for _, kind := range []string{"owner", "link", "cycle", "order", "start-alignment", "end-alignment", "bounds", "duplicate", "overlap", "33"} {
		t.Run(kind, func(t *testing.T) {
			count := 18
			if kind == "33" {
				count = 33
			}
			f := newStructRangeFixture(t, 4097, count, "shuffled", true, false, false, 7)
			c := f.c
			h := handleOf(f.parent)
			slots := make([]uint32, 0, count)
			for slot := c.handles[h].cardSlot; slot != 0; slot = c.objectCards[slot-1].next {
				slots = append(slots, slot)
			}
			bad := &c.objectCards[slots[16]-1]
			switch kind {
			case "owner":
				bad.handle = handleOf(mustStructCardChild(t, f))
			case "link":
				c.objectCards[slots[15]-1].next = uint32(len(c.objectCards) + 1)
			case "cycle":
				bad.next = slots[0]
			case "order":
				bad.index, bad.end = bad.end, bad.index
			case "start-alignment":
				bad.index++
			case "end-alignment":
				bad.end--
			case "bounds":
				bad.end = c.entry(f.parent).size - PayloadOffset
			case "duplicate", "overlap":
				first := c.objectCards[slots[0]-1]
				bad.index, bad.end = first.index, first.end
				if kind == "overlap" {
					bad.end += c.cardBytes
				}
			}
			c.refreshNativeCards()
			c.clearNurseryMarks()
			// Preserve non-empty pending work as well as unmarked children.
			c.markNurseryRef(mustStructCardChild(t, f))
			marks := append([]bool(nil), c.mark...)
			stack := append([]uint32(nil), c.markStack...)
			cards := append([]objectCard(nil), c.objectCards...)
			entries := append([]handleEntry(nil), c.handles...)
			c.cfg.Telemetry = new(Telemetry)
			c.cfg.Telemetry.active.active = true
			telemetry := *c.cfg.Telemetry
			if c.scanStructCardRanges(h) {
				t.Fatal("rejected metadata admitted")
			}
			if !reflect.DeepEqual(marks, c.mark) || !reflect.DeepEqual(stack, c.markStack) || !reflect.DeepEqual(cards, c.objectCards) || !reflect.DeepEqual(entries, c.handles) || c.cardFallback || !reflect.DeepEqual(telemetry, *c.cfg.Telemetry) {
				t.Fatal("rejection changed scan state")
			}
			full := kind != "duplicate" && kind != "overlap" && kind != "33"
			if !full {
				c.clearNurseryMarks()
				for slot := c.handles[h].cardSlot; slot != 0; slot = c.objectCards[slot-1].next {
					card := c.objectCards[slot-1]
					c.scanObjectPayloadRange(h, card.index, card.end)
				}
				want := append([]bool(nil), c.mark...)
				c.clearNurseryMarks()
				c.scanRememberedCards(h)
				if c.cardFallback || c.handles[h].cardSlot != slots[0] {
					t.Fatal("optimization rejection changed fallback rules")
				}
				if !reflect.DeepEqual(want, c.mark) {
					t.Fatal("rejected layout changed range-scanner semantics")
				}
				return
			}
			if err := c.verifyCardMetadata(); err == nil {
				t.Fatal("strict verifier accepted corruption")
			}
			// The final linked range is outside the valid prefix and holds a distinct
			// child. Checking every field also checks that child after full fallback.
			root := Root(f.parent)
			if err := c.CollectMinor(Slots{&root}); err != nil {
				t.Fatal(err)
			}
			f.parent = Ref(root)
			f.check(t, true)
			if !c.cardFallback || c.entry(f.parent).cardSlot != 0 {
				t.Fatal("corrupt chain was not detached")
			}
			if err := c.Verify(Slots{&root}); err == nil {
				t.Fatal("detached corruption was hidden")
			}
			if err := c.CollectMinor(Slots{&root}); err != nil {
				t.Fatal(err)
			}
			f.check(t, false)
			if c.cardFallback || len(c.objectCards) != 0 {
				t.Fatal("fallback survived nursery drain")
			}
			if err := c.Verify(Slots{&root}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func structCardDifferential(t *testing.T, seed uint64) {
	count := 15 + int(seed%19)
	order := "shuffled"
	if seed&4 != 0 {
		order = "shuffled-sparse"
	}
	f := newStructRangeFixture(t, 4097, count, order, seed&1 != 0, seed&2 != 0, false, int64(seed))
	c := f.c
	h := handleOf(f.parent)
	c.clearNurseryMarks()
	for slot := c.handles[h].cardSlot; slot != 0; slot = c.objectCards[slot-1].next {
		card := c.objectCards[slot-1]
		c.scanObjectPayloadRange(h, card.index, card.end)
	}
	want := append([]bool(nil), c.mark...)
	c.clearNurseryMarks()
	c.scanRememberedCards(h)
	if !reflect.DeepEqual(want, c.mark) {
		t.Fatalf("seed=%d marked child sets differ", seed)
	}
	if c.cardFallback {
		t.Fatal("valid generated layout fell back globally")
	}
}
func TestStructCardDifferential(t *testing.T) {
	for seed := uint64(0); seed < 57; seed++ {
		structCardDifferential(t, seed)
	}
}
func FuzzStructCardDifferential(f *testing.F) {
	for _, seed := range []uint64{0, 1, 16, 17, 18, 37, 38} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed uint64) { structCardDifferential(t, seed) })
}

func TestStructCardWarmAllocations(t *testing.T) {
	for _, count := range []int{15, 16, 32, 33} {
		f := newStructRangeFixture(t, 4097, count, "shuffled", true, false, false, 9)
		c := f.c
		h := handleOf(f.parent)
		c.clearNurseryMarks()
		c.scanRememberedCards(h)
		if len(c.markStack) != count {
			t.Fatalf("warm marked children=%d want=%d", len(c.markStack), count)
		}
		scan := func() { c.clearNurseryMarks(); c.scanRememberedCards(h) }
		if count == 16 || count == 32 {
			scan = func() {
				c.clearNurseryMarks()
				if !c.scanStructCardRanges(h) {
					panic("eligible scan rejected")
				}
			}
		}
		if allocs := testing.AllocsPerRun(100, scan); allocs != 0 {
			t.Fatalf("ranges=%d allocations=%g", count, allocs)
		}
	}
}

func TestStructCardFallbackWarmAllocations(t *testing.T) {
	f := newStructRangeFixture(t, 255, 16, "shuffled", true, false, true, 5)
	c := f.c
	scan := func() { c.clearNurseryMarks(); c.scanRememberedCards(handleOf(f.parent)) }
	scan()
	if allocs := testing.AllocsPerRun(100, scan); allocs != 0 {
		t.Fatalf("narrow fallback allocations=%g", allocs)
	}
	c, _, _ = structCardMixedParentFixture(t, 256, 2)
	h := c.objectCards[len(c.objectCards)-1].handle
	scan = func() { c.clearNurseryMarks(); c.scanRememberedCards(h) }
	scan()
	if allocs := testing.AllocsPerRun(100, scan); allocs != 0 {
		t.Fatalf("array fallback allocations=%g", allocs)
	}
}

func TestStructCardColdMarkStackAllocations(t *testing.T) {
	for _, count := range []int{16, 32} {
		f := newStructRangeFixture(t, 4097, count, "shuffled", true, false, false, 9)
		cold := testing.AllocsPerRun(10, func() {
			f.c.clearNurseryMarks()
			f.c.markStack = nil
			f.c.scanRememberedCards(handleOf(f.parent))
		})
		warm := testing.AllocsPerRun(100, func() {
			f.c.clearNurseryMarks()
			f.c.scanRememberedCards(handleOf(f.parent))
		})
		t.Logf("ranges=%d cold mark-stack allocations=%g warmed allocations=%g", count, cold, warm)
		if warm != 0 {
			t.Fatal("warmed scan allocated")
		}
	}
}

func TestStructCardFixtureGeometry(t *testing.T) {
	for _, count := range []int{15, 16, 32, 33} {
		f := newStructRangeFixture(t, 4097, count, "shuffled", true, false, false, 11)
		c := f.c
		payload := c.entry(f.parent).size - PayloadOffset
		coalesced, partial := false, false
		for slot := c.entry(f.parent).cardSlot; slot != 0; slot = c.objectCards[slot-1].next {
			card := c.objectCards[slot-1]
			coalesced = coalesced || card.end-card.index+1 > c.cardBytes
			partial = partial || card.end == payload-1 && (card.end+1)%c.cardBytes != 0
		}
		if !coalesced || !partial {
			t.Fatalf("ranges=%d coalesced=%v partial=%v", count, coalesced, partial)
		}
	}
}
