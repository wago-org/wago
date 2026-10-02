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
