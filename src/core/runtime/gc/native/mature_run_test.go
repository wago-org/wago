package gc

import "testing"

func newMatureRunHeap(t *testing.T) throughputHeap {
	t.Helper()
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 16 << 10, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096, NurseryRegionShift: 6}); err != nil {
		t.Fatal(err)
	}
	h.mem = makeAlignedBytes(16<<10, 16)
	return h
}

func TestMatureAllocRunMatchesIndividualAddresses(t *testing.T) {
	batched, individual := newMatureRunHeap(t), newMatureRunHeap(t)
	batched.bump, individual.bump = 4000, 4000
	batched.beginMatureAdmission(true)
	individual.beginMatureAdmission(true)
	run, ok, err := batched.tryAllocRun(24, 160, spaceOld)
	if err != nil || !ok {
		t.Fatalf("batched run ok=%t err=%v", ok, err)
	}
	for i := range 160 {
		want, err := individual.alloc(24, spaceOld)
		if err != nil {
			t.Fatal(err)
		}
		got := handleEntry{off: run.off + uint32(i)*run.allocSize, size: 24, class: run.class, space: spaceOld}
		if i == 159 && run.lastAllocSize != run.allocSize {
			got.class |= handleAbsorbedTailBit
		}
		if got != want {
			t.Fatalf("entry %d = %+v, want %+v", i, got, want)
		}
	}
	if batched.bump != individual.bump {
		t.Fatalf("batch bump %d, individual %d", batched.bump, individual.bump)
	}
}
