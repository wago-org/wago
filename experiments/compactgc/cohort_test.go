//go:build wago_gc_compact_cohort

package compactgc

import "testing"

func TestCohortCommittedAndFailedCycles(t *testing.T) {
	c := heap(t, 64, 256, Descriptor{Kind: KindStruct, Align: 4, PayloadBytes: 4}, Descriptor{Kind: KindStruct, Align: 4, PayloadBytes: 80})
	live := obj(t, c, 0)
	_ = obj(t, c, 0)
	if err := c.Minor([]*Ref{&live}); err != nil {
		t.Fatal(err)
	}
	d := c.CohortDiagnostics()
	if d.MinorSuccesses != 1 || d.Last.YoungAllocatedBytes != 32 || d.Last.YoungAllocatedObjects != 2 || d.Last.YoungLiveBytes != 16 || d.Last.YoungLiveObjects != 1 {
		t.Fatalf("minor: %+v", d)
	}
	large := obj(t, c, 1)
	bad := Ref(48)
	if err := c.Full([]*Ref{&bad}); err == nil {
		t.Fatal("expected bad root")
	}
	failed := c.CohortDiagnostics()
	if failed.FullFailures != 1 || failed.Last != d.Last || failed.FullSuccesses != 0 {
		t.Fatalf("failed: %+v", failed)
	}
	if err := c.Full([]*Ref{&live, &large}); err != nil {
		t.Fatal(err)
	}
	d = c.CohortDiagnostics()
	if d.FullSuccesses != 1 || d.Last.OldOccupiedBytes != 112 || d.Last.OldLiveBytes != 112 || d.Last.OversizedOldLiveBytes != 96 || d.OversizedAllocatedBytes != 96 || d.OversizedAllocatedObjects != 1 {
		t.Fatalf("full: %+v", d)
	}
	if d != c.CohortDiagnostics() {
		t.Fatal("snapshot mutated diagnostic")
	}
	if err := c.Full(nil); err != nil {
		t.Fatal(err)
	}
	if d = c.CohortDiagnostics(); d.Last.OldLiveBytes != 0 || d.Last.OldOccupiedBytes != 112 {
		t.Fatalf("dead: %+v", d)
	}
}
