//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func (f *fn) tryIndexMixStoreLoop(r *wasm.Reader) (bool, error) {
	if !f.opt(optBulkFillVector) || f.depth() != 0 || f.unreachable || f.interruptible || f.localBase != 0 || f.moduleEH || f.gcFrameRoots != nil || f.usesCalls || f.makesCalls || len(f.customInstructions) != 0 {
		return false, nil
	}
	p, ok := shared.InspectIndexMixStoreLoop(r, f.m)
	if !ok {
		return false, nil
	}
	check := func(x uint32) bool { return uint64(x) < uint64(len(f.localType)) && f.localType[x] == mtI32 }
	if !check(p.Destination) || !check(p.Seed) || !check(p.Temporary) || !p.BoundConstant && !check(p.Bound) {
		return false, nil
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
	if gp < 4 || fp < 6 {
		return false, nil
	}
	f.flush()
	f.invalidateStoreForward()
	f.reconcileLocals()
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
	bound := func() {
		if p.BoundConstant {
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(int32(p.Bound))})
		} else {
			get(p.Bound)
		}
	}
	alloc := func() Reg { reg := f.allocReg(0); f.pinned = f.pinned.add(reg); return reg }
	copyValue := func() Reg {
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
	readCount := func(n Reg) {
		get(p.Seed)
		src, owned := f.materializeRead(f.popValue())
		f.a.Sub32(n, n, src)
		if owned {
			f.release(src)
		}
	}
	savedPins := f.pinned
	get(p.Destination)
	dst := copyValue()
	bound()
	n := copyValue()
	readCount(n)
	tmp, cond := alloc(), alloc()
	var bad [4]int
	bad[0] = f.a.Cbz32(n)
	f.a.MovImm32(tmp, 1)
	f.a.And32(tmp, n, tmp)
	bad[1] = f.a.Cbnz32(tmp)
	f.a.LslImm64(tmp, n, 2)
	f.a.Add64(tmp, tmp, dst)
	f.a.MovImm64(cond, 1<<32)
	f.a.CmpReg64(tmp, cond)
	bad[2] = f.a.Bcond(a64.CondHI)
	f.a.MovImm64(cond, uint64(p.Offset))
	f.a.Add64(tmp, tmp, cond)
	f.ld64(cond, linMemReg, -int32(bdCurBytes))
	f.a.CmpReg64(tmp, cond)
	bad[3] = f.a.Bcond(a64.CondHI)
	f.a.MovImm32(cond, 1)
	skip := f.a.Branch()
	for _, site := range bad {
		f.a.PatchBranch19(site, f.a.Len())
	}
	f.a.MovImm32(cond, 0)
	f.a.PatchBranch26(skip, f.a.Len())
	for _, reg := range []Reg{dst, n, tmp} {
		f.release(reg)
	}
	f.pinned = savedPins
	f.pushReg(cond, mtI32)
	if err := f.emitCheckedLoop(r, p.End, func() error {
		savedPins, savedFPins := f.pinned, f.fpinned
		get(p.Destination)
		dst := copyValue()
		bound()
		n := copyValue()
		readCount(n)
		tmp := alloc()
		var v [6]Reg
		for i := range v {
			v[i] = f.allocFReg(calleeFP)
			f.fpinned = f.fpinned.add(v[i])
		}
		seeds, mul, mask, shift, work, stride := v[0], v[1], v[2], v[3], v[4], v[5]
		get(p.Seed)
		src, owned := f.materializeRead(f.popValue())
		f.a.MovReg32(tmp, src)
		if owned {
			f.release(src)
		}
		constant := alloc()
		f.a.MovImm32(constant, int32(p.Add))
		f.a.Add32(tmp, tmp, constant)
		f.a.NeonDupGprS(seeds, tmp)
		for lane := byte(1); lane < 4; lane++ {
			f.a.AddImm32(tmp, tmp, 1)
			f.a.NeonInsS(seeds, tmp, lane)
		}
		f.pinned = f.pinned.remove(constant)
		f.release(constant)
		// This arm cannot trap, call out or be interrupted. Commit outputs
		// after capturing their inputs to keep the owned GP budget at four.
		f.a.LslImm(tmp, n, 2, true)
		f.a.Add32(tmp, tmp, dst)
		f.pushReg(tmp, mtI32)
		f.setLocal(nil, int(p.Destination), false)
		bound()
		f.setLocal(nil, int(p.Seed), false)
		f.a.MovImm32(tmp, int32(p.Multiplier))
		f.a.NeonDupGprS(mul, tmp)
		f.a.MovImm32(tmp, int32(p.Mask))
		f.a.NeonDupGprS(mask, tmp)
		f.a.MovImm32(tmp, 4)
		f.a.NeonDupGprS(stride, tmp)
		f.a.MovImm64(tmp, uint64(p.Offset))
		f.a.Add64(dst, dst, tmp)
		f.a.Add64(dst, dst, linMemReg)
		mix := func() {
			if p.FirstShift == 0 {
				f.a.NeonEor16b(work, seeds, seeds)
			} else {
				f.a.NeonUshrS(shift, seeds, p.FirstShift)
				f.a.NeonEor16b(work, seeds, shift)
			}
			f.a.NeonMulS(work, work, mul)
			if p.LastShift == 0 {
				f.a.NeonEor16b(work, work, work)
			} else {
				f.a.NeonUshrS(shift, work, p.LastShift)
				f.a.NeonEor16b(work, work, shift)
			}
			f.a.NeonAnd16b(work, work, mask)
		}
		f.a.CmpImm32(n, 4)
		small := f.a.Bcond(a64.CondCC)
		loop := f.a.Len()
		mix()
		f.a.StrQ(dst, 0, work)
		f.a.NeonAddS(seeds, seeds, stride)
		f.a.AddImm64(dst, dst, 16)
		f.a.SubImm32(n, n, 4)
		f.a.CmpImm32(n, 4)
		f.a.PatchBranch19(f.a.Bcond(a64.CondCS), loop)
		f.a.PatchBranch19(small, f.a.Len())
		noTail := f.a.Cbz32(n)
		mix()
		// The final pair writes exactly eight bytes. Keep its high-first order.
		f.a.NeonUmovS(n, work, 1)
		f.st32(dst, 4, n)
		f.a.NeonUmovS(n, work, 0)
		f.st32(dst, 0, n)
		f.a.PatchBranch19(noTail, f.a.Len())
		// The last scalar tee is the product for the low word, Bound-2.
		get(p.Seed)
		src, owned = f.materializeRead(f.popValue())
		f.a.MovReg32(tmp, src)
		if owned {
			f.release(src)
		}
		f.a.MovImm32(n, int32(p.Add-2))
		f.a.Add32(tmp, tmp, n)
		f.a.LsrImm(n, tmp, p.FirstShift, true)
		f.a.Eor32(tmp, tmp, n)
		f.a.MovImm32(n, int32(p.Multiplier))
		f.a.Mul32(tmp, tmp, n)
		f.pushReg(tmp, mtI32)
		f.setLocal(nil, int(p.Temporary), false)
		for _, reg := range []Reg{dst, n, tmp} {
			f.release(reg)
		}
		for _, reg := range v {
			f.releaseF(reg)
		}
		f.pinned, f.fpinned = savedPins, savedFPins
		for _, x := range []uint32{p.Destination, p.Seed, p.Temporary} {
			if x < 64 {
				f.localWritten |= uint64(1) << x
			}
		}
		return nil
	}); err != nil {
		return false, err
	}
	f.stats.peep("index-mix-store-vector-loop")
	return true, nil
}
