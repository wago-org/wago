// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"bytes"
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var popcountLoopSummaryEnabled = os.Getenv("WAGO_ARM64_NO_POPCOUNT_LOOP_SUMMARY") != "1"

const (
	popcountLoopCells = 8
	popcountLoopStack = 4
)

type popcountLoopCell struct {
	index   uint32
	delta   uint32
	written bool
	pop     bool
}

type popcountLoopPlan struct {
	cells [popcountLoopCells]popcountLoopCell
	n     int
	pop   int
	end   int
}

type popcountLoopValue struct {
	kind uint8
	cell int
	bias uint32
}

const (
	popcountValueConst = iota
	popcountValueAffine
	popcountValueNext
)

func (f *fn) popcountLoopCell(p *popcountLoopPlan, index uint32) int {
	for i := 0; i < p.n; i++ {
		if p.cells[i].index == index {
			return i
		}
	}
	if p.n == len(p.cells) || int(index) >= f.nLocals || f.localType[index] != mtI32 {
		return -1
	}
	i := p.n
	p.n++
	p.cells[i].index = index
	return i
}

// inspectPopcountLoop recognizes the closed i32 recurrence x' = x & (x-1)
// together with independent affine updates a' = a + constant. Update order and
// commutative operand order are immaterial; unsupported effects or cross-local
// dependencies reject the candidate without changing the source reader.
func (f *fn) inspectPopcountLoop(source *wasm.Reader, p *popcountLoopPlan) bool {
	r := *source
	start := r.Offset()
	*p = popcountLoopPlan{pop: -1}
	var stack [popcountLoopStack]popcountLoopValue
	depth := 0
	push := func(v popcountLoopValue) bool {
		if depth == len(stack) {
			return false
		}
		stack[depth] = v
		depth++
		return true
	}
	for ops := 0; ops < 64 && r.Offset()-start < 256; ops++ {
		if !f.chargeAffineLoop(1) {
			return false
		}
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch op {
		case 0x01: // nop
		case 0x20: // local.get
			index, err := r.U32()
			if err != nil {
				return false
			}
			cell := f.popcountLoopCell(p, index)
			if cell < 0 {
				return false
			}
			c := p.cells[cell]
			v := popcountLoopValue{kind: popcountValueAffine, cell: cell, bias: c.delta}
			if c.pop {
				v.kind = popcountValueNext
			}
			if !push(v) {
				return false
			}
		case 0x41: // i32.const
			value, err := r.I32()
			if err != nil || !push(popcountLoopValue{kind: popcountValueConst, cell: -1, bias: uint32(value)}) {
				return false
			}
		case 0x6a, 0x6b: // i32.add/sub
			if depth < 2 {
				return false
			}
			a, b := stack[depth-2], stack[depth-1]
			var v popcountLoopValue
			switch {
			case a.kind == popcountValueConst && b.kind == popcountValueConst:
				v = a
				if op == 0x6a {
					v.bias += b.bias
				} else {
					v.bias -= b.bias
				}
			case a.kind == popcountValueAffine && b.kind == popcountValueConst:
				v = a
				if op == 0x6a {
					v.bias += b.bias
				} else {
					v.bias -= b.bias
				}
			case op == 0x6a && a.kind == popcountValueConst && b.kind == popcountValueAffine:
				v = b
				v.bias += a.bias
			default:
				return false
			}
			depth--
			stack[depth-1] = v
		case 0x71: // i32.and
			if depth < 2 {
				return false
			}
			a, b := stack[depth-2], stack[depth-1]
			if a.kind != popcountValueAffine || b.kind != popcountValueAffine || a.cell != b.cell || p.cells[a.cell].written ||
				!((a.bias == 0 && b.bias == ^uint32(0)) || (b.bias == 0 && a.bias == ^uint32(0))) {
				return false
			}
			depth--
			stack[depth-1] = popcountLoopValue{kind: popcountValueNext, cell: a.cell}
		case 0x21, 0x22: // local.set/tee
			index, err := r.U32()
			if err != nil || depth == 0 {
				return false
			}
			cell := f.popcountLoopCell(p, index)
			v := stack[depth-1]
			if cell < 0 || v.cell != cell {
				return false
			}
			switch v.kind {
			case popcountValueAffine:
				if p.cells[cell].pop {
					return false
				}
				p.cells[cell].delta = v.bias
				p.cells[cell].written = true
			case popcountValueNext:
				if p.pop >= 0 && p.pop != cell {
					return false
				}
				p.cells[cell].written = true
				p.cells[cell].pop = true
				p.pop = cell
			default:
				return false
			}
			if op == 0x21 {
				depth--
			}
		case 0x1a: // drop
			if depth == 0 {
				return false
			}
			depth--
		case 0x0d: // br_if 0; end
			label, err := r.U32()
			if err != nil || label != 0 || depth != 1 || p.pop < 0 || stack[0].kind != popcountValueNext || stack[0].cell != p.pop {
				return false
			}
			end, yes := r.Peek()
			if !yes || end != 0x0b || r.Offset()-start > 256 {
				return false
			}
			p.end = r.Offset()
			return true
		default:
			return false
		}
	}
	return false
}

func (f *fn) readPopcountLoopLocal(index int, target Reg) {
	if f.localConstZero(index) {
		f.a.MovImm32(target, 0)
	} else if reg, _, pinned := f.pinReg(index); pinned {
		f.recoverLocal(index)
		if reg != target {
			f.a.MovReg32(target, reg)
		}
	} else {
		f.ld32(target, SP, f.localOff(index))
	}
}

// emitPopcountLoopSummary replaces a proved post-test recurrence with one
// scalar population count and closed affine updates. No runtime guard or scalar
// fallback is needed because the candidate contains no effects other than its
// local assignments.
func (f *fn) emitPopcountLoopSummary(source *wasm.Reader) bool {
	if !popcountLoopSummaryEnabled || profileEnabled || f.unreachable || f.interruptible || f.moduleEH || f.localBase != 0 || f.inlineDepth != 0 ||
		f.nLocals > 64 || f.depth() != 0 || f.tracksGCFrameRoots() || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.affineLoopWork >= affineLoopWorkLimit {
		return false
	}
	// A byte screen keeps the ordinary miss below the full symbolic parser.
	probe := *source
	n := probe.BytesLeft()
	if n > 96 {
		n = 96
	}
	b, err := probe.Bytes(n)
	if err != nil || bytes.IndexByte(b, 0x71) < 0 {
		return false
	}
	var p popcountLoopPlan
	if !f.inspectPopcountLoop(source, &p) {
		return false
	}
	pop := int(p.cells[p.pop].index)
	count := f.allocReg(0)
	oldPins := f.pinned
	f.pinned = f.pinned.add(count)
	f.readPopcountLoopLocal(pop, count)
	v := f.allocFReg(0)
	f.a.FmovFromGpr(v, count, false)
	f.a.Cnt8b(v, v)
	f.a.Addv8b(v, v)
	f.a.FmovToGpr(count, v, false)
	f.releaseF(v)
	// Wasm loop is post-tested: an entry value of zero still executes once.
	f.a.CmpImm32(count, 0)
	f.a.Csinc32(count, count, ZR, condNE)

	for i := 0; i < p.n; i++ {
		c := p.cells[i]
		if !c.written || c.pop {
			continue
		}
		x := int(c.index)
		out := f.allocReg(maskOf(count))
		f.readPopcountLoopLocal(x, out)
		switch c.delta {
		case 0:
		case 1:
			f.a.Add32(out, out, count)
		case ^uint32(0):
			f.a.Sub32(out, out, count)
		default:
			f.a.MovImm32(X16, int32(c.delta))
			f.a.Madd32(out, count, X16, out)
		}
		f.pushReg(out, mtI32).st.setValueFacts(factUpper32Zero)
		f.setLocal(nil, x, false)
		f.release(out)
		f.localWritten |= uint64(1) << uint(x)
	}
	f.pushValue(zeroStorage(mtI32))
	f.setLocal(nil, pop, false)
	f.localWritten |= uint64(1) << uint(pop)
	f.pinned = oldPins
	f.release(count)
	if err := source.JumpTo(p.end); err != nil {
		panic(err)
	}
	f.stats.peep("popcount-loop-summary")
	return true
}
