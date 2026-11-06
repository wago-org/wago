//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var byteReduceLoopEnabled = true

type byteReducePlan struct {
	counter, limit, acc int
	offset              uint32
	bias                int32
	weighted            bool
}

// Admit only a read-only, unsigned ascending byte reduction. The complete body
// is checked, including its exit, accumulator update and unit-stride backedge.
// A failed runtime range guard executes the original scalar loop unchanged.
func (f *fn) inspectByteReduceLoop(reader *wasm.Reader) (p byteReducePlan, ok bool) {
	r := *reader
	op := func(want byte) bool { got, err := r.Byte(); return err == nil && got == want }
	local := func(want byte) (int, bool) {
		if !op(want) {
			return 0, false
		}
		x, err := r.U32()
		return int(x), err == nil && x < uint32(f.nLocals) && f.localType[x] == mtI32
	}
	label := func(want uint32) bool { x, err := r.U32(); return err == nil && x == want }
	constant := func() (int32, bool) {
		if !op(0x41) {
			return 0, false
		}
		x, err := r.I32()
		return x, err == nil
	}
	var yes bool
	if p.counter, yes = local(0x20); !yes {
		return p, false
	}
	if p.limit, yes = local(0x20); !yes || p.limit == p.counter || !op(0x4f) || !op(0x0d) || !label(1) {
		return p, false
	}
	if p.acc, yes = local(0x20); !yes || p.acc == p.counter || p.acc == p.limit {
		return p, false
	}
	index, yes := local(0x20)
	if !yes || index != p.counter || !op(0x2d) {
		return p, false
	}
	mem, offset, err := f.readMemArg(&r)
	if err != nil || mem != 0 || offset > 0xffffffff {
		return p, false
	}
	p.offset = uint32(offset)
	// Optional wrapping i32 weight: counter + any constant bias.
	next := r
	if weight, yes := local(0x20); yes {
		if weight != p.counter {
			return p, false
		}
		if p.bias, yes = constant(); !yes || !op(0x6a) || !op(0x6c) {
			return p, false
		}
		p.weighted = true
	} else {
		r = next
	}
	if !op(0x6a) {
		return p, false
	}
	if x, yes := local(0x21); !yes || x != p.acc {
		return p, false
	}
	if x, yes := local(0x20); !yes || x != p.counter {
		return p, false
	}
	if step, yes := constant(); !yes || step != 1 || !op(0x6a) {
		return p, false
	}
	if x, yes := local(0x21); !yes || x != p.counter || !op(0x0c) || !label(0) || !op(0x0b) || !op(0x0b) {
		return p, false
	}
	return p, true
}

type byteReduceState struct {
	counter, acc, depth, endSite int
}

func (f *fn) emitByteReduceLoop(r *wasm.Reader) {
	if !f.opt(optByteReduceLoop) || f.byteReduceLoop != nil || f.usesCalls || f.interruptible || f.moduleEH || f.localBase != 0 ||
		len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.depth() != 0 || f.memoryAddr64(0) || f.threadedMemory0 ||
		len(f.ctrl) == 0 || f.ctrl[len(f.ctrl)-1].kind != cfBlock || f.ctrl[len(f.ctrl)-1].branchArity() != 0 ||
		profileEnabled && f.stats != nil && f.stats.RecordSources {
		return
	}
	p, ok := f.inspectByteReduceLoop(r)
	if !ok {
		return
	}
	var localPins regallocReadMask
	if regallocCheckEnabled {
		localPins = f.checkProtectLocalPins()
		defer checkRestoreRead(f, localPins)
	}
	// This edge bypasses ordinary pin recovery. Select only genuinely free
	// carriers; the general allocator may relinquish a live local pin.
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved).add(RAX).add(RDX)
	var regs [6]Reg
	n := 0
	for _, reg := range gpAlloc {
		if blocked.has(reg) || f.regUser[reg] != nil {
			continue
		}
		regs[n] = reg
		n++
		if n == len(regs) {
			break
		}
	}
	if n != len(regs) {
		return
	}
	oldPinned := f.pinned
	oldFPinned := f.fpinned
	var vectors [5]Reg
	for i := range vectors {
		vectors[i] = regNone
	}
	defer func() {
		f.pinned = oldPinned
		f.fpinned = oldFPinned
		for _, reg := range vectors {
			if reg != regNone {
				f.releaseF(reg)
			}
		}
		for _, reg := range regs {
			if reg != regNone {
				f.release(reg)
			}
		}
	}()
	// Fixed scratch carriers never overlap the six loop values or local pins.
	f.pinned = f.pinned.add(RAX).add(RDX)
	for _, reg := range regs {
		f.pinned = f.pinned.add(reg)
	}
	vectorOK := !p.weighted || f.cpuHas(shared.AMD64SSSE3)
	if vectorOK {
		blocked := f.fpinned.union(f.fpinnedLocalMask).union(f.fconstMask()).union(f.v128ConstMask())
		free := 0
		for reg := Reg(0); reg < 16; reg++ {
			if !blocked.has(reg) && f.fregUser[reg] == nil {
				free++
			}
		}
		vectorOK = free >= len(vectors)
		if vectorOK {
			for i := range vectors {
				vectors[i] = f.allocFReg(0)
				f.fpinned = f.fpinned.add(vectors[i])
			}
		}
	}
	counter, limit, acc, partial, ptr, weight := regs[0], regs[1], regs[2], regs[3], regs[4], regs[5]
	read := func(local int, dst Reg) {
		if reg, _, pinned := f.pinReg(local); pinned {
			f.recoverLocal(local)
			f.a.MovRegReg32(dst, reg)
		} else if f.locals[local].state == lsConstZero {
			f.a.XorSelf32(dst)
		} else {
			f.a.Load32(dst, RSP, f.localAddr(local))
		}
	}
	read(p.counter, counter)
	read(p.limit, limit)
	read(p.acc, acc)
	f.a.Cmp32(counter, limit)
	empty := f.a.JccPlaceholder(condAE)
	// Widen the limit before adding the static offset. No loop local changes
	// until the entire range is known to fit; failed guards run the scalar loop.
	f.a.MovImm64(RAX, uint64(p.offset))
	f.a.Add64(RAX, limit)
	if f.memSizeReg != regNone {
		f.a.Cmp64(RAX, f.memSizeReg)
	} else {
		f.a.Load64(RDX, RBX, -bdCurBytes)
		f.a.Cmp64(RAX, RDX)
	}
	outOfBounds := f.a.JccPlaceholder(condA)
	f.a.MovReg64(ptr, RBX)
	if p.offset != 0 {
		f.a.MovImm64(RAX, uint64(p.offset))
		f.a.Add64(ptr, RAX)
	}
	f.a.XorSelf32(partial)
	if p.weighted {
		f.a.MovImm32(weight, p.bias)
		f.a.Add32(weight, counter)
	}
	if vectorOK {
		bytes, coeff, ones, tmp, zero := vectors[0], vectors[1], vectors[2], vectors[3], vectors[4]
		f.a.MovRegReg32(RDX, limit)
		f.a.Sub32(RDX, counter)
		f.a.AluRI(cmpDigit, RDX, 16, false)
		toScalar := f.a.JccPlaceholder(condB)
		f.a.SseRR(0x66, 0xef, zero, zero, false)
		if p.weighted {
			f.a.MovImm64(RAX, 0x0706050403020100)
			f.a.MovGprToXmm(coeff, RAX, true)
			f.a.MovImm64(RAX, 0x0f0e0d0c0b0a0908)
			f.a.MovGprToXmm(tmp, RAX, true)
			f.a.Punpcklqdq(coeff, tmp)
			f.a.MovImm64(RAX, 0x0001000100010001)
			f.a.MovGprToXmm(ones, RAX, true)
			f.a.Punpcklqdq(ones, ones)
		}
		f.a.AlignLoop32()
		loop := f.a.Len()
		f.mov128LoadIdx(bytes, ptr, counter, 0)
		if p.weighted {
			// The saturating byte-pair multiply is exact: 255*(14+15) <= 7395.
			// The complete relative weighted sum fits in 30600; wrapping happens only
			// in the original i32 accumulator and the base-weight multiplication.
			f.a.SseRR(0x66, 0x6f, tmp, bytes, false)
			f.a.SseMapRR(0x66, 0x38, 0x04, tmp, coeff)
			f.a.SseRR(0x66, 0xf5, tmp, ones, false)
			f.a.SseMapRR(0x66, 0x38, 0x02, tmp, tmp)
			f.a.SseMapRR(0x66, 0x38, 0x02, tmp, tmp)
			f.a.MovXmmToGpr(RAX, tmp, false)
		}
		f.a.SseRR(0x66, 0xf6, bytes, zero, false)
		f.a.Pshufd(tmp, bytes, 0x4e)
		f.a.SseRR(0x66, 0xd4, bytes, tmp, false)
		f.a.MovXmmToGpr(RDX, bytes, false)
		if p.weighted {
			f.a.IMul(RDX, weight, false)
			f.a.Add32(acc, RAX)
			f.a.AluRI(0, weight, 16, false)
		}
		f.a.Add32(acc, RDX)
		f.a.AluRI(0, counter, 16, false)
		f.a.MovRegReg32(RDX, limit)
		f.a.Sub32(RDX, counter)
		f.a.AluRI(cmpDigit, RDX, 16, false)
		f.a.PatchRel32(f.a.JccPlaceholder(condAE), loop)
		f.a.PatchRel32(toScalar, f.a.Len())
	}
	f.a.MovRegReg32(RDX, limit)
	f.a.Sub32(RDX, counter)
	f.a.AluRI(cmpDigit, RDX, 4, false)
	toTail := f.a.JccPlaceholder(condB)
	group := f.a.Len()
	for i := int32(0); i < 4; i++ {
		f.a.LoadIdx(RAX, ptr, counter, i, 1, false, false)
		sum := acc
		if i&1 != 0 {
			sum = partial
		}
		if p.weighted {
			f.a.IMul(RAX, weight, false)
			f.a.AluRI(0, weight, 1, false)
		}
		f.a.Add32(sum, RAX)
	}
	f.a.AluRI(0, counter, 4, false)
	f.a.MovRegReg32(RDX, limit)
	f.a.Sub32(RDX, counter)
	f.a.AluRI(cmpDigit, RDX, 4, false)
	f.a.PatchRel32(f.a.JccPlaceholder(condAE), group)
	f.a.PatchRel32(toTail, f.a.Len())
	f.a.Cmp32(counter, limit)
	noTail := f.a.JccPlaceholder(condE)
	tail := f.a.Len()
	f.a.LoadIdx(RAX, ptr, counter, 0, 1, false, false)
	if p.weighted {
		f.a.IMul(RAX, weight, false)
		f.a.AluRI(0, weight, 1, false)
	}
	f.a.Add32(acc, RAX)
	f.a.AluRI(0, counter, 1, false)
	f.a.Cmp32(counter, limit)
	f.a.PatchRel32(f.a.JccPlaceholder(condB), tail)
	f.a.PatchRel32(noTail, f.a.Len())
	f.a.Add32(acc, partial)
	write := func(local int, src Reg) {
		f.a.Store64(RSP, f.localAddr(local), src)
		if reg, _, pinned := f.pinReg(local); pinned {
			if regallocCheckEnabled {
				f.checkAllowLocalPinWrite(localPins, local)
			}
			f.a.MovRegReg32(reg, src)
		}
	}
	write(p.counter, counter)
	write(p.acc, acc)
	f.byteReduceLoop = &byteReduceState{counter: p.counter, acc: p.acc, depth: len(f.ctrl) + 1, endSite: f.a.JmpPlaceholder()}
	f.a.PatchRel32(empty, f.a.Len())
	f.a.PatchRel32(outOfBounds, f.a.Len())
	f.stats.peep("byte-reduce-loop")
}

func (f *fn) finishByteReduceLoop(depth int) {
	state := f.byteReduceLoop
	if state == nil || state.depth != depth {
		return
	}
	f.a.PatchRel32(state.endSite, f.a.Len())
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.byteReduceLoop = nil
}
