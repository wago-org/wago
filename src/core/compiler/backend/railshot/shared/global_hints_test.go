package shared

import "testing"

func TestGlobalHintAccumulatorExclusiveScratchAndFallback(t *testing.T) {
	var words [32]uint32
	for i := range words {
		words[i] = ^uint32(0)
	}
	var a, b GlobalHintAccumulator
	a.ResetWithScratch(8, words[:16:16])
	b.ResetWithScratch(8, words[16:32:32])
	a.Add(1, 9)
	b.Add(1, 3)
	a.MarkEligible(1)
	if got := a.AppendTo(nil); len(got) != 1 || got[0] != (GlobalHint{Index: 1, Score: 9, Eligible: true}) {
		t.Fatalf("first worker: %+v", got)
	}
	if got := b.AppendTo(nil); len(got) != 1 || got[0] != (GlobalHint{Index: 1, Score: 3}) {
		t.Fatalf("second worker: %+v", got)
	}
	if cap(a.scores) != 8 || cap(a.marks) != 8 || cap(b.scores) != 8 || cap(b.marks) != 8 {
		t.Fatal("scratch range is not capacity-bounded")
	}
	if allocs := testing.AllocsPerRun(100, func() { a.ResetWithScratch(8, words[:16:16]); a.Add(2, 1) }); allocs != 0 {
		t.Fatalf("scratch reuse allocations: %g", allocs)
	}
	a.ResetWithScratch(17, words[:16:16])
	a.Add(16, 7)
	if got := a.AppendTo(nil); len(got) != 1 || got[0].Index != 16 || got[0].Score != 7 {
		t.Fatalf("growth fallback: %+v", got)
	}
	if got := b.AppendTo(nil); got[0].Score != 3 {
		t.Fatalf("fallback changed other worker: %+v", got)
	}
}

func TestGlobalHintCapacityIndependentOfBatching(t *testing.T) {
	var a GlobalHintAccumulator
	var serial []GlobalHint
	for _, count := range []int{1, 7, 3, 19, 40, 80} {
		a.Reset(count)
		for i := 0; i < count; i++ {
			a.Add(uint32(i), int64(i+1))
		}
		serial = a.AppendTo(serial)
		parallel := make([]GlobalHint, len(serial), GlobalHintCapacity(len(serial)))
		if cap(serial) != cap(parallel) {
			t.Fatalf("count %d: serial cap %d, parallel cap %d", len(serial), cap(serial), cap(parallel))
		}
		if cap(serial) > 2*len(serial) {
			t.Fatalf("excess capacity %d for %d records", cap(serial), len(serial))
		}
	}
}

func TestGlobalHintCapacitySmallAndOverflow(t *testing.T) {
	for _, pair := range [][2]int{{0, 0}, {1, 1}, {2, 2}, {3, 4}, {5, 8}, {9, 16}, {int(^uint(0) >> 1), int(^uint(0) >> 1)}} {
		if got := GlobalHintCapacity(pair[0]); got != pair[1] {
			t.Fatalf("count %d: capacity %d, want %d", pair[0], got, pair[1])
		}
	}
}

func TestGlobalHintAccumulatorReusesDenseScratchAndSortsSparseRecords(t *testing.T) {
	var a GlobalHintAccumulator
	a.Reset(1024)
	a.Add(900, 10)
	a.Add(3, 4)
	a.Add(900, 5)
	a.MarkEligible(900)
	got := a.AppendTo(nil)
	if len(got) != 2 || got[0] != (GlobalHint{Index: 3, Score: 4}) || got[1] != (GlobalHint{Index: 900, Score: 15, Eligible: true}) {
		t.Fatalf("hints = %+v", got)
	}

	a.Reset(1024)
	a.Add(3, 1)
	got = a.AppendTo(got[:0])
	if len(got) != 1 || got[0] != (GlobalHint{Index: 3, Score: 1}) {
		t.Fatalf("reset hints = %+v", got)
	}
}

func TestGlobalHintAccumulatorEligibilityDoesNotLeakAcrossEpochWrap(t *testing.T) {
	var a GlobalHintAccumulator
	a.Reset(4)
	a.MarkEligible(2)
	if got := a.AppendTo(nil); len(got) != 1 || got[0] != (GlobalHint{Index: 2, Eligible: true}) {
		t.Fatalf("eligible hints = %+v", got)
	}

	a.epoch = globalHintEpochMask
	a.Reset(4)
	a.Add(2, 7)
	if got := a.AppendTo(nil); len(got) != 1 || got[0] != (GlobalHint{Index: 2, Score: 7}) {
		t.Fatalf("wrapped hints = %+v", got)
	}
}

func TestGlobalHintAccumulatorSortsAcrossInlineOverflow(t *testing.T) {
	var a GlobalHintAccumulator
	a.Reset(64)
	for i := uint32(0); i < 40; i++ {
		a.Add(63-i, int64(i+1))
	}
	got := a.AppendTo(nil)
	if len(got) != 40 {
		t.Fatalf("hint count = %d, want 40", len(got))
	}
	for i := range got {
		if i > 0 && got[i-1].Index >= got[i].Index {
			t.Fatalf("hints are not index sorted at %d: %+v", i, got)
		}
	}
}
