// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"fmt"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func readConstRefNullType(r *wasm.Reader, m *wasm.Module) (wasm.ValType, error) {
	exact, heap, err := r.RefHeapType()
	if err != nil {
		return wasm.ValType{}, err
	}
	switch heap {
	case -16: // func
		return wasm.FuncRef, nil
	case -17: // extern
		return wasm.ExternRef, nil
	case -13, -14, -15, -18, -19, -20, -21, -22: // GC abstract heaps
		if !wasmGCBuilt {
			return wasm.ValType{}, checkCompiledFeatures(CoreFeatureGC)
		}
	case -12, -23: // noexn and exn
		if buildfeatures.ExceptionHandling == 0 {
			return wasm.ValType{}, checkCompiledFeatures(CoreFeatureExceptionHandling)
		}
	default:
		if heap < 0 || m == nil {
			return wasm.ValType{}, fmt.Errorf("unsupported ref.null heap type %d", heap)
		}
		if buildfeatures.TypedFunctionReferences == 0 {
			return wasm.ValType{}, checkCompiledFeatures(CoreFeatureTypedFunctionReferences)
		}
		idx := uint32(heap)
		if _, ok := m.TypeSubType(idx); !ok {
			return wasm.ValType{}, fmt.Errorf("unsupported ref.null heap type %d", heap)
		}
		return wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: idx}), exact)), nil
	}
	// An abstract heap is encoded as a negative S33 immediate whose low
	// seven bits contain the abstract type byte.
	return wasm.RefVal(wasm.AbsRef(wasm.AbsHeapType(heap & 0x7f))), nil
}

func constRefNullASTType(in wasm.Instruction, ctx *constExprCompileContext) (wasm.ValType, error) {
	refType := wasm.RefVal(in.RefType())
	if !wasmGCBuilt || buildfeatures.ExceptionHandling == 0 || buildfeatures.TypedFunctionReferences == 0 {
		required := requiredFeaturesForValType(refType) & (CoreFeatureGC | CoreFeatureExceptionHandling | CoreFeatureTypedFunctionReferences)
		if err := checkCompiledFeatures(required); err != nil {
			return wasm.ValType{}, err
		}
	}
	var exact ValueTypeDescriptor
	var types []DefinedTypeDescriptor
	var err error
	if ctx != nil {
		types = ctx.types
		exact, err = ctx.converter.valueType(refType, -1)
	} else {
		exact, err = valueTypeDescriptorInModule(nil, refType)
	}
	if err != nil {
		return wasm.ValType{}, fmt.Errorf("unsupported ref.null type %s", refType.String())
	}
	if _, ok := exact.ABIType(types); err != nil || !ok {
		return wasm.ValType{}, fmt.Errorf("unsupported ref.null type %s", refType.String())
	}
	return refType, nil
}

func constRefFuncType(m *wasm.Module, index uint32) wasm.ValType {
	if buildfeatures.TypedFunctionReferences != 0 && m != nil {
		if typeIndex, ok := m.FuncTypeIndex(index); ok {
			return wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(typeIndex), false))
		}
	}
	return wasm.FuncRef
}

func referenceConstExprTypeMatches(actual, required wasm.ValType, ctx *constExprCompileContext) bool {
	if ctx == nil || actual.Kind() != wasm.ValRef || required.Kind() != wasm.ValRef {
		return false
	}
	a, err := ctx.converter.valueType(actual, -1)
	if err != nil {
		return false
	}
	b, err := ctx.converter.valueType(required, -1)
	return err == nil && valueTypeSubtype(a, ctx.types, b, ctx.types)
}
