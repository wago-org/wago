package gc

import (
	"testing"
)

type throughputDeferredModelPair struct {
	h     throughputHeap
	model throughputIntervalModel
	live  []handleEntry
}

func newThroughputDeferredModelPair(t testing.TB) *throughputDeferredModelPair {
	t.Helper()
	const limit = 64 << 10
	p := &throughputDeferredModelPair{model: throughputIntervalModel{limit: limit, classLimit: 32}}
	if err := p.h.Init(Config{ThroughputHeapBytes: limit, ThroughputPageBytes: 4096, ThroughputClassLimit: 32}); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *throughputDeferredModelPair) alloc(t testing.TB, size uint32) handleEntry {
	t.Helper()
	got, gotErr := p.h.alloc(size, spaceLarge)
	want, wantErr := p.model.alloc(size, spaceLarge)
	if gotErr != nil || wantErr != nil {
		t.Fatalf("alloc %d errors = %v/%v", size, gotErr, wantErr)
	}
	if got != want {
		t.Fatalf("alloc %d = %+v, want %+v", size, got, want)
	}
	p.live = append(p.live, got)
	p.assert(t)
	return got
}

func (p *throughputDeferredModelPair) release(t testing.TB, e handleEntry, deferred bool) {
	t.Helper()
	if deferred {
		if err := p.h.deferFree(e); err != nil {
			t.Fatal(err)
		}
		if err := p.model.deferRelease(e); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := p.h.free(e); err != nil {
			t.Fatal(err)
		}
		if err := p.model.release(e); err != nil {
			t.Fatal(err)
		}
	}
	for i := range p.live {
		if p.live[i] == e {
			p.live[i] = p.live[len(p.live)-1]
			p.live = p.live[:len(p.live)-1]
			p.assert(t)
			return
		}
	}
	t.Fatalf("released entry not live: %+v", e)
}

func (p *throughputDeferredModelPair) reconcileAll(t testing.TB) {
	t.Helper()
	if err := p.h.sweepAllPending(); err != nil {
		t.Fatal(err)
	}
	if err := p.model.reconcileAll(); err != nil {
		t.Fatal(err)
	}
	p.assert(t)
}

func (p *throughputDeferredModelPair) assert(t testing.TB) {
	t.Helper()
	assertThroughputMatchesModel(t, &p.h, &p.model, p.live)
}

func TestThroughputDeferredAllocationOrderingModel(t *testing.T) {
	p := newThroughputDeferredModelPair(t)
	low := p.alloc(t, 32)
	p.alloc(t, 32)
	high := p.alloc(t, 32)
	p.alloc(t, 32)
	p.release(t, high, false)
	p.release(t, low, true)

	got := p.alloc(t, 32)
	if got.off != high.off {
		t.Fatalf("indexed fit offset=%d, want higher reconciled offset %d", got.off, high.off)
	}
	if len(p.h.pendingFree) != 1 || p.h.pendingFree[0].off != low.off {
		t.Fatalf("lower pending fit was reconciled early: %v", p.h.pendingFree)
	}
}

func TestThroughputDeferredReconciliationModelCases(t *testing.T) {
	t.Run("non-address order", func(t *testing.T) {
		p := newThroughputDeferredModelPair(t)
		a, b, c := p.alloc(t, 32), p.alloc(t, 32), p.alloc(t, 32)
		p.alloc(t, 32)
		p.release(t, c, true)
		p.release(t, a, true)
		p.release(t, b, true)
		if got := p.alloc(t, 32); got.off != b.off {
			t.Fatalf("first LIFO pending fit offset=%d, want %d", got.off, b.off)
		}
		if len(p.h.pendingFree) != 2 {
			t.Fatalf("pending after partial reconciliation=%v", p.h.pendingFree)
		}
	})

	for _, tc := range []struct {
		name      string
		freeOrder []int
		deferred  int
		want      throughputFreeSpan
	}{
		{name: "indexed predecessor", freeOrder: []int{0}, deferred: 1, want: throughputFreeSpan{off: 0, size: 64}},
		{name: "indexed successor", freeOrder: []int{1}, deferred: 0, want: throughputFreeSpan{off: 0, size: 64}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newThroughputDeferredModelPair(t)
			entries := []handleEntry{p.alloc(t, 32), p.alloc(t, 32)}
			p.alloc(t, 32)
			for _, i := range tc.freeOrder {
				p.release(t, entries[i], false)
			}
			p.release(t, entries[tc.deferred], true)
			p.reconcileAll(t)
			if spans := p.h.freeSpans(); len(spans) != 1 || spans[0] != tc.want {
				t.Fatalf("coalesced spans=%v, want %v", spans, tc.want)
			}
		})
	}

	t.Run("bridge both indexed neighbors", func(t *testing.T) {
		p := newThroughputDeferredModelPair(t)
		a, bridge, c := p.alloc(t, 32), p.alloc(t, 32), p.alloc(t, 32)
		p.alloc(t, 32)
		p.release(t, a, false)
		p.release(t, c, false)
		p.release(t, bridge, true)
		p.reconcileAll(t)
		want := throughputFreeSpan{off: 0, size: 96}
		if spans := p.h.freeSpans(); len(spans) != 1 || spans[0] != want {
			t.Fatalf("bridged spans=%v, want %v", spans, want)
		}
	})

	t.Run("top reclamation", func(t *testing.T) {
		p := newThroughputDeferredModelPair(t)
		p.alloc(t, 32)
		top := p.alloc(t, 64)
		p.release(t, top, true)
		p.reconcileAll(t)
		if p.h.bump != top.off || p.model.bump != top.off || len(p.h.freeSpans()) != 0 {
			t.Fatalf("top reclamation bump/free=%d/%d %v", p.h.bump, p.model.bump, p.h.freeSpans())
		}
	})

	for _, descending := range []bool{false, true} {
		name := "ascending monotonic run"
		if descending {
			name = "descending monotonic run"
		}
		t.Run(name, func(t *testing.T) {
			p := newThroughputDeferredModelPair(t)
			entries := []handleEntry{p.alloc(t, 32), p.alloc(t, 32), p.alloc(t, 32)}
			p.alloc(t, 32)
			if descending {
				for i := len(entries) - 1; i >= 0; i-- {
					p.release(t, entries[i], true)
				}
			} else {
				for _, e := range entries {
					p.release(t, e, true)
				}
			}
			p.reconcileAll(t)
			want := throughputFreeSpan{off: 0, size: 96}
			if spans := p.h.freeSpans(); len(spans) != 1 || spans[0] != want {
				t.Fatalf("monotonic coalescing=%v, want %v", spans, want)
			}
		})
	}

	t.Run("handle reuse makes pending order non-monotonic", func(t *testing.T) {
		p := newThroughputDeferredModelPair(t)
		a, b, c := p.alloc(t, 32), p.alloc(t, 32), p.alloc(t, 32)
		p.alloc(t, 32)
		p.release(t, b, false)
		reused := p.alloc(t, 32)
		if reused.off != b.off {
			t.Fatalf("reused offset=%d, want %d", reused.off, b.off)
		}
		p.release(t, c, true)
		p.release(t, a, true)
		p.release(t, reused, true)
		p.reconcileAll(t)
		want := throughputFreeSpan{off: 0, size: 96}
		if spans := p.h.freeSpans(); len(spans) != 1 || spans[0] != want {
			t.Fatalf("non-monotonic reconciliation=%v, want %v", spans, want)
		}
	})

	t.Run("reconcile only until request fits", func(t *testing.T) {
		p := newThroughputDeferredModelPair(t)
		untouched := p.alloc(t, 32)
		p.alloc(t, 32)
		fit := p.alloc(t, 64)
		p.alloc(t, 32)
		small := p.alloc(t, 32)
		p.alloc(t, 32)
		p.release(t, untouched, true)
		p.release(t, fit, true)
		p.release(t, small, true)
		if got := p.alloc(t, 64); got.off != fit.off {
			t.Fatalf("partial reconciliation fit offset=%d, want %d", got.off, fit.off)
		}
		if len(p.h.pendingFree) != 1 || p.h.pendingFree[0].off != untouched.off {
			t.Fatalf("reconciled more debt than required: %v", p.h.pendingFree)
		}
	})
}

func TestThroughputModelMatchesRollbackAfterReconciliation(t *testing.T) {
	p := newThroughputDeferredModelPair(t)
	free := p.alloc(t, 32)
	p.alloc(t, 32)
	p.release(t, free, true)
	p.reconcileAll(t)

	tx := p.h.beginAllocTransaction()
	got, gotErr := p.h.alloc(32, spaceLarge)
	want, wantErr := p.model.alloc(32, spaceLarge)
	if gotErr != nil || wantErr != nil || got != want {
		t.Fatalf("transaction allocation=%+v/%v, want %+v/%v", got, gotErr, want, wantErr)
	}
	p.h.rollbackSuccessfulAlloc(got, tx.bump)
	p.h.restoreAllocTransaction(tx)
	if err := p.model.release(want); err != nil {
		t.Fatal(err)
	}
	p.assert(t)
}
