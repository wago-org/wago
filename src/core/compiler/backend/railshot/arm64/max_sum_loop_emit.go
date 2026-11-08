//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var maxSumLoopEnabled = os.Getenv("WAGO_ARM64_NO_MAX_SUM_LOOP") != "1"

func (f *fn) hasMaxSumPrefix(source *wasm.Reader) bool {
	r := *source
	var imm wasm.InstructionImmediate
	load := false
	for ops := 0; ops < 48 && f.integerResidualWork < integerResidualWorkLimit; ops++ {
		f.integerResidualWork++
		op, e := r.Byte()
		if e != nil {
			return false
		}
		if op == 0x1b {
			return load
		}
		switch op {
		case 0x20, 0x21, 0x22, 0x41, 0x6a, 0x47, 0x48, 0x4a, 0x28:
			if op == 0x28 {
				load = true
			}
			if f.classifier.ClassifyInto(&r, op, &imm) != nil {
				return false
			}
		default:
			return false
		}
	}
	return false
}

func (f *fn) emitMaxSumLoop(source *wasm.Reader) {
	if !maxSumLoopEnabled || !f.opt(optDotLoopVector) || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || f.tracksGCFrameRoots() || f.memoryAddr64(0) || f.threadedMemory0 || f.a.Len() > 64<<10 {
		return
	}
	if len(f.intervalLast) != 0 && (!integerMapIntervalsEnabled || !f.intervalControl || f.intervalActive != 0) {
		return
	}
	if !f.hasMaxSumPrefix(source) {
		return
	}
	f.emitMaxSumBody(source)
}

func (f *fn) emitMaxSumBody(source *wasm.Reader) {
	p, ok := f.inspectMaxSumLoop(source)
	if !ok {
		return
	}
	f.stats.peep("max-sum-recognized")
	f.emitMaxSumPlan(&p)
}

// Guard the complete read ranges and the invariant destination before changing
// guest state. A failed guard executes the original ordered body, preserving the
// visible store prefix before any trap. One or two original iterations finish
// every successful prefix, publishing all source temporaries without liveness.
func (f *fn) emitMaxSumPlan(p *maxSumPlan) {
	free := 0
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range gpAlloc {
		if f.regUser[r] == nil && !blocked.has(r) {
			free++
		}
	}
	if free < 7 {
		return
	}
	free = 0
	fpBlocked := f.blockedFRegs(0)
	for _, r := range fpAllocRegs {
		if !fpBlocked.has(r) && f.fregUser[r] == nil {
			free++
		}
	}
	if free < 3 {
		return
	}
	oldPins, oldFPins := f.pinned, f.fpinned
	var gp [7]Reg
	var fp [3]Reg
	for i := range gp {
		gp[i] = f.allocRegOrNone(0)
		f.pinned = f.pinned.add(gp[i])
	}
	for i := range fp {
		fp[i] = f.allocFReg(0)
		f.fpinned = f.fpinned.add(fp[i])
	}
	defer func() {
		f.pinned, f.fpinned = oldPins, oldFPins
		for _, r := range gp {
			f.release(r)
		}
		for _, r := range fp {
			f.releaseF(r)
		}
	}()
	a, b, dst, counter, remaining, acc, size := gp[0], gp[1], gp[2], gp[3], gp[4], gp[5], gp[6]
	va, vb, maximum := fp[0], fp[1], fp[2]
	for _, c := range p.locals[:p.localN] {
		f.recoverLocal(int(c.Index))
	}
	read := func(local int, out Reg) {
		if r, _, ok := f.pinReg(local); ok {
			f.a.MovReg32(out, r)
		} else if f.locals[local].state == lsConstZero {
			f.a.MovImm32(out, 0)
		} else {
			f.ld32(out, SP, f.localOff(local))
		}
	}
	read(p.counter, counter)
	if p.constantLimit {
		f.a.MovImm32(X16, int32(p.limitConst))
	} else {
		read(p.limit, X16)
	}
	var guards [16]int
	ng := 0
	guard := func(c Cond) { guards[ng] = f.a.Bcond(c); ng++ }
	f.cmpRR(X16, counter, false)
	guard(condBE)
	f.a.Sub32(remaining, X16, counter)
	f.cmpImm(remaining, 16, false)
	guard(condB)
	f.a.MovImm32(X16, 1<<20)
	f.cmpRR(remaining, X16, false)
	guard(condA)
	f.a.TstImm32(remaining, 1)
	guard(condNE)
	read(p.dst, dst)
	f.ld64(size, linMemReg, -int32(bdCurBytes))
	f.a.AddImm64(X16, dst, 4)
	f.cmpRR(X16, size, true)
	guard(condA)
	for i, ptr := range [2]Reg{a, b} {
		s := p.streams[i]
		f.a.MovImm32(ptr, int32(s.bias))
		for _, local := range s.locals[:s.n] {
			read(local, X16)
			f.a.Add32(ptr, ptr, X16)
		}
		// Dynamic Wasm additions above wrap in i32. The guarded stream span
		// below is widened separately and cannot wrap memory32 thereafter.
		f.a.SubImm64(X16, remaining, 1)
		f.a.MovImm64(X17, uint64(s.stride))
		f.a.Mul64(X16, X16, X17)
		f.a.Add64(X16, ptr, X16)
		f.a.AddImm64(X16, X16, 4)
		f.cmpRR(X16, size, true)
		guard(condA)
		f.a.MovImm64(X17, 1<<32)
		f.cmpRR(X16, X17, true)
		guard(condA)
		// Conservatively exclude the entire strided span, including holes.
		f.cmpRR(dst, X16, true)
		separate := f.a.Bcond(condAE)
		f.a.AddImm64(X17, dst, 4)
		f.cmpRR(X17, ptr, true)
		guard(condA)
		f.patchBranch19(separate, f.a.Len())
	}
	read(p.acc, acc)
	f.a.NeonDupGprS(maximum, acc)
	f.a.Add64(a, a, linMemReg)
	f.a.Add64(b, b, linMemReg)
	f.a.Add64(dst, dst, linMemReg)
	loop := f.a.Len()
	for i, ptr := range [2]Reg{a, b} {
		v := va
		if i == 1 {
			v = vb
		}
		stride := p.streams[i].stride
		if stride == 4 {
			f.a.LdrQ(v, ptr, 0)
		} else {
			for lane := byte(0); lane < 4; lane++ {
				f.floatMapOffset(X17, ptr, uint64(lane)*uint64(stride), true, X16)
				f.a.Load32(X16, X17, 0)
				f.a.NeonInsS(v, X16, lane)
			}
		}
	}
	f.a.NeonAddS(va, va, vb)
	f.a.NeonSmaxS(maximum, maximum, va)
	for i, ptr := range [2]Reg{a, b} {
		f.floatMapOffset(ptr, ptr, uint64(p.streams[i].stride)*4, true, X16)
	}
	f.a.AddImm32(counter, counter, 4)
	f.a.SubImm32(remaining, remaining, 4)
	f.cmpImm(remaining, 6, false)
	f.patchBranch19(f.a.Bcond(condAE), loop)
	f.a.NeonExt16b(vb, maximum, maximum, 8)
	f.a.NeonSmaxS(maximum, maximum, vb)
	f.a.NeonExt16b(vb, maximum, maximum, 4)
	f.a.NeonSmaxS(maximum, maximum, vb)
	f.a.NeonUmovS(acc, maximum, 0)
	f.a.Store32(acc, dst, 0)
	publish := func(local int, r Reg) {
		if out, _, ok := f.pinReg(local); ok {
			f.a.MovReg32(out, r)
			f.markLocalDirty(local)
		} else {
			f.st64(SP, f.localOff(local), r)
			f.locals[local].state = lsMem
		}
		f.setFactsForLocal(local, 0)
	}
	publish(p.counter, counter)
	publish(p.acc, acc)
	for i, ptr := range [2]Reg{a, b} {
		s := p.streams[i]
		f.a.Sub32(X16, ptr, linMemReg)
		for _, local := range s.locals[:s.n] {
			if local != s.pointer {
				read(local, X17)
				f.a.Sub32(X16, X16, X17)
			}
		}
		f.a.MovImm32(X17, int32(s.bias))
		f.a.Sub32(X16, X16, X17)
		publish(s.pointer, X16)
	}
	for _, branch := range guards[:ng] {
		f.patchBranch19(branch, f.a.Len())
	}
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.stats.peep("max-sum-vector")
}
