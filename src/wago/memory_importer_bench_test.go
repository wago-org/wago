package wago

import (
	"fmt"
	"testing"
)

// A transition measures one increment/decrement pair, including count reads.
// Setup and cleanup are excluded; the overflow table remains the production map.
func BenchmarkMemoryImporterCount(b *testing.B) {
	for _, count := range []uint32{62, 63, 64, 254, 255, 256} {
		b.Run(fmt.Sprintf("steady/%d", count), func(b *testing.B) {
			s := &memoryState{}
			s.setImporterCount(count)
			b.Cleanup(func() { memoryImporterOverflow.Delete(s) })
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.mu.Lock()
				s.setImporterCount(count)
				got := s.importerCount()
				s.mu.Unlock()
				if got != count {
					b.Fatalf("got %d, want %d", got, count)
				}
			}
		})
	}
	for _, low := range []uint32{62, 63, 254, 255} {
		b.Run(fmt.Sprintf("transition/%d-%d-%d", low, low+1, low), func(b *testing.B) {
			s := &memoryState{}
			s.setImporterCount(low)
			b.Cleanup(func() { memoryImporterOverflow.Delete(s) })
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.mu.Lock()
				s.setImporterCount(s.importerCount() + 1)
				high := s.importerCount()
				s.setImporterCount(high - 1)
				got := s.importerCount()
				s.mu.Unlock()
				if high != low+1 || got != low {
					b.Fatalf("counts = %d, %d; want %d, %d", high, got, low+1, low)
				}
			}
		})
	}
}

func BenchmarkMemoryImporterParallel(b *testing.B) {
	for _, low := range []uint32{62, 63, 255} {
		b.Run(fmt.Sprint(low), func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				s := &memoryState{}
				s.setImporterCount(low)
				defer s.setImporterCount(0)
				for pb.Next() {
					s.mu.Lock()
					s.setImporterCount(s.importerCount() + 1)
					s.setImporterCount(s.importerCount() - 1)
					s.mu.Unlock()
				}
			})
		})
	}
}
