// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var integerArgmaxEnabled = os.Getenv("WAGO_ARM64_NO_INTEGER_ARGMAX_VECTOR") != "1"

type integerArgmaxPlan struct {
	pointer, index, limit, maximum, selected, temporary int
	x, y, dx, dy                                        int
}

// Infer a contiguous interleaved word scan with wrapping cross products,
// wrapping signed absolute value, and strict unsigned argmax. The temporary
// is assigned before every use; a scalar tail restores its final value.
func (f *fn) inspectIntegerArgmax(source *wasm.Reader) (p integerArgmaxPlan, ok bool) {
	r := *source
	valid := true
	op := func(want byte) {
		got, err := r.Byte()
		valid = valid && err == nil && got == want
	}
	local := func(code byte) int {
		op(code)
		x, err := r.U32()
		valid = valid && err == nil && x < uint32(f.nLocals) && f.localType[x] == mtI32
		return int(x)
	}
	same := func(code byte, want int) { valid = (local(code) == want) && valid }
	constant := func(want int32) {
		op(0x41)
		x, err := r.I32()
		valid = valid && err == nil && x == want
	}
	load := func() {
		op(0x28)
		memory, offset, err := f.readMemArg(&r)
		valid = valid && err == nil && memory == 0 && offset == 0
	}
	p.x = local(0x20)
	p.pointer = local(0x20)
	constant(-4)
	op(0x6a)
	load()
	op(0x6b)
	p.dy = local(0x20)
	op(0x6c)
	same(0x20, p.pointer)
	load()
	p.y = local(0x20)
	op(0x6b)
	p.dx = local(0x20)
	op(0x6c)
	op(0x6a)
	p.temporary = local(0x22)
	same(0x20, p.temporary)
	constant(31)
	op(0x75)
	same(0x22, p.temporary)
	op(0x73)
	same(0x20, p.temporary)
	op(0x6b)
	same(0x22, p.temporary)
	p.maximum = local(0x20)
	same(0x20, p.temporary)
	same(0x20, p.maximum)
	op(0x4b)
	same(0x22, p.temporary)
	op(0x1b)
	same(0x21, p.maximum)
	p.index = local(0x20)
	p.selected = local(0x20)
	same(0x20, p.temporary)
	op(0x1b)
	same(0x21, p.selected)
	same(0x20, p.pointer)
	constant(8)
	op(0x6a)
	same(0x21, p.pointer)
	p.limit = local(0x20)
	same(0x20, p.index)
	constant(1)
	op(0x6a)
	same(0x22, p.index)
	op(0x47)
	op(0x0d)
	depth, err := r.U32()
	valid = valid && err == nil && depth == 0
	op(0x0b)
	if !valid {
		return p, false
	}
	writes := [...]int{p.pointer, p.index, p.maximum, p.selected, p.temporary}
	for i, x := range writes {
		for _, y := range writes[:i] {
			if x == y {
				return p, false
			}
		}
		for _, y := range []int{p.x, p.y, p.dx, p.dy, p.limit} {
			if x == y {
				return p, false
			}
		}
	}
	return p, true
}

func (f *fn) emitIntegerArgmax(source *wasm.Reader) {
	if !f.opt(optIntegerArgmax) || f.usesCalls || f.preserveCallerPins || f.interruptible || f.moduleEH ||
		f.gcFrameRoots != nil || f.localBase != 0 || f.inlineDepth != 0 ||
		len(f.customInstructions) != 0 || len(f.intervalLast) != 0 ||
		f.depth() != 0 || f.unreachable || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	p, ok := f.inspectIntegerArgmax(source)
	if !ok {
		return
	}
	available := 0
	blocked := f.blockedFRegs(0)
	for _, r := range fpAllocRegs {
		if !(r >= 8 && r < 16) && !blocked.has(r) && f.fregUser[r] == nil {
			available++
		}
	}
	if available < 12 {
		return
	}
	oldPinned, oldFPinned := f.pinned, f.fpinned
	a := f.allocRegOrNone(0)
	if a == regNone {
		return
	}
	f.pinned = f.pinned.add(a)
	b := f.allocRegOrNone(0)
	defer func() {
		f.pinned, f.fpinned = oldPinned, oldFPinned
		f.release(a)
		if b != regNone {
			f.release(b)
		}
	}()
	if b == regNone {
		return
	}
	f.pinned = f.pinned.add(b)
	f.materializePendingTraps()
	inputs := [...]int{p.pointer, p.index, p.maximum, p.selected, p.limit, p.x, p.y, p.dx, p.dy}
	for _, x := range inputs {
		_, _, pin := f.pinReg(x)
		if f.localConstZero(x) {
			f.materializeZeroLocal(x, !pin)
		}
		if pin {
			f.recoverLocal(x)
		}
	}
	read := func(x int, dst Reg) {
		if r, _, pin := f.pinReg(x); pin {
			f.a.MovReg32(dst, r)
		} else {
			f.ld32(dst, SP, f.localOff(x))
		}
	}
	read(p.pointer, a)
	read(p.index, X16)
	read(p.limit, X17)
	var guards []int
	guard := func(cc Cond) { guards = append(guards, f.a.Bcond(cc)) }
	f.a.CmpReg32(X16, X17)
	guard(condAE)
	f.a.Sub32(b, X17, X16)
	f.a.CmpImm32(b, 9)
	guard(condB)
	f.a.CmpImm32(a, 4)
	guard(condB)
	// Check the complete span in 64 bits. Both source address arithmetic
	// and induction then remain nonwrapping throughout the vector prefix.
	f.a.MovReg32(X17, b)
	f.a.SubImm64(X17, X17, 1)
	f.a.LslImm64(X17, X17, 3)
	f.a.Add64(X17, X17, a)
	f.a.AddImm64(X17, X17, 4)
	if f.memSizeReg != regNone {
		f.a.CmpReg64(X17, f.memSizeReg)
	} else {
		f.ld64(X16, linMemReg, -int32(bdCurBytes))
		f.a.CmpReg64(X17, X16)
	}
	guard(condA)
	// Keep the extra vector temporaries in the caller-saved bank. V8-V15
	// retain their ordinary ABI preservation without enlarging the frame.
	for r := Reg(8); r < 16; r++ {
		f.fpinned = f.fpinned.add(r)
	}
	var vr [12]Reg
	for i := range vr {
		vr[i] = f.allocFReg(0)
		f.fpinned = f.fpinned.add(vr[i])
	}
	defer func() {
		for _, r := range vr {
			f.releaseF(r)
		}
	}()
	x, y, dx, dy, maximum, selected, indices, step, low, high, value, mask := vr[0], vr[1], vr[2], vr[3], vr[4], vr[5], vr[6], vr[7], vr[8], vr[9], vr[10], vr[11]
	for i, local := range []int{p.x, p.y, p.dx, p.dy, p.maximum, p.selected, p.index} {
		read(local, X16)
		f.a.NeonDupGprS(vr[i], X16)
	}
	f.a.MovImm64(X16, 0)
	f.a.NeonDupGprS(step, X16)
	for lane := byte(1); lane < 4; lane++ {
		f.a.MovImm64(X16, uint64(lane))
		f.a.NeonInsS(step, X16, lane)
	}
	f.a.NeonAddS(indices, indices, step)
	f.a.MovImm64(X16, 4)
	f.a.NeonDupGprS(step, X16)
	f.a.SubImm32(b, b, 1)
	f.a.LsrImm32(b, b, 2)
	f.a.SubImm64(a, a, 4)
	f.a.Add64(a, a, linMemReg)
	header := f.a.Len()
	f.a.LdrQ(low, a, 0)
	f.a.LdrQ(high, a, 16)
	f.a.NeonUzp1S(value, low, high)
	f.a.NeonUzp2S(high, low, high)
	f.a.NeonSubS(value, x, value)
	f.a.NeonSubS(high, high, y)
	f.a.NeonMulS(value, value, dy)
	f.a.NeonMlaS(value, high, dx)
	f.a.NeonAbsS(value, value)
	f.a.NeonCmhiS(mask, value, maximum)
	f.a.NeonBit16b(maximum, value, mask)
	f.a.NeonBit16b(selected, indices, mask)
	f.a.NeonAddS(indices, indices, step)
	f.a.AddImm64(a, a, 32)
	f.a.SubsImm32(b, b, 1)
	f.patchBranch19(f.a.Bcond(condNE), header)
	f.a.NeonUmaxvS(low, maximum)
	f.a.NeonDupS(value, low)
	f.a.NeonCmeqS(mask, maximum, value)
	f.a.MovImm64(X16, uint64(^uint32(0)))
	f.a.NeonDupGprS(high, X16)
	f.a.NeonBsl16b(mask, selected, high)
	f.a.NeonUminvS(high, mask)
	f.a.Sub64(a, a, linMemReg)
	f.a.AddImm32(a, a, 4)
	f.a.NeonUmovS(b, indices, 0)
	write := func(local int, reg Reg) {
		if r, _, pin := f.pinReg(local); pin {
			f.a.MovReg32(r, reg)
			f.markLocalDirty(local)
		} else {
			f.storeLocalReg(local, reg, false)
			f.locals[local].state = lsMem
		}
		f.invalidateLocalWriteFacts(local)
		f.setFactsForLocal(local, 0)
	}
	write(p.pointer, a)
	write(p.index, b)
	f.a.NeonUmovS(X16, low, 0)
	write(p.maximum, X16)
	f.a.NeonUmovS(X16, high, 0)
	write(p.selected, X16)
	for _, site := range guards {
		f.patchBranch19(site, f.a.Len())
	}
	f.invalidateGlobalsCache()
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.stats.peep("integer-argmax-vector")
}
