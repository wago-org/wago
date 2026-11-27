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

type gcFrameOperandPlanning struct {
	exceptionOperands *[]*wasm.CollectorOperandRoots
}

type gcFrameModuleState struct {
	plans    *shared.GCModuleFrameRootPlan
	operands *[]*wasm.CollectorOperandRoots
}

func newGCFrameModuleState(opts *CompileOptions) gcFrameModuleState {
	return gcFrameModuleState{plans: opts.GCFrameRoots, operands: opts.exceptionOperands}
}

type gcFrameEmissionMode struct {
	plan            *shared.GCFrameRootPlan
	operandPlans    *[]*wasm.CollectorOperandRoots
	rootedDirectory *internalReferenceTypeDirectory
}

func (s *gcFrameModuleState) bindFunction(mode *emissionMode, function int) {
	mode.operandPlans = s.operands
	if s.plans != nil {
		mode.plan = s.plans.Function(function)
	}
}

func (m *gcFrameEmissionMode) gcFramePlan() *shared.GCFrameRootPlan { return m.plan }

func (m *gcFrameEmissionMode) setRootedTypeDirectory(directory *internalReferenceTypeDirectory) {
	m.rootedDirectory = directory
}

func (m *gcFrameEmissionMode) rootedTypeDirectory() internalReferenceTypeDirectory {
	if m.rootedDirectory == nil {
		return internalReferenceTypeDirectory{}
	}
	return *m.rootedDirectory
}

func (m *gcFrameEmissionMode) relocateGCFrameCallsites(prefixBytes int) error {
	if roots := m.plan; roots != nil {
		if !roots.VisitCallsites(func(_ int, callsite shared.GCFrameCallsite) bool {
			off := uint64(callsite.ReturnOffset()) + uint64(prefixBytes)
			if off > uint64(^uint32(0)) {
				return false
			}
			callsite.SetReturnOffset(uint32(off))
			return true
		}) {
			roots.Exact = false
			return unsupported("invalid GC frame-root callsite")
		}
	}
	return nil
}

func prepareCollectorOperandPlans(m *wasm.Module, opts *CompileOptions) error {
	if opts.GCFrameRoots == nil || !opts.gcFunctionEnvironments && (!(opts.OwnedExceptionReferences || opts.gcRootedScalar) || !exceptionOpaqueReferences(m)) {
		return nil
	}
	var selected []bool
	for function := range m.Code {
		if opts.GCFrameRoots.Function(function) != nil {
			if selected == nil {
				selected = make([]bool, len(m.Code))
			}
			selected[function] = true
		}
	}
	if selected == nil {
		return nil
	}
	plans, err := wasm.BuildCollectorOperandRootModule(m, selected)
	if err != nil {
		return err
	}
	opts.exceptionOperands = &plans
	return nil
}
