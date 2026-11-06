//go:build linux && amd64 && !tinygo && !wago_target_tinygo && !wago_guardpage

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
)

func TestConversionReceiptReuseIdentityDomainAndStaleRelease(t *testing.T) {
	store := newReferenceStore(false)
	a, pa, ta, wa := h2ReceiptFixture(t, store, 0)
	b, pb, tb, wb := h2ReceiptFixture(t, store, 6)
	defer a.Close()
	defer pa.Close()
	defer b.Close()
	defer pb.Close()
	h2TableSet(t, ta, 0, wa)
	old := h2AssertReceipt(t, ta.conversionContainers[0], 0)
	// Make a second object/identity in the same publisher domain.
	if _, err := a.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	s := a.existingGCExternConversionState()
	var nextWord uint64
	for _, e := range s.entries {
		if e.externWord != 0 && e.externWord != wa {
			nextWord = e.externWord
		}
	}
	if nextWord == 0 {
		t.Fatal("fixture requires distinct second identity")
	}
	h2TableSet(t, ta, 0, 0)
	h2TableSet(t, ta, 0, nextWord)
	next := h2AssertReceipt(t, ta.conversionContainers[0], 0)
	if next.root != old.root || next.identity == old.identity {
		t.Fatal("different identity did not reuse root")
	}
	h2WithAuthority(t, store, func() { old.release(); old.release() })
	if ta.conversionContainers[0].slots[0] != next {
		t.Fatal("stale release removed new receipt")
	}
	h2AssertReceipt(t, ta.conversionContainers[0], 0)
	h2TableSet(t, tb, 0, wb)
	other := h2AssertReceipt(t, tb.conversionContainers[0], 0)
	if other.identity.domain == next.identity.domain {
		t.Fatal("fixture requires independent domain")
	}
	h2TableSet(t, tb, 0, 0)
	h2TableSet(t, ta, 0, 0)
	h2TableSet(t, tb, 0, nextWord)
	moved := h2AssertReceipt(t, tb.conversionContainers[0], 0)
	if moved.root != next.root || moved.identity.domain != next.identity.domain {
		t.Fatal("physical owner transfer did not use identity domain pool")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	h2AssertReceipt(t, tb.conversionContainers[0], 0)
	h2TableSet(t, tb, 0, 0)
	h2TableSet(t, tb, 0, wb)
	h2AssertReceipt(t, tb.conversionContainers[0], 0)
}

func TestConversionReceiptReuseFailureBoundaries(t *testing.T) {
	store := newReferenceStore(false)
	p, provider, table, word := h2ReceiptFixture(t, store, 0)
	defer p.Close()
	defer provider.Close()
	s := p.existingGCExternConversionState()
	d := s.core.domain
	c := table.conversionContainers[0]
	h2TableSet(t, table, 0, word)
	old := h2AssertReceipt(t, c, 0)
	h2TableSet(t, table, 0, 0)
	h2WithAuthority(t, store, func() {
		count := len(d.receiptFreeSlots)
		// An invalid object ref must fail before the free-list pop.
		if _, err := acquireReceiptRoot(d, gc.Ref(0xfffffffe)); err == nil {
			t.Fatal("invalid ref accepted")
		}
		if len(d.receiptFreeSlots) != count || d.receiptFreeSlots[count-1] != old.root {
			t.Fatal("failed acquire consumed root")
		}
		if ref, err := d.collector.CheckedTableSlot(old.root); err != nil || !ref.IsNull() {
			t.Fatal("failed acquire changed root")
		}
		// Synthetic foreign-Runtime owner reaches bind's first failure through
		// prepare, without introducing pre-existing root ownership corruption.
		original := c.owner.refStore
		c.owner.refStore = newReferenceStore(false)
		tx, err := c.prepare(0, word, s)
		c.owner.refStore = original
		if err == nil || tx != nil {
			t.Fatal("foreign owner bind accepted")
		}
		if len(d.receiptFreeSlots) != count {
			t.Fatal("failed bind did not return exactly one root")
		}
		v, _ := managedFunctionCollectors.Load(d.collector)
		state := v.(*managedFunctionCollector)
		if state.tableOwners[old.root] != nil || state.conversionHostSlots[old.root] != nil {
			t.Fatal("failed bind retained owner")
		}
		for _, member := range state.graph.members {
			for _, slot := range member.slots {
				if slot.collector == d.collector && slot.kind == gc.SlotTable && slot.index == old.root {
					t.Fatal("failed bind retained backlink")
				}
			}
		}
		if ref, err := d.collector.CheckedTableSlot(old.root); err != nil || !ref.IsNull() {
			t.Fatal("failed bind did not clear root")
		}
	})
	drainManagedFunctionStoreReclaims(store)
	h2TableSet(t, table, 0, word)
	if got := h2AssertReceipt(t, c, 0); got.root != old.root {
		t.Fatal("failure retry grew root directory")
	}
}

func TestConversionReceiptReuseRootKindsAndCollection(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		leaf, err := gc.NewStructDesc(0, nil)
		if err != nil {
			t.Fatal(err)
		}
		cfg := gc.Config{Profile: profile, VerifyAfterCollect: true}
		if profile == gc.ProfileThroughput {
			cfg.NurseryBytes = 4096
			cfg.SurvivorBytes = 4096
		} else {
			cfg.TinyHeapBytes = 1 << 20
			cfg.TinyBlockBytes = 16
		}
		collector, err := gc.NewCollector(cfg, []gc.TypeDesc{leaf})
		if err != nil {
			t.Fatal(err)
		}
		d := &gcStoreDomain{collector: collector}
		root, err := acquireReceiptRoot(d, gc.I31New(7))
		if err != nil {
			t.Fatal(err)
		}
		clearReceiptRoot(d, root)
		d.receiptFreeSlots = append(d.receiptFreeSlots, root)
		for i := 0; i < 8; i++ {
			object, err := collector.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			got, err := acquireReceiptRoot(d, object)
			if err != nil || got != root {
				t.Fatal("object reuse", got, err)
			}
			if profile == gc.ProfileThroughput {
				if err := collector.CollectMinor(nil); err != nil {
					t.Fatal(err)
				}
			}
			if err := collector.CollectFull(nil); err != nil {
				t.Fatal(err)
			}
			if _, err := collector.ObjectMayHaveChildren(object); err != nil {
				t.Fatal("reused root failed to retain object", err)
			}
			clearReceiptRoot(d, root)
			d.receiptFreeSlots = append(d.receiptFreeSlots, root)
			got, err = acquireReceiptRoot(d, gc.I31New(int32(i)))
			if err != nil || got != root {
				t.Fatal("i31 reuse", got, err)
			}
			clearReceiptRoot(d, root)
			d.receiptFreeSlots = append(d.receiptFreeSlots, root)
		}
		if collector.PersistentSlotCount(gc.SlotTable) != 1 {
			t.Fatal("root-kind churn grew physical slots")
		}
		collector.Close()
	}
}
