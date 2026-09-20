package wago

import (
	"fmt"
	"testing"
)

// A transition measures one increment/decrement pair, including count reads.
// Setup and cleanup are excluded; memory64 uses the production overflow table.
func BenchmarkMemoryImporterCount(b *testing.B) {
	benchmarkMemoryImporterCount(b, false)
}

func BenchmarkMemory64ImporterCount(b *testing.B) {
	benchmarkMemoryImporterCount(b, true)
}

func benchmarkMemoryImporterCount(b *testing.B, addr64 bool) {
	for _, count := range []uint32{62, 63, 64, 126, 127, 128, 254, 255, 256, 1024} {
		b.Run(fmt.Sprintf("steady/%d", count), func(b *testing.B) {
			s := &memoryState{}
			s.set(memoryStateAddr64, addr64)
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
	for _, low := range []uint32{62, 63, 126, 127, 254, 255, 1023} {
		b.Run(fmt.Sprintf("transition/%d-%d-%d", low, low+1, low), func(b *testing.B) {
			s := &memoryState{}
			s.set(memoryStateAddr64, addr64)
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
	benchmarkMemoryImporterParallel(b, false)
}

func BenchmarkMemory64ImporterParallel(b *testing.B) {
	benchmarkMemoryImporterParallel(b, true)
}

func benchmarkMemoryImporterParallel(b *testing.B, addr64 bool) {
	for _, low := range []uint32{62, 63, 64, 126, 127, 128, 254, 255, 256, 1024} {
		b.Run(fmt.Sprint(low), func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				s := &memoryState{}
				s.set(memoryStateAddr64, addr64)
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
