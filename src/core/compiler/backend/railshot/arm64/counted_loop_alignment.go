// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var countedLoopBodyAlignEnabled = true

// A fused countdown backedge targets the body after the zero-trip CBZ, rather
// than the test itself. Align that actual target. Restrict the entry to a
// resident integer pin with no reconciliation code before the zero test.
func (f *fn) alignCountedLoopBody(r *wasm.Reader, loop *ctrlFrame) bool {
	if !f.opt(optCountedLoopBodyAlign) || !f.opt(optCountedLoopLatch) || !f.opt(optZeroBranch) ||
		f.interruptible || f.inlineDepth != 0 || f.nLocals > pollFreeLoopPhaseMaxLocals || f.depth() != 0 ||
		loop.paramN != 0 || loop.resultN != 0 || len(f.ctrl) == 0 {
		return false
	}
	outer := &f.ctrl[len(f.ctrl)-1]
	if outer.kind != cfBlock || outer.branchArity() != 0 || f.usesCalls && (!loop.has(ctrlLoopCallFree) || !outer.has(ctrlCallFreeRegion)) {
		return false
	}
	copy := *r
	op, err := copy.Byte()
	if err != nil || op != 0x20 {
		return false
	}
	index, err := copy.U32()
	if err != nil || int(index) >= len(f.localType) || f.localType[index] != mtI32 {
		return false
	}
	_, floating, pinned := f.pinReg(int(index))
	if !pinned || floating {
		return false
	}
	op, err = copy.Byte()
	if err != nil || op != 0x45 {
		return false
	}
	op, err = copy.Byte()
	if err != nil || op != 0x0d {
		return false
	}
	label, err := copy.U32()
	if err != nil || label != 1 {
		return false
	}
	if !f.countedLoopBodyHasLatch(copy, int(index)) {
		return false
	}
	f.emitPhasePadding()
	alignment := f.policy.LoopAlignLog2
	if alignment < 5 {
		alignment = 5
	}
	padding := alignmentPadding(f.a.Len()+4, alignment)
	for range padding / 4 {
		f.a.Nop()
	}
	f.stats.peep("counted-loop-body-align")
	return true
}

// A zero-test header alone is also used by pointer-chasing loops. Only align
// the post-test body when its sole backedge is the countdown latch emitted by
// tryCountedLoopLatch; otherwise preserve ordinary header alignment.
func (f *fn) countedLoopBodyHasLatch(r wasm.Reader, counter int) bool {
	if r.BytesLeft() > 4096 {
		return false
	}
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil || op < 0x20 {
			return false
		}
		if f.classifier.ClassifyInto(&r, op, &imm) != nil {
			return false
		}
		if op == 0x20 && int(imm.Index) == counter {
			if _, ok := inspectCountedLoopLatchTail(&r, counter); ok {
				return true
			}
		}
	}
	return false
}
