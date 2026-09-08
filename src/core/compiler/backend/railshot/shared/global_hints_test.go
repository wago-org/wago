package shared

import "testing"

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
		if cap(serial) > max(8, 2*len(serial)) {
			t.Fatalf("excess capacity %d for %d records", cap(serial), len(serial))
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
