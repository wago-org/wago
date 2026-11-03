//go:build wagodebug

package gc

import (
	"bytes"
	"slices"
	"testing"
	"unsafe"
)

func TestMaintainedOccupancyProductionRollbackRetry(t *testing.T) {
	h := newMatureRunHeap(t)
	h.limit = 65536
	h.mem = makeAlignedBytes(65536, 16)
	hole, err := h.alloc(32736, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.free(hole); err != nil {
		t.Fatal(err)
	}
	h.mature.occupancy = slices.Clip(h.mature.occupancy)
	c := &Collector{
		throughput:     h,
		nursery:        make([]byte, 32800),
		handles:        []handleEntry{{}, guard, {off: 0, size: 32, space: spaceNursery}, {off: 32, size: 32768, space: spaceNursery}},
		nurseryHandles: []uint32{2, 3},
		mark:           []bool{false, false, true, true},
	}
	for i := range c.nursery {
		c.nursery[i] = byte(i*31 + 7)
	}
	payload := slices.Clone(c.nursery)
	handles := slices.Clone(c.handles)
	occupancy := slices.Clone(c.throughput.mature.occupancy)
	free := c.throughput.freeSpans()
	bump := c.throughput.bump
	disarm := armFailure(c, failPromotionCommit, 0)
	_, _, err = c.promoteMarkedNurseryImmediate()
	disarm()
	if err == nil {
		t.Fatal("commit injection did not fire")
	}
	if !slices.Equal(handles, c.handles) || !bytes.Equal(payload, c.nursery) ||
		!slices.Equal(occupancy, c.throughput.mature.occupancy) || !slices.Equal(free, c.throughput.freeSpans()) ||
		c.throughput.bump != bump || len(c.throughput.pendingFree) != 0 || c.throughput.pendingBytes != 0 {
		t.Fatal("failed promotion did not restore handles/payload/occupancy/free/debt")
	}
	if err = c.throughput.verify(c.handles); err != nil {
		t.Fatal(err)
	}
	reservedBacking := unsafe.SliceData(c.throughput.mature.occupancy)
	if cap(c.throughput.mature.occupancy) < 2 {
		t.Fatalf("rollback lost preflight capacity: %d", cap(c.throughput.mature.occupancy))
	}
	if _, _, err = c.promoteMarkedNurseryImmediate(); err != nil {
		t.Fatal("retry", err)
	}
	if c.handles[2].off != 0 || c.handles[3].off != 32768 || c.handles[2].space != spaceOld || c.handles[3].space != spaceOld {
		t.Fatalf("retry handles = %+v %+v", c.handles[2], c.handles[3])
	}
	if !bytes.Equal(payload[16:32], c.throughput.bytes(c.handles[2])[16:]) || !bytes.Equal(payload[48:], c.throughput.bytes(c.handles[3])[16:]) {
		t.Fatal("retry payload")
	}
	if unsafe.SliceData(c.throughput.mature.occupancy) != reservedBacking || len(c.throughput.mature.occupancy) != 2 ||
		len(c.throughput.freeSpans()) != 1 || c.throughput.freeBytes != 32704 || len(c.throughput.pendingFree) != 0 {
		t.Fatalf("retry backing/occupancy/free/debt = %p/%d/%v/%d/%v", unsafe.SliceData(c.throughput.mature.occupancy), len(c.throughput.mature.occupancy), c.throughput.freeSpans(), c.throughput.freeBytes, c.throughput.pendingFree)
	}
	if err = c.throughput.verify(c.handles); err != nil {
		t.Fatal(err)
	}
}
