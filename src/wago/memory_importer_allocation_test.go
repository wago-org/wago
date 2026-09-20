package wago

import (
	"fmt"
	"sync"
	"testing"
)

func TestMemoryImporterUpdatesReuseStorage(t *testing.T) {
	for _, low := range []uint32{62, 63, 64, 254, 255, 256} {
		t.Run(fmt.Sprint(low), func(t *testing.T) {
			s := &memoryState{}
			defer s.setImporterCount(0)
			// Warm the overflow storage before measuring repeated changes.
			s.setImporterCount(low + 1)
			s.setImporterCount(low)
			allocs := testing.AllocsPerRun(1000, func() {
				s.mu.Lock()
				s.setImporterCount(s.importerCount() + 1)
				high := s.importerCount()
				s.setImporterCount(high - 1)
				got := s.importerCount()
				s.mu.Unlock()
				if high != low+1 || got != low {
					t.Fatalf("got %d -> %d; want %d -> %d", high, got, low+1, low)
				}
			})
			if allocs != 0 {
				t.Fatalf("allocations per round trip = %g, want 0", allocs)
			}
			s.setImporterCount(0)
			if _, retained := memoryImporterOverflow.Load(s); retained {
				t.Fatal("overflow table retained the released memory state")
			}
		})
	}
}

func TestMemoryImporterConcurrentStates(t *testing.T) {
	var wg sync.WaitGroup
	for worker := uint32(0); worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := &memoryState{}
			defer s.setImporterCount(0)
			for repeat := 0; repeat < 100; repeat++ {
				for _, count := range []uint32{0, 62, 63, 64 + worker, 254, 255, 256 + worker, ^uint32(0), 63, 62, 0} {
					s.mu.Lock()
					s.setImporterCount(count)
					got := s.importerCount()
					s.mu.Unlock()
					if got != count {
						t.Errorf("worker %d: got %d, want %d", worker, got, count)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}
