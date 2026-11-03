package gc

import (
	"slices"
	"testing"
)

func newMatureRunHeap(t *testing.T) throughputHeap {
	t.Helper()
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 16 << 10, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096, NurseryRegionShift: 6}); err != nil {
		t.Fatal(err)
	}
	h.mem = makeAlignedBytes(16<<10, 16)
	return h
}

func TestMatureAllocRunMatchesIndividualOccupancyAndAddresses(t *testing.T) {
	batched := newMatureRunHeap(t)
	individual := newMatureRunHeap(t)
	batched.bump, individual.bump = 4000, 4000
	batched.beginMatureAdmission(true)
	individual.beginMatureAdmission(true)
	run, ok, err := batched.tryAllocRun(24, 160, spaceOld)
	if err != nil || !ok {
		t.Fatalf("batched run ok=%t err=%v", ok, err)
	}
	want := make([]handleEntry, 160)
	for i := range want {
		want[i], err = individual.alloc(24, spaceOld)
		if err != nil {
			t.Fatal(err)
		}
		got := handleEntry{off: run.off + uint32(i)*run.allocSize, size: 24, class: run.class, space: spaceOld}
		if i == len(want)-1 && run.lastAllocSize != run.allocSize {
			got.class |= handleAbsorbedTailBit
		}
		if got != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, got, want[i])
		}
	}
	if batched.bump != individual.bump || !slices.Equal(batched.mature.touched, individual.mature.touched) || !slices.Equal(batched.mature.regions, individual.mature.regions) {
		t.Fatalf("batch metadata differs: bump %d/%d touched %v/%v regions %#v/%#v", batched.bump, individual.bump, batched.mature.touched, individual.mature.touched, batched.mature.regions, individual.mature.regions)
	}
	batched.commitMatureAdmission()
	individual.commitMatureAdmission()
	if !slices.Equal(batched.mature.regions, individual.mature.regions) {
		t.Fatal("committed batch occupancy differs from individual allocation")
	}
}

func TestMatureAllocRunWarmedDescriptorBoundary(t *testing.T) {
	for _, tc := range []struct {
		name        string
		bump, count uint32
		wantRegions int
	}{
		{name: "exact page", bump: 256, count: 120, wantRegions: 1},
		{name: "next page", bump: 256, count: 121, wantRegions: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batched := newMatureRunHeap(t)
			individual := newMatureRunHeap(t)
			batched.bump, individual.bump = tc.bump, tc.bump
			// Spare backing capacity is not initialized descriptor coverage.
			batched.mature.regions = make([]matureRegion, 1, 2)
			individual.mature.regions = make([]matureRegion, 1, 2)
			batched.mature.touched = make([]uint32, 0, tc.wantRegions)
			individual.mature.touched = make([]uint32, 0, tc.wantRegions)
			batched.beginMatureAdmission(true)
			individual.beginMatureAdmission(true)

			run, ok, err := batched.tryAllocRun(24, int(tc.count), spaceOld)
			if err != nil || !ok {
				t.Fatalf("batched run ok=%t err=%v", ok, err)
			}
			for range tc.count {
				if _, err = individual.alloc(24, spaceOld); err != nil {
					t.Fatal(err)
				}
			}
			if got := len(batched.mature.regions); got != tc.wantRegions {
				t.Fatalf("descriptor length = %d, want %d", got, tc.wantRegions)
			}
			if batched.bump != individual.bump || !slices.Equal(batched.mature.touched, individual.mature.touched) || !slices.Equal(batched.mature.regions, individual.mature.regions) {
				t.Fatalf("batch metadata differs: run=%+v bump %d/%d touched %v/%v regions %#v/%#v", run, batched.bump, individual.bump, batched.mature.touched, individual.mature.touched, batched.mature.regions, individual.mature.regions)
			}
		})
	}
}

func TestMatureAllocRunPreservesAbsorbedTailAndFallback(t *testing.T) {
	h := newMatureRunHeap(t)
	h.bump = 256
	h.mature.regions = make([]matureRegion, 1)
	h.mature.touched = make([]uint32, 0, 1)
	if err := h.insertFreeSpan(throughputFreeSpan{off: 0, size: 112}); err != nil {
		t.Fatal(err)
	}
	h.beginMatureAdmission(true)
	run, ok, err := h.tryAllocRun(24, 3, spaceOld)
	if err != nil || !ok || run.lastAllocSize != 48 {
		t.Fatalf("tail run=%+v ok=%t err=%v", run, ok, err)
	}
	if h.mature.regions[0].reserved != 112 {
		t.Fatalf("reserved tail bytes=%d, want 112", h.mature.regions[0].reserved)
	}
	h.releaseMatureRun(run, 24, 3, spaceOld)
	h.restoreAllocTransaction(throughputAllocTransaction{mem: h.mem, bump: 256})
	assertMatureAdmissionClean(t, &h)

	h = newMatureRunHeap(t)
	h.bump = 256
	if err := h.insertFreeSpan(throughputFreeSpan{off: 0, size: 32}); err != nil {
		t.Fatal(err)
	}
	if err := h.insertFreeSpan(throughputFreeSpan{off: 64, size: 128}); err != nil {
		t.Fatal(err)
	}
	h.beginMatureAdmission(true)
	if _, ok, err := h.tryAllocRun(24, 3, spaceOld); err != nil || ok {
		t.Fatalf("ineligible first-fit run ok=%t err=%v", ok, err)
	}
	if len(h.mature.touched) != 0 {
		t.Fatal("fallback path prepared mature occupancy")
	}
	h.cancelMatureAdmission()
}
