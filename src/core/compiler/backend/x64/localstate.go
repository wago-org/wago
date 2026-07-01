package x64

// WARP's STACK_REG lazy local-spill model (Common.cpp saveLocalsAndParamsFor
// FuncCall / recoverLocalToReg / recoverAllLocalsToRegBranch), for CALL-MAKING
// functions. Each pinned local has a dedicated register AND a frame slot; the
// live value is tracked in one of three states:
//
//	lsReg      — value only in the register (register is dirty vs the slot)
//	lsStackReg — value in BOTH register and slot (register is clean/mirrors memory)
//	lsMem      — value only in the slot (register was clobbered by a call)
//
// The point is to avoid spilling/reloading every pinned local around every call:
//   - at a call we store only DIRTY locals (a clean one is already in its slot),
//     then mark all as clobbered (lsMem) — and DON'T eagerly reload;
//   - a subsequent local.get reloads lazily (recoverLocal);
//   - branches converge everything to lsStackReg so all edges agree.
//
// Call-free functions never enter this path: their pinned locals live in
// registers for the whole function (no calls to clobber them), so localState is
// unused and no reconcile stores are emitted (keeps tight compute loops fast).

type locState uint8

const (
	lsReg      locState = iota // dirty: value only in the register
	lsStackReg                 // clean: value in both register and slot
	lsMem                      // spilled: value only in the slot
)

// pinReg returns local x's dedicated register (GP or XMM), whether it is a float
// register, and whether x is pinned at all.
func (f *fn) pinReg(x int) (reg Reg, isFloat, ok bool) {
	if r := f.localReg[x]; r != regNone {
		return r, false, true
	}
	if r := f.localFReg[x]; r != regNone {
		return r, true, true
	}
	return regNone, false, false
}

func (f *fn) storeLocalReg(x int, reg Reg, isFloat bool) {
	if isFloat {
		f.a.FStoreDisp(RBP, f.localOff(x), reg, f.localType[x] == mtF64)
	} else {
		f.a.Store64(RBP, f.localOff(x), reg)
	}
}

func (f *fn) loadLocalReg(x int, reg Reg, isFloat bool) {
	if isFloat {
		f.a.FLoadDisp(reg, RBP, f.localOff(x), f.localType[x] == mtF64)
	} else {
		f.a.Load64(reg, RBP, f.localOff(x))
	}
}

// recoverLocal ensures pinned local x's value is in its register before a read.
// Only acts in a call-making function when the value was spilled (lsMem).
func (f *fn) recoverLocal(x int) {
	if !f.usesCalls {
		return
	}
	reg, isFloat, ok := f.pinReg(x)
	if !ok {
		return
	}
	if f.localState[x] == lsMem {
		f.loadLocalReg(x, reg, isFloat)
		f.localState[x] = lsStackReg
	}
}

// markLocalDirty records that pinned local x was just written (value only in reg).
func (f *fn) markLocalDirty(x int) {
	if f.usesCalls {
		f.localState[x] = lsReg
	}
}

// spillLocalsForCall stores dirty pinned locals to their slots and marks all
// pinned locals clobbered (lsMem) — the WARP save-before-call step. No reload
// follows; the next read recovers lazily. Callers must emit this before a call.
func (f *fn) spillLocalsForCall() {
	for x := 0; x < f.nLocals; x++ {
		reg, isFloat, ok := f.pinReg(x)
		if !ok {
			continue
		}
		if f.localState[x] == lsReg { // dirty: write it back
			f.storeLocalReg(x, reg, isFloat)
		}
		f.localState[x] = lsMem // callee clobbers the register
	}
}

// reconcileLocals converges every pinned local to lsStackReg (value in both
// register and slot) at a control-flow boundary, so all edges into/out of it
// agree on where the value lives. No-op in call-free functions.
func (f *fn) reconcileLocals() {
	if !f.usesCalls {
		return
	}
	for x := 0; x < f.nLocals; x++ {
		reg, isFloat, ok := f.pinReg(x)
		if !ok {
			continue
		}
		switch f.localState[x] {
		case lsMem:
			f.loadLocalReg(x, reg, isFloat)
		case lsReg:
			f.storeLocalReg(x, reg, isFloat)
		}
		f.localState[x] = lsStackReg
	}
}

// resetLocalsToStackReg sets every pinned local's tracked state to lsStackReg
// WITHOUT emitting code — used where the incoming edge is known to already have
// reconciled the locals at runtime (an else body entered via the if's false edge,
// or a merge reached only by already-reconciled branch edges).
func (f *fn) resetLocalsToStackReg() {
	if !f.usesCalls {
		return
	}
	for x := 0; x < f.nLocals; x++ {
		if _, _, ok := f.pinReg(x); ok {
			f.localState[x] = lsStackReg
		}
	}
}
