//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func (f *fn) tryIntegerHashStoreLoop(r *wasm.Reader) (bool, error) {
	if !f.opt(optBulkFillVector) || f.unreachable || f.interruptible || f.localBase != 0 || f.moduleEH || f.gcFrameRoots != nil || f.usesCalls || f.makesCalls || len(f.customInstructions) != 0 {
		return false, nil
	}
	p, ok := shared.InspectIntegerHashStoreLoop(r, f.m)
	if !ok {
		return false, nil
	}
	for _, x := range []uint32{p.Destination, p.Seed, p.Temporary} {
		if uint64(x) >= uint64(len(f.localType)) || f.localType[x] != mtI32 {
			return false, nil
		}
	}
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	available := 0
	for _, reg := range gpAlloc {
		if !blocked.has(reg) {
			available++
		}
	}
	const calleeFP regMask = 0xff00
	vb := f.blockedFRegs(calleeFP)
	vectors := 0
	for _, reg := range fpAllocRegs {
		if !vb.has(reg) {
			vectors++
		}
	}
	if available < 11 || vectors < 7 {
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
	savedPins := f.pinned
	dst, seed := copyRead(p.Destination), copyRead(p.Seed)
	count, tmp, limit, cond := alloc(), alloc(), alloc(), alloc()
	var bad [4]int
	badN := 0
	fail := func(site int) { bad[badN] = site; badN++ }
	f.a.MovImm32(count, int32(p.Bound))
	f.a.CmpReg32(seed, count)
	fail(f.a.Bcond(a64.CondCS))
	f.a.Sub32(count, count, seed)
	f.a.MovImm32(tmp, 1)
	f.a.And32(tmp, count, tmp)
	fail(f.a.Cbnz32(tmp))
	// The span is wide: a finite seed suffix may still exceed memory32.
	f.a.LslImm(tmp, count, 2, false)
	f.a.Add64(tmp, tmp, dst)
	f.a.MovImm64(limit, 1<<32)
	f.a.CmpReg64(tmp, limit)
	fail(f.a.Bcond(a64.CondHI))
	f.a.MovImm64(limit, uint64(p.Offset))
	f.a.Add64(tmp, tmp, limit)
	f.ld64(limit, linMemReg, -int32(bdCurBytes))
	f.a.CmpReg64(tmp, limit)
	fail(f.a.Bcond(a64.CondHI))
	f.a.MovImm32(cond, 1)
	skip := f.a.Branch()
	for _, site := range bad[:badN] {
		f.a.PatchBranch19(site, f.a.Len())
	}
	f.a.MovImm32(cond, 0)
	f.a.PatchBranch26(skip, f.a.Len())
	for _, reg := range []Reg{dst, seed, count, tmp, limit} {
		f.release(reg)
	}
	f.pinned = savedPins
	f.pushReg(cond, mtI32)
	if err := f.emitCheckedLoop(r, p.End, func() error {
		savedPins, savedFPins := f.pinned, f.fpinned
		dst, seed := copyRead(p.Destination), copyRead(p.Seed)
		n, tmp, constant, finalDst := alloc(), alloc(), alloc(), alloc()
		f.a.MovImm32(n, int32(p.Bound))
		f.a.Sub32(n, n, seed)
		f.a.LslImm(finalDst, n, 2, true)
		f.a.Add32(finalDst, finalDst, dst)
		f.a.MovImm64(tmp, uint64(p.Offset))
		f.a.Add64(dst, dst, tmp)
		f.a.Add64(dst, dst, linMemReg)
		var v [7]Reg
		for i := range v {
			v[i] = f.allocFReg(calleeFP)
			f.fpinned = f.fpinned.add(v[i])
		}
		seeds, xor, mul, mask, work, shift, stride := v[0], v[1], v[2], v[3], v[4], v[5], v[6]
		f.a.MovReg32(tmp, seed)
		for lane := byte(0); lane < 4; lane++ {
			f.a.NeonInsS(seeds, tmp, lane)
			f.a.AddImm32(tmp, tmp, 1)
		}
		f.a.MovImm32(tmp, int32(p.Xor))
		f.a.NeonDupGprS(xor, tmp)
		f.a.MovImm32(tmp, int32(p.Multiplier))
		f.a.NeonDupGprS(mul, tmp)
		f.a.MovImm32(tmp, int32(p.Mask))
		f.a.NeonDupGprS(mask, tmp)
		f.a.MovImm32(tmp, 4)
		f.a.NeonDupGprS(stride, tmp)
		f.a.CmpImm32(n, 4)
		small := f.a.Bcond(a64.CondCC)
		loop := f.a.Len()
		f.a.NeonEor16b(work, seeds, xor)
		f.a.NeonMulS(work, work, mul)
		if p.Shift == 0 {
			f.a.NeonEor16b(work, work, work)
		} else {
			f.a.NeonUshrS(shift, work, p.Shift)
			f.a.NeonEor16b(work, work, shift)
		}
		f.a.NeonAnd16b(work, work, mask)
		f.a.StrQ(dst, 0, work)
		f.a.NeonAddS(seeds, seeds, stride)
		f.a.AddImm64(dst, dst, 16)
		f.a.SubImm32(n, n, 4)
		f.a.CmpImm32(n, 4)
		again := f.a.Bcond(a64.CondCS)
		f.a.PatchBranch19(again, loop)
		f.a.PatchBranch19(small, f.a.Len())
		noTail := f.a.Cbz32(n)
		f.a.MovImm32(seed, int32(p.Bound))
		f.a.Sub32(seed, seed, n)
		// A final pair uses exactly two word stores, never a vector over-write.
		for i := 0; i < 2; i++ {
			f.a.MovImm32(constant, int32(p.Xor))
			f.a.Eor32(tmp, seed, constant)
			f.a.MovImm32(constant, int32(p.Multiplier))
			f.a.Mul32(tmp, tmp, constant)
			f.a.LsrImm(constant, tmp, p.Shift, true)
			f.a.Eor32(constant, constant, tmp)
			f.a.MovImm32(n, int32(p.Mask))
			f.a.And32(constant, constant, n)
			f.st32(dst, 0, constant)
			f.a.AddImm64(dst, dst, 4)
			f.a.AddImm32(seed, seed, 1)
		}
		f.a.PatchBranch19(noTail, f.a.Len())
		// The last tee stores the multiplied intermediate for Bound-1.
		f.a.MovImm32(tmp, int32(p.Bound-1))
		f.a.MovImm32(constant, int32(p.Xor))
		f.a.Eor32(tmp, tmp, constant)
		f.a.MovImm32(constant, int32(p.Multiplier))
		f.a.Mul32(tmp, tmp, constant)
		f.pushReg(tmp, mtI32)
		f.setLocal(nil, int(p.Temporary), false)
		f.pushReg(finalDst, mtI32)
		f.setLocal(nil, int(p.Destination), false)
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(int32(p.Bound))})
		f.setLocal(nil, int(p.Seed), false)
		for _, reg := range []Reg{dst, seed, n, tmp, constant, finalDst} {
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
	f.stats.peep("integer-hash-store-vector-loop")
	return true, nil
}
