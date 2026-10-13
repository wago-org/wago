//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"math/bits"
)

func (f *fn) tryWeightedI32WindowLoop(r *wasm.Reader) (bool, error) {
	if !f.opt(optIntegerSumLoop) || f.depth() != 0 || f.unreachable || f.interruptible || f.localBase != 0 || f.moduleEH || f.gcFrameRoots != nil || f.usesCalls || f.makesCalls || len(f.customInstructions) != 0 {
		return false, nil
	}
	p, ok := shared.InspectWeightedI32WindowLoop(r, f.m)
	if !ok {
		return false, nil
	}
	check := func(x uint32) bool { return uint64(x) < uint64(len(f.localType)) && f.localType[x] == mtI32 }
	for _, x := range []uint32{p.Pointer, p.NextPointer, p.Counter, p.Hash} {
		if !check(x) {
			return false, nil
		}
	}
	for _, x := range p.Coefficients[:p.Terms] {
		if !check(x) {
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
	// Four owned GP values at most. Coefficients are packed before the hash
	// and sum temporaries are allocated; the runtime count resides in a V register.
	if gp < 4 || fp < p.Terms/4+7 {
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
	ptr, n := copyRead(p.Pointer), copyRead(p.Counter)
	tmp, cond := alloc(), alloc()
	var bad [3]int
	bad[0] = f.a.Cbz32(n)
	// Count zero would execute a full modular counter cycle. Positive counts
	// need exactly count-1 advances plus the complete final sample window.
	f.a.SubImm64(tmp, n, 1)
	f.a.LslImm64(tmp, tmp, 2)
	f.a.Add64(tmp, tmp, ptr)
	f.a.AddImm64(tmp, tmp, uint32(p.Terms*4))
	f.a.MovImm64(cond, 1<<32)
	f.a.CmpReg64(tmp, cond)
	bad[1] = f.a.Bcond(a64.CondHI)
	f.a.MovImm64(cond, uint64(p.Offset))
	f.a.Add64(tmp, tmp, cond)
	f.ld64(cond, linMemReg, -int32(bdCurBytes))
	f.a.CmpReg64(tmp, cond)
	bad[2] = f.a.Bcond(a64.CondHI)
	f.a.MovImm32(cond, 1)
	skip := f.a.Branch()
	for _, site := range bad {
		f.a.PatchBranch19(site, f.a.Len())
	}
	f.a.MovImm32(cond, 0)
	f.a.PatchBranch26(skip, f.a.Len())
	for _, reg := range []Reg{ptr, n, tmp} {
		f.release(reg)
	}
	f.pinned = savedPins
	f.pushReg(cond, mtI32)
	err := f.emitCheckedLoop(r, p.End, func() error {
		savedPins, savedFPins := f.pinned, f.fpinned
		ptr, n := copyRead(p.Pointer), copyRead(p.Counter)
		tmp := alloc()
		// This arm is nontrapping, call-free and uninterruptible. Owned copies
		// preserve the original inputs while final pointer/count locals are stored.
		f.a.LslImm64(tmp, n, 2)
		f.a.Add64(tmp, tmp, ptr)
		f.a.MovReg32(tmp, tmp)
		for _, x := range []uint32{p.Pointer, p.NextPointer} {
			f.pushReg(tmp, mtI32)
			f.setLocal(nil, int(x), false)
		}
		f.pushValue(zeroStorage(mtI32))
		f.setLocal(nil, int(p.Counter), false)
		var v [15]Reg
		used := p.Terms/4 + 7
		for i := 0; i < used; i++ {
			v[i] = f.allocFReg(calleeFP)
			f.fpinned = f.fpinned.add(v[i])
		}
		groups := p.Terms / 4
		sample, bias := v[groups], v[groups+1]
		acc := v[groups+2 : groups+6]
		counter := v[groups+6]
		for g := 0; g < groups; g++ {
			for lane := 0; lane < 4; lane++ {
				get(p.Coefficients[p.Terms-1-g*4-lane])
				src, owned := f.materializeRead(f.popValue())
				if lane == 0 {
					f.a.NeonDupGprS(v[g], src)
				} else {
					f.a.NeonInsS(v[g], src, byte(lane))
				}
				if owned {
					f.release(src)
				}
			}
		}
		f.a.NeonDupGprS(counter, n)
		f.pinned = f.pinned.remove(n)
		f.release(n)
		hash := copyRead(p.Hash)
		sum := alloc()
		f.a.MovImm32(tmp, p.Bias)
		f.a.NeonDupGprS(bias, tmp)
		// Distribute the uniform bias in the wrapping i32 ring. Coefficients
		// are invariant, so this correction is computed once per checked loop.
		f.a.NeonEor16b(acc[0], acc[0], acc[0])
		for _, weight := range v[:groups] {
			f.a.NeonAddS(acc[0], acc[0], weight)
		}
		f.a.NeonMulS(acc[0], acc[0], bias)
		f.a.NeonAddvS(bias, acc[0])
		f.a.MovImm64(tmp, uint64(p.Offset))
		f.a.Add64(ptr, ptr, tmp)
		f.a.Add64(ptr, ptr, linMemReg)
		loop := f.a.Len()
		for _, a := range acc {
			f.a.NeonEor16b(a, a, a)
		}
		for g := 0; g < groups; g++ {
			f.a.LdrQ(sample, ptr, int32(g*16))
			f.a.NeonMlaS(acc[g%4], sample, v[g])
		}
		f.a.NeonAddS(acc[0], acc[0], acc[1])
		f.a.NeonAddS(acc[2], acc[2], acc[3])
		f.a.NeonAddS(acc[0], acc[0], acc[2])
		f.a.NeonAddvS(acc[0], acc[0])
		f.a.NeonUmovS(sum, acc[0], 0)
		f.a.NeonUmovS(tmp, bias, 0)
		f.a.Add32(sum, sum, tmp)
		if p.Divisor != 1 {
			f.a.AsrImm(tmp, sum, 31, true)
			f.a.AndImm32(tmp, tmp, p.Divisor-1)
			f.a.Add32(sum, sum, tmp)
			f.a.AsrImm(sum, sum, uint8(bits.TrailingZeros32(p.Divisor)), true)
		}
		f.a.Eor32(hash, hash, sum)
		f.a.MovImm32(tmp, p.Multiplier)
		f.a.Mul32(hash, hash, tmp)
		f.a.AddImm64(ptr, ptr, 4)
		f.a.NeonUmovS(tmp, counter, 0)
		f.a.SubImm32(tmp, tmp, 1)
		f.a.NeonDupGprS(counter, tmp)
		again := f.a.Cbnz32(tmp)
		f.a.PatchBranch19(again, loop)
		f.pushReg(hash, mtI32)
		f.setLocal(nil, int(p.Hash), false)
		for _, reg := range []Reg{ptr, hash, sum, tmp} {
			f.release(reg)
		}
		for _, reg := range v[:used] {
			f.releaseF(reg)
		}
		f.pinned, f.fpinned = savedPins, savedFPins
		for _, x := range []uint32{p.Pointer, p.NextPointer, p.Counter, p.Hash} {
			if x < 64 {
				f.localWritten |= uint64(1) << x
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	f.stats.peep("weighted-i32-window-vector-loop")
	return true, nil
}
