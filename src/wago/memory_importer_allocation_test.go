package wago

import (
	"fmt"
	"sync"
	"testing"
)

func TestMemoryImporterUpdatesReuseStorage(t *testing.T) {
	for _, addr64 := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory64=%t", addr64), func(t *testing.T) {
			testMemoryImporterUpdatesReuseStorage(t, addr64)
		})
	}
}

func testMemoryImporterUpdatesReuseStorage(t *testing.T, addr64 bool) {
	for _, low := range []uint32{62, 63, 64, 254, 255, 256} {
		t.Run(fmt.Sprint(low), func(t *testing.T) {
			s := &memoryState{}
			s.set(memoryStateAddr64, addr64)
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
			s.set(memoryStateAddr64, worker%2 != 0)
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

func TestMemory32ImporterCountNeedsNoOverflowStorage(t *testing.T) {
	flags := memoryStateShared | memoryStateWasmShared | memoryStateLimitsKnown | memoryStateClosed | memoryStateWasmTypeKnown | memoryStateDeclaredShared
	for _, maximum := range []uint64{0, 1, 65535, 65536} {
		s := &memoryState{}
		s.set(flags, true)
		s.setDeclaredLimits(maximum, true)
		for _, count := range []uint32{0, 62, 63, 64, 254, 255, 256, 1 << 31, ^uint32(0), 63, 0} {
			s.setImporterCount(count)
			if got := s.importerCount(); got != count {
				t.Errorf("maximum %d: count = %d, want %d", maximum, got, count)
			}
			if _, stored := memoryImporterOverflow.Load(s); stored {
				t.Errorf("maximum %d: count %d used overflow storage", maximum, count)
			}
			if got := s.declaredMaximum(); got != maximum || uint16(s.meta>>memoryStateFlagsShift) != flags {
				t.Errorf("count %d changed maximum or flags: maximum = %d, meta = %#x", count, got, s.meta)
			}
			// A limits update must preserve every bit of the importer count.
			s.setDeclaredLimits(maximum, true)
			if got := s.importerCount(); got != count {
				t.Errorf("limits update changed count: got %d, want %d", got, count)
			}
		}
		s.setImporterCount(0)
	}
}

func TestMemoryFirstExportPreservesImporterCount(t *testing.T) {
	for _, addr64 := range []bool{false, true} {
		for _, count := range []uint32{0, 62, 63, 256, ^uint32(0)} {
			t.Run(fmt.Sprintf("memory64=%t/count=%d", addr64, count), func(t *testing.T) {
				m, err := NewMemory(1, 1)
				if err != nil {
					t.Fatal(err)
				}
				defer m.Close()
				s := &memoryState{}
				m.state.Store(s)
				defer s.setImporterCount(0)
				s.setImporterCount(count)
				maximum := uint64(65536)
				if addr64 {
					maximum = 1 << 48
				}
				if err := m.share(nil, memoryDef{Max: maximum, HasMax: true, Addr64: addr64}); err != nil {
					t.Fatal(err)
				}
				if got := s.importerCount(); got != count || s.declaredMaximum() != maximum {
					t.Fatalf("first export: count = %d, maximum = %d; want %d, %d", got, s.declaredMaximum(), count, maximum)
				}
			})
		}
	}
}
