// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"math/bits"
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var integerByteMapEnabled = os.Getenv("WAGO_ARM64_NO_INTEGER_BYTE_MAP") != "1"
var integerQuotientRecurrenceEnabled = os.Getenv("WAGO_ARM64_NO_INTEGER_QUOTIENT_RECURRENCE") != "1"

// Two interleaved affine byte producers. Roles and constants come entirely
// from the source; the runtime relation between the producers is guarded.
type integerByteMap struct {
	even, odd, scaledEven, scaledOdd int
	offset, divisor, delta, limit    uint32
}

func (f *fn) inspectIntegerByteMap(source *wasm.Reader) (p integerByteMap, ok bool) {
	r := *source
	valid := true
	op := func(want byte) {
		got, err := r.Byte()
		valid = valid && err == nil && got == want
	}
	local := func(code byte) int {
		op(code)
		x, err := r.U32()
		if err != nil || x >= uint32(f.nLocals) || f.localType[x] != mtI32 {
			valid = false
		}
		return int(x)
	}
	constant := func() uint32 {
		op(0x41)
		x, err := r.I32()
		valid = valid && err == nil
		return uint32(x)
	}
	store := func() {
		op(0x3a)
		memory, off, err := f.readMemArg(&r)
		valid = valid && err == nil && memory == 0 && off == 0
	}
	// This fixed grammar has a bounded instruction/byte count, even when the
	// containing function has arbitrarily many following instructions.
	p.odd = local(0x20)
	oddOffset := constant()
	op(0x6a)
	p.scaledOdd = local(0x20)
	valid = (local(0x20) == p.odd) && valid
	p.divisor = constant()
	op(0x6e)
	op(0x6a)
	store()
	valid = (local(0x20) == p.odd) && valid
	p.offset = constant()
	op(0x6a)
	p.scaledEven = local(0x20)
	p.even = local(0x20)
	valid = (constant() == p.divisor) && valid
	op(0x6e)
	op(0x6a)
	store()
	update := func(x int, tee bool) uint32 {
		valid = (local(0x20) == x) && valid
		d := constant()
		op(0x6a)
		code := byte(0x21)
		if tee {
			code = 0x22
		}
		valid = (local(code) == x) && valid
		return d
	}
	p.delta = update(p.scaledEven, false)
	valid = (update(p.scaledOdd, false) == p.delta) && valid
	valid = (update(p.even, false) == 2) && valid
	valid = (update(p.odd, true) == 2) && valid
	p.limit = constant()
	op(0x47)
	op(0x0d)
	depth, err := r.U32()
	valid = valid && err == nil && depth == 0
	op(0x0b)
	roles := [...]int{p.even, p.odd, p.scaledEven, p.scaledOdd}
	for i, x := range roles {
		for _, y := range roles[:i] {
			valid = valid && x != y
		}
	}
	return p, valid && p.divisor != 0 && p.offset != ^uint32(0) && oddOffset == p.offset+1
}

// Emit a prefix before the loop entry state is captured. The original loop
// always runs its final one or two iterations, or all iterations on guard
// failure. In particular, failed range checks never eagerly trap or store.
func (f *fn) emitIntegerByteMap(source *wasm.Reader) {
	if !f.opt(optIntegerByteMap) || f.usesCalls || f.preserveCallerPins || f.interruptible || f.moduleEH ||
		f.gcFrameRoots != nil || f.localBase != 0 || f.inlineDepth != 0 ||
		len(f.customInstructions) != 0 || len(f.intervalLast) != 0 ||
		f.depth() != 0 || f.unreachable || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	p, ok := f.inspectIntegerByteMap(source)
	if !ok {
		return
	}
	// A sixteen-byte packet adds at most 16 to a remainder smaller than d.
	// d<=240 keeps that sum representable in an unsigned byte.
	byteRecurrence := f.opt(optQuotientRecurrence) && p.divisor <= 240
	available := 0
	blocked := f.blockedFRegs(0)
	for _, r := range fpAllocRegs {
		if !blocked.has(r) && f.fregUser[r] == nil {
			available++
		}
	}
	if available < 8 {
		return
	}
	oldPinned, oldFPinned := f.pinned, f.fpinned
	var gp [4]Reg
	for i := range gp {
		gp[i] = regNone
	}
	defer func() {
		f.pinned, f.fpinned = oldPinned, oldFPinned
		for _, r := range gp {
			f.release(r)
		}
	}()
	for i := range gp {
		gp[i] = f.allocRegOrNone(0)
		if gp[i] == regNone {
			return
		}
		f.pinned = f.pinned.add(gp[i])
	}
	a, b, c, d := gp[0], gp[1], gp[2], gp[3]
	roles := [...]int{p.even, p.odd, p.scaledEven, p.scaledOdd}
	for i, x := range roles {
		reg, _, pin := f.pinReg(x)
		// Both the fallback and success paths must have initialized frame homes
		// for unpinned declared-zero locals before their storage state is merged.
		if f.localConstZero(x) {
			f.materializeZeroLocal(x, !pin)
		}
		if pin {
			f.recoverLocal(x)
			f.a.MovReg32(gp[i], reg)
		} else {
			f.ld32(gp[i], SP, f.localOff(x))
		}
	}
	var guards []int
	guard := func(cc Cond) { guards = append(guards, f.a.Bcond(cc)) }
	f.a.Sub32(X16, b, a)
	f.cmpImm(X16, 1, false)
	guard(condNE)
	if byteRecurrence {
		f.cmpRR(a, b, false)
		guard(condAE) // Decline the otherwise valid even=MaxUint32,odd=0 wrap.
	}
	f.a.Sub32(d, d, c) // C = scaledOdd-scaledEven, modulo 32 bits.
	f.a.Add32(X16, d, d)
	f.a.MovImm64(X17, uint64(p.delta))
	f.cmpRR(X16, X17, false)
	guard(condNE)
	f.a.MovImm64(X16, uint64(p.limit))
	f.cmpRR(b, X16, false)
	guard(condAE)
	f.a.Sub32(X16, X16, b)
	f.cmpImm(X16, 34, false) // At least eight vector packets plus scalar tail.
	guard(condB)
	f.a.TstImm32(X16, 1)
	guard(condNE)
	// For a nonwrapping even trip, the highest touched byte plus one is
	// limit+offset. Use 64 bits: Wasm's address additions wrap at 32 bits.
	f.a.MovImm64(X17, uint64(p.limit)+uint64(p.offset))
	f.a.MovImm64(X16, 1<<32)
	f.cmpRR(X17, X16, true)
	guard(condA)
	if f.memSizeReg != regNone {
		f.cmpRR(X17, f.memSizeReg, true)
	} else {
		f.ld64(X16, linMemReg, -int32(bdCurBytes))
		f.cmpRR(X17, X16, true)
	}
	guard(condA)
	var vr [8]Reg
	for i := range vr {
		vr[i] = f.allocFReg(0)
		f.fpinned = f.fpinned.add(vr[i])
	}
	defer func() {
		for _, r := range vr {
			f.releaseF(r)
		}
	}()
	n, scaled := vr[0], vr[1]
	if byteRecurrence {
		f.emitByteMapRecurrence(p, gp, vr)
	} else {
		magic, lo, hi, q, incN, incScaled := vr[2], vr[3], vr[4], vr[5], vr[6], vr[7]
		f.a.MovImm64(X16, 0)
		f.a.NeonDupGprS(n, X16)
		for lane := byte(1); lane < 4; lane++ {
			f.a.MovImm64(X16, uint64(lane))
			f.a.NeonInsS(n, X16, lane)
		}
		f.a.NeonDupGprS(scaled, d)
		f.a.NeonMulS(scaled, scaled, n)
		f.a.NeonDupGprS(q, c)
		f.a.NeonAddS(scaled, scaled, q)
		f.a.NeonDupGprS(q, a)
		f.a.NeonAddS(n, n, q)
		f.a.MovImm64(X16, 4)
		f.a.NeonDupGprS(incN, X16)
		f.a.MovImm64(X16, uint64(uint32(2*p.delta)))
		f.a.NeonDupGprS(incScaled, X16)
		m, shift, correction := railshot.MagicU(uint64(p.divisor), 32)
		f.a.MovImm64(X16, uint64(uint32(m)))
		f.a.NeonDupGprS(magic, X16)
		// b becomes packet count; a becomes the native output pointer.
		f.a.MovReg32(a, b)
		f.a.MovImm64(X16, uint64(p.limit))
		f.a.Sub32(b, X16, b)
		f.a.LsrImm32(b, b, 1)
		f.a.SubImm32(b, b, 1)
		f.a.LsrImm32(b, b, 1)
		f.a.MovImm64(X16, uint64(p.offset))
		f.a.Add64(a, a, X16)
		f.a.Add64(a, a, linMemReg)
		loop := f.a.Len()
		if p.divisor&(p.divisor-1) == 0 {
			if p.divisor == 1 {
				f.a.NeonOrr16b(q, n, n)
			} else {
				f.a.NeonUshrS(q, n, uint8(bits.TrailingZeros32(p.divisor)))
			}
		} else {
			f.a.NeonUmullDfromS(lo, n, magic)
			f.a.NeonUmull2DfromS(hi, n, magic)
			// Each widening product has alternating low/high 32-bit words.
			// Unzip their odd lanes to collect all four multiply-high results.
			f.a.NeonUzp2S(q, lo, hi)
			if correction {
				f.a.NeonSubS(lo, n, q)
				f.a.NeonUshrS(lo, lo, 1)
				f.a.NeonAddS(q, q, lo)
			}
			if shift != 0 {
				f.a.NeonUshrS(q, q, uint8(shift))
			}
		}
		f.a.NeonAddS(q, q, scaled)
		f.a.NeonXtnHfromS(q, q)
		f.a.NeonXtnBfromH(q, q)
		f.a.StrF(a, 0, q, false)
		f.a.NeonAddS(n, n, incN)
		f.a.NeonAddS(scaled, scaled, incScaled)
		f.a.AddImm64(a, a, 4)
		f.a.SubsImm32(b, b, 1)
		f.patchBranch19(f.a.Bcond(condNE), loop)
	}
	for i, x := range roles {
		v, lane := n, byte(i&1)
		if i >= 2 {
			v = scaled
		}
		if byteRecurrence {
			f.a.MovReg32(X16, gp[i])
		} else {
			f.a.NeonUmovS(X16, v, lane)
		}
		if reg, _, pin := f.pinReg(x); pin {
			f.a.MovReg32(reg, X16)
			f.markLocalDirty(x)
		} else {
			f.storeLocalReg(x, X16, false)
			f.locals[x].state = lsMem
		}
		f.invalidateLocalWriteFacts(x)
		f.setFactsForLocal(x, 0)
	}
	for _, branch := range guards {
		f.patchBranch19(branch, f.a.Len())
	}
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.stats.peep("integer-byte-map-vector")
}
