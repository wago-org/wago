package gc

import (
	"fmt"
	"strings"
	"testing"
)

func consumeNativeLargeArrayReservationForVerifyTest(t *testing.T, c *Collector, bytes uint32) Ref {
	t.Helper()
	s := &c.nativeLargeArrayAlloc
	h := s.Handle
	space := spaceKind((s.ChunkBump >> 16) & 0xff)
	c.handles[h] = handleEntry{off: s.ChunkStart, size: bytes, class: uint16(s.ChunkBump), space: space}
	flags := uint32(FlagPointerFree)
	if space == spaceLarge {
		flags |= FlagLarge
	}
	ref := makeObjRef(h)
	c.writeHeader(ref, ObjHeader{Size: bytes, Aux: (bytes - HeaderSize) / 4, Flags: flags})
	s.ChunkCursor = s.ChunkEnd
	s.Cursor = 1
	c.stats.Allocations++
	return ref
}

func TestPublicVerifyNativeLargeArrayReservationLifecycle(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	for _, shift := range []uint8{0, 6, 12} {
		for _, bytes := range []uint32{512, 40 << 10} {
			t.Run(fmt.Sprintf("shift=%d/bytes=%d", shift, bytes), func(t *testing.T) {
				c, err := NewCollector(Config{NurseryBytes: 16 << 10, NurseryRegionShift: shift}, []TypeDesc{desc})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if !c.PrepareNativeArrayAllocationImmediate(bytes) {
					t.Fatal("reservation refused")
				}
				if err := c.Verify(nil); err != nil {
					t.Fatalf("unconsumed reservation: %v", err)
				}
				ref := consumeNativeLargeArrayReservationForVerifyTest(t, c, bytes)
				root := Root(ref)
				if err := c.Verify(Slots{&root}); err != nil {
					t.Fatalf("consumed reservation: %v", err)
				}
				before := c.throughput.bump
				c.discardNativeLargeArrayAllocation()
				if c.throughput.bump != before {
					t.Fatal("discard reclaimed a consumed reservation")
				}
				if err := c.Verify(Slots{&root}); err != nil {
					t.Fatalf("discarded consumed reservation: %v", err)
				}
			})
		}
	}
}

func TestPublicVerifyCanceledNativeLargeArrayReservation(t *testing.T) {
	for _, shift := range []uint8{0, 6, 12} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			c, err := NewCollector(Config{NurseryBytes: 16 << 10, NurseryRegionShift: shift}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if !c.PrepareNativeArrayAllocationImmediate(40 << 10) {
				t.Fatal("reservation refused")
			}
			c.discardNativeLargeArrayAllocation()
			if err := c.Verify(nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublicVerifyNativeLOSReservationIncludesAbsorbedTail(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	for _, shift := range []uint8{0, 6, 12} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			c, err := NewCollector(Config{NurseryBytes: 16 << 10, NurseryRegionShift: shift}, []TypeDesc{desc})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			seed, err := c.throughput.alloc((40<<10)+16, spaceLarge)
			if err != nil {
				t.Fatal(err)
			}
			anchor, err := c.throughput.alloc(32, spaceLarge)
			if err != nil {
				t.Fatal(err)
			}
			h := c.newHandle(anchor)
			c.writeHeader(makeObjRef(h), ObjHeader{Size: 32, Aux: 4, Flags: FlagPointerFree | FlagLarge})
			if err := c.throughput.free(seed); err != nil {
				t.Fatal(err)
			}
			if !c.PrepareNativeArrayAllocationImmediate(40 << 10) {
				t.Fatal("reservation refused")
			}
			if c.nativeLargeArrayAlloc.ChunkBump&uint32(handleAbsorbedTailBit) == 0 {
				t.Fatalf("fixture did not absorb the indexed tail: %+v", c.nativeLargeArrayAlloc)
			}
			if err := c.Verify(nil); err != nil {
				t.Fatal(err)
			}
			c.discardNativeLargeArrayAllocation()
			if err := c.Verify(nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublicVerifyRejectsMalformedNativeLargeArrayReservation(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 16 << 10}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.PrepareNativeArrayAllocationImmediate(40 << 10) {
		t.Fatal("reservation refused")
	}
	want := c.nativeLargeArrayAlloc
	wantIndex := c.nativeLargeArrayNurseryIndex
	tests := []struct {
		name string
		edit func(*nativeLargeArrayAllocState)
	}{
		{"epoch", func(s *nativeLargeArrayAllocState) { s.Epoch++ }},
		{"count", func(s *nativeLargeArrayAllocState) { s.Count = 2 }},
		{"cursor", func(s *nativeLargeArrayAllocState) { s.Cursor = 2 }},
		{"handle", func(s *nativeLargeArrayAllocState) { s.Handle = uint32(len(c.handles)) }},
		{"chunk-cursor", func(s *nativeLargeArrayAllocState) { s.ChunkCursor++ }},
		{"space", func(s *nativeLargeArrayAllocState) { s.ChunkBump = uint32(spaceOld) << 16 }},
		{"class", func(s *nativeLargeArrayAllocState) { s.ChunkBump |= 1 << 31 }},
		{"extent", func(s *nativeLargeArrayAllocState) { s.ChunkEnd = c.throughput.bump + 16 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c.nativeLargeArrayAlloc = want
			tc.edit(&c.nativeLargeArrayAlloc)
			if err := c.Verify(nil); err == nil {
				t.Fatal("malformed reservation accepted")
			}
		})
	}
	c.nativeLargeArrayAlloc = want
	c.nativeLargeArrayNurseryIndex = -1
	if err := c.Verify(nil); err == nil || !strings.Contains(err.Error(), "handle ledger") {
		t.Fatalf("missing ledger entry: %v", err)
	}
	c.nativeLargeArrayAlloc = want
	c.nativeLargeArrayNurseryIndex = wantIndex
	c.handles[want.Handle] = handleEntry{off: want.ChunkStart, size: 40 << 10, class: uint16(want.ChunkBump), space: spaceLarge}
	if err := c.Verify(nil); err == nil || !strings.Contains(err.Error(), "unpublished") {
		t.Fatalf("premature handle publication: %v", err)
	}
	c.handles[want.Handle] = handleEntry{}
	ref := consumeNativeLargeArrayReservationForVerifyTest(t, c, 40<<10)
	root := Root(ref)
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatalf("valid consumed publication: %v", err)
	}
	c.handles[want.Handle].off += 16
	if err := c.Verify(Slots{&root}); err == nil || !strings.Contains(err.Error(), "publication mismatch") {
		t.Fatalf("mismatched consumed publication: %v", err)
	}
}

func TestPublicVerifyRejectsNativeLOSReservationOverlap(t *testing.T) {
	for _, overlap := range []string{"live", "indexed", "debt"} {
		t.Run(overlap, func(t *testing.T) {
			c, err := NewCollector(Config{NurseryBytes: 16 << 10}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if !c.PrepareNativeArrayAllocationImmediate(40 << 10) {
				t.Fatal("reservation refused")
			}
			s := c.nativeLargeArrayAlloc
			switch overlap {
			case "live":
				c.handles = append(c.handles, handleEntry{off: s.ChunkStart, size: 16, space: spaceLarge})
			case "indexed":
				if err := c.throughput.insertFreeSpan(throughputFreeSpan{off: s.ChunkStart, size: 16}); err != nil {
					t.Fatal(err)
				}
			case "debt":
				c.throughput.pendingFree = append(c.throughput.pendingFree, throughputFreeSpan{off: s.ChunkStart, size: 16})
				c.throughput.pendingBytes += 16
			}
			if err := c.Verify(nil); err == nil || !strings.Contains(err.Error(), "overlap") {
				t.Fatalf("overlap accepted: %v", err)
			}
			switch overlap {
			case "live":
				c.handles = c.handles[:len(c.handles)-1]
			case "indexed":
				node, ok := c.throughput.removeFreeSpan(s.ChunkStart)
				if !ok {
					t.Fatal("overlap fixture span disappeared")
				}
				c.throughput.releaseSpanNode(node)
			case "debt":
				c.throughput.pendingFree = c.throughput.pendingFree[:len(c.throughput.pendingFree)-1]
				c.throughput.pendingBytes -= 16
			}
		})
	}
}
