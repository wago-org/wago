package gc

import "testing"

func BenchmarkTinyAllocatorCommonSpanReuse(b *testing.B) {
	h := newTinyBenchmarkHeap(b, defaultTinyHeapBytes, defaultTinyBlockBytes)
	b.ReportAllocs()
	b.SetBytes(32)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		off, _, err := h.alloc(32)
		if err != nil {
			b.Fatal(err)
		}
		if err := h.free(off); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(h.metadataBytes()), "metadata-bytes")
}

func BenchmarkTinyAllocatorFragmentedFit(b *testing.B) {
	const heapBytes = 64 << 10
	h := newTinyBenchmarkHeap(b, heapBytes, defaultTinyBlockBytes)
	offsets := make([]uint32, 0, heapBytes/(2*defaultTinyBlockBytes))
	for {
		off, _, err := h.alloc(32)
		if err != nil {
			break
		}
		offsets = append(offsets, off)
	}
	for i := 0; i < len(offsets); i += 2 {
		if err := h.free(offsets[i]); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		off, _, err := h.alloc(32)
		if err != nil {
			b.Fatal(err)
		}
		if err := h.free(off); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTinyAllocatorFragmentedMiss(b *testing.B) {
	const heapBytes = 64 << 10
	h := newTinyBenchmarkHeap(b, heapBytes, defaultTinyBlockBytes)
	offsets := make([]uint32, 0, heapBytes/defaultTinyBlockBytes)
	for {
		off, _, err := h.alloc(defaultTinyBlockBytes)
		if err != nil {
			break
		}
		offsets = append(offsets, off)
	}
	for i := 0; i < len(offsets); i += 2 {
		if err := h.free(offsets[i]); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := h.alloc(2 * defaultTinyBlockBytes); err == nil {
			b.Fatal("fragmented heap unexpectedly satisfied two-block allocation")
		}
	}
}

func newTinyBenchmarkHeap(tb testing.TB, heapBytes, blockBytes uint32) *tinyHeap {
	tb.Helper()
	pf, err := NewStructDesc(0, []StorageKind{StorageI32, StorageI64})
	if err != nil {
		tb.Fatal(err)
	}
	c, err := NewCollector(Config{
		Profile:        ProfileTiny,
		TinyHeapBytes:  heapBytes,
		TinyBlockBytes: blockBytes,
	}, []TypeDesc{pf})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(c.Close)
	return &c.tiny
}
