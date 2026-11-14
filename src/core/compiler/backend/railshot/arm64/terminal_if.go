// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// A root scalar if immediately followed by the function end has no later local
// reads on either path. Bound the scan and decline inline, EH and custom bodies.
func (f *fn) terminalScalarIf(r *wasm.Reader, params, results int) bool {
	if !f.opt(optMergeNextUse) || f.nLocals > 64 || !f.usesCalls || f.moduleEH || len(f.customInstructions) != 0 || f.inlineDepth != 0 || len(f.ctrl) != 1 || params != 0 || results != 1 || len(f.ft.Results) != 1 || f.depth() != 1 || r.BytesLeft() > 4096 {
		return false
	}
	if f.gcFrameRoots != nil {
		return false
	}
	for _, typ := range f.ft.Params {
		if typ != wasm.I32 && typ != wasm.I64 {
			return false
		}
	}
	if f.ft.Results[0] != wasm.I32 && f.ft.Results[0] != wasm.I64 {
		return false
	}
	index := int(f.traceFuncIdx) - f.importedFunctionCount()
	if index < 0 || index >= len(f.m.Code) {
		return false
	}
	for _, run := range f.m.Code[index].Locals.Runs {
		if run.Type != wasm.I32 && run.Type != wasm.I64 {
			return false
		}
	}
	for _, typ := range f.localType {
		if typ != mtI32 && typ != mtI64 {
			return false
		}
	}
	if typ := mtOf(f.ft.Results[0]); typ != mtI32 && typ != mtI64 {
		return false
	}
	copy := *r
	depth := 0
	var imm wasm.InstructionImmediate
	for copy.HasNext() {
		op, err := copy.Byte()
		if err != nil || f.classifier.ClassifyInto(&copy, op, &imm) != nil {
			return false
		}
		switch op {
		case 0x02, 0x03, 0x04, 0x1f:
			depth++
		case 0x0b:
			if depth == 0 {
				next, err := copy.Byte()
				return err == nil && next == 0x0b && !copy.HasNext()
			}
			depth--
		}
	}
	return false
}

func (f *fn) frameLocalsDie(fr *ctrlFrame) bool {
	return fr.kind == cfIf && fr.has(ctrlIfLocalsDie)
}

// The all-dirty snapshot is zero, which ordinary merge snapshots use as their
// absent sentinel. A terminal if has an explicit entry snapshot even then.
func (f *fn) restoreTerminalIfEntry(fr *ctrlFrame) {
	state := f.frameEntryState(fr)
	for x := 0; x < f.nLocals; x++ {
		if _, _, ok := f.pinReg(x); ok {
			f.locals[x].state = state.get(x)
		}
	}
}

// Once the operand stack and arguments have been captured for a call, locals
// without any remaining read in this terminal arm need no canonical store.
// Reject all remaining control flow except closing ends: no path can return to
// an earlier read, and the terminal-if proof excludes later function reads.
func (f *fn) discardDeadTerminalCallLocals() {
	if !f.opt(optMergeNextUse) || f.inlineDepth != 0 || len(f.ctrl) == 0 || !f.frameLocalsDie(&f.ctrl[len(f.ctrl)-1]) {
		return
	}
	index := int(f.traceFuncIdx) - f.importedFunctionCount()
	if index < 0 || index >= len(f.m.Code) {
		return
	}
	r := wasm.ReaderFrom(f.m.Code[index].BodyBytes)
	if f.wasmPC < f.tracePCBase || r.JumpTo(int(f.wasmPC-f.tracePCBase)) != nil || r.BytesLeft() > 4096 {
		return
	}
	op, err := r.Byte()
	if err != nil || op != 0x10 {
		return
	}
	if _, err = r.U32(); err != nil {
		return
	}
	var reads uint64
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err = r.Byte()
		if err != nil {
			return
		}
		if op == 0x20 {
			local, e := r.U32()
			if e != nil || local >= 64 {
				return
			}
			reads |= uint64(1) << local
			continue
		}
		// The admitted integer terminal arm needs only numeric operators, local
		// writes, calls, drops and closing ends. Unknown extensions decline.
		if !(op == 0x0b || op == 0x10 || op == 0x1a || op == 0x21 || op == 0x22 || op >= 0x41 && op <= 0xc4) {
			return
		}
		if f.classifier.ClassifyInto(&r, op, &imm) != nil {
			return
		}
	}
	for x := 0; x < f.nLocals; x++ {
		if reads&(uint64(1)<<x) == 0 && f.locals[x].state == lsReg {
			f.locals[x].state = lsMem
			f.stats.peep("terminal-call-local-store-elide")
		}
	}
}
