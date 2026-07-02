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

// globalsBase returns a register holding the globals slot-array pointer
// ([RBX-GlobalsPtrOffset]), caching it across a straight-line run so repeated
// global accesses don't each reload this loop-invariant base. The register is
// pinned while cached (so nothing reallocates it) and allocated clear of the
// div/shift fixed-role registers RAX/RCX/RDX (which those ops clobber without
// consulting the operand model). Invalidated at every flush — see
// invalidateGlobalsBase. The pointer's value never changes, so re-deriving it
// after a boundary is always correct.
func (f *fn) globalsBase() Reg {
	if f.globalsBaseReg != regNone {
		return f.globalsBaseReg
	}
	r := f.allocReg(maskOf(RAX, RCX, RDX))
	f.a.Load64(r, RBX, -int32(abi.GlobalsPtrOffset))
	f.pinned = f.pinned.add(r)
	f.globalsBaseReg = r
	return r
}

// invalidateGlobalsBase drops the cached globals base (unpins its register).
// Called from flush, so the cache never spans a call or control-flow boundary.
func (f *fn) invalidateGlobalsBase() {
	if f.globalsBaseReg != regNone {
		f.pinned = f.pinned.remove(f.globalsBaseReg)
		f.globalsBaseReg = regNone
	}
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
	base := f.globalsBase()            // cached, pinned — must not be clobbered below
	cell := f.allocReg(0)              // fresh (base excluded via its pin)
	f.a.Load64(cell, base, int32(x*8)) // cell = &global[x]
	switch {
	case wasm.EqualValType(gtv, wasm.I64):
		f.a.Load64(cell, cell, 0)
		f.pushReg(cell, mtI64)
	case wasm.EqualValType(gtv, wasm.I32):
		f.a.Load32(cell, cell, 0) // low half of the 8-byte cell
		f.pushReg(cell, mtI32)
	case wasm.EqualValType(gtv, wasm.F32) || wasm.EqualValType(gtv, wasm.F64):
		f64 := wasm.EqualValType(gtv, wasm.F64)
		xmm := f.allocFReg(0)
		f.a.FLoadDisp(xmm, cell, 0, f64)
		f.release(cell)
		f.pushFReg(xmm, mtOf2(f64))
	default:
		f.release(cell)
		return fmt.Errorf("amd64: global.get type %s not yet supported (global %d)", gtv, x)
	}
	return nil
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
		base := f.globalsBase()
		cell := f.allocReg(0) // fresh (base excluded via its pin)
		f.a.Load64(cell, base, int32(x*8))
		f.a.FStoreDisp(cell, 0, xmm, f64)
		f.release(cell)
		f.fpinned = f.fpinned.remove(xmm)
		f.releaseF(xmm)
		return nil
	}
	rg := f.materialize(f.popValue())
	f.pinned = f.pinned.add(rg)
	base := f.globalsBase()
	cell := f.allocReg(maskOf(rg)) // fresh (base + rg excluded)
	f.a.Load64(cell, base, int32(x*8))
	switch {
	case wasm.EqualValType(gtv, wasm.I64):
		f.a.Store64(cell, 0, rg)
	case wasm.EqualValType(gtv, wasm.I32):
		f.a.Store32(cell, 0, rg)
	default:
		f.pinned = f.pinned.remove(rg)
		f.release(rg)
		f.release(cell)
		return fmt.Errorf("amd64: global.set type %s not yet supported (global %d)", gtv, x)
	}
	f.pinned = f.pinned.remove(rg)
	f.release(rg)
	f.release(cell)
	return nil
}
