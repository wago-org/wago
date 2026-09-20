//go:build wago_gcstats

package gc

import (
	"testing"
	"time"
)

func TestStructCardRangesTelemetryParity(t *testing.T) {
	for _, mode := range []string{"ordered", "reversed", "near"} {
		for _, count := range []int{16, 32} {
			c, parent, _ := structCardScanFixtureOrder(t, 8192, count, mode)
			h := handleOf(parent)
			c.clearNurseryMarks()
			c.cfg.Telemetry = new(Telemetry)
			c.cfg.Telemetry.active.active = true
			var payload, slots, dirty, useful uint64
			for slot := c.handles[h].cardSlot; slot != 0; slot = c.objectCards[slot-1].next {
				card := c.objectCards[slot-1]
				n, u := c.scanObjectPayloadRange(h, card.index, card.end)
				slots += uint64(n)
				useful += uint64(u)
				payload += uint64(card.end-card.index) + 1
				dirty += uint64(card.end/c.cardBytes-card.index/c.cardBytes) + 1
			}
			c.cfg.Telemetry.noteCardScan(time.Time{}, payload, slots, dirty, useful, payload >= uint64(c.handles[h].size-PayloadOffset))
			wantCards, wantTrace := c.cfg.Telemetry.active.cards, c.cfg.Telemetry.active.trace
			c.clearNurseryMarks()
			c.cfg.Telemetry = new(Telemetry)
			c.cfg.Telemetry.active.active = true
			if !c.scanStructCardRanges(h) {
				t.Fatal("expected range admission")
			}
			if c.cfg.Telemetry.active.cards != wantCards || c.cfg.Telemetry.active.trace != wantTrace {
				t.Fatalf("mode=%s count=%d telemetry differs", mode, count)
			}
		}
	}
}

func TestStructCardMixedRangesTelemetryParity(t *testing.T) {
	for _, order := range []string{"ordered", "reversed", "shuffled"} {
		for _, count := range []int{16, 32} {
			f := newStructRangeFixture(t, 4097, count, order, true, false, false, 31)
			c := f.c
			h := handleOf(f.parent)
			c.clearNurseryMarks()
			c.cfg.Telemetry = new(Telemetry)
			c.cfg.Telemetry.active.active = true
			var payload, slots, dirty, useful uint64
			for slot := c.handles[h].cardSlot; slot != 0; slot = c.objectCards[slot-1].next {
				card := c.objectCards[slot-1]
				n, u := c.scanObjectPayloadRange(h, card.index, card.end)
				slots += uint64(n)
				useful += uint64(u)
				payload += uint64(card.end-card.index) + 1
				dirty += uint64(card.end/c.cardBytes-card.index/c.cardBytes) + 1
			}
			c.cfg.Telemetry.noteCardScan(time.Time{}, payload, slots, dirty, useful, payload >= uint64(c.handles[h].size-PayloadOffset))
			wantCards, wantTrace := c.cfg.Telemetry.active.cards, c.cfg.Telemetry.active.trace
			c.clearNurseryMarks()
			c.cfg.Telemetry = new(Telemetry)
			c.cfg.Telemetry.active.active = true
			if !c.scanStructCardRanges(h) {
				t.Fatal("expected range admission")
			}
			if c.cfg.Telemetry.active.cards != wantCards || c.cfg.Telemetry.active.trace != wantTrace {
				t.Fatalf("order=%s ranges=%d telemetry differs", order, count)
			}
		}
	}
}
