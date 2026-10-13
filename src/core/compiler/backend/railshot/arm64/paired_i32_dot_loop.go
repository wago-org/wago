//go:build arm64

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func (f *fn) tryPairedI32DotLoop(r *wasm.Reader) (bool, error) {
	if !f.opt(optIntegerSumLoop) || f.unreachable || f.interruptible || f.localBase != 0 || f.moduleEH || f.gcFrameRoots != nil || f.usesCalls || f.makesCalls || len(f.customInstructions) != 0 {
		return false, nil
	}
	p, ok := shared.InspectPairedI32DotLoop(r, f.m)
	if !ok {
		return false, nil
	}
	for _, x := range []uint32{p.Left, p.Right, p.Counter, p.Accumulator, p.LeftAddress, p.RightAddress} {
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
	if available < 11 || vectors < 8 {
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
	left, right, counter := copyRead(p.Left), copyRead(p.Right), copyRead(p.Counter)
	tmp, limit, cond := alloc(), alloc(), alloc()
	var bad [6]int
	badN := 0
	fail := func(site int) { bad[badN] = site; badN++ }
	// The equality latch terminates without wrapping only for this finite suffix.
	f.a.MovImm32(tmp, int32(p.Bound))
	f.a.CmpReg32(counter, tmp)
	fail(f.a.Bcond(a64.CondCS))
	f.a.Sub32(tmp, tmp, counter)
	f.a.MovImm32(cond, 7)
	f.a.And32(cond, tmp, cond)
	fail(f.a.Cbnz32(cond))
	for _, base := range []Reg{left, right} {
		f.a.MovImm64(tmp, uint64(p.Bound))
		f.a.Add64(tmp, tmp, base)
		f.a.MovImm64(limit, 1<<32)
		f.a.CmpReg64(tmp, limit)
		fail(f.a.Bcond(a64.CondHI))
		f.a.MovImm64(limit, uint64(p.Offset))
		f.a.Add64(tmp, tmp, limit)
		f.ld64(limit, linMemReg, -int32(bdCurBytes))
		f.a.CmpReg64(tmp, limit)
		fail(f.a.Bcond(a64.CondHI))
	}
	f.a.MovImm32(cond, 1)
	skip := f.a.Branch()
	for _, site := range bad[:badN] {
		f.a.PatchBranch19(site, f.a.Len())
	}
	f.a.MovImm32(cond, 0)
	f.a.PatchBranch26(skip, f.a.Len())
	for _, reg := range []Reg{left, right, counter, tmp, limit} {
		f.release(reg)
	}
	f.pinned = savedPins
	f.pushReg(cond, mtI32)
	// Use ordinary control convergence for both paths, with the actual source
	// block type and positions. The scalar arm compiles the original loop verbatim.
	ifReader := *r
	if err := f.opBlock(&ifReader, 0x04); err != nil {
		return false, err
	}
	savedPins, savedFPins := f.pinned, f.fpinned
	left, right, n := copyRead(p.Left), copyRead(p.Right), copyRead(p.Counter)
	sum, tmp, product := alloc(), alloc(), alloc()
	f.a.Add32(left, left, n)
	f.a.Add32(right, right, n)
	f.a.MovImm32(tmp, int32(p.Bound))
	f.a.Sub32(n, tmp, n)
	f.a.MovImm64(tmp, uint64(p.Offset))
	f.a.Add64(left, left, tmp)
	f.a.Add64(right, right, tmp)
	f.a.Add64(left, left, linMemReg)
	f.a.Add64(right, right, linMemReg)
	// Four independent wrapping sums hide the multiply-accumulate dependency.
	// Reduce them only after the complete batches and exact remainder loads.
	var v [8]Reg
	for i := range v {
		v[i] = f.allocFReg(calleeFP)
		f.fpinned = f.fpinned.add(v[i])
	}
	x, y, lb, rb, acc, acc1, acc2, acc3 := v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]
	f.a.MovImm32(tmp, p.LeftBias)
	f.a.NeonDupGprS(lb, tmp)
	f.a.MovImm32(tmp, p.RightBias)
	f.a.NeonDupGprS(rb, tmp)
	f.a.NeonEor16b(acc, acc, acc)
	f.a.NeonEor16b(acc1, acc1, acc1)
	f.a.NeonEor16b(acc2, acc2, acc2)
	f.a.NeonEor16b(acc3, acc3, acc3)
	f.a.CmpImm32(n, 128)
	small := f.a.Bcond(a64.CondCC)
	loop := f.a.Len()
	addPair := func(off int32, into Reg) {
		f.a.LdrQ(x, left, off)
		f.a.LdrQ(y, right, off)
		f.a.NeonAddS(x, x, lb)
		f.a.NeonAddS(y, y, rb)
		f.a.NeonMlaS(into, x, y)
	}
	addPair(0, acc)
	addPair(16, acc1)
	addPair(32, acc2)
	addPair(48, acc3)
	addPair(64, acc)
	addPair(80, acc1)
	addPair(96, acc2)
	addPair(112, acc3)
	f.a.AddImm64(left, left, 128)
	f.a.AddImm64(right, right, 128)
	f.a.SubImm32(n, n, 128)
	f.a.CmpImm32(n, 128)
	again := f.a.Bcond(a64.CondCS)
	f.a.PatchBranch19(again, loop)
	f.a.PatchBranch19(small, f.a.Len())
	f.a.CmpImm32(n, 64)
	noSixteen := f.a.Bcond(a64.CondCC)
	addPair(0, acc)
	addPair(16, acc1)
	addPair(32, acc2)
	addPair(48, acc3)
	f.a.AddImm64(left, left, 64)
	f.a.AddImm64(right, right, 64)
	f.a.SubImm32(n, n, 64)
	f.a.PatchBranch19(noSixteen, f.a.Len())
	f.a.CmpImm32(n, 32)
	noEight := f.a.Bcond(a64.CondCC)
	addPair(0, acc)
	addPair(16, acc1)
	f.a.AddImm64(left, left, 32)
	f.a.AddImm64(right, right, 32)
	f.a.SubImm32(n, n, 32)
	f.a.PatchBranch19(noEight, f.a.Len())
	f.a.CmpImm32(n, 16)
	noHalf := f.a.Bcond(a64.CondCC)
	addPair(0, acc)
	f.a.AddImm64(left, left, 16)
	f.a.AddImm64(right, right, 16)
	f.a.SubImm32(n, n, 16)
	f.a.PatchBranch19(noHalf, f.a.Len())
	f.a.NeonAddS(acc, acc, acc1)
	f.a.NeonAddS(acc2, acc2, acc3)
	f.a.NeonAddS(acc, acc, acc2)
	f.a.NeonAddvS(acc, acc)
	f.a.NeonUmovS(sum, acc, 0)
	noTail := f.a.Cbz32(n)
	// Only a final pair remains. Scalar loads avoid any vector over-read.
	for _, off := range []int32{0, 4} {
		f.ld32(tmp, left, off)
		f.a.MovImm32(product, p.LeftBias)
		f.a.Add32(tmp, tmp, product)
		f.ld32(product, right, off)
		f.a.MovImm32(n, p.RightBias)
		f.a.Add32(product, product, n)
		f.a.Madd32(sum, tmp, product, sum)
	}
	f.a.PatchBranch19(noTail, f.a.Len())
	get(p.Accumulator)
	initial, owned := f.materializeRead(f.popValue())
	f.a.Add32(sum, sum, initial)
	if owned {
		f.release(initial)
	}
	f.pushReg(sum, mtI32)
	f.setLocal(nil, int(p.Accumulator), false)
	for _, pair := range [][2]uint32{{p.Left, p.LeftAddress}, {p.Right, p.RightAddress}} {
		get(pair[0])
		base, owned := f.materializeRead(f.popValue())
		f.a.MovImm32(product, int32(p.Bound-8))
		f.a.Add32(tmp, base, product)
		if owned {
			f.release(base)
		}
		f.pushReg(tmp, mtI32)
		f.setLocal(nil, int(pair[1]), false)
	}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(p.Bound)})
	f.setLocal(nil, int(p.Counter), false)
	for _, reg := range []Reg{left, right, n, sum, tmp, product} {
		f.release(reg)
	}
	for _, reg := range v {
		f.releaseF(reg)
	}
	f.pinned, f.fpinned = savedPins, savedFPins
	for _, x := range []uint32{p.Counter, p.Accumulator, p.LeftAddress, p.RightAddress} {
		if x < 64 {
			f.localWritten |= uint64(1) << x
		}
	}
	if err := f.opElse(); err != nil {
		return false, err
	}
	depth := len(f.ctrl)
	if err := f.opBlock(r, 0x03); err != nil {
		return false, err
	}
	if err := f.bodyLoop(r, depth); err != nil {
		return false, err
	}
	if r.Offset() != p.End {
		return false, fmt.Errorf("arm64: paired dot source end mismatch")
	}
	if err := f.opEnd(r); err != nil {
		return false, err
	}
	f.stats.peep("paired-i32-dot-vector-loop")
	return true, nil
}
