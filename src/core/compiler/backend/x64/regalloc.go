package x64

// On-the-fly register allocator — the core of WARP's speed. Values (locals,
// temporaries, deferred results) live in registers over the whole general-purpose
// file and are spilled to frame slots only when the allocator runs out. Ported
// from WARP's reqScratchReg / spillFromStack / liftToRegInPlace (Common.cpp) and
// the register occupancy the backend tracks (x86_64_backend.cpp).

const regNone Reg = 0xFF

// occupy records that value elem e now lives in register r. When e was a deferred
// node, its storage inherits the node's result type so downstream consumers
// (select width, result marshaling) see the correct machine type.
func (f *fn) occupy(e *elem, r Reg) {
	f.regUser[r] = e
	if e.kind == ekDeferred && e.typ != mtNone {
		e.st.typ = e.typ
	}
	e.kind = ekValue
	e.st.kind, e.st.reg = stReg, r
}

// release marks register r free (its value has been consumed or moved out).
func (f *fn) release(r Reg) {
	if r != regNone {
		f.regUser[r] = nil
	}
}

// allocReg returns a free allocatable GPR, spilling the deepest spillable value
// on the stack when none is free. Registers in `avoid` (live operands) and
// f.pinned are never chosen. Prefers freely-allocatable regs over the reserved
// scratch regs (gpAlloc lists scratch last, so first-fit does this naturally).
func (f *fn) allocReg(avoid regMask) Reg {
	block := avoid.union(f.pinned)
	for _, r := range gpAlloc {
		if f.regUser[r] == nil && !block.has(r) {
			return r
		}
	}
	// Spill a victim: the deepest (bottom-most) stack value in a register — it is
	// used furthest in the future, WARP's spill heuristic approximated by depth.
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue && e.st.kind == stReg && !block.has(e.st.reg) {
			r := e.st.reg
			f.spill(e)
			return r
		}
	}
	panic("x64: no register available to spill")
}

// spillIfUsed evicts register r's occupant to a frame slot if one is resident,
// freeing r for a fixed-role use (shift count in RCX, div operands in RAX/RDX).
func (f *fn) spillIfUsed(r Reg) {
	if u := f.regUser[r]; u != nil {
		f.spill(u)
	}
}

// spill evicts the register-resident value elem e to a fresh frame slot.
func (f *fn) spill(e *elem) {
	r := e.st.reg
	slot := f.allocSpillSlot()
	f.a.Store64(RBP, f.spillOff(slot), r)
	f.regUser[r] = nil
	e.st.kind, e.st.slot = stSlot, slot
}

// allocSpillSlot returns the next operand spill slot index, growing the frame.
func (f *fn) allocSpillSlot() int {
	slot := f.curSpillSlot()
	if slot+1 > f.maxSpill {
		f.maxSpill = slot + 1
	}
	return slot
}

// curSpillSlot counts how many stack values currently occupy spill slots, giving
// the next free slot index. (Simple bump within the current operand-stack extent;
// slots are reclaimed as values are consumed.)
func (f *fn) curSpillSlot() int {
	used := 0
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue && e.st.kind == stSlot {
			if e.st.slot+1 > used {
				used = e.st.slot + 1
			}
		}
	}
	return used
}

// materialize ensures value elem e lives in a register and returns it. A deferred
// node is condensed; a constant/local/slot value is loaded/moved into a fresh reg.
func (f *fn) materialize(e *elem) Reg {
	if e.isDeferred() {
		return f.condense(e, regNone)
	}
	switch e.st.kind {
	case stReg:
		return e.st.reg
	case stConst:
		r := f.allocReg(0)
		f.loadConst(r, e.st)
		f.occupy(e, r)
		return r
	case stSlot:
		r := f.allocReg(0)
		f.a.Load64(r, RBP, f.spillOff(e.st.slot))
		f.occupy(e, r)
		return r
	case stLocalRef:
		r := f.allocReg(0)
		f.a.Load64(r, RBP, f.localOff(e.st.idx))
		f.occupy(e, r)
		return r
	}
	panic("x64: cannot materialize storage")
}

// loadConst emits an immediate load of st's constant into r.
func (f *fn) loadConst(r Reg, st storage) {
	if st.typ.is64() {
		f.a.MovImm64(r, uint64(st.cval))
	} else {
		f.a.MovImm32(r, int32(st.cval))
	}
}
