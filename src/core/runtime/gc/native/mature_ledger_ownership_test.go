package gc

import (
	"slices"
	"testing"
)

func TestMatureLedgerIndexedTailRollback(t *testing.T) {
	h := newMatureRunHeap(t)
	seed, err := h.alloc(112, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := h.alloc(144, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.free(seed); err != nil {
		t.Fatal(err)
	}
	handles := []handleEntry{{}, anchor}
	if err := h.verify(handles); err != nil {
		t.Fatal(err)
	}
	before := slices.Clone(h.freeSpans())
	tx := h.beginAllocTransaction()
	run, ok, err := h.tryAllocRun(24, 3, spaceOld)
	if err != nil || !ok || run.lastAllocSize != 48 {
		t.Fatalf("run %+v %v %v", run, ok, err)
	}
	for i := 2; i >= 0; i-- {
		e := handleEntry{off: run.off + uint32(i)*run.allocSize, size: 24, class: run.class, space: spaceOld}
		if i == 2 {
			e.class |= handleAbsorbedTailBit
		}
		h.rollbackSuccessfulAlloc(e, tx.bump)
	}
	h.restoreAllocTransaction(tx)
	if !slices.Equal(before, h.freeSpans()) || h.freeBytes != 112 || h.pendingBytes != 0 || h.bump != 256 {
		t.Fatal("lost indexed physical extent", h.freeSpans())
	}
	if err := h.verify(handles); err != nil {
		t.Fatal(err)
	}
	retry, ok, err := h.tryAllocRun(24, 3, spaceOld)
	if err != nil || !ok || retry != run {
		t.Fatal("exact retry failed", retry, ok, err)
	}
}

func TestMatureLedgerFirstFitFallback(t *testing.T) {
	h := newMatureRunHeap(t)
	h.bump = 256
	for _, s := range []throughputFreeSpan{{off: 0, size: 32}, {off: 64, size: 128}} {
		if err := h.insertFreeSpan(s); err != nil {
			t.Fatal(err)
		}
	}
	before := slices.Clone(h.freeSpans())
	h.beginMatureAdmission(true)
	if _, ok, err := h.tryAllocRun(24, 3, spaceOld); err != nil || ok {
		t.Fatal("bypassed first fit", ok, err)
	}
	if !slices.Equal(before, h.freeSpans()) || h.freeBytes != 160 || h.bump != 256 {
		t.Fatal("fallback mutated ownership")
	}
	e, err := h.alloc(24, spaceOld)
	if err != nil || e.off != 0 {
		t.Fatal("individual first fit", e, err)
	}
}

func matureLedgerMixedPublic(t *testing.T) (*Collector, Slots, handleEntry) {
	t.Helper()
	c := stage4Collector(t, 6)
	dead := regionStruct(t, c, 0, EmptyRoots{})
	roots := Slots{(*Root)(&dead)}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	live, err := c.NewArrayDefaultWithRoots(2, 2048, roots)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ArraySet(live, 2047, I32Value(77)); err != nil {
		t.Fatal(err)
	}
	entry := *c.entry(live)
	if entry.off != 32 || entry.space != spaceLarge {
		t.Fatal("bad mixed fixture", entry)
	}
	return c, Slots{(*Root)(&live)}, entry
}
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

// Regression probe: a successful allocation omitted from handles and free/debt
// must not become invisible to verification after admission cancellation.
func TestMatureLedgerDetectLostOwnership(t *testing.T) {
	h := newMatureRunHeap(t)
	h.beginAllocTransaction()
	if _, err := h.alloc(32, spaceOld); err != nil {
		t.Fatal(err)
	}
	h.commitMatureAdmission()
	if err := h.verify([]handleEntry{{}}); err == nil {
		t.Fatal("Verify accepted 32 owned bytes absent from handles/index/debt")
	}
}

func TestMatureLedgerDetectOmittedIndexedRunRollback(t *testing.T) {
	h := newMatureRunHeap(t)
	seed, err := h.alloc(112, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := h.alloc(144, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.free(seed); err != nil {
		t.Fatal(err)
	}
	handles := []handleEntry{{}, anchor}
	if err = h.verify(handles); err != nil {
		t.Fatal("invalid initial fixture", err)
	}
	tx := h.beginAllocTransaction()
	if _, ok, err := h.tryAllocRun(24, 3, spaceOld); err != nil || !ok {
		t.Fatal(ok, err)
	}
	rejected := false
	func() {
		defer func() {
			if recover() != nil {
				rejected = true
			}
		}()
		h.restoreAllocTransaction(tx)
	}()
	if !rejected && h.verify(handles) == nil {
		t.Fatal("cancel and Verify both accepted omitted indexed rollback; 112 bytes lost")
	}
}
