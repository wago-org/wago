// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func mutableReferenceVectorLocalRuns(runs []wasm.LocalRun) bool {
	want := [...]wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.V128, wasm.F64}
	if len(runs) != len(want) {
		return false
	}
	for i := range want {
		if runs[i].Count != 1 || !wasm.EqualValType(runs[i].Type, want[i]) {
			return false
		}
	}
	return true
}

func mutableReferenceVectorLocalTransferLocals(m *wasm.Module) bool {
	return len(m.Code) == 6 && mutableReferenceVectorLocalRuns(m.Code[0].Locals.Runs) && mutableReferenceVectorLocalRuns(m.Code[1].Locals.Runs) && mutableReferenceVectorLocalRuns(m.Code[5].Locals.Runs)
}

func mutableReferenceVectorLocalTransferBodies(tail bool) (identity, transform, call []byte) {
	stage := func(body []byte, parameter, local byte) []byte {
		return append(body, 0x20, parameter, 0x21, local)
	}
	for i := byte(0); i < 5; i++ {
		identity = stage(identity, i, i+5)
		call = stage(call, i, i+5)
	}
	transform = append(transform, 0x20, 0, 0x41, 1, 0x6a, 0x21, 5)
	transform = stage(transform, 1, 6)
	transform = stage(transform, 2, 7)
	transform = append(transform, 0x20, 3, 0xfd, 77, 0x21, 8)
	transform = append(transform, 0x20, 4, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x21, 9)
	for i := byte(0); i < 5; i++ {
		identity = append(identity, 0x20, i+5)
		transform = append(transform, 0x20, i+5)
		call = append(call, 0x20, i+5)
	}
	call = append(call, 0x23, 0, 0x14, 0, 0x0b)
	if tail {
		call[len(call)-3] = 0x15
	}
	return append(identity, 0x0b), append(transform, 0x0b), call
}

func mutableReferenceVectorBranchTransferBodies(tail bool) (identity, transform, call []byte) {
	identity, _, call = mutableReferenceVectorLocalTransferBodies(tail)
	stage := func(body []byte, parameter, local byte) []byte {
		return append(body, 0x20, parameter, 0x21, local)
	}
	transform = []byte{0x20, 0, 0x04, 0x40}
	transform = append(transform, 0x20, 0, 0x41, 1, 0x6a, 0x21, 5)
	transform = stage(transform, 1, 6)
	transform = stage(transform, 2, 7)
	transform = append(transform, 0x20, 3, 0xfd, 77, 0x21, 8)
	transform = append(transform, 0x20, 4, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x21, 9, 0x05)
	for i := byte(0); i < 5; i++ {
		transform = stage(transform, i, i+5)
	}
	transform = append(transform, 0x0b)
	for i := byte(0); i < 5; i++ {
		transform = append(transform, 0x20, i+5)
	}
	return identity, append(transform, 0x0b), call
}

// privateIndexedFunctionGlobal admits the table-free typed cells used to hold
// same-instance recursive call_ref targets. Keep this separate from public
// funcref globals: the carrier is immutable, non-null, non-exact, initialized
// directly by ref.func, and its target must have the indexed function type
// named by the cell. No imported target or subtype conversion is accepted.
func privateIndexedFunctionGlobal(global wasm.Global, m *wasm.Module) (uint32, bool) {
	if !buildfeatures.ReferenceSignatures || m == nil || global.Type.Mutable || len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || global.Type.Type.Kind() != wasm.ValRef {
		return 0, false
	}
	ref := global.Type.Type.Ref()
	heap := ref.Heap()
	if ref.Nullable() || ref.Exact() || heap.Kind() != wasm.HeapTypeIndex || heap.Type().Rec || !internalFunctionReferenceWithTypes(global.Type.Type, true, internalReferenceTypes{decoded: m.Types}) {
		return 0, false
	}
	value, err := tableElementExpression(global.Init)
	if err != nil || value.Null || uint64(value.FuncIndex) >= uint64(len(m.Code)) {
		return 0, false
	}
	target := m.FuncTypes[value.FuncIndex]
	if target.Rec || target.Index != heap.Type().Index {
		return 0, false
	}
	return value.FuncIndex, true
}

// Public immutable cells use retained canonical descriptors. Imported cells
// require the retained-reference profile and frozen Runtime descriptor-owner roots.
func admitImmutableFunctionGlobal(global wasm.Global, m *wasm.Module, limit int) error {
	if !buildfeatures.ReferenceSignatures || global.Type.Mutable || !wasm.EqualValType(global.Type.Type, wasm.FuncRef) || len(m.Tables)+m.ImportedTableCount() > 1 && !admitPublicPrimitiveReferenceModule(m, false) && !admitPublicPrimitiveReferenceModule(m, true) {
		return unsupported("reference global requires immutable local funcref and retained references")
	}
	if len(global.Init.BodyBytes) > 0 && global.Init.BodyBytes[0] == 0x23 {
		if err := admitGlobalExpression(global.Init, m, wasm.FuncRef, limit); err != nil {
			return err
		}
		return nil
	}
	value, err := tableElementExpression(global.Init)
	if err != nil {
		return err
	}
	if !value.Null && uint64(value.FuncIndex) >= uint64(m.ImportedFuncCount()+len(m.Code)) {
		return unsupported("reference global function index")
	}
	return nil
}

// Export chains are validated before folding. Imported-function targets need
// same-store native binding validation at public instantiation.
func admitReferenceGlobalExport(m *wasm.Module, index uint32) error {
	if !buildfeatures.ReferenceSignatures {
		return unsupported("reference-valued globals are disabled in this build")
	}
	if admitPublicPrimitiveReferenceModule(m, false) || admitPublicPrimitiveReferenceModule(m, true) {
		if typ, ok := m.GlobalTypeByIndex(index); ok && (internalReferenceTypes{publicReferences: true, sourceDirectory: sourceFunctionTypeDirectory{module: m}}).publicFunctionReference(typ.Type) {
			return nil
		}
	}
	if index == 0 && (admitMutableFunctionGlobalLocalExport(m) || admitMutableFunctionGlobalLocalObserve(m) || admitMutableFunctionGlobalLocalCall(m)) {
		return nil
	}
	imported := m.ImportedGlobalCount()
	// Each copy reduces the global index. The public profile has at most 256
	// globals; no recursion or allocation is needed to find the literal target.
	for remaining := 256; remaining > 0; remaining-- {
		if uint64(index) < uint64(imported) {
			typ, ok := m.GlobalTypeByIndex(index)
			if !ok || !admitImportedFunctionGlobal(m, typ) {
				return unsupported("reference global import requires immutable funcref and retained references")
			}
			return nil
		}
		if uint64(index) >= uint64(imported+len(m.Globals)) {
			return unsupported("reference global export index")
		}
		global := m.Globals[int(index)-imported]
		if err := admitImmutableFunctionGlobal(global, m, int(index)); err != nil {
			return err
		}
		if global.Init.BodyBytes[0] == 0x23 {
			r := wasm.ReaderFrom(global.Init.BodyBytes[1:])
			index, _ = r.U32()
			continue
		}
		return nil
	}
	return unsupported("reference global initializer chain bound")
}

func admitImportedFunctionGlobal(m *wasm.Module, typ wasm.GlobalType) bool {
	if !buildfeatures.ReferenceSignatures {
		return false
	}
	if (internalReferenceTypes{publicReferences: true, sourceDirectory: sourceFunctionTypeDirectory{module: m}}).publicFunctionReference(typ.Type) && (admitPublicPrimitiveReferenceModule(m, false) || admitPublicPrimitiveReferenceModule(m, true)) {
		return true
	}
	return wasm.EqualValType(typ.Type, wasm.FuncRef) && (!typ.Mutable && len(m.Tables)+m.ImportedTableCount() <= 1 || typ.Mutable && (admitStateOnlyMutableFunctionGlobalImport(m) || admitMutableFunctionGlobalGet(m) || admitMutableFunctionGlobalClear(m) || admitMutableFunctionGlobalSetSelf(m) || admitMutableFunctionGlobalCopy(m) || admitMutableFunctionGlobalMove(m) || admitMutableFunctionGlobalBidirectionalCopy(m) || admitMutableFunctionGlobalLocalCopy(m) || admitMutableFunctionGlobalLocalPublish(m) || admitMutableFunctionGlobalLocalTransfer(m)))
}
