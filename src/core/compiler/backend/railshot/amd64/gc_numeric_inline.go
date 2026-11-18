// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Exact root masks describe the original source calls. A straight-line leaf
// that cannot allocate adds no safepoint of its own, so its source-call mask can
// be consumed without recording a native call. The normal inline classifier has
// already bounded body size, control flow and locals. This root-plan filter also
// rejects memory, global and allocating operations without adding another
// per-function side table.
func restrictGCNumericInlineTargets(m *wasm.Module, targets *inlineTargetTable) {
	if targets.empty() {
		return
	}
	for i := range targets.data.targets {
		t := &targets.data.targets[i]
		local := t.globalIdx - targets.first
		if !gcNumericInlineLeaf(m, local) && !gcRootSafeTinyLeaf(m, local, t) {
			targets.data.slots[local] |= inlineTargetInvalid
		}
	}
}

func gcRootSafeTinyLeaf(m *wasm.Module, local int, target *inlineTarget) bool {
	if target == nil || target.hasCtrl || target.touchesMem || len(target.body) > inlineTinyLeafMaxBodyBytes {
		return false
	}
	ft, ok := m.LocalFuncType(local)
	if !ok || !tinyLeafInlineSignature(ft) || len(m.Code[local].Locals.Runs) != 0 ||
		int(target.localTypeEnd-target.typeStart) != len(ft.Params) {
		return false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	r := wasm.NewReader(target.body)
	var imm wasm.InstructionImmediate
	instructions := 0
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil || classifier.ClassifyInto(r, op, &imm) != nil {
			return false
		}
		if op != 0x0b {
			instructions++
			if instructions > inlineTinyLeafMaxInstructions {
				return false
			}
		}
		if op == 0x23 || op == 0x24 {
			return false
		}
		switch imm.Kind {
		case wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrStructNewDesc, wasm.InstrStructNewDefaultDesc,
			wasm.InstrArrayNew, wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed, wasm.InstrArrayNewData, wasm.InstrArrayNewElem,
			wasm.InstrCall, wasm.InstrCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCall,
			wasm.InstrReturnCallIndirect, wasm.InstrReturnCallRef:
			return false
		}
	}
	return true
}

func gcNumericInlineLeaf(m *wasm.Module, local int) bool {
	ft, ok := m.LocalFuncType(local)
	if !ok || !sigFitsRegABI(ft) || !sigIsIntOnly(ft) {
		return false
	}
	for _, run := range m.Code[local].Locals.Runs {
		if !isIntValType(run.Type) {
			return false
		}
	}
	r := wasm.ReaderFrom(m.Code[local].BodyBytes)
	for r.BytesLeft() != 0 {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch {
		case op == 0x0b:
			return r.BytesLeft() == 0
		case op == 0x20 || op == 0x21 || op == 0x22:
			_, err = r.U32()
		case op == 0x41:
			_, err = r.I32()
		case op == 0x42:
			_, err = r.I64()
		case op == 0x01 || op == 0x1a || op == 0x1b, // nop/drop/untyped select
			op >= 0x45 && op <= 0x5a,               // i32/i64 comparisons
			op >= 0x67 && op <= 0x8a,               // i32/i64 arithmetic and bit operations
			op == 0xa7 || op == 0xac || op == 0xad, // integer width conversions
			op >= 0xc0 && op <= 0xc4:               // integer sign extension
		default:
			return false
		}
		if err != nil {
			return false
		}
	}
	return false
}
