// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

func funcSigHasGCRefs(sig FuncSig) bool {
	return hasValType(sig.Params, ValAnyRef) || hasValType(sig.Params, ValI31Ref) || hasValType(sig.Results, ValAnyRef) || hasValType(sig.Results, ValI31Ref)
}

func collectorObjectFrameRefType(m *wasm.Module, t wasm.ValType) bool {
	if t.Kind() != wasm.ValRef {
		return false
	}
	heap := t.Ref().Heap()
	switch heap.Kind() {
	case wasm.HeapAbs:
		switch heap.Abs() {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapStruct, wasm.HeapArray:
			return true
		default:
			return false
		}
	case wasm.HeapDefType:
		kind, valid := heap.DefCompKind()
		return !valid || kind == wasm.CompStruct || kind == wasm.CompArray
	case wasm.HeapTypeIndex:
		if m == nil {
			return true
		}
		index := heap.Type().Index
		for _, group := range m.Types {
			if index < uint32(len(group.SubTypes)) {
				kind := group.SubTypes[index].Comp.Kind
				return kind == wasm.CompStruct || kind == wasm.CompArray
			}
			index -= uint32(len(group.SubTypes))
		}
		return true
	default:
		return true
	}
}

func wasmFuncTypeTransfersCollectorRefs(m *wasm.Module, ft *wasm.CompType) bool {
	if ft == nil {
		return false
	}
	for _, typ := range ft.Params {
		if collectorObjectFrameRefType(m, typ) {
			return true
		}
	}
	for _, typ := range ft.Results {
		if collectorObjectFrameRefType(m, typ) {
			return true
		}
	}
	return false
}

func moduleHasCollectorReferenceCallBoundary(m *wasm.Module) bool {
	if !wasmGCBuilt || m == nil {
		return false
	}
	for i := range m.Imports {
		if m.Imports[i].Type.Kind != wasm.ExternFunc {
			continue
		}
		ft, ok := m.ImportFuncType(i)
		if !ok || wasmFuncTypeTransfersCollectorRefs(m, ft) {
			return true
		}
	}
	if compiledArchitecture == 4 {
		for _, export := range m.Exports {
			if export.Index.Kind != wasm.ExternFunc {
				continue
			}
			ft, ok := m.FuncSignature(export.Index.Index)
			if !ok || wasmFuncTypeTransfersCollectorRefs(m, ft) {
				return true
			}
		}
	}
	return false
}

// Internal collector-bearing signatures need exact frame declarations even
// when no exported boundary or allocating instruction selects a GC product.
func moduleHasInternalCollectorReferenceFunctions(m *wasm.Module) bool {
	if !wasmGCBuilt || m == nil {
		return false
	}
	imports := m.ImportedFuncCount()
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		ft, ok := m.FuncSignature(uint32(imports + i))
		if !ok || wasmFuncTypeTransfersCollectorRefs(m, ft) {
			return true
		}
		r := wasm.NewReader(m.Code[i].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false
			}
			var imm wasm.InstructionImmediate
			if classifier.ClassifyInto(r, op, &imm) != nil {
				return false
			}
			switch imm.Kind {
			case wasm.InstrCallRef, wasm.InstrReturnCallRef, wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect:
				ft, ok := m.TypeFunc(imm.Index)
				if !ok || wasmFuncTypeTransfersCollectorRefs(m, ft) {
					return true
				}
			}
		}
	}
	return false
}

func valueTypeTransfersCollectorObject(t ValueTypeDescriptor, types []DefinedTypeDescriptor) bool {
	if t.Kind != ValueTypeReference {
		return false
	}
	if t.Ref.Heap.Defined {
		if int(t.Ref.Heap.TypeIndex) >= len(types) {
			return true
		}
		kind := types[t.Ref.Heap.TypeIndex].Kind
		return kind == CompositeTypeStruct || kind == CompositeTypeArray
	}
	switch t.Ref.Heap.Abstract {
	case AbstractHeapAny, AbstractHeapEq, AbstractHeapStruct, AbstractHeapArray:
		return true
	default:
		return false
	}
}

func (c *Compiled) importTransfersCollectorObjects(index int) bool {
	if c == nil || index < 0 || index >= len(c.importFuncSigs) {
		return false
	}
	return c.signatureTransfersCollectorObjects(c.importFuncSigs[index])
}

func (c *Compiled) signatureTransfersCollectorObjects(sig FuncSig) bool {
	params, results, err := exactFuncSignatureView(sig, c.Types)
	if err != nil || !sig.HasTypeIndex {
		return funcSigHasGCRefs(sig)
	}
	for _, typ := range params {
		if valueTypeTransfersCollectorObject(typ, c.Types) {
			return true
		}
	}
	for _, typ := range results {
		if valueTypeTransfersCollectorObject(typ, c.Types) {
			return true
		}
	}
	return false
}

// hasCollectorReferenceCallBoundary reports whether an imported function or
// an RV64 exported function can transfer collector objects. Exact descriptors
// resolve indexed and recursive heap types; raw module-local indexes are never used as
// cross-module identities.
func (c *Compiled) hasCollectorReferenceCallBoundary() bool {
	if !wasmGCBuilt || c == nil {
		return false
	}
	for i := range c.importFuncSigs {
		if c.importTransfersCollectorObjects(i) {
			return true
		}
	}
	if compiledArchitecture == 4 && c.requiredFeatures.IsEnabled(CoreFeatureGC) {
		for _, index := range c.Exports {
			local := index - c.NumImports
			if local >= 0 && local < len(c.Funcs) && c.signatureTransfersCollectorObjects(c.Funcs[local]) {
				return true
			}
		}
	}
	return false
}

// Internal collector carriers can have no public call boundary and no GC
// instruction. Source proof still gives these native functions exact empty
// maps. This classification grants no native code or artifact authority.
func (c *Compiled) hasInternalCollectorReferenceFunctions() bool {
	if !wasmGCBuilt || compiledArchitecture != 4 || c == nil || !c.requiredFeatures.IsEnabled(CoreFeatureGC) {
		return false
	}
	for _, sig := range c.Funcs {
		if c.signatureTransfersCollectorObjects(sig) {
			return true
		}
	}
	// The persisted escape flag records collector-bearing dynamic results,
	// including validated dead calls whose target type has no local binding.
	if c.dynamicFuncrefEscape {
		for _, typ := range c.Types {
			if typ.Kind != CompositeTypeFunction {
				continue
			}
			for _, list := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
				for _, value := range list {
					if valueTypeTransfersCollectorObject(value, c.Types) {
						return true
					}
				}
			}
		}
	}
	return false
}
