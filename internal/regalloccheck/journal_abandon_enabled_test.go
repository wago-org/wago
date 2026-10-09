//go:build wago_regalloccheck

package regalloccheck

import "testing"

func TestJournalAbandonOwnedActiveAndExhausted(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		j := NewEmissionJournal(JournalLimits{})
		outer := j.Checkpoint(0)
		inner := j.Checkpoint(0)
		j.BeginEmission(0)
		j.ObserveGPWrites(1)
		if exhausted {
			j.work = j.limits.Work
			j.ObserveGPWrites(2)
		}
		if !j.Abandon(inner) || len(j.transactions) != 1 || j.transactions[0].mark != outer || j.active {
			t.Fatal("abandon did not clear owned active scope")
		}
		want := InvalidGraph
		if exhausted {
			want = ResourceLimit
		}
		if j.Result().Reason != want || j.Finalize(0, 0, nil).State == JournalReady || !j.Abandon(outer) {
			t.Fatal("abandon revived journal or lost enclosing mark", j.Result())
		}
		j.Close()
		if j.Abandon(outer) {
			t.Fatal("closed mark accepted")
		}
	}
}
func TestJournalAbandonForeignAndNonLIFO(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		j := NewEmissionJournal(JournalLimits{})
		other := NewEmissionJournal(JournalLimits{})
		outer := j.Checkpoint(0)
		inner := j.Checkpoint(0)
		bad := outer
		if foreign {
			bad = other.Checkpoint(0)
		}
		if j.Abandon(bad) || len(j.transactions) != 2 || j.Result().Reason != InvalidGraph {
			t.Fatal("foreign/nonLIFO cleanup changed owned stack")
		}
		if !j.Abandon(inner) || !j.Abandon(outer) {
			t.Fatal("sticky failure prevented owner cleanup")
		}
		j.Close()
		other.Close()
	}
}
