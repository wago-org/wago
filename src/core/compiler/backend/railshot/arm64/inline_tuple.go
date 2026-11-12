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
	"os"
	"strconv"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Consume two immediate result assignments in the restored caller context,
// before copying private-local results into temporary registers. Each set uses
// ordinary local lowering, which preserves aliased caller operands and locals.
func (f *fn) consumeInlineTupleStores(t *inlineTarget, r *wasm.Reader) error {
	if f.intervalControl || f.selectGroupEnd != 0 || f.streamingReduction || !f.inlineTupleFallsThrough(t) {
		return nil
	}
	rr := *r
	var ends [2]int
	for i := range ends {
		op, err := rr.Byte()
		if err != nil || op != 0x21 {
			return nil
		}
		if _, err := rr.U32(); err != nil {
			return nil
		}
		ends[i] = rr.Offset()
	}
	for _, end := range ends {
		f.wasmPC = f.tracePCBase + uint32(r.Offset())
		op, err := r.Byte()
		if err != nil {
			return err
		}
		f.prepareStoreForward(op)
		if err := f.emitPlain(r, op); err != nil {
			return err
		}
		if r.Offset() != end {
			return fmt.Errorf("arm64: inline tuple assignment reader mismatch")
		}
	}
	f.stats.peep("inline-tuple-result-stores")
	return nil
}

var inlineIntegerTuplesEnabled = true

const inlineTupleFallthroughSite = -4

func (fr *ctrlFrame) isInlineTupleFallthrough() bool {
	return fr.kind == cfBlock && fr.controlSite == inlineTupleFallthroughSite
}

// Only the function's final end may reach this synthetic inline boundary.
// Inner control still uses normal reconciliation; no return or branch may
// enter the boundary with results in canonical slots instead of live values.
func (f *fn) inlineTupleFallsThrough(t *inlineTarget) bool {
	if !t.tupleControl || !f.opt(optInlineIntegerTuples) || f.unreachable || f.makesCalls || profileEnabled || f.policy.CompactNative || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.stats != nil && f.stats.RecordSources {
		return false
	}
	r := wasm.NewReader(t.body)
	depth := uint32(0)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil || op == 0x0f {
			return false
		}
		if op == 0x0b {
			if depth == 0 {
				return !r.HasNext()
			}
			depth--
			continue
		}
		if f.classifier.ClassifyInto(r, op, &imm) != nil {
			return false
		}
		switch op {
		case 0x02, 0x03, 0x04:
			depth++
		case 0x0c, 0x0d:
			if imm.Index >= depth {
				return false
			}
		}
	}
	return false
}

// Explicit ordinary inliner limits continue to bound this class as well.
var inlineTupleMaxBytes = func() int {
	if v := os.Getenv("WAGO_INLINE_MAXBYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return min(n, 64)
		}
	}
	return 64
}()

func boundedIntegerTupleBody(code wasm.Func, facts inlineFacts, classifier wasm.ModuleInstructionClassifier) bool {
	if !facts.regABIIntOnly || facts.results != 2 || facts.params > 4 || facts.declaredLocals > 4 ||
		facts.params+facts.declaredLocals > 8 || facts.moduleEH || facts.hasControlCall || facts.calleeCount != 0 ||
		facts.touchesMem || facts.touchesGlobal || len(code.BodyBytes) == 0 || len(code.BodyBytes) > inlineTupleMaxBytes {
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

func (f inlineFacts) inlineIntegerTuple(policy CodegenPolicy) bool {
	return f.boundedTuple && !policy.CompactNative && policy.EnabledOption(optInlineIntegerTuples) && !profileEnabled
}
