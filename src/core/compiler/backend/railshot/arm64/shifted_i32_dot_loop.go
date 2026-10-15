//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func (f *fn) tryShiftedI32DotLoop(r *wasm.Reader) (bool, error) {
	if !f.opt(optIntegerSumLoop) || f.unreachable || f.interruptible || f.localBase != 0 || f.moduleEH || f.gcFrameRoots != nil || f.usesCalls || f.makesCalls || len(f.customInstructions) != 0 {
		return false, nil
	}
	p, ok := shared.InspectShiftedI32DotLoop(r, f.m)
	if !ok {
		return false, nil
	}
	for _, x := range []uint32{p.Pointer, p.Shift, p.Limit, p.Counter, p.Accumulator, p.RightAddress} {
		if uint64(x) >= uint64(len(f.localType)) || f.localType[x] != mtI32 {
			return false, nil
		}
	}
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	gp := 0
	for _, reg := range gpAlloc {
		if !blocked.has(reg) {
			gp++
		}
	}
	const calleeFP regMask = 0xff00
	vb := f.blockedFRegs(calleeFP)
	fp := 0
	for _, reg := range fpAllocRegs {
		if !vb.has(reg) {
			fp++
		}
	}
	// Seven fast-path GP values, a borrowed/materialized source, and headroom
	// for local convergence. Reject before changing source, pins or emission.
	if fp < 8 {
		return false, nil
	}
	// Literal caches are optional; preserve the vector path's full GP budget.
	borrowConstants := gp < 9
	if !f.canBorrowVectorConstants(gp, 9) {
		return false, nil
	}
	f.flush()
	f.invalidateStoreForward()
	f.reconcileLocals()
	var constants vectorConstantCache
	if borrowConstants {
		constants = f.borrowVectorConstants()
	}
	get := func(x uint32) {
		if reg, _, ok := f.pinReg(int(x)); ok {
			f.recoverLocal(int(x))
			f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: reg, idx: x})
		} else if f.localConstZero(int(x)) {
			f.pushValue(zeroStorage(mtI32))
		} else {
			f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: x})
		}
	}
	savedPins := f.pinned
	alloc := func() Reg { reg := f.allocReg(0); f.pinned = f.pinned.add(reg); return reg }
	copyRead := func(x uint32) Reg {
		get(x)
		src, owned := f.materializeRead(f.popValue())
		f.pinned = f.pinned.add(src)
		out := alloc()
		f.a.MovReg32(out, src)
		if owned {
			f.pinned = f.pinned.remove(src)
			f.release(src)
		}
		return out
	}
	left, right, n, limit := copyRead(p.Pointer), copyRead(p.Shift), copyRead(p.Counter), copyRead(p.Limit)
	tmp, size, cond := alloc(), alloc(), alloc()
	var bad [7]int
	count := 0
	fail := func(site int) { bad[count] = site; count++ }
	// Modular counter difference is exact: a nonzero even difference reaches
	// the limit after delta/2 decrements, including a wrapping counter suffix.
	f.a.Sub32(n, n, limit)
	fail(f.a.Cbz32(n))
	f.a.TstImm32(n, 1)
	fail(f.a.Bcond(a64.CondNE))
	f.a.LslImm64(n, n, 2)
	f.a.CmpImm32(left, 4)
	fail(f.a.Bcond(a64.CondCC))
	// Initial right-address wrapping is part of the source semantics.
	f.a.Add32(right, left, right)
	for i, base := range []Reg{left, right} {
		f.a.Add64(tmp, base, n)
		f.a.MovImm64(size, 1<<32)
		f.a.CmpReg64(tmp, size)
		fail(f.a.Bcond(a64.CondHI))
		if i == 0 {
			f.a.SubImm64(tmp, tmp, 4)
		}
		f.a.MovImm64(size, uint64(p.Offset))
		f.a.Add64(tmp, tmp, size)
		f.ld64(size, linMemReg, -int32(bdCurBytes))
		f.a.CmpReg64(tmp, size)
		fail(f.a.Bcond(a64.CondHI))
	}
	f.a.MovImm32(cond, 1)
	skip := f.a.Branch()
	for _, site := range bad[:count] {
		f.patchBranch19(site, f.a.Len())
	}
	f.a.MovImm32(cond, 0)
	f.patchBranch26(skip, f.a.Len())
	for _, reg := range []Reg{left, right, n, limit, tmp, size} {
		f.release(reg)
	}
	f.pinned = savedPins
	f.pushReg(cond, mtI32)
	err := f.emitCheckedLoop(r, p.End, func() error {
		savedPins, savedFPins := f.pinned, f.fpinned
		left, right, n, limit = copyRead(p.Pointer), copyRead(p.Shift), copyRead(p.Counter), copyRead(p.Limit)
		sum, tmp, product := alloc(), alloc(), alloc()
		f.a.Sub32(n, n, limit)
		f.a.LslImm64(n, n, 2)
		f.a.Add32(right, left, right)
		// The checked path has no calls, interrupts or possible traps. Commit
		// final locals now, while the owned pointer/counter copies still hold
		// the original inputs. This avoids keeping three final values live.
		f.a.Add64(tmp, left, n)
		f.a.MovReg32(tmp, tmp)
		f.pushReg(tmp, mtI32)
		f.setLocal(nil, int(p.Pointer), false)
		f.a.Add64(tmp, right, n)
		f.a.SubImm64(tmp, tmp, 8)
		f.a.MovReg32(tmp, tmp)
		f.pushReg(tmp, mtI32)
		f.setLocal(nil, int(p.RightAddress), false)
		f.pushReg(limit, mtI32)
		f.setLocal(nil, int(p.Counter), false)
		f.a.SubImm32(left, left, 4)
		f.a.MovImm64(tmp, uint64(p.Offset))
		f.a.Add64(left, left, tmp)
		f.a.Add64(right, right, tmp)
		f.a.Add64(left, left, linMemReg)
		f.a.Add64(right, right, linMemReg)
		v := f.emitI32DotVector(left, right, n, sum, tmp, product, p.LeftBias, p.RightBias)
		get(p.Accumulator)
		initial, owned := f.materializeRead(f.popValue())
		f.a.Add32(sum, sum, initial)
		if owned {
			f.release(initial)
		}
		f.pushReg(sum, mtI32)
		f.setLocal(nil, int(p.Accumulator), false)
		for _, reg := range []Reg{left, right, n, limit, sum, tmp, product} {
			f.release(reg)
		}
		for _, reg := range v {
			f.releaseF(reg)
		}
		f.pinned, f.fpinned = savedPins, savedFPins
		for _, x := range []uint32{p.Pointer, p.Counter, p.Accumulator, p.RightAddress} {
			if x < 64 {
				f.localWritten |= uint64(1) << x
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	f.restoreVectorConstants(constants)
	f.stats.peep("shifted-i32-dot-vector-loop")
	return true, nil
}
