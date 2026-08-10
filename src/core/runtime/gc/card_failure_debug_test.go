//go:build wagodebug

package gc

import (
	"fmt"
	"testing"
)

func TestInjectedObjectCardGrowthFallsBackToWholeObjectScan(t *testing.T) {
	c := newTestCollector(t, Config{VerifyAfterCollect: true})
	array, err := c.NewArrayDefault(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := armFailure(c, failObjectCardGrowth, 0)
	if err := c.ArraySet(array, 0, RefValue(child)); err != nil {
		cleanup()
		t.Fatal(err)
	}
	cleanup()
	if c.RememberedCount() != 1 || c.handles[handleOf(array)].cardSlot != 0 || !c.cardFallback {
		t.Fatalf("object-card fallback metadata remembered=%d slot=%d fallback=%v", c.RememberedCount(), c.handles[handleOf(array)].cardSlot, c.cardFallback)
	}
	root := Root(array)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(child).space != spaceOld {
		t.Fatalf("fallback-scanned child space=%v, want old", c.entry(child).space)
	}
}

func TestObjectCardFallbackSurvivesLaterSuccessfulWrite(t *testing.T) {
	c := newTestCollector(t, Config{VerifyAfterCollect: true})
	array, err := c.NewArrayDefault(3, 65)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	first, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := armFailure(c, failObjectCardGrowth, 0)
	if err := c.ArraySet(array, 0, RefValue(first)); err != nil {
		cleanup()
		t.Fatal(err)
	}
	cleanup()
	if err := c.ArraySet(array, 64, RefValue(second)); err != nil {
		t.Fatal(err)
	}
	if slot := c.handles[handleOf(array)].cardSlot; slot == 0 || !c.cardFallback {
		t.Fatalf("later write lost whole-object fallback: slot=%d fallback=%v", slot, c.cardFallback)
	}
	root := Root(array)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(first).space != spaceOld || c.entry(second).space != spaceOld {
		t.Fatalf("fallback-scanned child spaces=%v/%v, want old/old", c.entry(first).space, c.entry(second).space)
	}
	if slot := c.handles[handleOf(array)].cardSlot; slot != 0 || c.cardFallback {
		t.Fatalf("drained fallback slot/state = %d/%v, want 0/false", slot, c.cardFallback)
	}
}

func TestObjectCardFallbackPersistsWhileSurvivorEdgeRemains(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, SurvivorBytes: 4096, VerifyAfterCollect: true})
	array, err := c.NewArrayDefault(3, 65)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := armFailure(c, failObjectCardGrowth, 0)
	if err := c.ArraySet(array, 0, RefValue(child)); err != nil {
		cleanup()
		t.Fatal(err)
	}
	cleanup()
	root := Root(array)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if !c.entry(child).young() || c.handles[handleOf(array)].cardSlot != 0 || !c.cardFallback {
		t.Fatalf("first minor child/fallback = young:%v slot:%d fallback:%v", c.entry(child).young(), c.handles[handleOf(array)].cardSlot, c.cardFallback)
	}
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(child).young() || c.handles[handleOf(array)].cardSlot != 0 || c.handles[handleOf(array)].remembered || c.cardFallback {
		t.Fatalf("second minor child/fallback/remembered = young:%v slot:%d remembered:%v fallback:%v", c.entry(child).young(), c.handles[handleOf(array)].cardSlot, c.handles[handleOf(array)].remembered, c.cardFallback)
	}
}

func TestInjectedDisjointObjectCardGrowthWidensExistingCard(t *testing.T) {
	for _, verify := range []bool{false, true} {
		t.Run(fmt.Sprintf("verify=%v", verify), func(t *testing.T) {
			c := newTestCollector(t, Config{VerifyAfterCollect: verify})
			array, err := c.NewArrayDefault(3, 65)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ForcePromote(array); err != nil {
				t.Fatal(err)
			}
			first, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			second, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ArraySet(array, 0, RefValue(first)); err != nil {
				t.Fatal(err)
			}
			if slot := c.handles[handleOf(array)].cardSlot; slot == 0 {
				t.Fatal("first store did not establish an exact card")
			}
			cleanup := armFailure(c, failObjectCardGrowth, 0)
			if err := c.ArraySet(array, 64, RefValue(second)); err != nil {
				cleanup()
				t.Fatal(err)
			}
			cleanup()
			card := c.objectCards[c.handles[handleOf(array)].cardSlot-1]
			if card.index != 0 || card.end != c.handles[handleOf(array)].size-PayloadOffset-1 {
				t.Fatalf("growth fallback card = %+v, want whole payload", card)
			}
			root := Root(array)
			if err := c.CollectMinor(Slots{&root}); err != nil {
				t.Fatal(err)
			}
			if c.entry(first).space != spaceOld || c.entry(second).space != spaceOld {
				t.Fatalf("fallback-scanned child spaces=%v/%v, want old/old", c.entry(first).space, c.entry(second).space)
			}
		})
	}
}

func TestInjectedRootCardGrowthFallsBackToAllPersistentSlots(t *testing.T) {
	c := newTestCollector(t, Config{VerifyAfterCollect: true})
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	slot := c.NewGlobalSlot(Null())
	cleanup := armFailure(c, failSlotCardGrowth, 0)
	if err := c.SetGlobalSlot(slot, child); err != nil {
		cleanup()
		t.Fatal(err)
	}
	cleanup()
	if !c.cardFallback || len(c.slotCards) != 0 {
		t.Fatalf("root-card fallback=%v cards=%v", c.cardFallback, c.slotCards)
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if c.entry(child).space != spaceOld {
		t.Fatalf("fallback-rooted child space=%v, want old", c.entry(child).space)
	}
	if c.cardFallback {
		t.Fatal("minor collection did not clear root-card fallback")
	}
}
