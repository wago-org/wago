//go:build amd64

package amd64

// stageFlushSpills protects live sources before a prefix flush's canonical stores.
// The caller reserves the destination range with spillFloor. Deferred children
// and operands above the prefix must survive too, so inspect the physical stack.
func (f *fn) stageFlushSpills(belowSlots int, belowRoots []*elem) {
	if belowSlots == 0 {
		return
	}
	f.s.spillExtentValid = false
	nextSlot := f.spillFloor
	direct := true
	scratch := regNone
	for pass := 0; pass < 2; pass++ {
		rootIndex, canonicalSlot := 0, 0
		overlaps := false
		for e := f.s.head.next; e != f.s.head; e = e.next {
			canonical := false
			destination := -1
			if rootIndex < len(belowRoots) && e == belowRoots[rootIndex] {
				destination = canonicalSlot
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
				direct = direct && destination >= from
				continue
			}
			for i := 0; i < width; i++ {
				f.a.Load64(scratch, RSP, f.spillOff(from+i))
				f.a.Store64(RSP, f.spillOff(nextSlot+i), scratch)
			}
			e.st.slot = uint32(nextSlot)
			nextSlot += width
		}
		if !overlaps {
			return
		}
		if pass == 0 {
			// Use an allocator-owned scratch. A fixed RAX save/restore temporarily
			// overwrites pending load addresses and pinned arguments. Allocation
			// materializes or spills an owner before that register can be reused.
			scratch = f.allocReg(0)
			nextSlot = max(nextSlot, f.curSpillSlot())
			if direct {
				f.canonicalizeFlushSpills(belowSlots, belowRoots, scratch)
				break
			}
		}
	}
	// Staging moved live homes after curSpillSlot cached their old extent.
	f.s.spillExtentValid = false
	f.release(scratch)
	if nextSlot > f.maxSpill {
		f.maxSpill = nextSlot
	}
}

// When every overlapping source is a complete prefix root at or below its own
// destination, copy backwards directly to canonical homes. Each write then lies
// above all earlier sources; no temporary spill range or second copy is needed.
func (f *fn) canonicalizeFlushSpills(slot int, roots []*elem, scratch Reg) {
	f.s.spillExtentValid = false
	limit := slot
	for i := len(roots) - 1; i >= 0; i-- {
		root := roots[i]
		width := rootMachineType(root).stackSlots()
		slot -= width
		if !root.isValue() || root.st.kind != stSlot {
			continue
		}
		from := root.st.slotIndex()
		if from >= limit || from == slot {
			continue
		}
		// Copy both halves backwards when a vector overlaps its new home.
		for j := width - 1; j >= 0; j-- {
			f.a.Load64(scratch, RSP, f.spillOff(from+j))
			f.a.Store64(RSP, f.spillOff(slot+j), scratch)
		}
		root.st.slot = uint32(slot)
	}
}
