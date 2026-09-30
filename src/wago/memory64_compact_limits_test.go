package wago

import (
	"fmt"
	"testing"
)

func TestMemory64ImporterInlineCapacity(t *testing.T) {
	s := &memoryState{}
	s.set(memoryStateAddr64, true)
	defer s.setImporterCount(0)
	for _, count := range []uint32{0, 62, 63, 64, 126, 127, 128, 254, 63, 0} {
		s.setImporterCount(count)
		if got := s.importerCount(); got != count {
			t.Fatalf("count = %d, want %d", got, count)
		}
		if _, stored := memoryImporterOverflow.Load(s); stored {
			t.Fatalf("count %d used overflow storage below 255", count)
		}
	}
}

func TestMemory64PackedLimitBoundaries(t *testing.T) {
	for _, maximum := range []uint64{0, 1, (1 << 47) - 2, (1 << 47) - 1, 1 << 47, (1 << 47) + 1, 1 << 48} {
		for _, hasMax := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				if shared && !hasMax {
					continue
				}
				t.Run(fmt.Sprintf("max=%d/present=%t/shared=%t", maximum, hasMax, shared), func(t *testing.T) {
					m, err := NewMemory(0, 0)
					if err != nil {
						t.Fatal(err)
					}
					defer m.Close()
					s := &memoryState{}
					m.state.Store(s)
					defer s.setImporterCount(0)
					if err := m.share(nil, memoryDef{Max: maximum, HasMax: hasMax, Addr64: true, Shared: shared}); err != nil {
						t.Fatal(err)
					}
					if hasMax && s.declaredMaximum() != maximum {
						t.Fatalf("maximum = %d, want %d", s.declaredMaximum(), maximum)
					}
					for _, count := range []uint32{0, 62, 63, 64, 126, 127, 128, 254, 255, 256, 1024, 0} {
						s.setImporterCount(count)
						if s.importerCount() != count {
							t.Fatalf("count lost at %d", count)
						}
						if err := m.validateLimits(0, maximum, hasMax, true, shared); err != nil {
							t.Fatalf("matching import at count %d: %v", count, err)
						}
						if !hasMax {
							if err := m.validateLimits(0, 1<<48, true, true, shared); err == nil {
								t.Fatal("absent maximum satisfied a required maximum")
							}
						} else if maximum > 0 {
							if err := m.validateLimits(0, maximum-1, true, true, shared); err == nil {
								t.Fatal("narrow maximum accepted")
							}
						}
						before, owner := s.meta, s.owner
						if err := m.share(nil, memoryDef{HasMax: true, Shared: shared}); err == nil {
							t.Fatal("conflicting address form accepted")
						}
						if s.meta != before || s.owner != owner {
							t.Fatal("rejected address form changed state")
						}
						if err := m.share(nil, memoryDef{Max: maximum, HasMax: hasMax, Addr64: true, Shared: !shared}); err == nil {
							t.Fatal("conflicting sharing type accepted")
						}
						if s.meta != before || s.owner != owner {
							t.Fatal("rejected export changed state")
						}
						if err := m.share(nil, memoryDef{Max: maximum, HasMax: hasMax, Addr64: true, Shared: shared}); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}
