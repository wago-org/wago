// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

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
	if !f.opt(optByteReduceLoop) || f.byteReduceLoop != nil || f.dotLoop != nil ||
		f.usesCalls || f.interruptible || f.moduleEH || f.localBase != 0 ||
		len(f.customInstructions) != 0 || len(f.intervalLast) != 0 ||
		f.depth() != 0 || f.memoryAddr64(0) || f.threadedMemory0 ||
		len(f.ctrl) == 0 || f.ctrl[len(f.ctrl)-1].kind != cfBlock || f.ctrl[len(f.ctrl)-1].branchArity() != 0 ||
		profileEnabled && f.stats != nil && f.stats.RecordSources {
		return
	}
	p, ok := f.inspectByteReduceLoop(r)
	if !ok {
		return
	}
	oldPinned := f.pinned
	oldFPinned := f.fpinned
	var vectors [5]Reg
	for i := range vectors {
		vectors[i] = regNone
	}
	var regs [6]Reg
	for i := range regs {
		regs[i] = regNone
	}
	defer func() {
		f.pinned = oldPinned
		f.fpinned = oldFPinned
		for _, reg := range regs {
			if reg != regNone {
				f.release(reg)
			}
		}
		for _, reg := range vectors {
			if reg != regNone {
				f.releaseF(reg)
			}
		}
	}()
	for i := range regs {
		regs[i] = f.allocRegOrNone(0)
		if regs[i] == regNone {
			return
		}
		f.pinned = f.pinned.add(regs[i])
	}
	freeVectors := 0
	blocked := f.blockedFRegs(0)
	for _, reg := range fpAllocRegs {
		if !blocked.has(reg) && f.fregUser[reg] == nil {
			freeVectors++
		}
	}
	if freeVectors < len(vectors) {
		return
	}
	for i := range vectors {
		vectors[i] = f.allocFReg(0)
		f.fpinned = f.fpinned.add(vectors[i])
	}
	counter, limit, acc, partial, ptr, weight := regs[0], regs[1], regs[2], regs[3], regs[4], regs[5]
	read := func(local int, dst Reg) {
		if reg, _, pinned := f.pinReg(local); pinned {
			f.recoverLocal(local)
			f.a.MovReg32(dst, reg)
		} else if f.locals[local].state == lsConstZero {
			f.a.MovImm64(dst, 0)
		} else {
			f.ld32(dst, SP, f.localOff(local))
		}
	}
	read(p.counter, counter)
	read(p.limit, limit)
	read(p.acc, acc)
	f.cmpRR(counter, limit, false)
	empty := f.a.Bcond(condAE)
	// Widen before adding the static offset. This proves every byte and rejects
	// wrapping ranges even for a full 4 GiB memory32. No source local is changed.
	f.a.MovImm64(X16, uint64(p.offset))
	f.a.Add64(X16, X16, limit)
	mb := f.memSizeReg
	if mb == regNone {
		mb = X17
		f.ld64(mb, linMemReg, -int32(bdCurBytes))
	}
	f.cmpRR(X16, mb, true)
	outOfBounds := f.a.Bcond(condA)
	f.a.Add64(ptr, linMemReg, counter)
	if p.offset != 0 {
		f.a.MovImm64(X16, uint64(p.offset))
		f.a.Add64(ptr, ptr, X16)
	}
	f.a.MovImm64(partial, 0)
	if p.weighted {
		f.a.MovImm64(weight, uint64(uint32(p.bias)))
		f.a.Add32(weight, weight, counter)
	}
	// For sixteen bytes, sum(byte*(weight+i)) equals weight*sum(byte)
	// plus sum(byte*i), modulo 2^32. Both horizontal halfword sums are exact:
	// sum(byte) <= 4080 and sum(byte*i) <= 30600. No wider multiply is needed,
	// even when the original wrapping weight crosses zero or the signed boundary.
	f.a.Sub32(X17, limit, counter)
	f.cmpImm(X17, 64, false)
	toScalar := f.a.Bcond(condB)
	bytes, lo, hi, biasLo, biasHi := vectors[0], vectors[1], vectors[2], vectors[3], vectors[4]
	if p.weighted {
		f.buildV128Const(biasLo, 0x0003000200010000, 0x0007000600050004)
		f.buildV128Const(biasHi, 0x000b000a00090008, 0x000f000e000d000c)
	}
	vectorLoop := f.a.Len()
	f.a.LdrQ(bytes, ptr, 0)
	if p.weighted {
		f.a.NeonUxtlHfromB(lo, bytes)
		f.a.NeonUxtl2HfromB(hi, bytes)
		f.a.NeonMulH(lo, lo, biasLo)
		f.a.NeonMulH(hi, hi, biasHi)
		f.a.NeonAddH(lo, lo, hi)
		f.a.NeonAddvH(lo, lo)
		f.a.NeonUmovH(X17, lo, 0)
	}
	f.a.NeonUaddlpHfromB(bytes, bytes)
	f.a.NeonAddvH(bytes, bytes)
	f.a.NeonUmovH(X16, bytes, 0)
	if p.weighted {
		f.a.Madd32(acc, X16, weight, acc)
		f.a.Add32(acc, acc, X17)
		f.a.AddImm32(weight, weight, 16)
	} else {
		f.a.Add32(acc, acc, X16)
	}
	f.a.AddImm64(ptr, ptr, 16)
	f.a.AddImm32(counter, counter, 16)
	f.a.Sub32(X17, limit, counter)
	f.cmpImm(X17, 16, false)
	f.patchBranch19(f.a.Bcond(condAE), vectorLoop)
	f.patchBranch19(toScalar, f.a.Len())
	f.a.Sub32(X17, limit, counter)
	f.cmpImm(X17, 4, false)
	toTail := f.a.Bcond(condB)
	group := f.a.Len()
	for i := uint32(0); i < 4; i++ {
		f.a.Ldrb(X16, ptr, i)
		sum := acc
		if i&1 != 0 {
			sum = partial
		}
		if p.weighted {
			f.a.Madd32(sum, X16, weight, sum)
			f.a.AddImm32(weight, weight, 1)
		} else {
			f.a.Add32(sum, sum, X16)
		}
	}
	f.a.AddImm64(ptr, ptr, 4)
	f.a.AddImm32(counter, counter, 4)
	f.a.Sub32(X17, limit, counter)
	f.cmpImm(X17, 4, false)
	f.patchBranch19(f.a.Bcond(condAE), group)
	f.patchBranch19(toTail, f.a.Len())
	f.cmpRR(counter, limit, false)
	noTail := f.a.Bcond(condE)
	tail := f.a.Len()
	f.a.Ldrb(X16, ptr, 0)
	if p.weighted {
		f.a.Madd32(acc, X16, weight, acc)
		f.a.AddImm32(weight, weight, 1)
	} else {
		f.a.Add32(acc, acc, X16)
	}
	f.a.AddImm64(ptr, ptr, 1)
	f.a.AddImm32(counter, counter, 1)
	f.cmpRR(counter, limit, false)
	f.patchBranch19(f.a.Bcond(condB), tail)
	f.patchBranch19(noTail, f.a.Len())
	f.a.Add32(acc, acc, partial)
	write := func(local int, src Reg) {
		if reg, _, pinned := f.pinReg(local); pinned {
			f.a.MovReg32(reg, src)
		} else {
			f.st64(SP, f.localOff(local), src)
		}
	}
	write(p.counter, counter)
	write(p.acc, acc)
	f.byteReduceLoop = &byteReduceState{counter: p.counter, acc: p.acc, depth: len(f.ctrl) + 1, endSite: f.a.Branch()}
	f.patchBranch19(empty, f.a.Len())
	f.patchBranch19(outOfBounds, f.a.Len())
	f.stats.peep("byte-reduce-loop")
}

func (f *fn) finishByteReduceLoop(depth int) {
	state := f.byteReduceLoop
	if state == nil || state.depth != depth {
		return
	}
	f.patchBranch26(state.endSite, f.a.Len())
	f.setFactsForLocal(state.counter, 0)
	f.setFactsForLocal(state.acc, 0)
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.byteReduceLoop = nil
}
