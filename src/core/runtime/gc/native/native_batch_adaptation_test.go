package gc

import "testing"

// Model a completed native constructor, including the header and cursor commit.
func publishBatchTestStructs(c *Collector, n uint32) {
	s := &c.nativeStructAlloc
	for range n {
		h := s.Handles[s.Cursor]
		c.handles[h] = handleEntry{off: c.nurseryBump, size: 16, space: spaceNursery}
		c.writeHeader(makeObjRef(h), ObjHeader{TypeID: 0, Size: 16, Flags: FlagPointerFree})
		c.nurseryBump += 16
		s.Cursor++
	}
}

func batchTestCollector(t testing.TB) *Collector {
	t.Helper()
	desc, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestNativeStructBatchAdaptsAndRecovers(t *testing.T) {
	c := batchTestCollector(t)
	if !c.PrepareNativeStructHandles() || c.nativeStructAlloc.Count != uint32(nativeStructHandleBatchSize) {
		t.Fatalf("initial batch count = %d, want %d", c.nativeStructAlloc.Count, nativeStructHandleBatchSize)
	}
	publishBatchTestStructs(c, 1)
	c.CancelNativeAllocationBatch()
	if !c.PrepareNativeStructHandles() || c.nativeStructAlloc.Count != 8 {
		t.Fatalf("mixed allocation batch = %d, want 8", c.nativeStructAlloc.Count)
	}
	for want := uint32(8); want < uint32(nativeStructHandleBatchSize); {
		for refill := 0; refill < 64 && c.nativeStructAlloc.Count == want; refill++ {
			publishBatchTestStructs(c, want)
			if !c.PrepareNativeStructHandles() {
				t.Fatal("could not refill the full batch")
			}
			if refill < 8 && c.nativeStructAlloc.Count != want {
				t.Fatalf("short struct burst regrew batch: count=%d, want %d", c.nativeStructAlloc.Count, want)
			}
		}
		want = min(want*2, uint32(nativeStructHandleBatchSize))
		if c.nativeStructAlloc.Count != want {
			t.Fatalf("sustained struct allocation did not recover: count=%d, want %d", c.nativeStructAlloc.Count, want)
		}
	}
	c.CancelNativeAllocationBatch()
	if err := c.CollectMinor(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if got := c.Stats().LiveObjects; got != 0 {
		t.Fatalf("collection retained %d unreachable objects", got)
	}
}

func TestNativeStructBatchFeedbackIsLocalAndIgnoresArrayChunks(t *testing.T) {
	c := batchTestCollector(t)
	other := batchTestCollector(t)
	if !c.PrepareNativeStructHandles() {
		t.Fatal("could not reserve struct handles")
	}
	publishBatchTestStructs(c, 1)
	c.CancelNativeAllocationBatch()
	if !c.PrepareNativeAllocation(16) || c.nativeStructAlloc.Count != nativeArrayHandleBatch {
		t.Fatal("could not reserve the independent array batch")
	}
	c.CancelNativeAllocationBatch()
	if !c.PrepareNativeDeadStructAllocation(16) || c.nativeStructAlloc.Count != 1 {
		t.Fatal("could not reserve a dead-struct proof")
	}
	c.CancelNativeAllocationBatch()
	if !c.PrepareNativeStructHandles() || c.nativeStructAlloc.Count != 8 {
		t.Fatalf("array/dead reservation changed struct feedback: count=%d, want 8", c.nativeStructAlloc.Count)
	}
	if !other.PrepareNativeStructHandles() || other.nativeStructAlloc.Count != uint32(nativeStructHandleBatchSize) {
		t.Fatalf("another collector inherited feedback: count=%d", other.nativeStructAlloc.Count)
	}
}

func TestNativeStructBatchShortBurstsStaySmall(t *testing.T) {
	c := batchTestCollector(t)
	if !c.PrepareNativeStructHandles() {
		t.Fatal("could not reserve struct handles")
	}
	publishBatchTestStructs(c, 1)
	c.CancelNativeAllocationBatch()
	for phase := 0; phase < 16; phase++ {
		if !c.PrepareNativeStructHandles() || c.nativeStructAlloc.Count != 8 {
			t.Fatalf("mixed phase %d batch count=%d, want 8", phase, c.nativeStructAlloc.Count)
		}
		for range 4 {
			publishBatchTestStructs(c, 8)
			if !c.PrepareNativeStructHandles() || c.nativeStructAlloc.Count != 8 {
				t.Fatalf("short burst regrew batch in phase %d: count=%d", phase, c.nativeStructAlloc.Count)
			}
		}
		publishBatchTestStructs(c, 1)
		c.CancelNativeAllocationBatch()
	}
	if err := c.CollectMinor(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkNativeStructBatchCancellation(b *testing.B) {
	for _, tc := range []struct {
		name string
		used uint32
	}{{"one-live", 1}, {"eight-live", 8}} {
		b.Run(tc.name, func(b *testing.B) {
			c := batchTestCollector(b)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for range tc.used {
					if !c.PrepareNativeStructHandles() {
						b.Fatal("could not reserve struct handles")
					}
					publishBatchTestStructs(c, 1)
				}
				c.CancelNativeAllocationBatch()
				if i%1024 == 1023 {
					if err := c.CollectMinor(EmptyRoots{}); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
