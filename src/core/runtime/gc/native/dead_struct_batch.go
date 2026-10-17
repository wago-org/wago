package gc

import "errors"

// ReserveDeadStructAllocations preserves the allocation effects of count
// pointer-free structs whose identities and payloads cannot escape. It coalesces
// only consecutive nursery charges that fit before the next collection boundary.
// The caller must prove that the removed initializers and consumers have no
// effects or traps, and that the root set stays unchanged throughout the batch.
func (c *Collector) ReserveDeadStructAllocations(typeID TypeID, count uint32, roots RootSet) error {
	return c.ReserveDeadStructAllocationsWithPoll(typeID, count, roots, nil)
}

// ReserveDeadStructAllocationsWithPoll polls before each nursery batch and
// slow allocation. The caller may abort from poll by unwinding its invocation.
func (c *Collector) ReserveDeadStructAllocationsWithPoll(typeID TypeID, count uint32, roots RootSet, poll func()) error {
	d, err := c.desc(typeID)
	if err != nil {
		return err
	}
	size, err := StructSize(d)
	if err != nil {
		return err
	}
	if !d.PointerFree() {
		return errors.New("gc: batched dead structs require pointer-free storage")
	}
	if err := c.errIfClosed(); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	c.CancelNativeAllocationBatch()
	alignment := max(d.Align, uint32(8))
	stride := align(size, alignment)
	for count != 0 {
		if poll != nil {
			poll()
		}
		if !failureInjectionEnabled && c.nativeAllocationAllowed(size) && !c.shouldAllocateLarge(size) {
			start := align(c.nurseryBump, alignment)
			limit := c.edenBytes()
			if start <= limit && size <= limit-start {
				batch := min(count, 1+(limit-start-size)/stride)
				cycle := c.collectionCycle()
				if c.nativeView.deadNurseryCycle() != cycle {
					c.stats.LiveObjects = 0
					c.nativeView.setDeadNurseryCycle(cycle)
				}
				c.nurseryBump = start + (batch-1)*stride + size
				c.stats.Allocations += uint64(batch)
				c.stats.LiveObjects += batch
				count -= batch
				continue
			}
		}
		// Preserve profile-specific exhaustion, stress collection, injected failures,
		// survivor handling, and exact allocation ordering at every slow boundary.
		if _, err := c.ReserveDeadStructAllocation(typeID, roots); err != nil {
			return err
		}
		count--
	}
	return nil
}
