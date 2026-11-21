// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Reference walkers for the fused plan, kept out of production builds.
// legacyCollectInlinedCallees scans the caller body once and returns the distinct
// inline targets it calls, in first-call order. Computed before frame setup so
// the caller's guard-page pin exclusion can be re-derived from the callees
// (whether any touches memory), and reused by reserveInlineLocals.
func legacyCollectInlinedCallees(caller *wasm.Func, targets inlineTargetTable) []*inlineTarget {
	if targets.empty() || len(caller.BodyBytes) == 0 {
		return nil
	}
	var out []*inlineTarget
	var smallSeen [inlineLinearSeenTargets]int
	seenN := 0
	var largeSeen map[int]struct{}
	r := wasm.NewReader(caller.BodyBytes)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return out
		}
		if _, ok := wasm.ImmediateFreeInstructionKind(op); ok {
			continue
		}
		var t *inlineTarget
		if op == 0x10 { // call
			idx, err := r.U32()
			if err != nil {
				return out
			}
			t = targets.target(int(idx))
		} else {
			switch op {
			case 0x05, 0x0b: // else, end
				continue
			case 0x08, 0x0c, 0x0d, 0x12, 0x14, 0x15,
				0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0xd2, 0xd5, 0xd6:
				if _, err := r.U32(); err != nil {
					return out
				}
				continue
			case 0x41:
				if _, err := r.I32(); err != nil {
					return out
				}
				continue
			case 0x42:
				if _, err := r.I64(); err != nil {
					return out
				}
				continue
			case 0x43:
				if _, err := r.Bytes(4); err != nil {
					return out
				}
				continue
			case 0x44:
				if _, err := r.Bytes(8); err != nil {
					return out
				}
				continue
			}
			if err := targets.classifier.ClassifyInto(r, op, &imm); err != nil {
				return out
			}
			if imm.Kind != wasm.InstrCall {
				continue
			}
			t = targets.target(int(imm.Index))
		}
		if t == nil {
			continue
		}
		duplicate := false
		if largeSeen != nil {
			_, duplicate = largeSeen[t.globalIdx]
		} else {
			for _, globalIdx := range smallSeen[:seenN] {
				if globalIdx == t.globalIdx {
					duplicate = true
					break
				}
			}
		}
		if duplicate {
			continue
		}
		if largeSeen != nil {
			largeSeen[t.globalIdx] = struct{}{}
		} else if seenN < len(smallSeen) {
			smallSeen[seenN] = t.globalIdx
			seenN++
		} else {
			largeSeen = make(map[int]struct{}, 2*len(smallSeen))
			for _, globalIdx := range smallSeen {
				largeSeen[globalIdx] = struct{}{}
			}
			largeSeen[t.globalIdx] = struct{}{}
		}
		out = append(out, t)
	}
	return out
}

// legacyAllCallsWillInline reports whether every call in caller is a direct call that
// the current inline plan will splice. This lets frame/register planning treat
// such a caller as truly call-free: no BL remains to clobber LR or local pins.
// Calls in loops honor the same regression guard as callOp.
func legacyAllCallsWillInline(caller *wasm.Func, targets inlineTargetTable, policy CodegenPolicy) bool {
	if targets.empty() || len(caller.BodyBytes) == 0 {
		return false
	}
	r := wasm.NewReader(caller.BodyBytes)
	var imm wasm.InstructionImmediate
	var controlLoops uint64 // one bit per ordinary control frame
	controlDepth := 0
	var deepControls []bool // pathological depth beyond the fixed 64-bit stack
	loopDepth := 0
	sawCall := false
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if err := targets.classifier.ClassifyInto(r, op, &imm); err != nil {
			return false
		}
		switch op {
		case 0x02, 0x04: // block, if
			if controlDepth < 64 {
				controlLoops &^= uint64(1) << controlDepth
			} else {
				deepControls = append(deepControls, false)
			}
			controlDepth++
		case 0x03: // loop
			if controlDepth < 64 {
				controlLoops |= uint64(1) << controlDepth
			} else {
				deepControls = append(deepControls, true)
			}
			controlDepth++
			loopDepth++
		case 0x0b: // end (the final function end has no matching entry here)
			if controlDepth != 0 {
				controlDepth--
				wasLoop := false
				if controlDepth < 64 {
					bit := uint64(1) << controlDepth
					wasLoop = controlLoops&bit != 0
					controlLoops &^= bit
				} else {
					i := controlDepth - 64
					wasLoop = deepControls[i]
					deepControls = deepControls[:i]
				}
				if wasLoop {
					loopDepth--
				}
			}
		}
		switch imm.Kind {
		case wasm.InstrCall:
			sawCall = true
			t := targets.target(int(imm.Index))
			if t == nil || t.recursive() || (loopDepth != 0 && t.inlineInLoopIsRegressive()) {
				return false
			}
		case wasm.InstrReturnCall, wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCallRef:
			return false
		}
	}
	return sawCall
}
