// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// IsStraightLineBlock proves that a small block has only ordinary numeric,
// local/global and native memory operations followed by its end. Up to eight
// nested ordinary blocks preserve fallthrough; branches, calls, loops, conditions
// and instructions making fallthrough unreachable are excluded.
// Keeping the control frame is still necessary for its stack-height contract;
// only entry/exit reconciliation can be omitted. The original reader is unchanged.
func IsStraightLineBlock(source *wasm.Reader, classifier wasm.ModuleInstructionClassifier) bool {
	return IsStraightLineBlockWithSplicedCalls(source, classifier, nil)
}

// IsStraightLineBlockWithSplicedCalls additionally admits calls for which the
// backend proves that lowering will splice a bounded native fallthrough body.
// The predicate must reject every actual call/helper and prove any spliced
// control remains inside a private callee boundary, never reaching this block.
// All structural bounds and reader-preservation rules remain unchanged.
func IsStraightLineBlockWithSplicedCalls(source *wasm.Reader, classifier wasm.ModuleInstructionClassifier, spliced func(uint32) bool) bool {
	return IsFallthroughBlock(source, classifier, spliced, false)
}

// IsFallthroughBlock can additionally admit fully contained if/else regions.
// Branches, loops, calls and nonlocal control transfers retain the same exclusions.
// With no incoming edge to the outer block, its own canonicalization is redundant;
// each nested if continues to reconcile its private split and merge normally.
func IsFallthroughBlock(source *wasm.Reader, classifier wasm.ModuleInstructionClassifier, spliced func(uint32) bool, allowIf bool) bool {
	r := *source
	start := r.Offset()
	depth := 0
	var controls [8]byte
	var imm wasm.InstructionImmediate
	for n := 0; n < 32 && r.Offset()-start < 128; n++ {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if op == 0x0b {
			if depth == 0 {
				return true
			}
			depth--
			continue
		}
		if op == 0x05 {
			if !allowIf || depth == 0 || controls[depth-1] != 0x04 {
				return false
			}
			controls[depth-1] = 0x05
			continue
		}
		if op == 0x02 || allowIf && op == 0x04 {
			if depth == 8 {
				return false
			}
			controls[depth] = op
			depth++
		}
		if op == 0x10 && spliced != nil {
			if err := classifier.ClassifyInto(&r, op, &imm); err != nil || !spliced(imm.Index) {
				return false
			}
			continue
		}
		if !(op == 0x02 || allowIf && op == 0x04 || op == 0x01 || op >= 0x1a && op <= 0x1c || op >= 0x20 && op <= 0x24 || op >= 0x28 && op <= 0x3f || op >= 0x41 && op <= 0xc4 || op == 0xfd || op == 0xfc || op == 0xfe) {
			return false
		}
		if err := classifier.ClassifyInto(&r, op, &imm); err != nil {
			return false
		}
		// memory.fill is lowered entirely in native code on both targets. Other
		// miscellaneous operations can call helpers or alter memory/table state.
		// Native load/store/RMW/CAS/fence operations keep barrier semantics in
		// their lowering. Wait/notify can enter a host helper and must reconcile.
		if op == 0xfe && !(imm.Kind >= wasm.InstrAtomicFence && imm.Kind <= wasm.InstrAtomicCmpxchg) {
			return false
		}
		if op == 0xfc && imm.Kind != wasm.InstrMemoryFill {
			return false
		}
	}
	return false
}
