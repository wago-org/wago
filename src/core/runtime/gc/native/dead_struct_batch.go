package gc

import "errors"

// ReserveDeadStructAllocations preserves the individual allocation effects of
// count pointer-free structs whose identities and payloads cannot escape.
// The caller must prove that the removed initializers and consumers have no
// effects or traps, and that the root set stays unchanged throughout the batch.
func (c *Collector) ReserveDeadStructAllocations(typeID TypeID, count uint32, roots RootSet) error {
	return c.ReserveDeadStructAllocationsWithPoll(typeID, count, roots, nil)
}

// ReserveDeadStructAllocationsWithPoll polls before each allocation. The caller
// may abort from poll by unwinding its invocation.
func (c *Collector) ReserveDeadStructAllocationsWithPoll(typeID TypeID, count uint32, roots RootSet, poll func()) error {
	d, err := c.desc(typeID)
	if err != nil {
		return err
	}
	if _, err := StructSize(d); err != nil {
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
	for count != 0 {
		if poll != nil {
			poll()
		}
		// Use the ordinary single-object path so exhaustion, stress collection,
		// injected failures, survivor handling, and ordering remain per object.
		if _, err := c.ReserveDeadStructAllocation(typeID, roots); err != nil {
			return err
		}
		count--
	}
	return nil
}
