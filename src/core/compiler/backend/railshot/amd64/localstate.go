//go:build amd64

package amd64

// WARP's STACK_REG lazy local-spill model (Common.cpp saveLocalsAndParamsFor
// FuncCall / recoverLocalToReg / recoverAllLocalsToRegBranch), for CALL-MAKING
// functions. Each pinned local has a dedicated register AND a frame slot; the
// live value is tracked in one of four states:
//
//	lsConstZero — declared local's initial zero; neither register nor slot is live
//	lsReg       — value only in the register (register is dirty vs the slot)
//	lsStackReg — value in BOTH register and slot (register is clean/mirrors memory)
//	lsMem       — value only in the slot (register was clobbered by a call)
//
// The point is to avoid spilling/reloading every pinned local around every call:
//   - at a call we store only DIRTY locals (a clean one is already in its slot),
//     then mark all as clobbered (lsMem) — and DON'T eagerly reload;
//   - a subsequent local.get reloads lazily (recoverLocal);
//   - branches converge everything to lsStackReg so all edges agree.
//
// Call-free functions never enter this path: their pinned locals live in
// registers for the whole function (no calls to clobber them), so locals[].state is
// unused and no reconcile stores are emitted (keeps tight compute loops fast).

type locState uint8

const (
	lsReg       locState = iota // dirty: value only in the register; keep zero-value for old eager path
	lsStackReg                  // clean: value in both register and slot
	lsMem                       // spilled: value only in the slot
	lsConstZero                 // declared local's initial zero, not materialized yet
)

type localDef struct {
	typ     machineType
	reg     Reg
	isFloat bool
	state   locState
}

// pinReg returns local x's dedicated register (GP or XMM), whether it is a float
// register, and whether x is pinned at all.
func (f *fn) pinReg(x int) (reg Reg, isFloat, ok bool) {
	if x < 0 || x >= len(f.locals) {
		return regNone, false, false
	}
	d := f.locals[x]
	if d.reg == regNone {
		return regNone, false, false
	}
	return d.reg, d.isFloat, true
}

func zeroStorage(typ machineType) storage {
	return storage{kind: stConst, typ: typ, cval: 0}
}

func (f *fn) localConstZero(x int) bool {
	return x >= 0 && x < len(f.locals) && f.locals[x].state == lsConstZero
}

func (f *fn) markDeclaredLocalZero(x int) {
	f.locals[x].state = lsConstZero
}

func (f *fn) storeLocalReg(x int, reg Reg, isFloat bool) {
	if f.localType[x] == mtV128 {
		f.a.VMovdquStoreDisp(RSP, f.localOff(x), reg)
	} else if isFloat {
		f.a.FStoreDisp(RSP, f.localOff(x), reg, f.localType[x] == mtF64)
	} else {
		f.a.Store64(RSP, f.localOff(x), reg)
	}
}

func (f *fn) loadLocalReg(x int, reg Reg, isFloat bool) {
	if f.localType[x] == mtV128 {
		f.a.VMovdquLoadDisp(reg, RSP, f.localOff(x))
	} else if isFloat {
		f.a.FLoadDisp(reg, RSP, f.localOff(x), f.localType[x] == mtF64)
	} else {
		f.a.Load64(reg, RSP, f.localOff(x))
	}
}

func (f *fn) materializeZeroLocal(x int, needSlot bool) {
	reg, isFloat, ok := f.pinReg(x)
	if ok {
		if isFloat {
			f.a.SseRR(0x66, 0x57, reg, reg, false) // xorpd reg,reg -> +0.0
		} else {
			f.a.XorSelf32(reg)
		}
		if needSlot {
			f.storeLocalReg(x, reg, isFloat)
			f.locals[x].state = lsStackReg
		} else {
			f.locals[x].state = lsReg
		}
		return
	}
	if needSlot {
		r := f.allocReg(0)
		f.a.XorSelf32(r)
		f.a.Store64(RSP, f.localOff(x), r)
		f.release(r)
		f.locals[x].state = lsMem
	}
}

// recoverLocal ensures pinned local x's value is in its register before a read.
// It materializes lazy declared-zero locals even in call-free functions.
func (f *fn) recoverLocal(x int) {
	reg, isFloat, ok := f.pinReg(x)
	if !ok {
		return
	}
	if f.locals[x].state == lsConstZero {
		f.materializeZeroLocal(x, false)
		return
	}
	if !f.usesCalls {
		return
	}
	if f.locals[x].state == lsMem {
		f.loadLocalReg(x, reg, isFloat)
		f.locals[x].state = lsStackReg
	}
}

// markLocalDirty records that pinned local x was just written (value only in reg).
func (f *fn) markLocalDirty(x int) {
	if f.usesCalls || f.lazyZero {
		f.locals[x].state = lsReg
	}
}

// spillLocalsForCall stores dirty pinned locals to their slots and marks all
// pinned locals clobbered (lsMem) — the WARP save-before-call step. No reload
// follows; the next read recovers lazily. Callers must emit this before a call.
func (f *fn) spillLocalsForCall() {
	for _, x := range f.pinnedLocals {
		reg, isFloat := f.locals[x].reg, f.locals[x].isFloat
		if !f.usesCalls {
			f.storeLocalReg(x, reg, isFloat) // old model: store all; reloaded after the call
			continue
		}
		if f.locals[x].state == lsConstZero {
			continue // a clobbered register does not change the wasm local's zero value
		}
		if f.locals[x].state == lsReg { // dirty: write it back
			f.storeLocalReg(x, reg, isFloat)
		}
		f.locals[x].state = lsMem // callee clobbers the register
	}
}

// reloadLocalsForCall restores every pinned local after a call — only for the
// non-STACK_REG model (usesCalls false); STACK_REG reloads lazily on read.
func (f *fn) reloadLocalsForCall() {
	if f.usesCalls {
		return
	}
	for _, x := range f.pinnedLocals {
		f.loadLocalReg(x, f.locals[x].reg, f.locals[x].isFloat)
	}
}

// reconcileLocals converges local state at a control-flow boundary. Lazy zero
// locals are materialized before paths diverge so unpinned locals have a real
// slot value on every edge. In call-making functions, pinned locals are also
// converged to lsStackReg so branches and fall-through agree on storage.
// Used where an eager full converge is the right call: loop entries (hoisting
// post-call reloads out of the body) and br_table (one state satisfying every
// target). Other edges use convergeEdgeTo's lazier per-frame agreement.
func (f *fn) reconcileLocals() {
	for x := 0; x < f.nLocals; x++ {
		if f.locals[x].state == lsConstZero {
			f.materializeZeroLocal(x, true) // leaves pinned locals in lsStackReg
		}
	}
	if !f.usesCalls {
		return
	}
	for _, x := range f.pinnedLocals {
		switch f.locals[x].state {
		case lsMem:
			f.loadLocalReg(x, f.locals[x].reg, f.locals[x].isFloat)
		case lsReg:
			f.storeLocalReg(x, f.locals[x].reg, f.locals[x].isFloat)
		}
		f.locals[x].state = lsStackReg
	}
}

// convergeEdgeTo converges pinned-local state for a control edge into the
// per-frame target *target, RECORDING the target from the current state when
// this is the frame's first edge. Targets are per-local, ∈ {lsStackReg, lsMem}:
//   - lsStackReg: register AND slot valid at the merge;
//   - lsMem: only the slot is guaranteed — a call-clobbered local stays
//     unloaded across the merge until a read actually needs it (the lazy-merge
//     win: post-call branch-dense code stops reloading every pinned local at
//     every boundary).
//
// An edge may arrive STRONGER than the target (lsStackReg where lsMem is
// recorded) — always safe: the merge assumes only the target. The merge point
// itself must then install the recorded target as the tracked state
// (setLocalsState).
// newLocStateBuf returns a length-nLocals []locState for a frame merge target,
// recycled from lsPool when available. Callers overwrite every element (both
// convergeEdgeTo callers do), so the buffer is not zeroed. freeLocStateBuf
// returns one to the pool when its owning frame is popped.
func (f *fn) newLocStateBuf() []locState {
	if n := len(f.lsPool); n > 0 {
		b := f.lsPool[n-1]
		f.lsPool[n-1] = nil
		f.lsPool = f.lsPool[:n-1]
		return b[:f.nLocals]
	}
	return make([]locState, f.nLocals)
}

func (f *fn) freeLocStateBuf(b []locState) {
	if cap(b) >= f.nLocals && f.nLocals > 0 {
		f.lsPool = append(f.lsPool, b[:cap(b)])
	}
}

// frameAddEnd appends a forward-jump site to a frame's end-patch list, drawing
// the backing slice from endsPool on first use. Frames are LIFO, so returning the
// slice on pop (freeEndsBuf) bounds live buffers by nesting depth rather than
// total frame count.
func (f *fn) frameAddEnd(fr *ctrlFrame, site int) {
	if fr.ends == nil {
		if n := len(f.endsPool); n > 0 {
			fr.ends = f.endsPool[n-1][:0]
			f.endsPool[n-1] = nil
			f.endsPool = f.endsPool[:n-1]
		}
	}
	fr.ends = append(fr.ends, site)
}

func (f *fn) freeEndsBuf(b []int) {
	if cap(b) > 0 {
		f.endsPool = append(f.endsPool, b[:0])
	}
}

func (f *fn) convergeEdgeTo(target *[]locState) {
	// Lazy zeros always materialize to the slot so unpinned declared-zero locals
	// have a real slot value on every edge (all locals — const-zero ones may be
	// unpinned). materializeZeroLocal leaves a pinned local in lsStackReg, so the
	// pinned-spill pass below correctly skips it.
	for x := 0; x < f.nLocals; x++ {
		if f.locals[x].state == lsConstZero {
			f.materializeZeroLocal(x, true)
		}
	}
	if !f.usesCalls {
		return
	}
	// Dirty pinned registers materialize to the slot too.
	for _, x := range f.pinnedLocals {
		if f.locals[x].state == lsReg {
			f.storeLocalReg(x, f.locals[x].reg, f.locals[x].isFloat)
			f.locals[x].state = lsStackReg
		}
	}
	if *target == nil { // first edge fixes the frame's merge state
		t := f.newLocStateBuf()
		for x := range t {
			t[x] = f.locals[x].state
		}
		*target = t
		return
	}
	t := *target
	for _, x := range f.pinnedLocals {
		if t[x] == lsStackReg && f.locals[x].state == lsMem {
			f.loadLocalReg(x, f.locals[x].reg, f.locals[x].isFloat)
			f.locals[x].state = lsStackReg
		}
	}
}

// setLocalsState installs a merge point's recorded target as the tracked state
// (no code): every reaching edge guaranteed at least this much.
func (f *fn) setLocalsState(t []locState) {
	if !f.usesCalls || t == nil {
		return
	}
	for _, x := range f.pinnedLocals {
		f.locals[x].state = t[x]
	}
}
