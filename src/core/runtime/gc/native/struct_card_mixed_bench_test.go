package gc

import (
	"fmt"
	"testing"
)

func structCardMixedParentFixture(tb testing.TB, fields, ranges int) (*Collector, Ref, Ref) {
	tb.Helper()
	c, parent, child := structCardScanFixture(tb, fields, ranges, false)
	array, err := NewArrayDesc(2, StorageRefNull)
	if err != nil {
		tb.Fatal(err)
	}
	if err = c.AddTypes([]TypeDesc{array}); err != nil {
		tb.Fatal(err)
	}
	other, err := c.NewArrayDefault(2, 32769)
	if err != nil {
		tb.Fatal(err)
	}
	if err = c.ForcePromote(other); err != nil {
		tb.Fatal(err)
	}
	for i := 0; i < 256; i++ {
		if err = c.ArraySet(other, uint32(i*128), RefValue(child)); err != nil {
			tb.Fatal(err)
		}
	}
	if len(c.objectCards) != ranges+256 {
		tb.Fatalf("got%d global ranges, want%d", len(c.objectCards), ranges+256)
	}
	count := 0
	for slot := c.handles[handleOf(parent)].cardSlot; slot != 0; slot = c.objectCards[slot-1].next {
		count++
	}
	if count != ranges {
		tb.Fatalf("target has%d ranges, want%d", count, ranges)
	}
	c.clearNurseryMarks()
	c.scanRememberedCards(handleOf(parent))
	if !c.mark[handleOf(child)] {
		tb.Fatal("mixed target scan lost child")
	}
	return c, parent, child
}

func BenchmarkGCStructCardScanMixedParent(b *testing.B) {
	for _, fields := range []int{256, 8192} {
		for _, ranges := range []int{1, 2, 8, 15} {
			if fields/ranges < 128 {
				continue
			}
			b.Run(fmt.Sprintf("fields=%d/ranges=%d", fields, ranges), func(b *testing.B) {
				c, parent, _ := structCardMixedParentFixture(b, fields, ranges)
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

func TestStructCardMixedParentFixtures(t *testing.T) {
	for _, fields := range []int{256, 8192} {
		for _, ranges := range []int{1, 2, 8, 15} {
			if fields/ranges < 128 {
				continue
			}
			structCardMixedParentFixture(t, fields, ranges)
		}
	}
}
