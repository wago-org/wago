// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && (!wago_no_gc || !wago_no_exception_handling)

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (f *integerEmitter) prepareCollectorOperands(fn *wasm.Func, swar bool) error {
	m := f.mode.tableModuleSource
	if m == nil {
		m = f.mode.exceptionModule
	}
	if m == nil || !(f.mode.functionEnvironmentRoots() || f.mode.opaqueExternRoots()) || f.mode.plan == nil || f.mode.operandPlans == nil && !f.mode.gcFunctionEnvironments && !exceptionOpaqueReferences(m) {
		return nil
	}
	function := -1
	for i := range m.Code {
		if &m.Code[i] == fn {
			function = i
			break
		}
	}
	if function < 0 {
		return unsupported("exception collector function identity")
	}
	var plan *wasm.CollectorOperandRoots
	if f.mode.operandPlans != nil {
		plans := *f.mode.operandPlans
		if function >= len(plans) || plans[function] == nil {
			return unsupported("missing exception collector function plan")
		}
		plan = plans[function]
	} else {
		var err error
		plan, err = wasm.BuildCollectorOperandRoots(m, function)
		if err != nil {
			return err
		}
	}
	f.gcOperandRoots, f.gcOperandStride = plan, 1
	if swar {
		f.gcOperandStride = 2
	}
	return nil
}

func (f *integerEmitter) collectorOperandSlots(hidden []bool, pc uint32, visit func(int)) bool {
	if len(hidden) == 0 {
		return true
	}
	if f.gcOperandRoots == nil {
		for slot, root := range hidden {
			if root {
				visit(slot)
			}
		}
		return true
	}
	roots, ok := f.gcOperandRoots.At(pc)
	if !ok {
		f.err = unsupported("missing typed exception operand roots")
		return false
	}
	for _, logical := range roots {
		slot := int(logical) * f.gcOperandStride
		if f.customSpillOffsets != nil {
			if int(logical) >= len(f.customSpillOffsets) {
				f.err = unsupported("exception collector operand home")
				return false
			}
			slot = int(f.customSpillOffsets[logical] / 8)
		}
		// Native custom carriers keep their own class and never become roots
		// merely because their erased Wasm type is a reference carrier.
		if slot < len(hidden) && hidden[slot] {
			visit(slot)
		}
	}
	return true
}

func (f *integerEmitter) referenceOperandOffsets(hidden []bool, pc uint32, visit func(uint32)) bool {
	if !f.collectorOperandSlots(hidden, pc, func(slot int) { visit(uint32(f.SpillOffset(slot))) }) {
		return false
	}
	if len(hidden) == 0 || f.gcOperandRoots == nil {
		return true
	}
	if f.mode.functionEnvironmentRoots() && !f.opaqueOperandOffsets(hidden, pc, shared.GCFrameFuncRefRootFlag, visit) {
		return false
	}
	return !f.mode.opaqueExternRoots() || f.opaqueOperandOffsets(hidden, pc, shared.GCFrameExternRefRootFlag, visit)
}

func (f *integerEmitter) opaqueOperandOffsets(hidden []bool, pc, flag uint32, visit func(uint32)) bool {
	var roots []uint32
	var ok bool
	if flag == shared.GCFrameFuncRefRootFlag {
		roots, ok = f.gcOperandRoots.FunctionsAt(pc)
	} else {
		roots, ok = f.gcOperandRoots.ExternsAt(pc)
	}
	if !ok {
		f.err = unsupported("missing typed function operand roots")
		return false
	}
	for _, logical := range roots {
		slot := int(logical) * f.gcOperandStride
		if f.customSpillOffsets != nil {
			if int(logical) >= len(f.customSpillOffsets) {
				f.err = unsupported("exception function operand home")
				return false
			}
			slot = int(f.customSpillOffsets[logical] / 8)
		}
		if slot < len(hidden) && hidden[slot] {
			visit(uint32(f.SpillOffset(slot)) | flag)
		}
	}
	return true
}
