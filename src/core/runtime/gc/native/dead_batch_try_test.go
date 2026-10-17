package gc

import "testing"

func TestTryReserveDeadAllocationBatch(t *testing.T) {
	if !NativeDeadBatchSupported {
		t.Skip("injected allocation failures use ordinary path")
	}
	d, err := NewStructDesc(0, []StorageKind{StorageI32, StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	size, err := StructSize(d)
	if err != nil {
		t.Fatal(err)
	}
	if !c.PrepareNativeStructAllocation(size) {
		t.Fatal("reservation unavailable")
	}
	for _, args := range [][3]uint32{{0, size, 8}, {1, 0, 8}, {1, size, 0}, {1, size, 7}, {1, size, 24}, {1, size, 8192}, {1, 4096, 8}, {^uint32(0), size, 8}} {
		before, bump := c.Stats(), c.nurseryBump
		if c.TryReserveDeadAllocationBatch(args[0], args[1], args[2]) {
			t.Fatalf("accepted invalid or exhausting batch %v", args)
		}
		if c.Stats() != before || c.nurseryBump != bump {
			t.Fatalf("retry mutated collector for %v", args)
		}
	}
	for _, count := range []uint32{1, 7, 64} {
		before, bump := c.Stats(), c.nurseryBump
		if !c.TryReserveDeadAllocationBatch(count, size, max(d.Align, 8)) {
			t.Fatal("valid batch rejected")
		}
		if c.Stats().Allocations-before.Allocations != uint64(count) || c.Stats().LiveObjects-before.LiveObjects != count || c.nurseryBump-bump != count*size {
			t.Fatal("charge mismatch")
		}
	}
	c.CancelNativeAllocationBatch()
	before, bump := c.Stats(), c.nurseryBump
	if c.TryReserveDeadAllocationBatch(1, size, 8) || c.Stats() != before || c.nurseryBump != bump {
		t.Fatal("cancelled reservation admitted or mutated")
	}
}

func TestPreparedDeadAllocationBatchLiveState(t *testing.T) {
	if !NativeDeadBatchSupported {
		t.Skip("ordinary allocation profile")
	}
	d, err := NewStructDesc(0, []StorageKind{StorageI32, StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	size, err := StructSize(d)
	if err != nil {
		t.Fatal(err)
	}
	batch := c.PrepareDeadAllocationBatch(size, 8)
	if batch.collector != c {
		t.Fatal("policy rejected")
	}
	reject := func(n uint32) {
		t.Helper()
		before, bump := c.stats, c.nurseryBump
		if batch.TryReserve(n) || c.stats != before || c.nurseryBump != bump {
			t.Fatal("failed charge mutated collector")
		}
	}
	reject(1) // Preparation does not manufacture a native reservation.
	if !c.PrepareNativeStructAllocation(size) {
		t.Fatal("reservation unavailable")
	}
	reject(0)
	reject(^uint32(0))
	count := c.nativeStructAlloc.Count
	c.nativeStructAlloc.Count = NativeStructAllocHandleCapacity + 1
	reject(1)
	c.nativeStructAlloc.Count = count
	c.nativeStructAlloc.ChunkEnd = 16
	reject(1)
	c.nativeStructAlloc.ChunkEnd = 0
	originalEden := c.cfg.NurseryBytes
	c.cfg.NurseryBytes = c.nurseryBump + size
	reject(2)
	c.cfg.NurseryBytes = originalEden
	if !batch.TryReserve(2) {
		t.Fatal("did not observe restored live capacity")
	}
	c.CancelNativeAllocationBatch()
	reject(1)
	if !c.PrepareNativeStructAllocation(size) || !batch.TryReserve(1) {
		t.Fatal("did not observe renewed reservation")
	}
	c.Close()
	reject(1)
	var zero PreparedDeadAllocationBatch
	if zero.TryReserve(1) {
		t.Fatal("zero capability admitted")
	}
}

func TestPreparedDeadAllocationBatchPolicy(t *testing.T) {
	d, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []Config{{Profile: ProfileTiny}, {DisableCollection: true}, {CollectEveryAlloc: true}} {
		c, err := NewCollector(cfg, []TypeDesc{d})
		if err != nil {
			t.Fatal(err)
		}
		batch := c.PrepareDeadAllocationBatch(8, 8)
		if batch.collector != nil || batch.TryReserve(1) {
			t.Fatal("unsupported policy admitted")
		}
		c.Close()
	}
}

// Synthetic capacity values exercise the widened charge arithmetic without
// allocating a multi-gigabyte nursery. Dead charges do not dereference payloads.
func TestPreparedDeadBatchEndBounds(t *testing.T) {
	if !NativeDeadBatchSupported {
		t.Skip("injected failures use ordinary allocation")
	}
	d, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.PrepareNativeDeadStructAllocation(16) {
		t.Fatal("no reservation")
	}
	originalCfg, originalBump, originalStats := c.cfg, c.nurseryBump, c.stats
	defer func() { c.cfg, c.nurseryBump, c.stats = originalCfg, originalBump, originalStats }()
	for _, tc := range []struct {
		name                                string
		bump, limit, count, size, alignment uint32
		want                                bool
		end                                 uint32
	}{
		{"last-byte", 0xfffffff0, 0xffffffff, 1, 15, 16, true, 0xffffffff},
		{"end-crosses-u32", 0xfffffff0, 0xffffffff, 1, 16, 16, false, 0},
		{"alignment-crosses-u32", 0xfffffff1, 0xffffffff, 1, 1, 16, false, 0},
		{"wide-count-product", 0, 0xffffffff, 0xffffffff, 16, 16, false, 0},
		{"aligned-tail", 4095, 4097, 1, 1, 8, true, 4097},
		{"alignment-exhaustion", 4095, 4096, 1, 1, 8, false, 0},
		{"padded-stride", 0, 4097, 2, 1, 4096, true, 4097},
		{"padded-stride-exhaustion", 0, 4096, 2, 1, 4096, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.cfg.NurseryBytes, c.nurseryBump, c.stats = tc.limit, tc.bump, originalStats
			batch := c.PrepareDeadAllocationBatch(tc.size, tc.alignment)
			if got := batch.TryReserve(tc.count); got != tc.want {
				t.Fatalf("admission=%v want %v", got, tc.want)
			}
			if tc.want {
				if c.nurseryBump != tc.end || c.stats.Allocations-originalStats.Allocations != uint64(tc.count) {
					t.Fatalf("charge bump=%#x allocations=%d", c.nurseryBump, c.stats.Allocations-originalStats.Allocations)
				}
			} else if c.nurseryBump != tc.bump || c.stats != originalStats {
				t.Fatal("rejected charge changed state")
			}
		})
	}
}
