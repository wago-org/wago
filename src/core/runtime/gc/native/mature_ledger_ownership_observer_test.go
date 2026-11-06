//go:build (linux || (darwin && arm64)) && !tinygo && !wago_target_tinygo

package gc

import (
	"slices"
	"testing"
)

func TestMatureLedgerPublicMixedFull(t *testing.T) {
	c, roots, entry := matureLedgerMixedPublic(t)
	callbacks := 0
	remove := c.AddReclaimObserver(func(r Ref) {
		e := *c.entry(r)
		if e.off != 0 || e.space != spaceOld {
			t.Fatal("callback identity already mutated", e)
		}
		if c.throughput.freeBytes != 0 || c.throughput.pendingBytes != 0 {
			t.Fatal("reuse published before callback")
		}
		callbacks++
	})
	defer remove()
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.throughput.pendingBytes != 0 || !slices.Equal(c.throughput.freeSpans(), []throughputFreeSpan{{off: 0, size: 32}}) {
		t.Fatal("public mixed debt not reconciled")
	}
	if callbacks != 1 {
		t.Fatal("missing callback", callbacks)
	}
	live := roots[0].GetRef()
	if *c.entry(live) != entry {
		t.Fatal("LOS moved")
	}
	v, err := c.ArrayGet(live, 2047)
	if err != nil || v.I32() != 77 {
		t.Fatal("LOS payload", v, err)
	}
	if err = c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}
