//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func (f *fn) tryGlobalXorShiftLoop(r *wasm.Reader) (bool, error) {
	if !f.opt(optIntegerSumLoop) || f.unreachable || f.interruptible || f.localBase != 0 || f.moduleEH || f.gcFrameRoots != nil || f.usesCalls || f.makesCalls || len(f.customInstructions) != 0 {
		return false, nil
	}
	p, ok := shared.InspectGlobalXorShiftLoop(r, f.m)
	if !ok {
		return false, nil
	}
	for _, x := range []uint32{p.Counter, p.Accumulator, p.Bound} {
		if uint64(x) >= uint64(len(f.localType)) || f.localType[x] != mtI32 {
			return false, nil
		}
	}
	for _, frame := range f.ctrl {
		if frame.kind == cfLoop {
			return false, nil
		}
	}
	if int(p.Counter) < f.nParams || !shared.IntegerSumCounterUntouched(r, p.Counter, f.classifier) {
		return false, nil
	}
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	available := 0
	for _, reg := range gpAlloc {
		if !blocked.has(reg) {
			available++
		}
	}
	// Six scratch GPRs plus materialized sources and canonical setters. Only
	// caller-saved vector registers are used; no new callee-save contract is needed.
	const calleeFP regMask = 0xff00
	vectorBlocked := f.blockedFRegs(calleeFP)
	vectors := 0
	for _, reg := range fpAllocRegs {
		if !vectorBlocked.has(reg) {
			vectors++
		}
	}
	if available < 11 || vectors < 8 {
		return false, nil
	}
	if err := r.JumpTo(p.End); err != nil {
		return false, err
	}
	f.flush()
	f.invalidateStoreForward()
	savedPins, savedFPins := f.pinned, f.fpinned
	alloc := func() Reg { reg := f.allocReg(0); f.pinned = f.pinned.add(reg); return reg }
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
	copyRead := func(wide bool) Reg {
		src, owned := f.materializeRead(f.popValue())
		f.pinned = f.pinned.add(src)
		out := alloc()
		if wide {
			f.a.MovReg64(out, src)
		} else {
			f.a.MovReg32(out, src)
		}
		if owned {
			f.pinned = f.pinned.remove(src)
			f.release(src)
		}
		return out
	}
	if err := f.globalGetIndex(p.Global); err != nil {
		return false, err
	}
	global := copyRead(true)
	get(p.Bound)
	n := copyRead(false)
	nonzero := f.a.Cbnz32(n)
	f.a.MovImm32(n, 1)
	f.patchBranch19(nonzero, f.a.Len())
	step, tail, term, sum := alloc(), alloc(), alloc(), alloc()
	var v [8]Reg
	for i := range v {
		v[i] = f.allocFReg(calleeFP)
		f.fpinned = f.fpinned.add(v[i])
	}
	globals := v[:4]
	acc0, acc1, stride, tmp := v[4], v[5], v[6], v[7]
	f.a.MovImm64(step, p.Step)
	f.a.MovReg64(tail, global)
	for lane := byte(0); lane < 8; lane++ {
		f.a.Add64(tail, tail, step)
		f.a.NeonInsD(globals[lane/2], tail, lane&1)
	}
	// Preserve the final global independently of vector batching and scalar tail.
	f.a.Madd64(global, n, step, global)
	f.a.MovImm64(step, p.Step*8)
	f.a.NeonDupGprD(stride, step)
	f.a.NeonEor16b(acc0, acc0, acc0)
	f.a.NeonEor16b(acc1, acc1, acc1)
	f.a.CmpImm32(n, 8)
	small := f.a.Bcond(a64.CondCC)
	loop := f.a.Len()
	for i, g := range globals {
		if p.Shift == 0 {
			f.a.NeonEor16b(tmp, g, g)
		} else {
			f.a.NeonUshrD(tmp, g, p.Shift)
			f.a.NeonEor16b(tmp, tmp, g)
		}
		acc := acc0
		if i&1 == 1 {
			acc = acc1
		}
		f.a.NeonAddD(acc, acc, tmp)
		f.a.NeonAddD(g, g, stride)
	}
	f.a.SubImm32(n, n, 8)
	f.a.CmpImm32(n, 8)
	again := f.a.Bcond(a64.CondCS)
	f.patchBranch19(again, loop)
	f.patchBranch19(small, f.a.Len())
	// Independent accumulators remove the dependency between adjacent pairs.
	// Combining modulo 2^64 preserves the observed wrapping i32 reduction.
	f.a.NeonAddD(acc0, acc0, acc1)
	f.a.NeonUmovD(sum, acc0, 0)
	f.a.NeonUmovD(term, acc0, 1)
	f.a.Add32(sum, sum, term)
	noTail := f.a.Cbz32(n)
	f.a.NeonUmovD(tail, globals[0], 0)
	f.a.MovImm64(step, p.Step)
	tailLoop := f.a.Len()
	f.a.LsrImm(term, tail, p.Shift, false)
	f.a.Eor64(term, term, tail)
	f.a.Add32(sum, sum, term)
	f.a.Add64(tail, tail, step)
	f.a.SubImm32(n, n, 1)
	again = f.a.Cbnz32(n)
	f.patchBranch19(again, tailLoop)
	f.patchBranch19(noTail, f.a.Len())
	get(p.Accumulator)
	initial, owned := f.materializeRead(f.popValue())
	f.a.Add32(sum, sum, initial)
	if owned {
		f.release(initial)
	}
	f.pushReg(global, mtI64)
	if err := f.globalSetIndex(p.Global); err != nil {
		return false, err
	}
	f.pushReg(sum, mtI32)
	f.setLocal(nil, int(p.Accumulator), false)
	get(p.Bound)
	bound, owned := f.materializeRead(f.popValue())
	f.a.MovReg32(n, bound)
	if owned {
		f.release(bound)
	}
	f.a.MovImm32(term, 1)
	f.a.CmpImm32(n, 0)
	f.a.Csel32(n, n, term, a64.CondNE)
	f.pushReg(n, mtI32)
	f.setLocal(nil, int(p.Counter), false)
	for _, reg := range []Reg{global, n, step, tail, term, sum} {
		f.release(reg)
	}
	for _, reg := range v {
		f.releaseF(reg)
	}
	f.pinned, f.fpinned = savedPins, savedFPins
	for _, x := range []uint32{p.Counter, p.Accumulator} {
		if x < 64 {
			f.localWritten |= uint64(1) << x
		}
	}
	f.stats.peep("global-xor-shift-vector-loop")
	return true, nil
}
