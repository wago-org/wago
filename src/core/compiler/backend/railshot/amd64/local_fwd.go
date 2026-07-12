//go:build amd64

package amd64

// Store-forwarding for unpinned integer locals (localFwdEnabled). After a
// local.set stores the value to the local's frame slot, the value's register is
// kept as a clean copy (value in BOTH register and slot) instead of being freed,
// so a following local.get uses the register (a reg-reg op) rather than a
// memory-operand read that stalls on store-to-load forwarding.
//
// Safety rests on one invariant: the slot is ALWAYS canonical (the store is
// eager), so a forwarding register is a pure, discardable optimization. It can be
// reclaimed for the allocator with no store (reclaimFwd, refs fall back to the
// slot) and is dropped wholesale at every flush. Only call-free functions and only
// integer locals participate, and forwards never cross a flush — so the STACK_REG
// / canonical-slot reconciliation model is entirely untouched.

// localFwdRegOf returns local x's forwarding register, or regNone (bounds-safe:
// inlined-callee locals may index past fwdReg).
func (f *fn) localFwdRegOf(x int) Reg {
	if x < 0 || x >= len(f.fwdReg) {
		return regNone
	}
	return f.fwdReg[x]
}

// localFwdEligible reports whether local x qualifies for store-forwarding.
func (f *fn) localFwdEligible(x int) bool {
	if !localFwdEnabled || f.usesCalls || x < 0 || x >= len(f.fwdReg) {
		return false
	}
	if _, _, pinned := f.pinReg(x); pinned {
		return false
	}
	t := f.localType[x]
	return t == mtI32 || t == mtI64
}

// setLocalFwd records that register r holds a clean copy of local x (the value is
// also in x's slot). The caller must NOT release r — it is now owned by the
// forward and freed only via reclaimFwd or a flush.
func (f *fn) setLocalFwd(x int, r Reg) {
	f.clearFwd(x)
	f.fwdReg[x] = r
	f.fwdRegs = f.fwdRegs.add(r)
	f.regUser[r] = nil // not an operand-stack value; tracked only via fwdReg/fwdRegs
}

// clearFwd drops x's forwarding register from the tracking, returning the register
// to the free pool (its value is safe in the slot). No-op if x has no forward.
func (f *fn) clearFwd(x int) {
	if x >= 0 && x < len(f.fwdReg) {
		if r := f.fwdReg[x]; r != regNone {
			f.fwdRegs = f.fwdRegs.remove(r)
			f.fwdReg[x] = regNone
		}
	}
}

// demoteFwdRefs rewrites every operand-stack reference that borrows x's forwarding
// register into a plain slot reference. The slot is canonical, so this is a
// value-preserving reclassification with no emitted code.
func (f *fn) demoteFwdRefs(x int) {
	r := f.fwdReg[x]
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue && e.st.kind == stLocalReg && e.st.idx == x && e.st.reg == r {
			e.st.kind = stLocalRef
			e.st.reg = regNone
		}
	}
}

// reclaimFwd frees a forwarding register for the allocator: references to it fall
// back to the canonical slot, the tracking is cleared, and the register is
// returned. No store is emitted (the value is already in the slot).
func (f *fn) reclaimFwd(x int) Reg {
	r := f.fwdReg[x]
	f.demoteFwdRefs(x)
	f.clearFwd(x)
	return r
}

// clearAllFwd drops all forwarding at a flush. The flush frees every register and
// materializes the operand stack to canonical slots, so nothing else is needed.
func (f *fn) clearAllFwd() {
	if f.fwdRegs == 0 {
		return
	}
	for x := range f.fwdReg {
		f.fwdReg[x] = regNone
	}
	f.fwdRegs = 0
}
