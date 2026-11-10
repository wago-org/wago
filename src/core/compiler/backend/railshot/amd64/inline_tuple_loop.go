// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Loop-containing helpers use the established synthetic inline control frame.
// Private locals use reserved register homes when the caller can spare them.
// The proof excludes calls and hidden module effects; normal block merges,
// return routing and trap materialization still apply at each splice.
func (f inlineFacts) inlineTupleLoop(policy CodegenPolicy) bool {
	return f.boundedTupleLoop && !policy.CompactNative && !profileEnabled
}

func boundedIntegerTupleLoopBody(code wasm.Func, facts inlineFacts, classifier wasm.ModuleInstructionClassifier) bool {
	if !facts.hasLoop || !facts.regABIIntOnly || facts.results != 2 || facts.params > 4 || facts.declaredLocals > 4 ||
		facts.params+facts.declaredLocals > 8 || facts.moduleEH || facts.hasControlCall || facts.calleeCount != 0 ||
		facts.touchesMem || facts.touchesGlobal || len(code.BodyBytes) == 0 || len(code.BodyBytes) > min(inlineMaxBytes, 64) {
		return false
	}
	declared := uint64(0)
	for _, run := range code.Locals.Runs {
		if run.Type != wasm.I32 && run.Type != wasm.I64 {
			return false
		}
		declared += uint64(run.Count)
		if declared > 4 {
			return false
		}
	}
	if declared != uint64(facts.declaredLocals) {
		return false
	}
	r := wasm.NewReader(code.BodyBytes)
	depth := 0
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if op == 0x0b {
			if depth == 0 {
				return !r.HasNext()
			}
			depth--
			continue
		}
		// Structured numeric control and locals add no native calls or hidden
		// memory/global/reference effects to the caller's register frame.
		if !(op <= 0x05 || op == 0x0c || op == 0x0d || op == 0x0f || op >= 0x1a && op <= 0x1c ||
			op >= 0x20 && op <= 0x22 || op >= 0x41 && op <= 0xc4) || classifier.ClassifyInto(r, op, &imm) != nil {
			return false
		}
		if op == 0x02 || op == 0x03 || op == 0x04 {
			if depth == 8 {
				return false
			}
			depth++
		}
	}
	return false
}

// A private tuple loop needs the ordinary call-free register budget. Keeping a
// slim prepared entry would reserve every callee-saved GP before these homes
// are planned, forcing the whole private loop through stack slots.
func hasInlineTupleLoop(callees []*inlineTarget) bool {
	for _, t := range callees {
		if t.tupleLoop() || t.tupleSelect() {
			return true
		}
	}
	return false
}

func (f *fn) pinInlineTupleLoopLocals(t *inlineTarget, base int) {
	if !t.tupleLoop() && !t.tupleSelect() || f.makesCalls || f.usesCalls || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || f.policy.CompactNative || f.pinnedLocalMask.count() == 0 {
		return
	}
	var storage [16]Reg
	pool := gpPinPool(storage[:0], true, f.nParams, true, f.opt(optEntryArgPins))
	// Retain a wider transient floor than ordinary pinning because the callee's
	// arithmetic scratch obligations are not included in the caller's hints.
	limit := gpPinLimit(f.reserved, 6)
	for local := base; local < len(f.locals); local++ {
		for _, reg := range pool {
			if f.reserved.has(reg) || f.pinnedLocalMask.has(reg) || f.pinnedLocalMask.count() >= limit {
				continue
			}
			f.locals[local].reg = reg
			if reg == mergeReg {
				// Like ordinary local pinning, reserve RBP before any control
				// frame is emitted and use canonical slots for scalar merges.
				f.regMerge = false
			}
			f.pinnedLocalMask = f.pinnedLocalMask.add(reg)
			f.pinnedLocals = append(f.pinnedLocals, local)
			f.sc.pinnedLocals = f.pinnedLocals
			if t.tupleSelect() {
				f.stats.peep("inline-tuple-select-local-pin")
			} else {
				f.stats.peep("inline-tuple-loop-local-pin")
			}
			break
		}
	}
}
