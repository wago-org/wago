// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var guardedDivRemEnabled = true

// tryGuardedDivRem proves an immediate source sequence:
//
// local.get b; eqz; br_if L; local.get a; local.get b; div/rem; local.set r
//
// The untaken edge proves b nonzero. Consume and materialize exactly this one
// operation before restoring the proof, so no join or later local write can
// inherit it. Other branches, source instructions and deferred operands fall back.
func (f *fn) tryGuardedDivRem(r *wasm.Reader) (bool, error) {
	if !f.opt(optGuardedDivRem) || !f.opt(optBorrowedDivRem) || f.unreachable || profileEnabled || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.intervalControl || f.selectGroupEnd != 0 || f.streamingReduction || f.policy.CompactNative || f.stats != nil && f.stats.RecordSources {
		return false, nil
	}
	condition := f.s.back()
	if condition == nil || !condition.isDeferred() || condition.deferredOp() != opEqz {
		return false, nil
	}
	input := f.s.arg0(condition)
	if input == nil || input.elemKind() != ekValue || (input.st.kind != stLocalReg && input.st.kind != stLocalRef) || (input.st.typ != mtI32 && input.st.typ != mtI64) {
		return false, nil
	}
	rr := *r
	label, err := rr.U32()
	if err != nil || uint64(label) >= uint64(len(f.ctrl)) {
		return false, nil
	}
	branchEnd := rr.Offset()
	target := &f.ctrl[len(f.ctrl)-1-int(label)]
	if target.branchArity() != 0 || f.depth() != target.height+1 {
		return false, nil
	}
	var ends [4]int
	var indices [2]uint32
	for i := range indices {
		op, err := rr.Byte()
		if err != nil || op != 0x20 {
			return false, nil
		}
		indices[i], err = rr.U32()
		if err != nil {
			return false, nil
		}
		ends[i] = rr.Offset()
	}
	divisor := uint64(indices[1]) + uint64(f.localBase)
	if indices[1] < f.inlineForwardN {
		divisor = uint64(f.inlineForward[indices[1]])
	}
	if divisor != uint64(input.st.idx) || divisor >= uint64(len(f.localType)) || f.localType[divisor] != input.st.typ {
		return false, nil
	}
	_, floating, pinned := f.pinReg(int(divisor))
	if !pinned || floating {
		return false, nil
	}
	operation, err := rr.Byte()
	if err != nil {
		return false, nil
	}
	first := byte(0x6d)
	if input.st.typ == mtI64 {
		first = 0x7f
	}
	if operation < first || operation > first+3 {
		return false, nil
	}
	ends[2] = rr.Offset()
	op, err := rr.Byte()
	if err != nil || op != 0x21 {
		return false, nil
	}
	if _, err := rr.U32(); err != nil {
		return false, nil
	}
	ends[3] = rr.Offset()
	if err := f.opBr(r, true); err != nil {
		return true, err
	}
	if r.Offset() != branchEnd {
		return true, fmt.Errorf("arm64: guarded branch advanced beyond proved source")
	}
	f.branchHintUnlikely = false
	// local.get instructions cannot write b, call a helper or introduce a label.
	for i := 0; i < 2; i++ {
		f.wasmPC = f.tracePCBase + uint32(r.Offset())
		op, err := r.Byte()
		if err != nil {
			return true, err
		}
		f.prepareStoreForward(op)
		if err := f.emitPlain(r, op); err != nil {
			return true, err
		}
		if r.Offset() != ends[i] {
			return true, fmt.Errorf("arm64: guarded div/rem advanced beyond proved source")
		}
	}
	previous := f.guardedNonzeroDivisor
	f.guardedNonzeroDivisor = f.s.back()
	defer func() { f.guardedNonzeroDivisor = previous }()
	// Keep the proof scoped to the exact divisor operand while its immediate
	// arithmetic and assignment use the normal lowering/allocation machinery.
	for i := 2; i < 4; i++ {
		f.wasmPC = f.tracePCBase + uint32(r.Offset())
		op, err := r.Byte()
		if err != nil {
			return true, err
		}
		f.prepareStoreForward(op)
		if err := f.emitPlain(r, op); err != nil {
			return true, err
		}
		if r.Offset() != ends[i] {
			return true, fmt.Errorf("arm64: guarded div/rem advanced beyond proved source")
		}
	}
	return true, nil
}
