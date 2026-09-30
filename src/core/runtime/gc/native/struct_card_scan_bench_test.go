package gc

import (
	"fmt"
	"testing"
)

func structCardScanFixture(tb testing.TB, fields, cards int, unordered bool) (*Collector, Ref, Ref) {
	mode := "ordered"
	if unordered {
		mode = "reversed"
	}
	return structCardScanFixtureOrder(tb, fields, cards, mode)
}
func structCardScanFixtureOrder(tb testing.TB, fields, cards int, mode string) (*Collector, Ref, Ref) {
	tb.Helper()
	kinds := make([]StorageKind, fields)
	for i := range kinds {
		kinds[i] = StorageRefNull
	}
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		tb.Fatal(err)
	}
	desc, err := NewStructDesc(1, kinds)
	if err != nil {
		tb.Fatal(err)
	}
	if mode == "near" {
		n := len(desc.Fields)
		desc.Fields[n-2], desc.Fields[n-1] = desc.Fields[n-1], desc.Fields[n-2]
	}
	if mode == "reversed" {
		for i, j := 0, len(desc.Fields)-1; i < j; i, j = i+1, j-1 {
			desc.Fields[i], desc.Fields[j] = desc.Fields[j], desc.Fields[i]
		}
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 4 << 20, DisableMovingNursery: true}, []TypeDesc{leaf, desc})
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
	child, err := c.NewStructDefault(0)
	if err != nil {
		tb.Fatal(err)
	}
	for i := 0; i < cards; i++ {
		index := fields - 1
		if cards > 1 {
			index = i * (fields - 1) / (cards - 1)
		}
		if err = c.StructSet(parent, uint32(index), RefValue(child)); err != nil {
			tb.Fatal(err)
		}
	}
	if len(c.objectCards) != cards {
		tb.Fatalf("got %d ranges, want %d", len(c.objectCards), cards)
	}
	c.clearNurseryMarks()
	c.scanRememberedCards(handleOf(parent))
	if !c.mark[handleOf(child)] {
		tb.Fatal("scan did not mark child")
	}
	return c, parent, child
}

func BenchmarkGCStructCardScan(b *testing.B) {
	for _, fields := range []int{8, 256, 8192} {
		for _, cards := range []int{1, 2, 4, 8, 15, 16, 32, 33} {
			if cards > 1 && fields/cards < 128 {
				continue
			}
			for _, unordered := range []bool{false, true} {
				b.Run(fmt.Sprintf("fields=%d/ranges=%d/unordered=%t", fields, cards, unordered), func(b *testing.B) {
					c, parent, _ := structCardScanFixture(b, fields, cards, unordered)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						c.clearNurseryMarks()
						c.scanRememberedCards(handleOf(parent))
					}
				})
			}
		}
	}
}

func TestGCStructCardScanFixtures(t *testing.T) {
	for _, unordered := range []bool{false, true} {
		for _, cards := range []int{1, 2, 4, 8, 15, 16, 32, 33} {
			c, parent, child := structCardScanFixture(t, 8192, cards, unordered)
			root := Root(parent)
			if err := c.CollectMinor(Slots{&root}); err != nil {
				t.Fatal(err)
			}
			if !c.validObjectRef(child) {
				t.Fatal("minor collection lost a carded child")
			}
		}
	}
}

func BenchmarkGCStructCardScanNearOrdered(b *testing.B) {
	for _, fields := range []int{256, 8192} {
		for _, cards := range []int{1, 2, 4, 8, 15, 16, 32, 33} {
			if fields/cards < 128 {
				continue
			}
			b.Run(fmt.Sprintf("fields=%d/ranges=%d", fields, cards), func(b *testing.B) {
				c, parent, _ := structCardScanFixtureOrder(b, fields, cards, "near")
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					c.clearNurseryMarks()
					c.scanRememberedCards(handleOf(parent))
				}
			})
		}
	}
}
func TestGCStructCardScanNearOrdered(t *testing.T) {
	for _, cards := range []int{1, 2, 4, 8, 15, 16, 32, 33} {
		c, parent, child := structCardScanFixtureOrder(t, 8192, cards, "near")
		root := Root(parent)
		if err := c.CollectMinor(Slots{&root}); err != nil {
			t.Fatal(err)
		}
		if !c.validObjectRef(child) {
			t.Fatal("near ordered child lost")
		}
	}
}
