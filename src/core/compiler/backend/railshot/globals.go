package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Globals. Each instance holds a globals slot-array pointer in basedata at
// [linMem - GlobalsPtrOffset]; entry x is an 8-byte pointer to global x's cell.
// i32 values occupy the low half of the 8-byte cell. (Float globals land with the
// SSE work in Phase 5.) Matches src/core/encoder/amd64's layout against this runtime.

// globalCellPtr returns a register holding &global[x]'s cell (the pointer at
// [[RBX-GlobalsPtrOffset] + x*8]), caching it across a straight-line run. Both the
// array base and each cell pointer are loop-invariant, so a hot global reloads
// neither after its first access: the value load/store then reads/writes [cellReg]
// directly. Single-entry — a different global evicts the previous one. The pinned
// register is allocated clear of the div/shift fixed-role registers RAX/RCX/RDX
// (clobbered without consulting the operand model). Invalidated at every flush, so
// it never spans a call or control-flow boundary; the pointer never changes, so
// re-deriving it after a boundary is always correct.
func (f *fn) globalCellPtr(x uint32) Reg {
	if f.globalCellReg != regNone && f.globalCellIdx == x {
		return f.globalCellReg
	}
	f.invalidateGlobalsCache() // evict a stale cell for a different global
	r := f.allocReg(maskOf(RAX, RCX, RDX))
	f.a.Load64(r, RBX, -int32(abi.GlobalsPtrOffset)) // r = slot array (base), used transiently
	f.a.Load64(r, r, int32(x*8))                     // r = &global[x]
	f.pinned = f.pinned.add(r)
	f.globalCellReg, f.globalCellIdx = r, x
	return r
}

// invalidateGlobalsCache drops the cached global cell pointer (unpins its
// register). Called from flush, so the cache never spans a call/control boundary.
func (f *fn) invalidateGlobalsCache() {
	if f.globalCellReg != regNone {
		f.pinned = f.pinned.remove(f.globalCellReg)
		f.globalCellReg = regNone
	}
}

// pinnedGlobalValueReg returns the register holding global x's live value, when x
// is value-pinned (a hot mutable int global in a call-free function). See
// assignPinnedLocals / loadPinnedGlobals / storePinnedGlobals.
func (f *fn) pinnedGlobalValueReg(x uint32) (Reg, bool) {
	if int(x) < len(f.globalReg) && f.globalReg[x] != regNone {
		return f.globalReg[x], true
	}
	return regNone, false
}

func (f *fn) globalGet(r *wasm.Reader) error {
	x, err := r.U32()
	if err != nil {
		return err
	}
	gt, ok := f.m.GlobalTypeByIndex(x)
	if !ok {
		return fmt.Errorf("amd64: unknown global %d", x)
	}
	gtv := wasm.GlobalValueType(gt)
	// Value-pinned (int) global: the current value already lives in a register.
	// Push a borrowed reference (WARP liftToRegInPlace) — no copy, no memory access
	// at all — kept sound by realize-on-set (realizeGlobalRefs) and by flush/
	// flushBelow materializing it before a call or control-flow boundary.
	if reg, ok := f.pinnedGlobalValueReg(x); ok {
		typ := mtI32
		if wasm.EqualValType(gtv, wasm.I64) {
			typ = mtI64
		}
		f.pushValue(storage{kind: stGlobReg, typ: typ, reg: reg, idx: int(x)})
		return nil
	}
	cell := f.globalCellPtr(x) // cached, pinned — read the value into a separate reg
	switch {
	case wasm.EqualValType(gtv, wasm.I64):
		dst := f.allocReg(0)
		f.a.Load64(dst, cell, 0)
		f.pushReg(dst, mtI64)
	case wasm.EqualValType(gtv, wasm.I32):
		dst := f.allocReg(0)
		f.a.Load32(dst, cell, 0) // low half of the 8-byte cell
		f.pushReg(dst, mtI32)
	case wasm.EqualValType(gtv, wasm.F32) || wasm.EqualValType(gtv, wasm.F64):
		f64 := wasm.EqualValType(gtv, wasm.F64)
		xmm := f.allocFReg(0)
		f.a.FLoadDisp(xmm, cell, 0, f64)
		f.pushFReg(xmm, mtOf2(f64))
	default:
		return fmt.Errorf("amd64: global.get type %s not yet supported (global %d)", gtv, x)
	}
	return nil
}

// realizeGlobalRefs forces any pending operand-stack references to value-pinned
// global x into registers before x's register is overwritten, mirroring
// realizeLocalRefs. skipFrom (non-nil) marks the base of the value-being-set's
// valent block for an in-place self-update (`global.set $x (binop (global.get
// $x) …)`): refs to x inside that block are consumed directly into x's register
// by condenseInto, so realizing them here would force a wasteful copy-out +
// copy-back. Refs BELOW it still need x's pre-set value and are realized.
func (f *fn) realizeGlobalRefs(x uint32, skipFrom *elem) {
	for e := f.s.head.next; e != f.s.head; {
		if e == skipFrom {
			break
		}
		next := e.next
		switch {
		case e.kind == ekValue && e.st.kind == stGlobReg && uint32(e.st.idx) == x:
			f.materialize(e)
		case e.kind == ekDeferred && subtreeRefsGlobal(e, x):
			f.condense(e, regNone)
		}
		e = next
	}
}

// subtreeRefsGlobal reports whether the valent block rooted at e reads
// value-pinned global x.
func subtreeRefsGlobal(e *elem, x uint32) bool {
	if e == nil {
		return false
	}
	if e.kind == ekValue {
		return e.st.kind == stGlobReg && uint32(e.st.idx) == x
	}
	if e.kind == ekDeferred {
		return subtreeRefsGlobal(e.arg0, x) || subtreeRefsGlobal(e.arg1, x)
	}
	return false
}

func (f *fn) globalSet(r *wasm.Reader) error {
	x, err := r.U32()
	if err != nil {
		return err
	}
	gt, ok := f.m.GlobalTypeByIndex(x)
	if !ok {
		return fmt.Errorf("amd64: unknown global %d", x)
	}
	gtv := wasm.GlobalValueType(gt)
	if wasm.EqualValType(gtv, wasm.F32) || wasm.EqualValType(gtv, wasm.F64) {
		f64 := wasm.EqualValType(gtv, wasm.F64)
		xmm := f.materializeF(f.popValue())
		f.fpinned = f.fpinned.add(xmm)
		cell := f.globalCellPtr(x) // cached, pinned
		f.a.FStoreDisp(cell, 0, xmm, f64)
		f.fpinned = f.fpinned.remove(xmm)
		f.releaseF(xmm)
		return nil
	}
	// Value-pinned (int) global: compute the new value straight into its register
	// (no memory write here — the coherent write-back to the cell happens once at the
	// function epilogue, since value-pinning is only used in call-free functions).
	if reg, ok := f.pinnedGlobalValueReg(x); ok {
		e := f.s.back()
		// In-place self-update `global.set $x (binop (global.get $x) …)`: let
		// condenseInto consume the top expression straight into x's register instead
		// of pre-copying its (global.get $x) operand (mirrors setLocal's skipFrom).
		var skipFrom *elem
		if e != nil && e.isDeferred() && isBinALU(e.op) {
			skipFrom = baseOfValentBlock(e)
		}
		f.realizeGlobalRefs(x, skipFrom)
		f.condenseInto(e, reg)
		f.release(reg)
		f.erase(e)
		f.globalDirty[x] = true
		return nil
	}
	rg := f.materialize(f.popValue())
	f.pinned = f.pinned.add(rg)
	cell := f.globalCellPtr(x) // cached, pinned (rg excluded)
	switch {
	case wasm.EqualValType(gtv, wasm.I64):
		f.a.Store64(cell, 0, rg)
	case wasm.EqualValType(gtv, wasm.I32):
		f.a.Store32(cell, 0, rg)
	default:
		f.pinned = f.pinned.remove(rg)
		f.release(rg)
		return fmt.Errorf("amd64: global.set type %s not yet supported (global %d)", gtv, x)
	}
	f.pinned = f.pinned.remove(rg)
	f.release(rg)
	return nil
}
