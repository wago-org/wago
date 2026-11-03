package gc

import (
	"fmt"
	"testing"
)

// Match generated AMD64 publication, including its logical-size chunk cursor.
func consumeReservationLogicalEndpoint(c *Collector, bytes uint32) Ref {
	s := &c.nativeLargeArrayAlloc
	h := s.Handle
	sp := spaceKind((s.ChunkBump >> 16) & 255)
	c.handles[h] = handleEntry{off: s.ChunkStart, size: bytes, class: uint16(s.ChunkBump), space: sp}
	flags := uint32(FlagPointerFree)
	if sp == spaceLarge {
		flags |= FlagLarge
	}
	r := makeObjRef(h)
	c.writeHeader(r, ObjHeader{Size: bytes, Aux: (bytes - HeaderSize) / 4, Flags: flags})
	s.ChunkCursor = s.ChunkStart + bytes
	s.Cursor = 1
	c.stats.Allocations++
	return r
}

func TestNativeConsumedReservationTransitions(t *testing.T) {
	for _, shift := range []uint8{0, 6, 12} {
		for _, bytes := range []uint32{512, 40 << 10} {
			for _, action := range []string{"mutate", "promote", "cancel-promote", "discard-promote"} {
				t.Run(fmt.Sprintf("shift=%d/bytes=%d/%s", shift, bytes, action), func(t *testing.T) {
					d, _ := NewArrayDesc(0, StorageI32)
					c, err := NewCollector(Config{NurseryBytes: 16 << 10, NurseryRegionShift: shift}, []TypeDesc{d})
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					if !c.PrepareNativeArrayAllocationImmediate(bytes) {
						t.Fatal("prepare")
					}
					r := consumeReservationLogicalEndpoint(c, bytes)
					root := Root(r)
					if err := c.Verify(Slots{&root}); err != nil {
						t.Fatalf("before: %v", err)
					}
					if action == "cancel-promote" {
						c.CancelNativeAllocationBatch()
					}
					if action == "discard-promote" {
						c.discardNativeLargeArrayAllocation()
					}
					if action == "mutate" {
						if err := c.ArraySet(r, 0, I32Value(123)); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := c.ForcePromote(r); err != nil {
							t.Fatal(err)
						}
					}
					if err := c.Verify(Slots{&root}); err != nil {
						t.Fatalf("after %s: %v; reservation=%+v handle=%+v ledger=%v", action, err, c.nativeLargeArrayAlloc, *c.entry(r), c.nurseryHandles)
					}
				})
			}
		}
	}
}

func TestNativeConsumedReservationLogicalSize(t *testing.T) {
	for _, shift := range []uint8{0, 6, 12} {
		for _, bytes := range []uint32{520, (40 << 10) + 8} {
			t.Run(fmt.Sprintf("shift=%d/bytes=%d", shift, bytes), func(t *testing.T) {
				d, _ := NewArrayDesc(0, StorageI32)
				c, err := NewCollector(Config{NurseryBytes: 16 << 10, NurseryRegionShift: shift}, []TypeDesc{d})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if !c.PrepareNativeArrayAllocationImmediate(bytes) {
					t.Fatal("prepare")
				}
				if err := c.Verify(nil); err != nil {
					t.Fatal(err)
				}
				r := consumeReservationLogicalEndpoint(c, bytes)
				root := Root(r)
				if err := c.Verify(Slots{&root}); err != nil {
					t.Fatalf("logical publication: %v", err)
				}
			})
		}
	}
}

func TestNativeReservationCannotMaskMissingExtent(t *testing.T) {
	c, err := NewCollector(Config{NurseryBytes: 16 << 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.PrepareNativeArrayAllocationImmediate(40 << 10) {
		t.Fatal("prepare")
	}
	if _, err := c.throughput.alloc(32, spaceOld); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(nil); err == nil {
		t.Fatal("missing second extent accepted")
	}
}

func TestNativeReservationLedgerCompaction(t *testing.T) {
	for _, consumed := range []bool{false, true} {
		for _, bytes := range []uint32{512, 40 << 10} {
			t.Run(fmt.Sprintf("consumed=%t/bytes=%d", consumed, bytes), func(t *testing.T) {
				d, _ := NewArrayDesc(0, StorageI32)
				c, err := NewCollector(Config{NurseryBytes: 16 << 10}, []TypeDesc{d})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				earlier, err := c.NewArrayDefault(0, 4)
				if err != nil {
					t.Fatal(err)
				}
				if !c.PrepareNativeArrayAllocationImmediate(bytes) {
					t.Fatal("prepare")
				}
				if consumed {
					consumeReservationLogicalEndpoint(c, bytes)
				}
				if err := c.Verify(nil); err != nil {
					t.Fatal(err)
				}
				if err := c.ForcePromote(earlier); err != nil {
					t.Fatal(err)
				}
				if err := c.Verify(nil); err != nil {
					t.Fatalf("reservation rejected after unrelated promotion: %v; cached index=%d ledger=%v", err, c.nativeLargeArrayNurseryIndex, c.nurseryHandles)
				}
			})
		}
	}
}
