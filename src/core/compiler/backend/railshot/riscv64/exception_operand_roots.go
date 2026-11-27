// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func exceptionOpaqueReferences(m *wasm.Module) bool {
	opaque := func(typ wasm.ValType) bool { return typ.Kind() == wasm.ValRef && !wasm.CollectorReferenceType(m, typ) }
	for _, group := range m.Types {
		for _, sub := range group.SubTypes {
			for _, values := range [][]wasm.ValType{sub.Comp.Params, sub.Comp.Results} {
				for _, typ := range values {
					if opaque(typ) {
						return true
					}
				}
			}
		}
	}
	for i := 0; i < m.ImportedGlobalCount()+len(m.Globals); i++ {
		if global, ok := m.GlobalTypeByIndex(uint32(i)); ok && opaque(global.Type) {
			return true
		}
	}
	for i := 0; i < m.TableCount(); i++ {
		if table, ok := m.TableType(uint32(i)); ok && opaque(wasm.RefVal(table.Ref)) {
			return true
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for _, fn := range m.Code {
		for _, run := range fn.Locals.Runs {
			if opaque(run.Type) {
				return true
			}
		}
		r := wasm.ReaderFrom(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return true
			}
			var imm wasm.InstructionImmediate
			if classifier.ClassifyInto(&r, op, &imm) != nil {
				return true
			}
			if imm.Kind == wasm.InstrAnyConvertExtern || imm.Kind == wasm.InstrExternConvertAny || imm.Kind == wasm.InstrRefFunc || imm.Kind == wasm.InstrRefNull && imm.HasHeapType && opaque(wasm.RefVal(wasm.Ref(true, imm.HeapType, false))) {
				return true
			}
		}
	}
	return false
}

func (mode emissionMode) functionEnvironmentRoots() bool {
	return mode.ownedExceptionReferences || mode.gcFunctionEnvironments
}

func (mode emissionMode) opaqueExternRoots() bool {
	return mode.gcRootedScalar || mode.ownedExceptionReferences
}

func (f *integerEmitter) functionMutationSafepoint(roots [][]bool) uint32 {
	if len(roots) == 0 {
		return 0
	}
	return f.recordGCFrameSafepoint(roots[0])
}

func (f *integerEmitter) functionMutation(kind wasm.InstrKind, typeIndex, fieldIndex uint32) bool {
	if !f.mode.functionEnvironmentRoots() && !f.mode.opaqueExternRoots() {
		return false
	}
	m := f.mode.tableModuleSource
	if m == nil {
		m = f.mode.exceptionModule
	}
	return f.mode.functionEnvironmentRoots() && wasm.FunctionStorageMutation(m, kind, typeIndex, fieldIndex) || f.mode.opaqueExternRoots() && wasm.ExternStorageMutation(m, kind, typeIndex, fieldIndex)
}

func (f *integerEmitter) externPersistentRoots(kind wasm.InstrKind, index, index2 uint32, refs []bool) [][]bool {
	m := f.mode.tableModuleSource
	if m == nil {
		m = f.mode.exceptionModule
	}
	if f.mode.opaqueExternRoots() && wasm.ExternPersistentMutation(m, kind, index, index2) {
		return [][]bool{refs}
	}
	return nil
}
