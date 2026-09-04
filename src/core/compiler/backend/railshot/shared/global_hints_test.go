package shared

import "testing"

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
