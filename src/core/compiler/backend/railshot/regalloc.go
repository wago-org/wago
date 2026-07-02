package amd64

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
	f.removeRef(e)
	f.regUser[r] = e
	if e.kind == ekDeferred && e.typ != mtNone {
		e.st.typ = e.typ
	}
	e.kind = ekValue
	e.st.kind, e.st.reg = stReg, r
	f.addRef(e)
}

// pushReg pushes a register-resident value of the given type onto the operand
// stack and records the register's new owner.
func (f *fn) pushReg(r Reg, typ machineType) *elem {
	e := f.pushValue(storage{kind: stReg, typ: typ, reg: r})
	f.regUser[r] = e
	return e
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
	r := f.allocRegOrNone(avoid)
	if r == regNone {
		panic("amd64: no register available to spill")
	}
	return r
}

// allocRegOrNone is allocReg's non-panicking form: regNone when every candidate
// is blocked and nothing on the stack is spillable. Callers with a memory-operand
// fallback (condenseBinary's RHS relocation) use it to degrade to a spill slot
// instead of failing under extreme pressure.
func (f *fn) allocRegOrNone(avoid regMask) Reg {
	block := avoid.union(f.pinned).union(f.pinnedLocalMask).union(f.reserved)
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
	// Under high pressure, a pending deferred load holds an address register: emit
	// its load and spill the result to free the register.
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue && e.st.kind == stMemRef && !block.has(e.st.reg) {
			r := e.st.reg
			f.loadMemRef(r, e.st)
			f.occupy(e, r)
			f.spill(e)
			return r
		}
	}
	return regNone
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
	f.a.Store64(RSP, f.spillOff(slot), r)
	f.regUser[r] = nil
	f.replaceStorage(e, storage{kind: stSlot, typ: e.st.typ, slot: slot})
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
		f.a.Load64(r, RSP, f.spillOff(e.st.slot))
		f.occupy(e, r)
		return r
	case stLocalRef:
		r := f.allocReg(0)
		f.a.Load64(r, RSP, f.localOff(e.st.idx))
		f.occupy(e, r)
		return r
	case stLocalReg:
		// Borrowed pinned-local register: copy its value into an owned register so
		// the caller may clobber it without corrupting the local.
		r := f.allocReg(0)
		f.a.MovReg64(r, e.st.reg)
		f.occupy(e, r)
		return r
	case stGlobReg:
		// Borrowed value-pinned global register: copy out, mirroring stLocalReg.
		r := f.allocReg(0)
		f.a.MovReg64(r, e.st.reg)
		f.occupy(e, r)
		return r
	case stMemRef:
		// Deferred load: emit the mov now, reusing an OWNED address register as
		// the destination; a borrowed (pinned-local) address loads into a fresh one.
		dst := e.st.reg
		if e.st.memBorrow() >= 0 {
			dst = f.allocReg(maskOf(e.st.reg))
		}
		f.loadMemRef(dst, e.st)
		f.occupy(e, dst)
		return dst
	}
	panic("amd64: cannot materialize storage")
}

// materializeRead returns a register holding e's value for an IMMEDIATE,
// READ-ONLY use, plus whether the caller owns (and must release) it. A borrowed
// pinned-local register is returned in place — no copy (WARP's
// liftToRegInPlace with writable=false) — which is safe only when the use is
// emitted before anything that could write the local (no deferral, no
// local.set in between).
func (f *fn) materializeRead(e *elem) (Reg, bool) {
	if e.kind == ekValue && (e.st.kind == stLocalReg || e.st.kind == stGlobReg) {
		return e.st.reg, false
	}
	return f.materialize(e), true
}

// memRefValue emits a deferred load and returns an OWNED register holding the
// value (the address register is reused when owned; a borrowed pinned-local
// address loads into a fresh register). The caller releases the result.
func (f *fn) memRefValue(st storage) Reg {
	dst := st.reg
	if st.memBorrow() >= 0 {
		dst = f.allocReg(maskOf(st.reg))
	}
	f.loadMemRef(dst, st)
	return dst
}

// releaseMemRef frees a consumed deferred load's address register — unless it
// was a borrowed pinned-local register, which is never allocator-owned.
func (f *fn) releaseMemRef(st storage) {
	if st.memBorrow() < 0 {
		f.release(st.reg)
	}
}

// loadMemRef emits the actual load for a deferred memory value into dst.
func (f *fn) loadMemRef(dst Reg, st storage) {
	f.a.LoadIdx(dst, RBX, st.reg, st.memDisp(), st.memSize(), st.memSigned(), st.typ.is64())
}

// materializePendingLoads forces every deferred load on the operand stack to be
// emitted. Called before a linear-memory write so a deferred load reads the
// pre-write value (WARP's load-before-store ordering).
func (f *fn) materializePendingLoads() {
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue && e.st.kind == stMemRef {
			f.materialize(e)
		}
	}
}

// loadConst emits an immediate load of st's constant into r.
func (f *fn) loadConst(r Reg, st storage) {
	if st.typ.is64() {
		f.a.MovImm64(r, uint64(st.cval))
	} else {
		f.a.MovImm32(r, int32(st.cval))
	}
}
