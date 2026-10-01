//go:build amd64

package amd64

// stageFlushSpills moves existing sources out of a prefix flush's destination
// range. The caller reserves that range with spillFloor. Deferred children and
// operands above the prefix must survive too, so inspect the physical stack.
func (f *fn) stageFlushSpills(belowSlots int, belowRoots []*elem) {
	if belowSlots == 0 {
		return
	}
	nextSlot := f.spillFloor
	scratchSlot := 0
	for pass := 0; pass < 2; pass++ {
		rootIndex, canonicalSlot := 0, 0
		overlaps := false
		for e := f.s.head.next; e != f.s.head; e = e.next {
			canonical := false
			if rootIndex < len(belowRoots) && e == belowRoots[rootIndex] {
				canonical = e.isValue() && e.st.kind == stSlot && e.st.slotIndex() == canonicalSlot
				canonicalSlot += rootMachineType(e).stackSlots()
				rootIndex++
			}
			if !e.isValue() || e.st.kind != stSlot {
				continue
			}
			from, width := e.st.slotIndex(), e.st.typ.stackSlots()
			if pass == 0 && from+width > nextSlot {
				nextSlot = from + width
			}
			// A root already at its destination is not written by flushBelow.
			if from >= belowSlots || canonical {
				continue
			}
			overlaps = true
			if pass == 0 {
				continue
			}
			for i := 0; i < width; i++ {
				f.a.Load64(RAX, RSP, f.spillOff(from+i))
				f.a.Store64(RSP, f.spillOff(nextSlot+i), RAX)
			}
			e.st.slot = uint32(nextSlot)
			nextSlot += width
		}
		if !overlaps {
			return
		}
		if pass == 0 {
			// RAX may still own a condition/address or a pinned call argument. Preserve
			// its physical value without changing ownership or moving the stack pointer.
			scratchSlot = nextSlot
			nextSlot++
			f.a.Store64(RSP, f.spillOff(scratchSlot), RAX)
		}
	}
	f.a.Load64(RAX, RSP, f.spillOff(scratchSlot))
	if nextSlot > f.maxSpill {
		f.maxSpill = nextSlot
	}
}
