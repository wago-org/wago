package gc

import (
	"fmt"
	"slices"
	"testing"
	"unsafe"
)

func TestNativeLargeArrayCancellationPreservesLedger(t *testing.T) {
	for _, checked := range []bool{false, true} {
		for _, bytes := range []uint32{512, 40 << 10} {
			for _, later := range []string{"none", "go-allocation", "struct-reservation"} {
				t.Run(fmt.Sprintf("checked=%t/bytes=%d/later=%s", checked, bytes, later), func(t *testing.T) {
					desc, err := NewStructDesc(0, nil)
					if err != nil {
						t.Fatal(err)
					}
					c, err := NewCollector(Config{}, []TypeDesc{desc})
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					if checked {
						if err := c.EnableCheckedHandles(); err != nil {
							t.Fatal(err)
						}
					}
					for range 8 {
						if _, err := c.NewStructDefault(0); err != nil {
							t.Fatal(err)
						}
					}
					bump, heapBump := c.nurseryBump, c.throughput.bump
					if !c.PrepareNativeArrayAllocationImmediate(bytes) {
						t.Fatal("large reservation unavailable")
					}
					reserved := c.nativeLargeArrayAlloc
					var generation uint64
					if checked {
						generation = (*c.checkedHandles)[reserved.Handle]
					}
					switch later {
					case "go-allocation":
						if _, err := c.NewStructDefault(0); err != nil {
							t.Fatal(err)
						}
						bump = c.nurseryBump
					case "struct-reservation":
						if !c.PrepareNativeStructAllocation(16) {
							t.Fatal("struct reservation unavailable")
						}
					}
					want := make([]uint32, 0, len(c.nurseryHandles)-1)
					for _, h := range c.nurseryHandles {
						if h != reserved.Handle {
							want = append(want, h)
						}
					}
					free := len(c.freeHandles)
					epoch := c.nativeLargeArrayEpoch
					c.discardNativeLargeArrayAllocation()
					if !slices.Equal(c.nurseryHandles, want) {
						t.Fatalf("nursery ledger = %v, want %v", c.nurseryHandles, want)
					}
					if len(c.freeHandles) != free+1 || c.freeHandles[free] != reserved.Handle {
						t.Fatalf("reservation was not recycled once: %v", c.freeHandles)
					}
					if c.nurseryBump != bump || c.throughput.bump != heapBump {
						t.Fatalf("backing after cancel: nursery %d/%d throughput %d/%d", c.nurseryBump, bump, c.throughput.bump, heapBump)
					}
					if c.nativeLargeArrayEpoch == epoch || c.nativeLargeArrayAlloc != (nativeLargeArrayAllocState{Epoch: c.nativeLargeArrayEpoch}) {
						t.Fatalf("reservation was not invalidated: %+v epoch %d/%d", c.nativeLargeArrayAlloc, c.nativeLargeArrayEpoch, epoch)
					}
					if !c.PrepareNativeArrayAllocationImmediate(bytes) {
						t.Fatal("replacement reservation unavailable")
					}
					if c.nativeLargeArrayAlloc.Handle != reserved.Handle {
						t.Fatalf("replacement identity = %d, want %d", c.nativeLargeArrayAlloc.Handle, reserved.Handle)
					}
					if checked && (*c.checkedHandles)[reserved.Handle] != generation+1 {
						t.Fatal("replacement reservation did not advance checked generation")
					}
				})
			}
		}
	}
}

func TestNativeLargeArrayCancellationKeepsConsumedObject(t *testing.T) {
	for _, bytes := range []uint32{512, 40 << 10} {
		t.Run(fmt.Sprint(bytes), func(t *testing.T) {
			desc, err := NewArrayDesc(0, StorageI32)
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewCollector(Config{}, []TypeDesc{desc})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if !c.PrepareNativeArrayAllocationImmediate(bytes) {
				t.Fatal("large reservation unavailable")
			}
			s := &c.nativeLargeArrayAlloc
			c.nativeLargeArrayEpoch = ^uint32(0)
			s.Epoch = c.nativeLargeArrayEpoch
			h := s.Handle
			space := spaceKind((s.ChunkBump >> 16) & 0xff)
			c.handles[h] = handleEntry{off: s.ChunkStart, size: bytes, class: uint16(s.ChunkBump), space: space}
			flags := uint32(FlagPointerFree)
			if space == spaceLarge {
				flags |= FlagLarge
			}
			ref := makeObjRef(h)
			c.writeHeader(ref, ObjHeader{Size: bytes, Aux: (bytes - HeaderSize) / 4, Flags: flags})
			s.Cursor, s.ChunkCursor = 1, s.ChunkEnd
			c.stats.Allocations++
			entry, bump, heapBump := c.handles[h], c.nurseryBump, c.throughput.bump
			c.discardNativeLargeArrayAllocation()
			if c.nativeLargeArrayEpoch != 1 || *s != (nativeLargeArrayAllocState{Epoch: 1}) {
				t.Fatalf("epoch wrap did not invalidate consumed reservation: %+v epoch %d", *s, c.nativeLargeArrayEpoch)
			}
			if !slices.Equal(c.nurseryHandles, []uint32{h}) || len(c.freeHandles) != 0 || c.handles[h] != entry || c.nurseryBump != bump || c.throughput.bump != heapBump {
				t.Fatal("cancel reclaimed a consumed object")
			}
			root := Root(ref)
			if err := c.Verify(Slots{&root}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeLargeArrayCancellationRemovesOneDuplicate(t *testing.T) {
	c, err := NewCollector(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.PrepareNativeArrayAllocationImmediate(512) {
		t.Fatal("large reservation unavailable")
	}
	h := c.nativeLargeArrayAlloc.Handle
	// A duplicate ledger entry must not cause the reservation to be freed twice.
	c.nurseryHandles = append(c.nurseryHandles, h)
	c.discardNativeLargeArrayAllocation()
	if !slices.Equal(c.nurseryHandles, []uint32{h}) || !slices.Equal(c.freeHandles, []uint32{h}) {
		t.Fatalf("duplicate removal changed multiplicity: nursery %v free %v", c.nurseryHandles, c.freeHandles)
	}
	// Remove the deliberately retained free entry before collector teardown.
	c.nurseryHandles = c.nurseryHandles[:0]
}

func TestNativeLargeArrayCancellationLedgerPositions(t *testing.T) {
	const live = 2048
	for _, after := range []int{0, 1, 15, 16, 17, 512, 1024, live} {
		t.Run(fmt.Sprintf("later-handles=%d", after), func(t *testing.T) {
			desc, err := NewStructDesc(0, nil)
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewCollector(Config{NurseryBytes: 128 << 10}, []TypeDesc{desc})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			want := make([]uint32, 0, live)
			allocate := func(count int) {
				for range count {
					ref, err := c.NewStructDefault(0)
					if err != nil {
						t.Fatal(err)
					}
					want = append(want, handleOf(ref))
				}
			}
			allocate(live - after)
			if !c.PrepareNativeArrayAllocationImmediate(512) {
				t.Fatal("large reservation unavailable")
			}
			reserved := c.nativeLargeArrayAlloc.Handle
			allocate(after)
			c.discardNativeLargeArrayAllocation()
			if !slices.Equal(c.nurseryHandles, want) || !slices.Equal(c.freeHandles, []uint32{reserved}) {
				t.Fatalf("cancellation changed live order or recycled twice: later=%d nursery=%v free=%v", after, c.nurseryHandles, c.freeHandles)
			}
		})
	}
}

func TestNativeLargeArrayCancellationAfterStructFallback(t *testing.T) {
	desc, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ref, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(ref)
	if !c.PrepareNativeStructAllocation(16) || !c.PrepareNativeArrayAllocationImmediate(512) {
		t.Fatal("interleaved reservations unavailable")
	}
	reserved := c.nativeLargeArrayAlloc.Handle
	// Collection first cancels the struct batch. The later large reservation
	// forces its non-tail fallback to compact all free entries from the ledger.
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	occurrences := 0
	for _, h := range c.freeHandles {
		if h == reserved {
			occurrences++
		}
	}
	if occurrences != 1 || c.nativeLargeArrayAlloc.Count != 0 || !slices.Equal(c.nurseryHandles, []uint32{handleOf(ref)}) {
		t.Fatalf("interleaved cancel lost or duplicated an identity: nursery %v free %v state %+v", c.nurseryHandles, c.freeHandles, c.nativeLargeArrayAlloc)
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeLargeArrayCancellationAfterPrefixCompaction(t *testing.T) {
	for _, removed := range []int{1, 2} {
		t.Run(fmt.Sprintf("removed=%d", removed), func(t *testing.T) {
			desc, err := NewStructDesc(0, nil)
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewCollector(Config{}, []TypeDesc{desc})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for range 3 {
				if _, err := c.NewStructDefault(0); err != nil {
					t.Fatal(err)
				}
			}
			if !c.PrepareNativeArrayAllocationImmediate(512) {
				t.Fatal("large reservation unavailable")
			}
			reserved := c.nativeLargeArrayAlloc.Handle
			if _, err := c.NewStructDefault(0); err != nil {
				t.Fatal(err)
			}
			// Model a compacted prefix while the unused large reservation remains.
			// Its recorded position must be treated only as a validated hint.
			for _, h := range c.nurseryHandles[:removed] {
				c.free(h)
			}
			copy(c.nurseryHandles, c.nurseryHandles[removed:])
			c.nurseryHandles = c.nurseryHandles[:len(c.nurseryHandles)-removed]
			want := make([]uint32, 0, len(c.nurseryHandles)-1)
			for _, h := range c.nurseryHandles {
				if h != reserved {
					want = append(want, h)
				}
			}
			free := len(c.freeHandles)
			c.discardNativeLargeArrayAllocation()
			if !slices.Equal(c.nurseryHandles, want) || len(c.freeHandles) != free+1 || c.freeHandles[free] != reserved {
				t.Fatalf("compaction changed a live identity or lost the reservation: nursery %v free %v", c.nurseryHandles, c.freeHandles)
			}
		})
	}
}

func BenchmarkNativeLargeArrayCancellation(b *testing.B) {
	type positionCase struct {
		name  string
		after int
	}
	for _, prefix := range []int{1_000, 100_000, 1_000_000} {
		positions := []positionCase{{"tail=true", 0}, {"tail=false", 1}, {"distance=512", 512}}
		if prefix >= 1024 {
			positions = append(positions, positionCase{"distance=1024", 1024})
		}
		positions = append(positions, positionCase{"front", prefix})
		for _, position := range positions {
			b.Run(fmt.Sprintf("prefix=%d/%s", prefix, position.name), func(b *testing.B) {
				desc, err := NewStructDesc(0, nil)
				if err != nil {
					b.Fatal(err)
				}
				c, err := NewCollector(Config{NurseryBytes: uint32(prefix*16 + 4096), SurvivorBytes: 4096}, []TypeDesc{desc})
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				for range prefix {
					if _, err := c.NewStructDefault(0); err != nil {
						b.Fatal(err)
					}
				}
				if !c.PrepareNativeArrayAllocationImmediate(512) {
					b.Fatal("large reservation unavailable")
				}
				c.discardNativeLargeArrayAllocation()
				later := make([]uint32, position.after)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					// Reproduce append order without allocating more live objects:
					// reserve first, then publish the saved later identities.
					before := len(c.nurseryHandles) - len(later)
					copy(later, c.nurseryHandles[before:])
					c.nurseryHandles = c.nurseryHandles[:before]
					if !c.PrepareNativeArrayAllocationImmediate(512) {
						b.Fatal("large reservation unavailable")
					}
					c.nurseryHandles = append(c.nurseryHandles, later...)
					c.discardNativeLargeArrayAllocation()
				}
				b.StopTimer()
				b.ReportMetric(float64(unsafe.Sizeof(Collector{})), "collector-bytes")
				b.ReportMetric(float64(prefix), "prefix-handles")
				b.ReportMetric(float64(position.after), "later-handles")
			})
		}
	}
}
