// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var floatStorePacketsEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_STORE_PACKETS") != "1"

const floatStoreWorkLimit = 1024

type floatStorePacket struct {
	nodes                                 [32]shared.LoopExpression
	stores                                [2]shared.LoopStore[int]
	n, start, end, base, peak, arithmetic int
	offset                                uint64
	loads                                 [2]int
}

func (f *fn) floatStorePacketEligible() bool {
	return floatStorePacketsEnabled && !profileEnabled && !f.usesCalls && !f.preserveCallerPins && !f.interruptible && !f.moduleEH && !f.tracksGCFrameRoots() && len(f.customInstructions) == 0 && f.localBase == 0 && f.inlineDepth == 0 && f.selectGroupEnd == 0 && !f.streamingReduction && !f.threadedMemory0 && f.s.head.prev == sentinelNodeID && f.floatStoreWork < floatStoreWorkLimit
}

func (f *fn) tryFloatStorePacket(source *wasm.Reader) (bool, error) {
	// Keep the ordinary empty-stack local.get path independent of plan storage.
	r := *source
	index, e := r.U32()
	if e != nil || index >= uint32(f.nLocals) || f.localType[index] != mtI32 {
		return false, nil
	}
	if op, ok := r.Peek(); !ok || op != 0x20 {
		return false, nil
	}
	return f.emitFloatStoreCandidate(source)
}

func (f *fn) inspectFloatStorePacket(source *wasm.Reader) (p floatStorePacket, ok bool) {
	r := *source
	p.start = r.Offset() - 1
	if r.JumpTo(p.start) != nil {
		return p, false
	}
	var stack [12]int
	depth, stores := 0, 0
	push := func(v shared.LoopExpression) bool {
		if depth == len(stack) || p.n == len(p.nodes) {
			return false
		}
		p.nodes[p.n] = v
		stack[depth] = p.n
		p.n++
		depth++
		return true
	}
	for stores < 2 && r.Offset()-p.start < 192 && f.floatStoreWork < floatStoreWorkLimit {
		f.floatStoreWork++
		pc := r.Offset()
		op, e := r.Byte()
		if e != nil {
			return p, false
		}
		switch op {
		case 0x20:
			index, e := r.U32()
			if e != nil || index >= uint32(f.nLocals) {
				return p, false
			}
			typ := wasm.I32
			if f.localType[index] == mtF64 {
				typ = wasm.F64
			} else if f.localType[index] != mtI32 {
				return p, false
			}
			if !push(shared.LoopExpression{Op: op, Type: typ, Bits: uint64(index), PC: uint32(pc)}) {
				return p, false
			}
		case 0x2b:
			memory, offset, e := f.readMemArg(&r)
			if e != nil || memory != 0 || f.memoryAddr64(0) || depth < 1 {
				return p, false
			}
			addr := stack[depth-1]
			a := p.nodes[addr]
			if a.Op != 0x20 || a.Type != wasm.I32 {
				return p, false
			}
			depth--
			if !push(shared.LoopExpression{Op: op, Type: wasm.F64, Left: addr, Bits: offset, PC: uint32(pc)}) {
				return p, false
			}
			p.loads[stores]++
			if p.loads[stores] != 1 {
				return p, false
			}
		case 0xa0, 0xa1, 0xa2, 0xa3:
			if depth < 2 {
				return p, false
			}
			a, b := stack[depth-2], stack[depth-1]
			if p.nodes[a].Type != wasm.F64 || p.nodes[b].Type != wasm.F64 {
				return p, false
			}
			depth -= 2
			if !push(shared.LoopExpression{Op: op, Type: wasm.F64, Left: a, Right: b, PC: uint32(pc)}) {
				return p, false
			}
		case 0x39:
			memory, offset, e := f.readMemArg(&r)
			if e != nil || memory != 0 || f.memoryAddr64(0) || depth != 2 || p.loads[stores] != 1 {
				return p, false
			}
			addr, value := stack[0], stack[1]
			a := p.nodes[addr]
			if a.Op != 0x20 || a.Type != wasm.I32 || p.nodes[value].Type != wasm.F64 {
				return p, false
			}
			if stores == 0 {
				p.base = int(a.Bits)
				p.offset = offset
			} else if p.base != int(a.Bits) || offset != p.offset+8 {
				return p, false
			}
			p.stores[stores] = shared.LoopStore[int]{Address: addr, Value: value, Offset: offset, PC: uint32(pc)}
			stores++
			depth = 0
		default:
			return p, false
		}
	}
	if stores != 2 || r.Offset()-p.start > 192 {
		return p, false
	}
	p.end = r.Offset()
	// A regional pointer must already be resident. No new cache activation may
	// be required only on the scalar path after the paths have diverged.
	if len(f.intervalLast) != 0 {
		if _, _, pinned := f.pinReg(p.base); !pinned {
			return p, false
		}
	}
	var match func(int, int, int) (int, bool)
	match = func(a, b, level int) (int, bool) {
		if level > 6 {
			return 0, false
		}
		x, y := p.nodes[a], p.nodes[b]
		if x.Op != y.Op || x.Type != wasm.F64 || y.Type != wasm.F64 {
			return 0, false
		}
		switch x.Op {
		case 0x20:
			for _, v := range []shared.LoopExpression{x, y} {
				_, fp, pin := f.pinReg(int(v.Bits))
				if !pin || !fp {
					return 0, false
				}
			}
			return 1, true
		case 0x2b:
			if int(p.nodes[x.Left].Bits) != p.base || int(p.nodes[y.Left].Bits) != p.base || x.Bits != p.offset || y.Bits != p.offset+8 {
				return 0, false
			}
			return 1, true
		case 0xa0, 0xa1, 0xa2, 0xa3:
			p.arithmetic++
			lp, ok := match(x.Left, y.Left, level+1)
			if !ok {
				return 0, false
			}
			if x.Op == 0xa2 && p.uniformLocal(x.Right, y.Right) {
				_, fp, pinned := f.pinReg(int(p.nodes[x.Right].Bits))
				return lp, fp && pinned
			}
			rp, ok := match(x.Right, y.Right, level+1)
			return max(lp, 1+rp), ok
		}
		return 0, false
	}
	p.peak, ok = match(p.stores[0].Value, p.stores[1].Value, 0)
	// At least three scalar operations per lane amortize packet construction.
	return p, ok && p.arithmetic >= 3 && p.peak <= 6
}

func (p *floatStorePacket) uniformLocal(a, b int) bool {
	x, y := p.nodes[a], p.nodes[b]
	return x.Op == 0x20 && y.Op == 0x20 && x.Type == wasm.F64 && y.Type == wasm.F64 && x.Bits == y.Bits
}

func (f *fn) emitFloatStoreCandidate(source *wasm.Reader) (bool, error) {
	p, ok := f.inspectFloatStorePacket(source)
	if !ok {
		return false, nil
	}
	freeGP := 0
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range gpAlloc {
		if !blocked.has(r) && f.regUser[r] == nil {
			freeGP++
		}
	}
	// Scalar replay may preserve an old address at an exact-offset final get.
	// Four free GPs cover those source operations without regional eviction.
	if freeGP < 4 {
		return false, nil
	}
	var pool [6]Reg
	np := 0
	fb := f.blockedFRegs(0)
	for _, r := range fpAllocRegs {
		if !fb.has(r) && f.fregUser[r] == nil {
			pool[np] = r
			np++
			if np == p.peak {
				break
			}
		}
	}
	if np < p.peak {
		return false, nil
	}
	oldPins, oldFPins := f.pinned, f.fpinned
	a := f.allocRegOrNone(0)
	if a == regNone {
		return false, nil
	}
	f.pinned = f.pinned.add(a)
	b := f.allocRegOrNone(0)
	if b == regNone {
		f.pinned = oldPins
		return false, nil
	}
	f.pinned = f.pinned.add(b)
	for _, r := range pool[:np] {
		f.fpinned = f.fpinned.add(r)
	}
	f.invalidateStoreForward()
	f.flush()
	f.reconcileLocals()
	oldPins = f.pinned.remove(a).remove(b)
	for _, n := range p.nodes[:p.n] {
		if n.Op == 0x20 {
			f.recoverLocal(int(n.Bits))
		}
	}
	if r, _, pin := f.pinReg(p.base); pin {
		f.a.MovReg32(a, r)
	} else if f.localConstZero(p.base) {
		f.a.MovImm32(a, 0)
	} else {
		f.ld32(a, SP, f.localOff(p.base))
	}
	if !f.addFoldImm3(b, a, int64(p.offset+16), true) {
		f.a.MovImm64(b, p.offset+16)
		f.a.Add64(b, a, b)
	}
	if f.memSizeReg != regNone {
		f.a.CmpReg64(b, f.memSizeReg)
	} else {
		f.ld64(X16, linMemReg, -int32(bdCurBytes))
		f.a.CmpReg64(b, X16)
	}
	fallback := f.a.Bcond(condA)
	f.a.SubImm64(b, b, 16)
	f.a.Add64(a, linMemReg, b)
	used := [6]bool{}
	take := func() Reg {
		for i := 0; i < np; i++ {
			if !used[i] {
				used[i] = true
				return pool[i]
			}
		}
		panic("float store packet pressure mismatch")
	}
	put := func(r Reg) {
		for i := 0; i < np; i++ {
			if pool[i] == r {
				used[i] = false
				return
			}
		}
	}
	var emit func(int, int) Reg
	emit = func(ai, bi int) Reg {
		x, y := p.nodes[ai], p.nodes[bi]
		switch x.Op {
		case 0x20:
			r := take()
			s0, _, _ := f.pinReg(int(x.Bits))
			s1, _, _ := f.pinReg(int(y.Bits))
			if x.Bits == y.Bits {
				f.a.NeonDupD(r, s0)
			} else {
				f.a.FmovReg(r, s0, true)
				f.a.NeonInsLaneD(r, 1, s1)
			}
			return r
		case 0x2b:
			r := take()
			f.a.LdrQ(r, a, 0)
			return r
		}
		left := emit(x.Left, y.Left)
		if x.Op == 0xa2 && p.uniformLocal(x.Right, y.Right) {
			r, _, _ := f.pinReg(int(p.nodes[x.Right].Bits))
			f.a.NeonFmulLaneD(left, left, r, 0)
			f.stats.peep("float-store-packet-lane-mul")
			return left
		}
		right := emit(x.Right, y.Right)
		switch x.Op {
		case 0xa0:
			f.a.NeonFadd(left, left, right, true)
		case 0xa1:
			f.a.NeonFsub(left, left, right, true)
		case 0xa2:
			f.a.NeonFmul(left, left, right, true)
		case 0xa3:
			f.a.NeonFdiv(left, left, right, true)
		}
		put(right)
		return left
	}
	result := emit(p.stores[0].Value, p.stores[1].Value)
	f.a.StrQ(a, 0, result)
	join := f.a.Branch()
	f.patchBranch19(fallback, f.a.Len())
	f.pinned, f.fpinned = oldPins, oldFPins
	f.release(a)
	f.release(b)
	for _, r := range pool[:np] {
		f.releaseF(r)
	}
	// Source replay retains ordinary traps and all original partial stores.
	r := *source
	if e := r.JumpTo(p.start); e != nil {
		return true, e
	}
	previousPC := f.wasmPC
	for r.Offset() < p.end {
		pos := r.Offset()
		op, e := r.Byte()
		if e != nil {
			return true, e
		}
		f.wasmPC = f.tracePCBase + uint32(pos)
		f.prepareStoreForward(op)
		if e = f.emitPlain(&r, op); e != nil {
			return true, e
		}
	}
	f.wasmPC = previousPC
	f.patchBranch26(join, f.a.Len())
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.invalidateGlobalsCache()
	f.frameForwardEnd, f.wideReciprocalEnd = 0, 0
	*source = r
	f.stats.peep("float-store-packet")
	return true, nil
}
