// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var _ [railshot.MaxModuleTables - maxRV64ExecutionTables]struct{}
var _ [maxRV64ExecutionTables - railshot.MaxModuleTables]struct{}
var _ [railshot.MaxModuleExports - rv64MaxModuleExports]struct{}
var _ [rv64MaxModuleExports - railshot.MaxModuleExports]struct{}
var _ [railshot.MaxFunctionImports - rv64RootedMaxHostFunctionImports]struct{}
var _ [rv64RootedMaxHostFunctionImports - railshot.MaxFunctionImports]struct{}

func rv64FreshRootedGC(c *Compiled) bool {
	if !rv64RootedReceiptsForImage || c == nil || c.nativeGCABIRequirement() != gc.NativeABIVersion {
		return false
	}
	memo := c.loadValidateMemo()
	contract := c.rv64ExecutionContract()
	if contract != nil && memo != nil && memo.executionView() == c && len(c.code) != 0 && contract.rooted.Load() == c {
		return true
	}
	receipt := memo.rootedReceipt()
	var fallback rv64RootedReceipt
	if memo != nil && contract != nil {
		if !rv64SourceContractValid(c) {
			return false
		}
		if receipt == nil {
			if err := rv64PopulateSourceReceipt(&fallback, c); err != nil {
				return false
			}
			receipt = &fallback
		}
	}
	if receipt == nil || sha256.Sum256(c.code) != receipt.codeDigest || c.syncHostSlots != receipt.syncHostSlots {
		return false
	}
	if len(c.Types) == 0 || len(c.Types) > railshot.MaxFunctions || len(c.Funcs) == 0 && !c.hasNoFunctionCode() && !(len(c.code) == 0 && c.NumImports > 0 && c.hasCollectorReferenceCallBoundary()) || len(c.Funcs) > railshot.MaxModuleFunctions ||
		len(c.FuncTypeID) > railshot.MaxModuleFunctions || len(c.Exports) > railshot.MaxModuleExports ||
		compiledStructuralRequiredFeatures(c)&^c.requiredFeatures != 0 {
		return false
	}
	typeDigest, validTypes := rv64RootedTypeDigest(c)
	if !validTypes || typeDigest != receipt.typeDigest {
		return false
	}
	structProduct, arrayProduct := c.stagedGCStructProduct(), c.stagedGCArrayProduct()
	structHelpers, arrayHelpers := structProduct == stagedGCStructGeneric, arrayProduct == stagedGCArrayProductGeneric
	// Reference-transport-only modules publish exact callsite roots without
	// staging either allocation/mutation helper family.
	if structProduct != 0 && !structHelpers || arrayProduct != 0 && !arrayHelpers ||
		c.usesGCStructHelpers() != structHelpers || c.usesGCArrayHelpers() != arrayHelpers || c.syncHostSlots < 4 ||
		c.nativeGCABIRequirement() != gc.NativeABIVersion || c.NumImports < 0 || c.NumImports > railshot.MaxFunctionImports || len(c.Imports) != c.NumImports ||
		len(c.importFuncSigs) != c.NumImports || c.dynamicImports != (c.NumImports != 0) || c.tableCount() > maxRV64ExecutionTables || c.memoryCount() > rv64RootedMaxMemories ||
		len(c.GlobalImports) > 256 || len(c.GlobalImports) > len(c.Globals) || len(c.Globals) > 256 || len(c.Elems) > 256 ||
		len(c.passiveElems) > 256 || len(c.PassiveData) > 256 {
		return false
	}
	importDigest, validImports := rv64RootedImportDigest(c)
	if !validImports || importDigest != receipt.importDigest {
		return false
	}
	if c.HasStart != receipt.hasStart {
		return false
	}
	if c.HasStart {
		if c.StartIsImport != receipt.startImported {
			return false
		}
		if c.StartIsImport {
			if c.StartLocalFunc != 0 || c.StartImportIdx < 0 || c.StartImportIdx >= c.NumImports || c.StartImportIdx >= len(c.importFuncSigs) ||
				uint32(c.StartImportIdx) != receipt.startImportIndex || receipt.startLocalIndex != 0 ||
				len(c.importFuncSigs[c.StartImportIdx].Params) != 0 || len(c.importFuncSigs[c.StartImportIdx].Results) != 0 {
				return false
			}
		} else if c.StartImportIdx != 0 || c.StartLocalFunc < 0 || uint64(c.StartLocalFunc) >= uint64(len(c.Funcs)) ||
			uint32(c.StartLocalFunc) != receipt.startLocalIndex || receipt.startImportIndex != 0 || c.StartLocalFunc >= len(c.Entry) ||
			len(c.Funcs[c.StartLocalFunc].Params) != 0 || len(c.Funcs[c.StartLocalFunc].Results) != 0 {
			return false
		}
	} else if c.StartIsImport || c.StartImportIdx != 0 || c.StartLocalFunc != 0 || receipt.startImported || receipt.startLocalIndex != 0 || receipt.startImportIndex != 0 {
		return false
	}
	directory := c.memoryDir
	if directory == nil || len(directory.defs) > rv64RootedMaxMemories || !directory.exactExports || directory.staged != receipt.memoryStaged || len(directory.defs) > 1 && !directory.staged ||
		directory.hasGCInitializers() ||
		(directory.hasExceptionTagMetadata()) && !rv64ExceptionHandling(c) || uint8(len(directory.defs)) != receipt.memoryCount {
		return false
	}
	if len(directory.defs) != 0 {
		primary := directory.defs[0]
		if !c.HasMemory || uint64(c.MemMinPages) != primary.Min || uint64(c.MemMaxPages) < primary.Min || primary.HasMax && uint64(c.MemMaxPages) > primary.Max || c.MemHasMax != primary.HasMax || c.memoryImport != primary.ImportKey ||
			primary.Min != receipt.memoryMin || primary.Max != receipt.memoryMax || c.MemMaxPages != receipt.memoryReserveMax {
			return false
		}
		for i := range directory.defs {
			memory := directory.defs[i]
			if memory.Shared && !rv64ThreadedMemoryProfile(c) || memory.HasMax && memory.Min > memory.Max {
				return false
			}
		}
	} else if c.HasMemory || c.MemMinPages != 0 || c.MemMaxPages != 0 || c.MemHasMax || c.memoryImport != "" ||
		receipt.memoryMin != 0 || receipt.memoryMax != 0 || receipt.memoryReserveMax != 0 {
		return false
	}
	dataDigest, validData := rv64RootedActiveDataDigest(c)
	if !validData || dataDigest != receipt.dataDigest {
		return false
	}
	exportDigest, validExports := rv64RootedMemoryExportDigest(c)
	if !validExports || exportDigest != receipt.exportDigest {
		return false
	}
	globalDigest, validGlobals := rv64RootedGlobalDigest(c)
	if !validGlobals || globalDigest != receipt.globalDigest {
		return false
	}
	for i := range c.Globals {
		global := &c.Globals[i]
		if global.HasInitGlobal {
			if isGCRefValType(global.Type) || global.Type == ValExternRef && c.compactExternFrameRoots() {
				exact, err := c.globalExactType(i)
				if err != nil || !rv64InitializerReferenceGlobal(c, global.InitGlobal, exact, i) {
					return false
				}
			} else if !rv64InitializerGlobal(c, global.InitGlobal, global.Type, i) {
				return false
			}
		}
		if len(global.InitExpr) != 0 {
			if isGCRefValType(global.Type) || global.Type == ValExternRef && c.compactExternFrameRoots() {
				if !wasmGCBuilt {
					return false
				}
				exact, err := c.globalExactType(i)
				if err != nil || c.validateGCConstExpr(global.InitExpr, exact, i) != nil {
					return false
				}
			} else if rv64ValidateIntegerInitializer(c, global.InitExpr, global.Type, i) != nil {
				return false
			}
		}
	}
	tableDigest, validTable := rv64RootedTableDigest(c)
	if !validTable || tableDigest != receipt.tableDigest {
		return false
	}
	remainingData := railshot.MaxModuleBytes
	for i := 0; i < c.activeDataCount(); i++ {
		data := c.activeDataAt(i)
		if uint64(data.MemoryIndex) >= uint64(len(directory.defs)) || len(data.Bytes) > remainingData {
			return false
		}
		remainingData -= len(data.Bytes)
	}
	for i := range c.PassiveData {
		if len(c.PassiveData[i].Bytes) > remainingData {
			return false
		}
		remainingData -= len(c.PassiveData[i].Bytes)
	}
	roots := c.genericGCFrameRoots()
	if roots == nil || len(c.Types) != len(c.GCTypeDescs) {
		return false
	}
	if err := validateCompiledGCFrameRoots(c, roots); err != nil {
		return false
	}
	opaqueNoCollect := c.needsOwnedExceptionReferences() && rv64ExceptionHandling(c) && len(roots.safepoints) == 0 && len(roots.callsites) == 0
	ownedFunctionRoots := c.needsOwnedExceptionReferences() && rv64ExceptionHandling(c) && c.functionEnvironmentFrameRoots()
	rootedAbstract := func(heap AbstractHeapType) (wasm.AbsHeapType, bool) {
		switch heap {
		case AbstractHeapFunc:
			return wasm.HeapFunc, opaqueNoCollect || ownedFunctionRoots || c.functionEnvironmentFrameRoots()
		case AbstractHeapNoFunc:
			return wasm.HeapNoFunc, opaqueNoCollect || ownedFunctionRoots || c.functionEnvironmentFrameRoots()
		case AbstractHeapExtern:
			return wasm.HeapExtern, opaqueNoCollect || c.compactExternFrameRoots()
		case AbstractHeapNoExtern:
			return wasm.HeapNoExtern, opaqueNoCollect || c.compactExternFrameRoots()
		case AbstractHeapExn:
			return wasm.HeapExn, rv64ExceptionHandling(c)
		case AbstractHeapNoExn:
			return wasm.HeapNoExn, rv64ExceptionHandling(c)
		case AbstractHeapNone:
			return wasm.HeapNone, true
		case AbstractHeapAny:
			return wasm.HeapAny, true
		case AbstractHeapEq:
			return wasm.HeapEq, true
		case AbstractHeapI31:
			return wasm.HeapI31, true
		case AbstractHeapStruct:
			return wasm.HeapStruct, true
		case AbstractHeapArray:
			return wasm.HeapArray, true
		default:
			return 0, false
		}
	}
	rootedValue := func(value ValueTypeDescriptor, owner int) bool {
		if value.Kind <= ValueTypeV128 {
			return value.Ref == (ReferenceTypeDescriptor{})
		}
		if value.Kind != ValueTypeReference {
			return false
		}
		if !value.Ref.Heap.Defined {
			if value.Ref.Exact {
				return false
			}
			_, ok := rootedAbstract(value.Ref.Heap.Abstract)
			return ok && value.Ref.Heap.TypeIndex == 0
		}
		if int(value.Ref.Heap.TypeIndex) >= owner || int(value.Ref.Heap.TypeIndex) >= len(c.Types) {
			return false
		}
		target := c.Types[value.Ref.Heap.TypeIndex]
		return target.Kind == CompositeTypeStruct || target.Kind == CompositeTypeArray || (opaqueNoCollect || ownedFunctionRoots || c.functionEnvironmentFrameRoots()) && target.Kind == CompositeTypeFunction
	}
	rootedAbstractGlobal := func(heap AbstractHeapType) bool {
		switch heap {
		case AbstractHeapExtern, AbstractHeapNoExtern:
			return opaqueNoCollect || c.compactExternFrameRoots()
		case AbstractHeapFunc, AbstractHeapNoFunc:
			return opaqueNoCollect || c.functionEnvironmentFrameRoots()
		case AbstractHeapNone:
			return opaqueNoCollect
		case AbstractHeapExn, AbstractHeapNoExn:
			return c.needsOwnedExceptionReferences() && rv64ExceptionHandling(c)
		case AbstractHeapAny, AbstractHeapEq, AbstractHeapI31, AbstractHeapStruct, AbstractHeapArray:
			return true
		default:
			return false
		}
	}
	rootedExportValue := func(value ValueTypeDescriptor) bool {
		if value.Kind <= ValueTypeV128 {
			return value.Ref == (ReferenceTypeDescriptor{})
		}
		if value.Kind != ValueTypeReference {
			return false
		}
		if !value.Ref.Heap.Defined {
			return !value.Ref.Exact && rootedAbstractGlobal(value.Ref.Heap.Abstract) && value.Ref.Heap.TypeIndex == 0
		}
		return rootedValue(value, len(c.Types))
	}
	for i := range c.Globals {
		global := &c.Globals[i]
		if !isGCRefValType(global.Type) {
			continue
		}
		exact, err := c.globalExactType(i)
		abiType, abiOK := exact.ABIType(c.Types)
		rootedGlobalType := exact.Ref.Heap.Defined || rootedAbstractGlobal(exact.Ref.Heap.Abstract)
		if err != nil || !abiOK || global.Type != abiType || exact.Kind != ValueTypeReference ||
			exact.Ref.Exact || !rootedGlobalType || !rootedValue(exact, len(c.Types)) ||
			!exact.Ref.Heap.Defined && !exact.Ref.Nullable ||
			(global.Type != ValI31Ref && global.Bits != 0 || global.Type == ValI31Ref && (global.Bits>>32 != 0 || global.Bits != 0 && global.Bits&1 == 0)) || global.HasInitFunc || global.InitFunc != 0 ||
			(global.HasInitGlobal && (len(global.InitExpr) != 0 || !rv64InitializerReferenceGlobal(c, global.InitGlobal, exact, i))) ||
			(!global.HasInitGlobal && (global.InitGlobal != 0 || i >= len(c.GlobalImports) && len(global.InitExpr) == 0 && !exact.Ref.Nullable)) {
			return false
		}
	}
	for _, index := range c.GlobalExports {
		if index < 0 || index >= len(c.Globals) {
			return false
		}
		if isGCRefValType(c.Globals[index].Type) {
			exact, err := c.globalExactType(index)
			if err != nil || !rootedExportValue(exact) {
				return false
			}
		}
	}
	storageKind := func(storage StorageTypeDescriptor, owner int) (gc.StorageKind, bool) {
		if storage.Packed {
			switch storage.PackedType {
			case PackedTypeI8:
				return gc.StorageI8, true
			case PackedTypeI16:
				return gc.StorageI16, true
			default:
				return 0, false
			}
		}
		if storage.Value.Kind == ValueTypeReference {
			ref := storage.Value.Ref
			if !ref.Heap.Defined {
				if ref.Exact || ref.Heap.TypeIndex != 0 {
					return 0, false
				}
				switch ref.Heap.Abstract {
				case AbstractHeapExn, AbstractHeapNoExn:
					if !c.needsOwnedExceptionReferences() || !rv64ExceptionHandling(c) {
						return 0, false
					}
					if ref.Nullable {
						return gc.StorageRefNull, true
					}
					return gc.StorageRef, true
				case AbstractHeapExtern, AbstractHeapNoExtern:
					if !c.compactExternFrameRoots() {
						return 0, false
					}
					if ref.Nullable {
						return gc.StorageExternRefNull, true
					}
					return gc.StorageExternRef, true
				case AbstractHeapFunc:
					if ref.Nullable {
						return gc.StorageFuncRefNull, true
					}
					return gc.StorageFuncRef, true
				case AbstractHeapNone, AbstractHeapAny, AbstractHeapEq, AbstractHeapI31, AbstractHeapStruct, AbstractHeapArray:
					if ref.Nullable {
						return gc.StorageRefNull, true
					}
					return gc.StorageRef, true
				default:
					return 0, false
				}
			}
			targetIndex := int(ref.Heap.TypeIndex)
			if targetIndex >= len(c.Types) || targetIndex > owner && c.Types[targetIndex].RecGroup != c.Types[owner].RecGroup ||
				ref.Exact && c.Types[targetIndex].RecGroup == c.Types[owner].RecGroup {
				return 0, false
			}
			target := c.Types[ref.Heap.TypeIndex]
			if target.Kind == CompositeTypeFunction && !ref.Exact && c.functionEnvironmentFrameRoots() {
				if ref.Nullable {
					return gc.StorageFuncRefNull, true
				}
				return gc.StorageFuncRef, true
			}
			if target.Kind != CompositeTypeStruct && target.Kind != CompositeTypeArray {
				return 0, false
			}
			if ref.Nullable {
				return gc.StorageRefNull, true
			}
			return gc.StorageRef, true
		}
		if storage.Value.Ref != (ReferenceTypeDescriptor{}) {
			return 0, false
		}
		switch storage.Value.Kind {
		case ValueTypeI32:
			return gc.StorageI32, true
		case ValueTypeI64:
			return gc.StorageI64, true
		case ValueTypeF32:
			return gc.StorageF32, true
		case ValueTypeF64:
			return gc.StorageF64, true
		case ValueTypeV128:
			return gc.StorageV128, true
		default:
			return 0, false
		}
	}
	hasStruct, hasArray := false, false
	groupSizes := make([]int, len(c.Types))
	for i := range c.Types {
		if int(c.Types[i].RecGroup) >= len(groupSizes) {
			return false
		}
		groupSizes[c.Types[i].RecGroup]++
	}
	requiredSlots := 4
	if arrayHelpers {
		requiredSlots = rv64GenericArrayFixedSlots
	}
	for i, desc := range c.GCTypeDescs {
		typ := &c.Types[i]
		if typ.HasDescribes || typ.HasDescriptor {
			return false
		}
		if groupSizes[typ.RecGroup] > 1 && typ.Kind != CompositeTypeFunction && (!typ.Final || len(typ.Supers) != 0) {
			return false
		}
		if typ.Kind == CompositeTypeFunction {
			if len(typ.Supers) > 1 {
				return false
			}
		} else if typ.Kind == CompositeTypeStruct || typ.Kind == CompositeTypeArray {
			if len(typ.Supers) > 1 {
				return false
			}
			if len(typ.Supers) == 1 {
				super := typ.Supers[0]
				if uint64(super) >= uint64(i) || c.Types[super].Kind != typ.Kind || c.Types[super].Final {
					return false
				}
			}
		} else {
			return false
		}
		wantSuper := len(typ.Supers) == 1
		var super gc.TypeID
		if wantSuper {
			super = gc.TypeID(typ.Supers[0])
		}
		if desc.Final != typ.Final || desc.HasSuper != wantSuper || desc.Super != super {
			return false
		}
		switch desc.Kind {
		case gc.KindFunc:
			if typ.Kind != CompositeTypeFunction || len(typ.Params) > 256 || len(typ.Results) > 256 || len(desc.Fields) != 0 || desc.Elem != 0 || desc.HasRefs {
				return false
			}
			for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
				for _, value := range values {
					// Function signatures also describe validated inline custom
					// imports. Opaque carriers are metadata here; source root maps
					// and native binding admission govern their executable uses.
					if !rootedValue(value, i) && !rv64PrimitiveReferenceValue(c, value) {
						return false
					}
				}
			}
		case gc.KindStruct:
			if !wasmGCBuilt || typ.Kind != CompositeTypeStruct || len(typ.Fields) > 255 || len(typ.Fields) != len(desc.Fields) {
				return false
			}
			kinds := make([]gc.StorageKind, len(typ.Fields))
			for field := range typ.Fields {
				kind, ok := storageKind(typ.Fields[field].Storage, i)
				if !ok {
					return false
				}
				kinds[field] = kind
			}
			expected, err := gc.NewStructDesc(gc.TypeID(i), kinds)
			expected.Final, expected.Super, expected.HasSuper = typ.Final, super, wantSuper
			if err != nil || desc.ID != expected.ID || desc.Kind != expected.Kind || desc.Elem != expected.Elem || desc.Size != expected.Size ||
				desc.ElemSize != expected.ElemSize || desc.Align != expected.Align || desc.HasRefs != expected.HasRefs || desc.Final != expected.Final ||
				desc.Super != expected.Super || desc.HasSuper != expected.HasSuper || !slices.Equal(desc.Fields, expected.Fields) {
				return false
			}
			requiredSlots = max(requiredSlots, len(typ.Fields)+1)
			hasStruct = true
		case gc.KindArray:
			if !wasmGCBuilt || typ.Kind != CompositeTypeArray || len(desc.Fields) != 0 {
				return false
			}
			kind, ok := storageKind(typ.Array.Storage, i)
			if !ok {
				return false
			}
			expected, err := gc.NewArrayDesc(gc.TypeID(i), kind)
			expected.Final, expected.Super, expected.HasSuper = typ.Final, super, wantSuper
			if err != nil || desc.ID != expected.ID || desc.Kind != expected.Kind || desc.Elem != expected.Elem || desc.Size != expected.Size ||
				desc.ElemSize != expected.ElemSize || desc.Align != expected.Align || desc.HasRefs != expected.HasRefs || desc.Final != expected.Final ||
				desc.Super != expected.Super || desc.HasSuper != expected.HasSuper {
				return false
			}
			hasArray = true
		default:
			return false
		}
	}
	if err := c.validateGCTypeDescriptors(); err != nil {
		return false
	}
	if len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return false
	}
	groupCount := int(c.Types[len(c.Types)-1].RecGroup) + 1
	groupStarts := make([]uint32, groupCount)
	groupMembers := make([]int, groupCount)
	for i := range c.Types {
		group := int(c.Types[i].RecGroup)
		if groupMembers[group] == 0 {
			groupStarts[group] = uint32(i)
		}
		groupMembers[group]++
	}
	structural := &wasm.Module{Types: make([]wasm.RecType, groupCount)}
	for group := range structural.Types {
		structural.Types[group].SubTypes = make([]wasm.SubType, 0, groupMembers[group])
	}
	toWasmValue := func(owner int, value ValueTypeDescriptor) (wasm.ValType, bool) {
		switch value.Kind {
		case ValueTypeI32:
			return wasm.I32, true
		case ValueTypeI64:
			return wasm.I64, true
		case ValueTypeF32:
			return wasm.F32, true
		case ValueTypeF64:
			return wasm.F64, true
		case ValueTypeV128:
			return wasm.V128, true
		case ValueTypeReference:
			if value.Ref.Heap.Defined {
				index := wasm.TypeIdx{Index: value.Ref.Heap.TypeIndex}
				if c.Types[value.Ref.Heap.TypeIndex].RecGroup == c.Types[owner].RecGroup {
					index.Index -= groupStarts[c.Types[owner].RecGroup]
					index.Rec = true
				}
				return wasm.RefVal(wasm.Ref(value.Ref.Nullable, wasm.IndexedHeap(index), value.Ref.Exact)), true
			}
			heap, ok := rootedAbstract(value.Ref.Heap.Abstract)
			if !ok && c.Types[owner].Kind == CompositeTypeFunction && rv64PrimitiveReferenceValue(c, value) {
				switch value.Ref.Heap.Abstract {
				case AbstractHeapFunc:
					heap, ok = wasm.HeapFunc, true
				case AbstractHeapNoFunc:
					heap, ok = wasm.HeapNoFunc, true
				case AbstractHeapExtern:
					heap, ok = wasm.HeapExtern, true
				case AbstractHeapNoExtern:
					heap, ok = wasm.HeapNoExtern, true
				}
			}
			if !ok || value.Ref.Heap.TypeIndex != 0 || value.Ref.Exact {
				return wasm.ValType{}, false
			}
			return wasm.RefVal(wasm.Ref(value.Ref.Nullable, wasm.AbsHeap(heap), false)), true
		default:
			return wasm.ValType{}, false
		}
	}
	toWasmField := func(owner int, field FieldTypeDescriptor) (wasm.FieldType, bool) {
		var storage wasm.StorageType
		if field.Storage.Packed {
			switch field.Storage.PackedType {
			case PackedTypeI8:
				storage = wasm.StoragePacked(wasm.PackI8)
			case PackedTypeI16:
				storage = wasm.StoragePacked(wasm.PackI16)
			default:
				return wasm.FieldType{}, false
			}
		} else if value := field.Storage.Value; value.Kind == ValueTypeReference && !value.Ref.Heap.Defined &&
			!value.Ref.Exact && value.Ref.Heap.Abstract == AbstractHeapFunc && value.Ref.Heap.TypeIndex == 0 {
			storage = wasm.StorageVal(wasm.RefVal(wasm.Ref(value.Ref.Nullable, wasm.AbsHeap(wasm.HeapFunc), false)))
		} else {
			value, ok := toWasmValue(owner, field.Storage.Value)
			if !ok {
				return wasm.FieldType{}, false
			}
			storage = wasm.StorageVal(value)
		}
		mut := wasm.Const
		if field.Mutable {
			mut = wasm.Var
		}
		return wasm.NewFieldType(storage, mut), true
	}
	for i := range c.Types {
		typ := &c.Types[i]
		comp := wasm.CompType{}
		switch typ.Kind {
		case CompositeTypeFunction:
			comp.Kind = wasm.CompFunc
			comp.Params = make([]wasm.ValType, len(typ.Params))
			comp.Results = make([]wasm.ValType, len(typ.Results))
			for j := range typ.Params {
				var ok bool
				comp.Params[j], ok = toWasmValue(i, typ.Params[j])
				if !ok {
					return false
				}
			}
			for j := range typ.Results {
				var ok bool
				comp.Results[j], ok = toWasmValue(i, typ.Results[j])
				if !ok {
					return false
				}
			}
		case CompositeTypeStruct:
			comp.Kind = wasm.CompStruct
			comp.Fields = make([]wasm.FieldType, len(typ.Fields))
			for j := range typ.Fields {
				var ok bool
				comp.Fields[j], ok = toWasmField(i, typ.Fields[j])
				if !ok {
					return false
				}
			}
		case CompositeTypeArray:
			comp.Kind = wasm.CompArray
			var ok bool
			comp.Array, ok = toWasmField(i, typ.Array)
			if !ok {
				return false
			}
		default:
			return false
		}
		sub := wasm.SubType{Final: typ.Final, HasPrefix: !typ.Final || len(typ.Supers) != 0, Comp: comp}
		if len(typ.Supers) == 1 {
			index := wasm.TypeIdx{Index: typ.Supers[0]}
			if c.Types[typ.Supers[0]].RecGroup == typ.RecGroup {
				index.Index -= groupStarts[typ.RecGroup]
				index.Rec = true
			}
			sub.Supers = []wasm.TypeIdx{index}
		}
		group := typ.RecGroup
		structural.Types[group].SubTypes = append(structural.Types[group].SubTypes, sub)
	}
	if err := wasm.ValidateTypeGraph(structural); err != nil {
		return false
	}
	validateFunction := func(sig *FuncSig, ordinal int) bool {
		if !sig.HasTypeIndex || int(sig.TypeIndex) >= len(c.Types) {
			return false
		}
		typ := &c.Types[sig.TypeIndex]
		if typ.Kind != CompositeTypeFunction || len(sig.Params) != len(typ.Params) || len(sig.Results) != len(typ.Results) {
			return false
		}
		key, ok := structural.StructuralTypeKeyChecked(sig.TypeIndex)
		if !ok || ordinal < 0 || ordinal >= len(c.FuncTypeID) || c.FuncTypeID[ordinal] != key {
			return false
		}
		for i, exact := range typ.Params {
			abi, ok := exact.ABIType(c.Types)
			if !ok || sig.Params[i] != abi {
				return false
			}
		}
		for i, exact := range typ.Results {
			abi, ok := exact.ABIType(c.Types)
			if !ok || sig.Results[i] != abi {
				return false
			}
		}
		return true
	}
	for function := range c.importFuncSigs {
		if !validateFunction(&c.importFuncSigs[function], function) {
			return false
		}
	}
	for function := range c.Funcs {
		if !validateFunction(&c.Funcs[function], c.NumImports+function) {
			return false
		}
	}
	for _, function := range c.Exports {
		if function < 0 || function >= c.NumImports+len(c.Funcs) {
			return false
		}
		var sig *FuncSig
		if function < c.NumImports {
			sig = &c.importFuncSigs[function]
		} else {
			sig = &c.Funcs[function-c.NumImports]
		}
		if !sig.HasTypeIndex || int(sig.TypeIndex) >= len(c.Types) {
			return false
		}
		typ := &c.Types[sig.TypeIndex]
		for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
			for _, value := range values {
				if !rootedExportValue(value) {
					return false
				}
			}
		}
	}
	// The struct-dispatch helper family also owns dynamic ref.test/ref.cast for
	// both defined aggregate kinds. An array-only rooted module can therefore
	// require it without declaring a struct descriptor. The fresh compiler
	// receipt and code digest authenticate the actual helper callsites.
	valid := (!structHelpers || hasStruct || hasArray) && (!arrayHelpers || hasArray) && int(c.syncHostSlots) >= requiredSlots
	if valid && contract != nil && c.loadValidateMemo().executionView() == c {
		contract.rooted.Store(c)
	}
	return valid
}

// rv64StateOnlyMutableFuncrefImport is the persisted-artifact witness for the
// first mutable funcref-global boundary. It admits exactly one store-bound,
// nullable funcref cell which is imported and re-exported without executable
// code. Executable reads, the exact null-only write, and broader mixed state use
// separate admission and lowering.
func rv64StateOnlyMutableFuncrefImport(c *Compiled) bool {
	if !rv64MutableFuncrefGlobalsForImage || c == nil || !c.hasNoFunctionCode() || c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Types) != 0 || len(c.GCTypeDescs) != 0 || len(c.FuncTypeID) != 0 || len(c.Funcs) != 0 || len(c.Exports) != 0 {
		return false
	}
	if len(c.Globals) != 1 || len(c.GlobalImports) != 1 || len(c.GlobalExports) != 1 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 || c.tagImportCount() != 0 || c.HasStart {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 {
		return false
	}
	for _, index := range c.GlobalExports {
		if index != 0 {
			return false
		}
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imp, global := c.GlobalImports[0], c.Globals[0]
	exact, err := c.globalExactType(0)
	if err != nil || exact != plain || c.ValueTypes[0] != plain || !imp.HasValueType || imp.ValueTypeIndex != 0 || imp.Type != ValFuncRef || !imp.Mutable {
		return false
	}
	if global.Type != ValFuncRef || !global.Mutable || !global.HasValueType || global.ValueTypeIndex != 0 || global.HasInitFunc || global.InitFunc != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) {
		return false
	}
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal
	return c.requiredFeatures == required && compiledStructuralRequiredFeatures(c) == required
}

// Loading and hand-built metadata must enforce the same bounded public ABI as
// source compilation before any executable image or host thunk is published.
func railshotValidateCompiled(c *Compiled) error {
	if !tablesForImage && c != nil && (c.hasTableDeclarationMetadata() || c.nativeTable64ABI != nativeTable64NoABI || c.requiredFeatures.IsEnabled(CoreFeatureTable64)) {
		return fmt.Errorf("%w: public RV64 table declarations exceed the standalone runtime profile", railshot.ErrUnsupported)
	}
	if !multiTableForImage && c != nil && len(c.extraTables) != 0 {
		return fmt.Errorf("%w: public RV64 table declarations exceed the standalone runtime profile", railshot.ErrUnsupported)
	}
	if !tableImportsForImage && c != nil && c.tableImport != "" {
		return fmt.Errorf("%w: public RV64 table imports exceed the standalone runtime profile", railshot.ErrUnsupported)
	}
	if !globalsForImage && c != nil && (len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0) {
		return fmt.Errorf("%w: public RV64 globals exceed the standalone runtime profile", railshot.ErrUnsupported)
	}
	if !globalImportsForImage && c != nil && len(c.GlobalImports) != 0 {
		return fmt.Errorf("%w: public RV64 global imports exceed the standalone runtime profile", railshot.ErrUnsupported)
	}
	if !memoriesForImage && c != nil && (c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 || c.activeDataCount() != 0) {
		return fmt.Errorf("%w: public RV64 memories exceed the standalone runtime profile", railshot.ErrUnsupported)
	}
	if c != nil && (!activeDataForImage && c.activeDataCount() != 0 || !passiveDataForImage && len(c.PassiveData) != 0) {
		return fmt.Errorf("%w: public RV64 data exceeds the standalone runtime profile", railshot.ErrUnsupported)
	}
	if !rv64MutableFuncrefGlobalsForImage && c != nil && (c.rv64ExecutionContract() == nil || c.hasMutableFuncrefGlobals()) {
		return fmt.Errorf("%w: public RV64 mutable funcref global profile requires a bound image without mutable funcref globals", railshot.ErrUnsupported)
	}
	if c != nil && (!activeElementsForImage && len(c.Elems) != 0 || !passiveElementsForImage && len(c.passiveElems) != 0) {
		return fmt.Errorf("%w: public RV64 element declarations exceed the standalone runtime profile", railshot.ErrUnsupported)
	}
	if !hostFunctionImportsBuilt && c != nil && c.NumImports != 0 {
		return fmt.Errorf("%w: public RV64 function imports exceed the standalone runtime profile", railshot.ErrUnsupported)
	}
	if !syncHostControlForImage && railshotRequiresNativeHostControl(c) {
		return fmt.Errorf("%w: public RV64 native host control exceeds the standalone runtime profile", railshot.ErrUnsupported)
	}
	if !exceptionHandlingForImage && c != nil && c.requiredFeatures.IsEnabled(CoreFeatureExceptionHandling) {
		return fmt.Errorf("%w: public RV64 exception handling exceeds the standalone runtime profile", railshot.ErrUnsupported)
	}
	if err := c.validateCompactData(); err != nil {
		return fmt.Errorf("%w: public RV64 %v", railshot.ErrUnsupported, err)
	}
	if !referenceSignaturesBuilt && c != nil && (c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI) {
		return fmt.Errorf("%w: public RV64 reference ABI is disabled in this build", railshot.ErrUnsupported)
	}
	if c != nil && (!rv64ReferenceFunctionsForImage && c.nativeReferenceFunctionABI ||
		!rv64ReferenceCallsForImage && (c.nativeReferenceCallABI || c.nativeReferenceControlABI)) {
		return fmt.Errorf("%w: public RV64 reference ABI exceeds the standalone runtime profile", railshot.ErrUnsupported)
	}
	if c != nil && c.rv64ExecutionContract() != nil {
		// A general contract cannot fall back to an older image profile when
		// its bound code or metadata changes, including for state-only modules.
		if !rv64SourceContractValid(c) {
			return fmt.Errorf("%w: RV64 native execution contract lacks admitted source or artifact metadata", railshot.ErrUnsupported)
		}
		if compiledStructuralRequiredFeatures(c)&^c.requiredFeatures != 0 {
			return fmt.Errorf("%w: RV64 native execution feature contract", railshot.ErrUnsupported)
		}
	} else if standaloneBareImage && !rv64LegacyGCProductsForImage && wasmGCBuilt && c != nil && c.requiredFeatures.IsEnabled(CoreFeatureGC) {
		return fmt.Errorf("%w: RV64 standalone GC requires its native execution contract", railshot.ErrUnsupported)
	}
	// A state-only module has no native entry or imported execution bridge.
	// Its tables, globals, descriptors, and initializers still pass the common
	// metadata and resource checks in Compiled.validate.
	if c != nil && c.hasNoFunctionCode() {
		return nil
	}
	if !functionCodeForImage {
		return fmt.Errorf("%w: public RV64 function code exceeds the standalone runtime profile", railshot.ErrUnsupported)
	}
	reject := func(detail string) error { return fmt.Errorf("%w: public RV64 %s", railshot.ErrUnsupported, detail) }
	mutableFuncrefGlobalGet := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalGet(c)
	mutableFuncrefGlobalClear := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalClear(c)
	mutableFuncrefGlobalStoreSelf := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalStoreSelf(c)
	mutableFuncrefGlobalCopy := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalCopy(c)
	mutableFuncrefGlobalMove := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalMove(c)
	mutableFuncrefGlobalBidirectionalCopy := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalBidirectionalCopy(c)
	mutableFuncrefGlobalLocalCopy := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalLocalCopy(c)
	mutableFuncrefGlobalLocalPublish := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalLocalPublish(c)
	mutableFuncrefGlobalLocalTransfer := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalLocalTransfer(c)
	mutableFuncrefGlobalLocalExport := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalLocalExport(c)
	mutableFuncrefGlobalLocalObserve := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalLocalObserve(c)
	mutableFuncrefGlobalLocalCall := referenceSignaturesBuilt && rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalLocalCall(c)
	freshRootedGC := rv64FreshRootedGC(c)
	qualifiedEH := rv64ExceptionHandling(c)
	primitiveReferences := rv64PrimitiveReferenceContract(c)
	functionOnlyTypes := compiledFunctionOnlyTypeGraph(c)
	// Dynamic tail metadata is executable state. Require its source or artifact
	// receipt before any declared type graph can grant execution permission.
	if rv64HasDynamicTail(rv64NativeTailModule(c)) && !rv64SourceContractValid(c) && c.rv64ExecutionContract() == nil {
		return reject("dynamic tail requires admitted source or artifact metadata")
	}
	// A verified general contract binds the full code and metadata. Legacy
	// image fingerprints remain required when that broader proof is absent.
	if !primitiveReferences {
		if rv64MutableFuncrefGlobalGetExecutionCode(c) && !mutableFuncrefGlobalGet {
			return reject("mutable funcref global.get image requires its exact retained metadata")
		}
		if rv64MutableFuncrefGlobalClearExecutionCode(c) && !mutableFuncrefGlobalClear {
			return reject("mutable funcref global.clear image requires its exact retained metadata")
		}
		if rv64MutableFuncrefGlobalStoreSelfExecutionCode(c) && !mutableFuncrefGlobalStoreSelf {
			return reject("mutable funcref self-store image requires its exact retained metadata")
		}
		if rv64MutableFuncrefGlobalCopyExecutionCode(c) && !mutableFuncrefGlobalCopy {
			return reject("mutable funcref copy image requires its exact retained metadata")
		}
		if rv64MutableFuncrefGlobalMoveExecutionCode(c) && !mutableFuncrefGlobalMove {
			return reject("mutable funcref move image requires its exact retained metadata")
		}
		if rv64MutableFuncrefGlobalBidirectionalCopyExecutionCode(c) && !mutableFuncrefGlobalBidirectionalCopy {
			return reject("mutable funcref bidirectional-copy image requires its exact retained metadata")
		}
		if rv64MutableFuncrefGlobalLocalCopyExecutionCode(c) && !mutableFuncrefGlobalLocalCopy {
			return reject("mutable funcref local-copy image requires its exact retained metadata")
		}
		if rv64MutableFuncrefGlobalLocalPublishExecutionCode(c) && !mutableFuncrefGlobalLocalPublish {
			return reject("mutable funcref local-publish image requires its exact retained metadata")
		}
		if rv64MutableFuncrefGlobalLocalTransferExecutionCode(c) && !mutableFuncrefGlobalLocalTransfer {
			return reject("mutable funcref local-transfer image requires its exact retained metadata")
		}
		if rv64MutableFuncrefGlobalLocalExportExecutionCode(c) && !mutableFuncrefGlobalLocalExport {
			return reject("mutable funcref local-export image requires its exact retained metadata")
		}
		if rv64MutableFuncrefGlobalLocalObserveExecutionCode(c) && !mutableFuncrefGlobalLocalObserve {
			return reject("mutable funcref local-observe image requires its exact retained metadata")
		}
		if rv64MutableFuncrefGlobalLocalCallExecutionCode(c) && !mutableFuncrefGlobalLocalCall {
			return reject("mutable funcref local-call image requires its exact retained metadata")
		}
	}
	var legacyGC rv64LegacyGCProducts
	if rv64LegacyGCProductsForImage {
		var err error
		legacyGC, err = admitRV64LegacyGCProducts(c, primitiveReferences, freshRootedGC, reject)
		if err != nil {
			return err
		}
	}
	if rv64LegacyI31ProductsForImage && rv64I31CoreExecutionCode(c) && !rv64I31Core(c) {
		return reject("i31 execution image requires its exact retained metadata")
	}
	i31 := i31ExecutionForImage && rv64I31Core(c)
	if c != nil && c.stagedGCI31Product() != 0 && !i31 && !freshRootedGC {
		return reject("i31 execution product requires its exact retained image")
	}
	immediateReferences := i31ExecutionForImage && rv64ImmediateReferenceContract(c)
	generalI31 := i31ExecutionForImage && rv64GeneralI31Contract(c)
	exactAnyI31 := rv64LegacyI31ProductsForImage && rv64I31OfficialAnyGlobal(c)
	typedI31 := immediateReferences || rv64LegacyI31ProductsForImage &&
		(rv64I31OfficialCore(c) || rv64I31OfficialTableGlobalInitializer(c) || rv64I31OfficialGlobalInitializer(c) || exactAnyI31)
	exactCore3RefIsNull := rv64Core3ReferenceWitnessesForImage && rv64Core3RefIsNullExecution(c)
	// General function-only GC images require the binding that joins their code
	// and declarations. Old artifacts retain their independent exact proof.
	if functionOnlyTypes && c.requiredFeatures.IsEnabled(CoreFeatureGC) && c.rv64ExecutionContract() == nil && !freshRootedGC && !qualifiedEH && !typedI31 && !exactCore3RefIsNull {
		if !legacyGC.admitsFunctionOnlySubtype() {
			return reject("function-only GC execution requires its general binding or a legacy proof")
		}
	}
	// Profiles requiring a validated execution contract already reject unbound code.
	if (rv64Core3ReferenceWitnessesForImage || rv64MutableFuncrefGlobalsForImage) &&
		rv64Core3RefIsNullExecutionCode(c) && !exactCore3RefIsNull {
		return reject("Core 3 ref.is_null image requires its exact retained metadata")
	}
	if !c.nativeContextTrapABI {
		return reject("native-context trap ABI is required")
	}
	if len(c.code) > railshot.MaxPublicCodeBytes || uint64(len(c.Types)) > uint64(railshot.MaxPublicCodeBytes)/8 || len(c.GCTypeDescs) > len(c.Types) || len(c.Globals) > railshot.MaxPublicGlobals {
		return reject("bounded artifact shape")
	}
	if len(c.Funcs) == 0 && c.NumImports == 0 && !c.hasNoFunctionCode() {
		return reject("state-only artifact contains code or function entries")
	}
	if activeDataForImage || passiveDataForImage {
		if detail := rv64DataBudgetError(c); detail != "" {
			return reject(detail)
		}
	}
	if c.NumImports < 0 || c.NumImports > railshot.MaxFunctionImports || len(c.Imports) != c.NumImports || len(c.importFuncSigs) != c.NumImports || uint64(c.NumImports)+uint64(len(c.Funcs)) > uint64(railshot.MaxPublicCodeBytes)/16 || c.dynamicImports != (c.NumImports > 0) {
		return reject("dynamic function import shape")
	}
	publicImports := c.NumImports + len(c.GlobalImports)
	if memoryImportsForImage {
		publicImports += c.memoryImportCount()
	}
	if tableImportsForImage {
		publicImports += c.tableImportCount()
	}
	if publicImports > railshot.MaxPublicImports {
		return reject("aggregate import bound")
	}
	if rv64ReferenceCallsForImage && referenceSignaturesBuilt && !freshRootedGC && !qualifiedEH {
		if err := railshotValidateReferenceCalls(c); err != nil {
			return reject(err.Error())
		}
	}
	if err := rv64ValidateNativeTail(c); err != nil {
		return reject(err.Error())
	}
	if rv64ReferenceFunctionsForImage && referenceSignaturesBuilt && !freshRootedGC && !qualifiedEH && (!rv64LegacyGCProductsForImage || !legacyGC.typeSubtypingRefFuncGlobals && !legacyGC.admitsLinkedSubtype()) {
		if err := rv64ValidateReferenceFunctions(c); err != nil {
			return reject(err.Error())
		}
	}
	if m := rv64NativeTailModule(c); m != nil {
		for _, p := range m.Plans {
			if p.Kind > 2 {
				return reject("dynamic tail requires a qualified native-owner plan")
			}
		}
	}
	vectorProfile := c.requiredFeatures.IsEnabled(CoreFeatureSIMD)
	if hostFunctionImportsBuilt && c.NumImports != 0 {
		if !c.needsFuncRefContextHeader {
			return reject("host imports require an instance context anchor")
		}
		maxImportArity := railshot.MaxVectorFunctionValues
		publicHostReferenceBoundary := primitiveReferences || rv64PublicHostReferenceBoundary(c) || rv64PublicDynamicHostReferenceBoundary(c) || legacyGC.admitsLinkedSubtype()
		for _, sig := range c.importFuncSigs {
			if len(sig.Params) > maxImportArity || len(sig.Results) > maxImportArity {
				return reject("function import signature arity")
			}
			for _, values := range [][]ValType{sig.Params, sig.Results} {
				for _, typ := range values {
					if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 && (!vectorProfile || typ != ValV128) &&
						(!(publicHostReferenceBoundary || c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes)) || typ != ValFuncRef && typ != ValExternRef) && (!freshRootedGC || !isGCRefValType(typ)) {
						return reject("unsupported function import signature")
					}
				}
			}
		}
	}
	if c.HasStart {
		if _, err := compiledStartIndex(c); err != nil {
			return reject(err.Error())
		}
	}

	if memoriesForImage && (c.boundsMode == BoundsChecksSignalsBased && c.memoryCount() != 0 && !rv64GuardedMemoryProfile(c) || c.memoryCount() > railshot.MaxProductionMemories) ||
		c.genericGCFrameRoots() != nil && !freshRootedGC || c.usesDynamicFuncRefTest() && !primitiveReferences && !freshRootedGC {
		return reject("resource/host/reference lowering is pending")
	}
	mutableGlobalImports := mutableFuncrefGlobalGet || mutableFuncrefGlobalClear || mutableFuncrefGlobalStoreSelf ||
		mutableFuncrefGlobalCopy || mutableFuncrefGlobalMove || mutableFuncrefGlobalBidirectionalCopy ||
		mutableFuncrefGlobalLocalCopy || mutableFuncrefGlobalLocalPublish || mutableFuncrefGlobalLocalTransfer
	if globalImportsForImage {
		if detail := rv64GlobalImportError(c, primitiveReferences, freshRootedGC, mutableGlobalImports); detail != "" {
			return reject(detail)
		}
	}
	for _, desc := range c.GCTypeDescs {
		if !freshRootedGC && !generalI31 && !qualifiedEH && !legacyGC.admitted() && (desc.Kind != gc.KindFunc || !primitiveReferences && !functionOnlyTypes && (!desc.Final || desc.HasSuper)) {
			if !primitiveReferences {
				return reject("GC descriptor lowering is pending")
			}
		}
	}
	qualifiedFeatures := platformCoreFeatures() | CoreFeatureTailCall
	if qualifiedEH {
		qualifiedFeatures |= CoreFeatureExceptionHandling
	}
	if memoriesForImage && rv64ThreadedMemoryProfile(c) {
		qualifiedFeatures |= CoreFeatureThreads
	}
	if freshRootedGC {
		qualifiedFeatures |= CoreFeatureGC | CoreFeatureTypedFunctionReferences
	}
	if primitiveReferences {
		// Aggregate declarations can participate in canonical function identity
		// without requiring collector-backed execution.
		qualifiedFeatures |= CoreFeatureGC | CoreFeatureTypedFunctionReferences
	}
	if i31 {
		qualifiedFeatures |= CoreFeatureGC
		if typedI31 {
			qualifiedFeatures |= CoreFeatureTypedFunctionReferences
		}
	}
	if legacyGC.admitted() {
		qualifiedFeatures |= CoreFeatureGC
	}
	if rv64LegacyGCProductsForImage && legacyGC.typedReferences {
		qualifiedFeatures |= CoreFeatureTypedFunctionReferences
	}
	if rv64LegacyGCProductsForImage && legacyGC.bulkMemory {
		qualifiedFeatures |= CoreFeatureBulkMemoryOperations
	}
	if tablesForImage {
		if detail := rv64TableWitnessError(c, primitiveReferences); detail != "" {
			return reject(detail)
		}
	}
	// Dead validated reference-tail instructions need no emitted plan or ABI
	// flag. Their retained syntax feature still needs its full code binding.
	emptyTypedTail := rv64NativeTailModule(c) != nil && len(rv64NativeTailModule(c).Plans) == 0 && rv64SourceContractValid(c)
	if primitiveReferences || functionOnlyTypes {
		qualifiedFeatures |= CoreFeatureGC
	}
	if functionOnlyTypes {
		qualifiedFeatures |= CoreFeatureTypedFunctionReferences
	}
	if rv64HasReferenceTail(c) || c.nativeReferenceCallABI || c.nativeReferenceControlABI || emptyTypedTail {
		qualifiedFeatures |= CoreFeatureTypedFunctionReferences
	}
	if c.requiredFeatures & ^qualifiedFeatures != 0 || c.requiredAMD64Features != 0 || c.preparedIsolatedTables {
		return reject("feature or ISA contract")
	}
	for i, entry := range c.Entry {
		if entry&15 != 0 || i >= len(c.InternalEntry) {
			return reject("entry is not an aligned framed wrapper")
		}
		internal := c.InternalEntry[i]
		adapter := internalEntryOffset(internal) != entry && rv64DirectIntegerAdapter(c, i)
		if internalEntryOffset(internal) != entry && !adapter || !rv64DirectIntegerMarkerShape(internal, adapter) {
			return reject("entry lacks its exact checked integer adapter")
		}
	}

	if globalsForImage && vectorProfile {
		if len(c.GlobalExports) > railshot.MaxModuleExports {
			return reject("global export count bound")
		}
		for _, index := range c.GlobalExports {
			if index < 0 || index >= len(c.Globals) {
				return reject("global export index bound")
			}
		}
	}
	maxArity := 256
	if vectorProfile {
		maxArity = railshot.MaxVectorFunctionValues
	}
	declarationExtern := c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || primitiveReferences || legacyGC.admitsExternrefSignature() || exactCore3RefIsNull ||
		rv64Core3ReferenceWitnessesForImage && rv64Core3ReferenceDeclarationWitness(c)
	if referenceSignaturesBuilt && !declarationExtern {
		mutableReferenceCarrier := rv64MutableFuncrefGlobalLocalCallCodeVariant(c)
		declarationExtern = rv64PublicReferenceCallBoundary(c) || rv64PublicHostReferenceBoundary(c) || rv64PublicDynamicHostReferenceBoundary(c) || mutableReferenceCarrier >= 19 && mutableReferenceCarrier <= 28
	}
	freshRootedReference := func(typ ValType) bool {
		roots := c.genericGCFrameRoots()
		opaqueNoCollect := qualifiedEH && c.needsOwnedExceptionReferences() && roots != nil && len(roots.safepoints) == 0 && len(roots.callsites) == 0
		return freshRootedGC && (typ == ValAnyRef || typ == ValI31Ref || opaqueNoCollect && (typ == ValFuncRef || typ == ValExternRef))
	}
	legacyAnyref := legacyGC.admitsAnyrefSignature()
	for _, sig := range c.Funcs {
		if len(sig.Params) > maxArity || len(sig.Results) > maxArity {
			return reject("signature arity")
		}
		for _, types := range [][]ValType{sig.Params, sig.Results} {
			for _, typ := range types {
				if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 && (!vectorProfile || typ != ValV128) && (!(c.nativeReferenceFunctionABI || primitiveReferences || c.functionEnvironmentFrameRoots()) || typ != ValFuncRef) && (!declarationExtern || typ != ValExternRef) && (!typedI31 || typ != ValI31Ref) && !freshRootedReference(typ) && (!qualifiedEH || typ != ValExnRef) && (!legacyAnyref || typ != ValAnyRef) {
					return reject("unsupported signature")
				}
			}
		}
	}
	if !freshRootedGC && !qualifiedEH && !typedI31 && !legacyGC.admitted() {
		if err := rv64ValidateDeclaredControlTypes(c); err != nil {
			return reject(err.Error())
		}
	}
	if !rv64LegacyGCProductsForImage || !legacyGC.typeSubtypingRefFuncGlobals && !legacyGC.typeSubtypingRefTestSingle && !legacyGC.admitsLinkedSubtype() {
		if err := rv64ValidateOwnedTable(c); err != nil {
			return reject(err.Error())
		}
	}
	if globalsForImage {
		profile := rv64GlobalAdmission{
			primitiveReferences: primitiveReferences,
			freshRootedGC:       freshRootedGC,
			immediateReferences: immediateReferences,
			typedI31:            typedI31,
			exactAnyI31:         exactAnyI31,
			mutableImports:      mutableGlobalImports,
			mutableLocalCall:    mutableFuncrefGlobalLocalCall,
		}
		if detail := rv64GlobalDeclarationError(c, &legacyGC, profile); detail != "" {
			return reject(detail)
		}
	}
	if activeDataForImage {
		if detail := rv64DataInitializerError(c); detail != "" {
			return reject(detail)
		}
	}
	if memoriesForImage {
		if detail := rv64MemoryDeclarationError(c); detail != "" {
			return reject(detail)
		}
	}
	if c.memoryDir != nil && (len(c.memoryDir.exceptionTags()) != 0 && !qualifiedEH || len(c.memoryDir.gcStructGlobals()) != 0 && !legacyGC.admitsStructInitializers() || len(c.memoryDir.gcArrayGlobals()) != 0 && !legacyGC.admitsArrayInitializers() || c.memoryDir.gcArrayElement() != nil && (!rv64LegacyGCProductsForImage || !legacyGC.arrayReference)) {
		return reject("GC/EH initializer lowering is pending")
	}
	return nil
}

// Imported dispatch still reads descriptor zero. Typed and collector profiles
// also retain their established descriptor anchor protocol. Plain local
// functions use the separate, arena-owned native context until a ref.func use.
func railshotNeedsImportContextHeader(c *Compiled) bool {
	return c.NumImports != 0 || c.usesAtomicWaitHelpers() || !c.hasNoFunctionCode() && c.requiredFeatures&(CoreFeatureGC|CoreFeatureTypedFunctionReferences) != 0
}

// Binding policy runs before prepareCodeMapping, including trusted artifacts.
// Numeric/vector native bindings use retained framed bridges. Go callbacks and
// owned host handles retain their separately qualified scalar protocol.
func railshotValidateResolvedImports(c *Compiled, imports resolvedImports, store *referenceStore) error {
	c = c.executionView()
	if c.NumImports == 0 && len(c.Imports) == 0 &&
		(!tableImportsForImage || c.tableImportCount() == 0) &&
		(!globalImportsForImage || len(c.GlobalImports) == 0) {
		return nil
	}
	if err := railshotValidateCompiled(c); err != nil {
		return err
	}
	if nativeTailPlansForImage {
		if err := rv64ValidateTailImports(c, imports); err != nil {
			return err
		}
	}
	if globalImportsForImage && referenceSignaturesBuilt {
		if err := rv64ValidateResolvedGlobals(c, imports, store); err != nil {
			return err
		}
	}
	if tableImportsForImage {
		if err := rv64ValidateResolvedTable(c, imports, store); err != nil {
			return err
		}
	}
	if hostFunctionImportsBuilt {
		if err := rv64ValidateImportedFunctionReferences(c, imports, store); err != nil {
			return err
		}
	}
	for _, index := range c.Exports {
		if index < 0 || index >= c.NumImports+len(c.Funcs) {
			return fmt.Errorf("%w: public RV64 function export index", railshot.ErrUnsupported)
		}
	}
	if !hostFunctionImportsBuilt {
		return nil
	}
	return rv64ValidateResolvedFunctionImports(c, imports, store)
}

func rv64ValidateImportedFunctionReferences(c *Compiled, imports resolvedImports, store *referenceStore) error {
	hasReferenceGlobal := false
	for _, global := range c.Globals {
		if global.Type == ValFuncRef {
			hasReferenceGlobal = true
			break
		}
	}
	if hasReferenceGlobal {
		// Immutable exported reference chains are bounded and validated above.
		// Reject unqualified token owners before mapping or attaching any import.
		for _, index := range c.GlobalExports {
			global := c.Globals[index]
			if global.Type != ValFuncRef {
				continue
			}
			for global.HasInitGlobal {
				global = c.Globals[global.InitGlobal]
			}
			if !global.HasInitFunc || int(global.InitFunc) >= c.NumImports {
				continue
			}
			binding := imports[c.functionImportBindingKey(int(global.InitFunc))]
			if host, ok := binding.(*HostFuncRef); ok {
				if err := host.validateImport(store, c.importFuncSigs[global.InitFunc]); err != nil {
					return err
				}
				continue // The scalar signature gate below also applies.
			}
			ex, ok := binding.(*InstanceExport)
			if !ok || ex == nil || ex.inst == nil || store == nil {
				return fmt.Errorf("%w: RV64 reference export requires a same-Runtime native function", railshot.ErrUnsupported)
			}
			ex.inst.lifeMu.Lock()
			sameStore := ex.inst.refStore == store
			ex.inst.lifeMu.Unlock()
			if !sameStore {
				return fmt.Errorf("%w: RV64 reference export requires the same Runtime", railshot.ErrUnsupported)
			}
		}
	}
	return nil
}

func rv64ValidateResolvedFunctionImports(c *Compiled, imports resolvedImports, store *referenceStore) error {
	freshRootedGC := rv64FreshRootedGC(c)
	for i, key := range c.Imports {
		value := imports[c.functionImportBindingKey(i)]
		switch value := value.(type) {
		case nativeCustomInstructionBinding:
			if err := value.validate(c.importFuncSigs[i]); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
			continue
		case *HostFuncRef:
			if wasmGCBuilt && funcSigHasGCRefs(c.importFuncSigs[i]) && (value == nil || !value.allowsGCBridge() || store == nil || value.store != store) {
				return fmt.Errorf("public RV64 host import %q cannot transfer collector references: use Runtime.NewGCHostFuncRef from the owning Runtime", key)
			}
			if !freshRootedGC && !rv64PrimitiveReferenceContract(c) && !rv64PublicHostReferenceBoundary(c) && !rv64PublicDynamicHostReferenceBoundary(c) {
				if err := rv64ScalarHostSignature(c.importFuncSigs[i]); err != nil {
					return fmt.Errorf("public RV64 import %q: %w", key, err)
				}
			}
			// The shared attachment path repeats this check under its ownership
			// lock before retaining the handle; reject invalid input before mapping.
			if err := value.validateImport(store, c.importFuncSigs[i]); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
			continue
		case *InstanceExport:
			if err := railshotValidateNativeExport(c, i, value); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
			continue
		}
		if isI32HostEvent(value) {
			// Events have no guest capability and can only use the bounded
			// post-invocation log. Shared mode checks reject synchronous owners.
			if _, err := bindI32HostEvent(value, c.importFuncSigs[i]); err != nil {
				return fmt.Errorf("%w: public RV64 import %q: %v", railshot.ErrUnsupported, key, err)
			}
			for _, global := range c.GlobalImports {
				if valTypeMayCarryFuncref(global.Type) {
					return fmt.Errorf("%w: deferred host-event instance cannot import a funcref global", railshot.ErrUnsupported)
				}
			}
			continue
		}
		if !freshRootedGC && !rv64PrimitiveReferenceContract(c) && !rv64PublicHostReferenceBoundary(c) && !rv64PublicDynamicHostReferenceBoundary(c) {
			if err := rv64ScalarHostSignature(c.importFuncSigs[i]); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
		}
		if _, err := bindSyncHostImport(value, c.importFuncSigs[i]); err != nil {
			return fmt.Errorf("public RV64 import %q: %w", key, err)
		}
	}
	return nil
}

func rv64ImportedInitializer(c *Compiled, index int, typ ValType) bool {
	return index >= 0 && index < len(c.GlobalImports) && !c.GlobalImports[index].Mutable && c.GlobalImports[index].Type == typ
}

// This binding-dependent gate runs for every host import, including unused
// signatures and trusted artifacts, before executable mapping or owner retention.
func rv64ScalarHostSignature(sig FuncSig) error {
	if len(sig.Params) > railshot.MaxVectorFunctionValues || len(sig.Results) > railshot.MaxVectorFunctionValues {
		return fmt.Errorf("%w: public RV64 scalar host signature arity", railshot.ErrUnsupported)
	}
	for _, types := range [][]ValType{sig.Params, sig.Results} {
		for _, typ := range types {
			if typ == ValV128 {
				return fmt.Errorf("%w: v128 host callbacks are not supported", railshot.ErrUnsupported)
			}
			if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 {
				return fmt.Errorf("%w: public RV64 nonscalar host signature", railshot.ErrUnsupported)
			}
		}
	}
	return nil
}

// Reject incompatible storage/targets before executable mapping or element writes.
// A temporary physical lease makes row authentication safe even during retirement.
func rv64ValidateResolvedTable(c *Compiled, imports resolvedImports, store *referenceStore) error {
	importCount := c.tableImportCount()
	if importCount == 0 {
		return nil
	}
	tables := make([]*Table, 0, importCount)
	// The descriptors themselves also need leases: host tables have no instance
	// root. Balance every successful acquisition after the native scan releases
	// its transient owner roots.
	defer func() {
		for i := len(tables) - 1; i >= 0; i-- {
			tables[i].detachImporter(store)
		}
	}()
	var roots rv64NativeTableTargets
	// Final-root release can reenter native execution. Unlock first.
	defer roots.release()
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	for tableIndex := 0; tableIndex < importCount; tableIndex++ {
		def, imported := c.tableImportAt(tableIndex)
		if !imported {
			return fmt.Errorf("%w: imported table directory %d is missing", railshot.ErrUnsupported, tableIndex)
		}
		table, ok := imports.table(c.tableImportBindingKey(tableIndex))
		if !ok || table == nil {
			return fmt.Errorf("missing imported table %q", def.Key)
		}
		exact, err := c.tableExactType(tableIndex)
		if err != nil {
			return err
		}
		elementType := c.tableElementType(tableIndex)
		var collector *gc.Collector
		if isGCRefValType(elementType) && table.owner != nil {
			table.owner.mu.Lock()
			if table.owner.instance != nil {
				collector = table.owner.instance.gc
			}
			table.owner.mu.Unlock()
		}
		if err := table.validateImportWithCollector(elementType, exact, c.Types, store, collector, def.Addr64); err != nil {
			return fmt.Errorf("imported table %d: %w", tableIndex, err)
		}
		if err := table.attachImporterWithCollector(elementType, exact, c.Types, store, collector, def.Addr64); err != nil {
			return fmt.Errorf("imported table %d: %w", tableIndex, err)
		}
		tables = append(tables, table)
	}
	edges := 0
	for tableIndex, table := range tables {
		def, _ := c.tableImportAt(tableIndex)
		entryBytes := c.tableEntryBytes(tableIndex)
		if rv64CallableTableRowsForImage && entryBytes == coreruntime.TableEntryBytes {
			if err := roots.tableRoots(table, &edges); err != nil {
				return fmt.Errorf("%w: imported table %d: %v", railshot.ErrUnsupported, tableIndex, err)
			}
		}
		table.owner.mu.Lock()
		table.mu.Lock()
		valid := false
		var size uint64
		if len(table.desc) >= 8 {
			size = uint64(binary.LittleEndian.Uint32(table.desc))
			capacity := uint64(binary.LittleEndian.Uint32(table.desc[4:]))
			maximum, hasMaximum := rv64DeclaredTableMaximumLocked(table, capacity)
			valid = size >= def.Min && size <= capacity && 8+capacity*uint64(entryBytes) <= uint64(len(table.desc)) && (!def.HasMax || hasMaximum && maximum <= def.Max)
		}
		table.mu.Unlock()
		table.owner.mu.Unlock()
		if size < def.Min {
			return fmt.Errorf("imported table %q size %d < required minimum %d", def.Key, size, def.Min)
		}
		if !valid {
			return fmt.Errorf("%w: imported table %q capacity, extent, or required maximum %d", railshot.ErrUnsupported, def.Key, def.Max)
		}
	}
	if err := roots.validateReachable(); err != nil {
		return fmt.Errorf("%w: %v", railshot.ErrUnsupported, err)
	}
	for tableIndex, table := range tables {
		entryBytes := c.tableEntryBytes(tableIndex)
		if rv64CallableTableRowsForImage && entryBytes == coreruntime.TableEntryBytes {
			if err := roots.validateTable(table.desc, entryBytes); err != nil {
				return fmt.Errorf("%w: imported table %d: %v", railshot.ErrUnsupported, tableIndex, err)
			}
		} else if entryBytes != 8 {
			return fmt.Errorf("%w: imported table %d has unsupported %d-byte entries", railshot.ErrUnsupported, tableIndex, entryBytes)
		}
	}
	return nil
}

// Compact local references need their own feature witness; the codec's generic
// structural feature scan does not infer local scope from these indices.
func rv64InitializerGlobal(c *Compiled, index int, expected ValType, limit int) bool {
	if index < 0 || index >= limit || index >= len(c.Globals) {
		return false
	}
	if index < len(c.GlobalImports) {
		return rv64ImportedInitializer(c, index, expected)
	}
	g := c.Globals[index]
	return c.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) && !g.Mutable && g.Type == expected
}

func rv64ValidateIntegerInitializer(c *Compiled, body []byte, expected ValType, limit int) error {
	if !globalInitExpressionsForImage && !segmentOffsetExpressionsForImage {
		return fmt.Errorf("integer expressions are absent from the standalone profile")
	}
	if (expected != ValI32 && expected != ValI64) || (!c.requiredFeatures.IsEnabled(CoreFeatureExtendedConst) && !c.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) && !rv64CoreIntegerInitializer(body)) {
		return fmt.Errorf("integer expression requires its numeric feature metadata")
	}
	var resolve func(uint32) (wasm.ValType, bool)
	if constExprLowOpcodesForImage&(1<<0x23) != 0 {
		resolve = func(index uint32) (wasm.ValType, bool) {
			if uint64(index) >= uint64(len(c.Globals)) {
				return wasm.ValType{}, false
			}
			g := c.Globals[index]
			return rv64NumericWasmType(g.Type), rv64InitializerGlobal(c, int(index), g.Type, limit)
		}
	}
	return railshot.ValidateIntegerInitializer(body, rv64NumericWasmType(expected), resolve)
}

// Memory64 keeps even literal/global offsets as Expr to retain all 64 bits.
// This representation alone does not imply extended-constant instructions.
// The common validator below still proves the result type and global scope.
func rv64CoreIntegerInitializer(body []byte) bool {
	if len(body) > railshot.MaxIntegerInitializerBytes {
		return false
	}
	r := wasm.ReaderFrom(body)
	op, err := r.Byte()
	if err != nil {
		return false
	}
	switch {
	case constExprNumericOpcodesForImage&(1<<(0x41-0x40)) != 0 && op == 0x41:
		_, err = r.I32()
	case constExprNumericOpcodesForImage&(1<<(0x42-0x40)) != 0 && op == 0x42:
		_, err = r.I64()
	case constExprLowOpcodesForImage&(1<<0x23) != 0 && op == 0x23:
		_, err = r.U32()
	default:
		return false
	}
	if err != nil {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && !r.HasNext()
}

// Called under both table locks with its owner retained by the native scope.
// The owner snapshot supplies exact declared limits; descriptor capacity is a
// storage reservation. Host-created tables declare that reservation as a max.
func rv64DeclaredTableMaximumLocked(table *Table, capacity uint64) (uint64, bool) {
	owner := table.owner
	if owner == nil || !owner.declaredHasMax {
		return 0, false
	}
	if owner.instance == nil {
		return capacity, true
	}
	instance := owner.instance
	if instance.c == nil {
		return 0, false
	}
	for index := 0; index < instance.c.tableCount(); index++ {
		if _, imported := instance.c.tableImportAt(index); imported {
			continue
		}
		descriptor := instance.tableDescriptor(index)
		if len(descriptor) == len(table.desc) && offHeapSlicePtr(descriptor) == offHeapSlicePtr(table.desc) {
			definition := instance.c.tableDef(index)
			return definition.Max, definition.HasMax
		}
	}
	return 0, false
}

// Guard mode elides only memory-zero memory32 accesses. Indexed accesses retain
// explicit checks, and importing a primary memory requires a guarded backing.
func rv64GuardedMemoryProfile(c *Compiled) bool {
	if c == nil || !guardPageBuilt || c.threadedMemory0() {
		return false
	}
	return c.memoryCount() == 0 || !c.memoryDef(0).Addr64
}
