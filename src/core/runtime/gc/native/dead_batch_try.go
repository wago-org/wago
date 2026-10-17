package gc

// PreparedDeadAllocationBatch binds an immutable validated layout and allocation
// policy to one collector. Its zero value is invalid. Nursery growth and live
// reservations are deliberately not cached; TryReserve checks them each time.
type PreparedDeadAllocationBatch struct {
	collector *Collector
	layout    uint64
}

// PrepareDeadAllocationBatch validates the compiler-proven dead object's layout
// and the collector's fixed profile/collection/large-object policy. The collector
// owns those policy fields for its lifetime; only nursery capacity can grow.
func (c *Collector) PrepareDeadAllocationBatch(size, alignment uint32) PreparedDeadAllocationBatch {
	if !NativeDeadBatchSupported || size == 0 || size > 4095 || alignment < 8 || alignment > 4096 || alignment&(alignment-1) != 0 || !c.nativeAllocationAllowed(size) {
		return PreparedDeadAllocationBatch{}
	}
	stride := (size + alignment - 1) &^ (alignment - 1)
	return PreparedDeadAllocationBatch{c, uint64(size) | uint64(alignment-1)<<12 | uint64(stride)<<24}
}

// TryReserve charges compiler-proven nonescaping pointer-free objects without
// collecting. The caller exclusively owns the bound collector. All guards
// precede mutation so false permits an ordinary allocating retry.
func (batch PreparedDeadAllocationBatch) TryReserve(count uint32) bool {
	c := batch.collector
	if c == nil || c.closed || count == 0 || c.nativeStructAlloc.Count == 0 || c.nativeStructAlloc.Count > NativeStructAllocHandleCapacity || c.nativeStructAlloc.ChunkEnd != 0 {
		return false
	}
	size := batch.layout & 4095
	mask := (batch.layout >> 12) & 4095
	stride := batch.layout >> 24
	start := (uint64(c.nurseryBump) + mask) &^ mask
	total := uint64(count-1)*stride + size
	limit := uint64(c.edenBytes())
	if start > limit || total > limit-start {
		return false
	}
	c.nurseryBump = uint32(start + total)
	c.stats.Allocations += uint64(count)
	c.stats.LiveObjects += count
	return true
}

// TryReserveDeadAllocationBatch is the checked one-shot form. Prepared callers
// can hoist only immutable policy/layout checks with PrepareDeadAllocationBatch.
func (c *Collector) TryReserveDeadAllocationBatch(count, size, alignment uint32) bool {
	return c.PrepareDeadAllocationBatch(size, alignment).TryReserve(count)
}
