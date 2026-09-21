package profile

import (
	"sync"
	"testing"
)

func TestRailshotCountersConcurrentSnapshotAndReset(t *testing.T) {
	counters, err := NewRailshotCounters([32]byte{1}, 3)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	const iterations = 10_000
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for range iterations {
				counters.RecordFunction(1)
			}
		}()
	}
	group.Wait()

	first, err := counters.Snapshot(PhaseStartup, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation != 1 || first.Source != SourceRailshot || first.FunctionCounts[1] != workers*iterations {
		t.Fatalf("first snapshot = %#v", first)
	}
	drained, err := counters.Snapshot(PhaseSteady, true)
	if err != nil {
		t.Fatal(err)
	}
	if drained.Generation != 2 || drained.FunctionCounts[1] != workers*iterations {
		t.Fatalf("drained snapshot = %#v", drained)
	}
	empty, err := counters.Snapshot(PhaseRare, false)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Generation != 3 || empty.FunctionCounts[1] != 0 {
		t.Fatalf("post-reset snapshot = %#v", empty)
	}
}

func TestRailshotCountersBoundsAndSaturation(t *testing.T) {
	if _, err := NewRailshotCounters([32]byte{}, MaxRailshotFunctionCounters+1); err == nil {
		t.Fatal("oversized Railshot counter slab accepted")
	}
	counters, err := NewRailshotCounters([32]byte{2}, 1)
	if err != nil {
		t.Fatal(err)
	}
	counters.RecordFunctionN(0, ^uint64(0)-1)
	counters.RecordFunctionN(0, 10)
	counters.RecordFunction(1)
	profile, err := counters.Snapshot(PhaseSteady, false)
	if err != nil {
		t.Fatal(err)
	}
	if profile.FunctionCounts[0] != ^uint64(0) {
		t.Fatalf("saturated count = %d", profile.FunctionCounts[0])
	}
}
