// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Recognize two contiguous i32 signed-min updates with one invariant memory
// operand. The fixed-size scan admits no other memory or control effects.
type minPlusPlan struct {
	counter, dst, src, pivot int
	limit                    uint32
	scalarReloadPivot        bool
}

func (f *fn) inspectMinPlusLoop(r *wasm.Reader) (p minPlusPlan, ok bool) {
	if f.nLocals > 32 {
		return
	}
	for _, t := range f.localType {
		if t != mtI32 {
			return
		}
	}
	var nodes [128]dotNode
	var third [128]int
	var values [32]int
	var written, initialRead [32]bool
	var stack [24]int
	var stores [2][2]int
	var storeAfter [2]int
	n, depth, ns, loads := f.nLocals, 0, 0, 0
	for i := 0; i < n; i++ {
		nodes[i] = dotNode{op: 0x20, a: i}
		values[i] = i
	}
	push := func(x int) bool {
		if depth == len(stack) {
			return false
		}
		stack[depth] = x
		depth++
		return true
	}
	node := func(v dotNode) int {
		if n == len(nodes) {
			return -1
		}
		nodes[n] = v
		n++
		return n - 1
	}
	scan := *r
	start := scan.Offset()
	for scan.Offset()-start < 384 {
		op, e := scan.Byte()
		if e != nil {
			return p, false
		}
		switch op {
		case 0x20, 0x21, 0x22:
			x, e := scan.U32()
			if e != nil || x >= uint32(f.nLocals) {
				return p, false
			}
			if op == 0x20 {
				if values[x] == int(x) {
					initialRead[x] = true
				}
				if !push(values[x]) {
					return p, false
				}
			} else {
				if depth == 0 {
					return p, false
				}
				values[x] = stack[depth-1]
				written[x] = true
				if op == 0x21 {
					depth--
				}
			}
		case 0x41:
			v, e := scan.I32()
			if e != nil {
				return p, false
			}
			x := node(dotNode{op: op, v: uint32(v)})
			if x < 0 || !push(x) {
				return p, false
			}
		case 0x6a, 0x47, 0x4a:
			if depth < 2 {
				return p, false
			}
			depth -= 2
			x := node(dotNode{op: op, a: stack[depth], b: stack[depth+1]})
			if x < 0 || !push(x) {
				return p, false
			}
		case 0x1b:
			if depth < 3 {
				return p, false
			}
			depth -= 3
			x := node(dotNode{op: op, a: stack[depth], b: stack[depth+1]})
			if x < 0 {
				return p, false
			}
			third[x] = stack[depth+2]
			if !push(x) {
				return p, false
			}
		case 0x28, 0x36:
			mem, off, e := f.readMemArg(&scan)
			if e != nil || mem != 0 || off > 16 {
				return p, false
			}
			if op == 0x28 {
				if depth == 0 {
					return p, false
				}
				x := node(dotNode{op: op, a: stack[depth-1], v: uint32(off)})
				if x < 0 {
					return p, false
				}
				stack[depth-1] = x
				loads++
			} else {
				if depth < 2 || ns == 2 || off != 0 {
					return p, false
				}
				depth -= 2
				stores[ns] = [2]int{stack[depth], stack[depth+1]}
				storeAfter[ns] = n
				ns++
			}
		case 0x0d:
			label, e := scan.U32()
			if e != nil || label != 0 || depth != 1 || ns != 2 || loads != 6 {
				return p, false
			}
			end, e := scan.Byte()
			if e != nil || end != 0x0b {
				return p, false
			}
			c := nodes[stack[0]]
			if c.op != 0x47 {
				return p, false
			}
			inc, lim := c.a, c.b
			if nodes[lim].op != 0x41 {
				inc, lim = lim, inc
			}
			if nodes[lim].op != 0x41 || nodes[inc].op != 0x6a {
				return p, false
			}
			v := nodes[inc]
			local, step := v.a, v.b
			if nodes[local].op != 0x20 {
				local, step = step, local
			}
			if nodes[local].op != 0x20 || nodes[step].op != 0x41 || nodes[step].v != 8 {
				return p, false
			}
			p.counter, p.limit = nodes[local].a, nodes[lim].v
			if values[p.counter] != inc || !written[p.counter] || p.limit < 24 || p.limit > 1<<30 || p.limit%8 != 0 {
				return p, false
			}
			var usedLoads [128]bool
			for i := 0; i < 2; i++ {
				dst, off, yes := affineDotAddress(nodes[:n], stores[i][0], p.counter)
				if !yes || off != uint32(i*4) {
					return p, false
				}
				sel := nodes[stores[i][1]]
				if sel.op != 0x1b {
					return p, false
				}
				old, sum := sel.a, sel.b
				cmp := nodes[third[stores[i][1]]]
				if cmp.op != 0x4a || cmp.a != sum || cmp.b != old {
					return p, false
				}
				load := nodes[old]
				if load.op != 0x28 || load.v != 0 || load.a != stores[i][0] {
					return p, false
				}
				add := nodes[sum]
				if add.op != 0x6a {
					return p, false
				}
				for _, x := range [3]int{old, add.a, add.b} {
					if usedLoads[x] {
						return p, false
					}
					usedLoads[x] = true
				}
				srcLoad, pivotLoad := nodes[add.a], nodes[add.b]
				if srcLoad.op != 0x28 || pivotLoad.op != 0x28 || pivotLoad.v != 0 {
					return p, false
				}
				src, off, yes := affineDotAddress(nodes[:n], srcLoad.a, p.counter)
				if !yes || off+srcLoad.v != uint32(i*4) {
					return p, false
				}
				if i == 1 {
					// Node order records whether the second pivot load occurs
					// after the first store, as the generated scalar packet does.
					p.scalarReloadPivot = add.b >= storeAfter[0]
				}
				pivot := nodes[pivotLoad.a]
				if pivot.op != 0x20 {
					return p, false
				}
				if i == 0 {
					p.dst, p.src, p.pivot = dst, src, pivot.a
				} else if dst != p.dst || src != p.src || pivot.a != p.pivot {
					return p, false
				}
			}
			if written[p.dst] || written[p.src] || written[p.pivot] {
				return p, false
			}
			for i := 0; i < f.nLocals; i++ {
				if written[i] && initialRead[i] && i != p.counter {
					return p, false
				}
			}
			return p, true
		default:
			return p, false
		}
	}
	return p, false
}

// Run a guarded vector prefix, then the original scalar tail. Executing the
// tail restores every source temporary, including values observable after the
// loop. Failed guards leave all locals and memory untouched.
func (f *fn) emitMinPlusLoop(r *wasm.Reader) bool {
	if !f.opt(optDotLoopVector) || f.dotLoop != nil || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.memoryAddr64(0) || f.threadedMemory0 {
		return false
	}
	p, ok := f.inspectMinPlusLoop(r)
	if !ok {
		return false
	}
	oldPinned, oldFPinned := f.pinned, f.fpinned
	alloc := func() Reg {
		x := f.allocRegOrNone(0)
		if x != regNone {
			f.pinned = f.pinned.add(x)
		}
		return x
	}
	d, s, pivot, counter, size := alloc(), alloc(), alloc(), alloc(), alloc()
	regs := []Reg{d, s, pivot, counter, size}
	for _, x := range regs {
		if x == regNone {
			f.pinned = oldPinned
			for _, v := range regs {
				if v != regNone {
					f.release(v)
				}
			}
			return false
		}
	}
	read := func(local int, dst Reg) {
		if reg, _, pin := f.pinReg(local); pin {
			f.recoverLocal(local)
			f.a.MovReg32(dst, reg)
		} else if f.locals[local].state == lsConstZero {
			f.a.MovImm64(dst, 0)
		} else {
			f.ld32(dst, SP, f.localOff(local))
		}
	}
	read(p.dst, d)
	read(p.src, s)
	read(p.pivot, pivot)
	read(p.counter, counter)
	f.ld64(size, linMemReg, -int32(bdCurBytes))
	var guards [12]int
	ng := 0
	guard := func(cc Cond) { guards[ng] = f.a.Bcond(cc); ng++ }
	compare := func(limit uint32) {
		if f.fitsAddSubImmediate(int64(limit)) {
			f.cmpImmS(counter, int64(limit), false)
		} else {
			f.a.MovImm64(X16, uint64(limit))
			f.cmpRR(counter, X16, false)
		}
	}
	compare(p.limit - 24)
	guard(condA)
	f.a.TstImm32(counter, 7)
	guard(condNE)
	// Check complete ranges in 64 bits, including the memory32 wrap boundary.
	for _, base := range []Reg{d, s} {
		f.a.MovImm64(X16, uint64(p.limit))
		f.a.Add64(X16, base, X16)
		f.cmpRR(X16, size, true)
		guard(condA)
		f.a.MovImm64(X17, 1<<32)
		f.cmpRR(X16, X17, true)
		guard(condA)
	}
	f.a.AddImm64(X16, pivot, 4)
	f.cmpRR(X16, size, true)
	guard(condA)
	// Equal rows are safe. Subtracting one from the absolute distance
	// wraps zero above the unsigned overlap range.
	f.a.Subs64(X16, d, s)
	f.a.Sub64(X17, s, d)
	f.a.Csel64(X16, X16, X17, condAE)
	f.a.SubImm64(X16, X16, 1)
	f.a.MovImm64(X17, uint64(p.limit-1))
	f.cmpRR(X16, X17, true)
	guard(condB)
	f.a.Add64(d, d, counter)
	f.a.Add64(d, d, linMemReg)
	f.a.Add64(s, s, counter)
	f.a.Add64(s, s, linMemReg)
	f.a.Add64(pivot, pivot, linMemReg)
	vec := func() Reg { x := f.allocFReg(0); f.fpinned = f.fpinned.add(x); return x }
	vd, vs, vp := vec(), vec(), vec()
	f.a.Load32(X16, pivot, 0)
	f.a.NeonDupGprS(vp, X16)
	loop := f.a.Len()
	// Leave the final eight bytes to the original scalar loop. Stop before
	// the pivot unless its entire four-byte value lies behind the destination.
	// Recompute this bound after a scalar pivot-overlap packet.
	f.a.MovImm64(X16, uint64(p.limit-8))
	f.a.Sub64(X16, X16, counter)
	f.a.Add64(size, d, X16)
	f.a.AddImm64(X16, pivot, 3)
	f.cmpRR(X16, d, true)
	pivotBehind := f.a.Bcond(condB)
	f.cmpRR(pivot, size, true)
	pivotAhead := f.a.Bcond(condAE)
	// A word-aligned overlap can stay vectorized if the scalar signed-min
	// update leaves the pivot unchanged. The row guards make its source word
	// stable until that update, even when the two rows are identical.
	f.a.Sub64(X16, pivot, d)
	f.a.TstImm32(X16, 3)
	partialPivot := f.a.Bcond(condNE)
	f.a.LdrIdx(X16, s, X16, 4, false, false)
	f.a.Load32(X17, pivot, 0)
	f.a.Add32(X16, X16, X17)
	f.cmpRR(X16, X17, false)
	stablePivot := f.a.Bcond(condGE)
	f.patchBranch19(partialPivot, f.a.Len())
	f.a.MovReg64(size, pivot)
	for _, branch := range []int{pivotBehind, pivotAhead, stablePivot} {
		f.patchBranch19(branch, f.a.Len())
	}
	// Compare the destination directly against the last legal packet start.
	// Materialize the bound once per packet width.
	comparePacket := func() {
		f.cmpRR(d, size, true)
	}
	emitVector := func(bytes uint32) {
		packetStart := f.a.Len()
		f.a.LdrQ(vd, d, 0)
		f.a.LdrQPost(vs, s, int32(bytes))
		f.a.NeonAddS(vs, vs, vp)
		f.a.NeonSminS(vd, vd, vs)
		f.a.StrQPost(d, int32(bytes), vd)
		for off := 16 - int32(bytes); off < 0; off += 16 {
			f.a.LdrQ(vd, d, off)
			f.a.LdrQ(vs, s, off)
			f.a.NeonAddS(vs, vs, vp)
			f.a.NeonSminS(vd, vd, vs)
			f.a.StrQ(d, off, vd)
		}
		f.a.AddImm32(counter, counter, bytes)
		comparePacket()
		f.patchBranch19(f.a.Bcond(condBE), packetStart)
	}
	// Require four wider iterations to amortize the extra code and tail dispatch.
	packetBytes := uint32(32)
	if p.limit >= 4*64+8 {
		packetBytes = 64
	}
	if p.limit >= 4*packetBytes+8 {
		// Amortize loop and alias checks over several SIMD blocks without adding
		// registers. Keep one original packet for the observable scalar tail.
		f.a.SubImm64(size, size, packetBytes)
		comparePacket()
		tail := f.a.Bcond(condA)
		emitVector(packetBytes)
		f.stats.peep("minplus-loop-unroll")
		f.patchBranch19(tail, f.a.Len())
		f.a.AddImm64(size, size, packetBytes-16)
	} else {
		f.a.SubImm64(size, size, 16)
	}
	comparePacket()
	noPacket := f.a.Bcond(condA)
	emitVector(16)
	f.patchBranch19(noPacket, f.a.Len())
	var done int
	if p.scalarReloadPivot {
		compare(p.limit - 24)
		done = f.a.Bcond(condA)
		// This packet is safe only when the source reloads the pivot between
		// stores. Otherwise overlap resumes the original body below.
		for off := uint32(0); off < 8; off += 4 {
			f.a.Load32(X16, s, off)
			f.a.Load32(X17, pivot, 0)
			f.a.Add32(X16, X16, X17)
			f.a.Load32(X17, d, off)
			f.cmpRR(X16, X17, false)
			f.a.Csel32(X16, X16, X17, condL)
			f.a.Store32(X16, d, off)
		}
		f.a.AddImm64(d, d, 8)
		f.a.AddImm64(s, s, 8)
		f.a.AddImm32(counter, counter, 8)
		f.a.Load32(X16, pivot, 0)
		f.a.NeonDupGprS(vp, X16)
		f.patchBranch26(f.a.Branch(), loop)
	}
	// Both a completed prefix and pivot overlap resume at the current counter.
	if p.scalarReloadPivot {
		f.patchBranch19(done, f.a.Len())
	}
	if reg, _, pin := f.pinReg(p.counter); pin {
		f.a.MovReg32(reg, counter)
	} else {
		f.st64(SP, f.localOff(p.counter), counter)
	}
	for i := 0; i < ng; i++ {
		f.patchBranch19(guards[i], f.a.Len())
	}
	f.pinned, f.fpinned = oldPinned, oldFPinned
	for _, x := range regs {
		f.release(x)
	}
	for _, x := range []Reg{vd, vs, vp} {
		f.releaseF(x)
	}
	f.setFactsForLocal(p.counter, 0)
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.stats.peep("minplus-loop-vector")
	return true
}
