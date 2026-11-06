package gc

import "errors"

// ReserveDeadArrayAllocations preserves the individual allocation effects of count
// fixed-length pointer-free arrays whose identities and payloads cannot escape.
// The caller must prove that the removed initializers and consumers have no
// effects or traps, and that the root set stays unchanged throughout the batch.
func (c *Collector) ReserveDeadArrayAllocations(typeID TypeID, length, count uint32, roots RootSet) error {
	return c.ReserveDeadArrayAllocationsWithPoll(typeID, length, count, roots, nil)
}

// ReserveDeadArrayAllocationsWithPoll polls before each allocation. The caller
// may abort from poll by unwinding its invocation.
func (c *Collector) ReserveDeadArrayAllocationsWithPoll(typeID TypeID, length, count uint32, roots RootSet, poll func()) error {
	d, err := c.desc(typeID)
	if err != nil {
		return err
	}
	if _, err := ArraySize(d, length); err != nil {
		return err
	}
	if !d.PointerFree() {
		return errors.New("gc: batched dead arrays require pointer-free storage")
	}
	if err := c.errIfClosed(); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	c.CancelNativeAllocationBatch()
	for count != 0 {
		if poll != nil {
			poll()
		}
		// Use the ordinary single-object path so exhaustion, stress collection,
		// injected failures, survivor handling, and ordering remain per object.
		if _, err := c.ReserveDeadArrayAllocation(typeID, length, roots); err != nil {
			return err
		}
		count--
	}
	return nil
}
