package gc

import (
	"strings"
	"testing"
)

func TestMalformedObjectCardChainsFallBackToWholeObjectScan(t *testing.T) {
	tests := []struct {
		name   string
		cards  func(h, other, payloadBytes uint32) []objectCard
		head   func(cards []objectCard) uint32
		verify string
	}{
		{
			name: "invalid head slot",
			cards: func(h, _, _ uint32) []objectCard {
				return []objectCard{{handle: h, index: 0, end: 127}}
			},
			head:   func(cards []objectCard) uint32 { return uint32(len(cards) + 1) },
			verify: "stale object card slot",
		},
		{
			name: "invalid next slot",
			cards: func(h, _, _ uint32) []objectCard {
				return []objectCard{{handle: h, index: 0, end: 127, next: 2}}
			},
			verify: "stale object card slot",
		},
		{
			name: "wrong owner",
			cards: func(_, other, _ uint32) []objectCard {
				return []objectCard{{handle: other, index: 0, end: 127}}
			},
			verify: "owner=",
		},
		{
			name: "self cycle",
			cards: func(h, _, _ uint32) []objectCard {
				return []objectCard{{handle: h, index: 0, end: 127, next: 1}}
			},
			verify: "multiply linked",
		},
		{
			name: "multi-node cycle",
			cards: func(h, _, _ uint32) []objectCard {
				return []objectCard{
					{handle: h, index: 0, end: 127, next: 2},
					{handle: h, index: 128, end: 255, next: 1},
				}
			},
			verify: "multiply linked",
		},
		{
			name: "reversed range",
			cards: func(h, _, _ uint32) []objectCard {
				return []objectCard{{handle: h, index: 128, end: 127}}
			},
			verify: "invalid object card range",
		},
		{
			name: "range beyond payload",
			cards: func(h, _, payloadBytes uint32) []objectCard {
				return []objectCard{{handle: h, index: 0, end: payloadBytes}}
			},
			verify: "invalid object card range",
		},
		{
			name: "valid prefix then malformed range",
			cards: func(h, _, _ uint32) []objectCard {
				return []objectCard{
					{handle: h, index: 0, end: 127, next: 2},
					{handle: h, index: 256, end: 255},
				}
			},
			verify: "invalid object card range",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := newTestCollector(t, Config{NurseryBytes: 4096, SurvivorBytes: 4096})
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
			// Publish the only young edge outside the valid 0..127-byte prefix.
			if err := c.ArraySet(array, 64, RefValue(child)); err != nil {
				t.Fatal(err)
			}

			h := handleOf(array)
			payloadBytes := c.handles[h].size - PayloadOffset
			cards := test.cards(h, handleOf(child), payloadBytes)
			c.objectCards = cards
			c.freeObjectCardSlot = 0
			c.handles[h].cardSlot = 1
			if test.head != nil {
				c.handles[h].cardSlot = test.head(cards)
			}
			c.refreshNativeCards()

			root := Root(array)
			if err := c.verifyCardMetadata(); err == nil || !strings.Contains(err.Error(), test.verify) {
				t.Fatalf("strict card verifier error = %v, want substring %q", err, test.verify)
			}
			if err := c.CollectMinor(Slots{&root}); err != nil {
				t.Fatalf("minor collection: %v", err)
			}
			if !c.validObjectRef(child) || !c.entry(child).young() {
				t.Fatalf("young child outside valid card prefix was reclaimed: entry=%+v", c.entry(child))
			}
			if !c.cardFallback {
				t.Fatal("malformed chain did not leave conservative fallback active")
			}
			if err := c.Verify(Slots{&root}); err == nil {
				t.Fatal("strict verifier accepted detached malformed card records")
			}

			// The fallback remains authoritative for another minor. Once the child
			// tenures and no young object remains, clearing all card metadata is safe.
			if err := c.CollectMinor(Slots{&root}); err != nil {
				t.Fatalf("second minor collection: %v", err)
			}
			if c.entry(child).young() || c.cardFallback || len(c.objectCards) != 0 {
				t.Fatalf("drained malformed metadata: child young=%v fallback=%v cards=%d", c.entry(child).young(), c.cardFallback, len(c.objectCards))
			}
			if err := c.Verify(Slots{&root}); err != nil {
				t.Fatalf("verify after safe metadata clear: %v", err)
			}
		})
	}
}
