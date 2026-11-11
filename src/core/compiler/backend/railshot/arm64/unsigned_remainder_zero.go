// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var unsignedRemainderLoopSelectionEnabled = os.Getenv("WAGO_ARM64_NO_UNSIGNED_REMAINDER_LOOP_SELECTION") != "1"

var unsignedRemainderZeroEnabled = os.Getenv("WAGO_ARM64_NO_UNSIGNED_REMAINDER_ZERO") != "1"

// The caller has already preserved both operands and emitted every unproved
// zero-divisor trap. Below the divisor, the unsigned remainder is the dividend.
// Avoid extra leases when the destination aliases the divisor. Quotient scratch
// is distinct from all operands and the result, as in the ordinary lowering.
func (f *fn) unsignedRemainder(result, dividend, divisor, quotient Reg, wide bool) {
	if unsignedRemainderZeroEnabled && result != divisor {
		if wide {
			if result != dividend {
				f.a.MovReg64(result, dividend)
			}
		} else {
			// Even an aliased i32 destination must clear the upper word.
			f.a.MovReg32(result, dividend)
		}
		f.cmpRR(dividend, divisor, wide)
		done := f.a.Bcond(condB)
		f.udiv(quotient, dividend, divisor, wide)
		f.msub(result, quotient, divisor, dividend, wide)
		f.patchBranch19(done, f.a.Len())
		f.stats.peep("unsigned-remainder-zero-quotient")
		return
	}
	f.udiv(quotient, dividend, divisor, wide)
	f.msub(result, quotient, divisor, dividend, wide)
}

// A copied reader admits at most 128 operations/512 source bytes, looking for
// remainder within 24 operations. Reject nested control, calls, and unsupported
// effects. Only one flat candidate can be active; its bitmap dies at loop end.
// No operand descriptor, allocator node, or local assignment history is retained.
func (f *fn) inspectUnsignedRemainderLoop(source *wasm.Reader) (writes uint64, ok bool) {
	if !unsignedRemainderZeroEnabled || !unsignedRemainderLoopSelectionEnabled || f.localBase != 0 || f.inlineForwardN != 0 || f.nLocals > 64 || len(f.ctrl) >= 65535 {
		return 0, false
	}
	scan := *source
	start := scan.Offset()
	found := false
	var imm wasm.InstructionImmediate
	for operations := 0; operations < 128 && scan.Offset()-start < 512; operations++ {
		if operations == 24 && !found {
			return 0, false
		}
		op, err := scan.Byte()
		if err != nil {
			return 0, false
		}
		if op == 0x0b {
			return writes, found
		}
		if op == 0x70 || op == 0x82 {
			found = true
		}
		if op >= 0x45 && op <= 0x8a {
			continue
		}
		if op >= 0x20 && op <= 0x22 {
			x, err := scan.U32()
			if err != nil || x >= uint32(f.nLocals) {
				return 0, false
			}
			if op != 0x20 {
				writes |= uint64(1) << x
			}
		} else {
			if !(op == 0x01 || op == 0x1a || op == 0x1b || op == 0x0c || op == 0x0d || op >= 0x28 && op <= 0x3e || op == 0x41 || op == 0x42) {
				return 0, false
			}
			if f.classifier.ClassifyInto(&scan, op, &imm) != nil {
				return 0, false
			}
		}
		if scan.Offset()-start > 512 {
			return 0, false
		}
	}
	return 0, false
}

// This is a cost screen, not an arithmetic range proof. The native comparison
// proves each shortcut. Prefer a changing dividend with an invariant divisor.
func (f *fn) preferUnsignedRemainderZero(left, right *elem) bool {
	if !unsignedRemainderLoopSelectionEnabled {
		return true
	}
	if f.remainderZeroLoopDepth == 0 || len(f.ctrl) != int(f.remainderZeroLoopDepth) || left.st.kind != stLocalReg || right.st.kind != stLocalReg || left.st.idx >= 64 || right.st.idx >= 64 {
		return false
	}
	writes := f.remainderZeroLoopWrites
	return writes&(uint64(1)<<left.st.idx) != 0 && writes&(uint64(1)<<right.st.idx) == 0
}
