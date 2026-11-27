// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_reference_types || !wago_no_typed_function_references || !wago_no_gc || !wago_no_exception_handling)

package wago

import (
	"crypto/sha256"
	"slices"

	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

const rv64HostReferenceBoundaryForImage = hostFunctionImportsBuilt && rv64ReferenceFunctionsForImage
const rv64DynamicHostReferenceBoundaryForImage = rv64HostReferenceBoundaryForImage && rv64ReferenceCallsForImage
const rv64LocalReferenceCallBoundaryForImage = rv64ReferenceFunctionsForImage && rv64ReferenceCallsForImage

func rv64PublicReferenceCallBoundary(c *Compiled) bool {
	return rv64LocalReferenceCallBoundaryForImage && rv64PublicReferenceCallBoundaryWithPlan(c, true)
}

func rv64PublicHostReferenceBoundary(c *Compiled) bool {
	return rv64HostReferenceBoundaryForImage && rv64PublicHostReferenceBoundaryWithPlan(c, true)
}

func rv64PublicDynamicHostReferenceBoundary(c *Compiled) bool {
	return rv64DynamicHostReferenceBoundaryForImage && rv64PublicDynamicHostReferenceBoundaryWithPlan(c, true)
}

var rv64PublicDynamicHostReferenceTypes = []ValType{ValI32, ValFuncRef, ValExternRef, ValI64, ValF32, ValF64}

// rv64PublicDynamicHostReferenceBoundary is the retained counterpart of the
// fixed wire-level call_ref/return_call_ref host bridge. Artifacts do not retain Wasm
// bodies, so this independently binds every surviving type, index, feature and
// resource directory before ordinary code or the two canonical tail plans can
// become callable.
func rv64PublicDynamicHostReferenceBoundaryWithPlan(c *Compiled, requirePlan bool) bool {
	if !rv64DynamicHostReferenceBoundaryForImage || c == nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI {
		return false
	}
	if c.NumImports != 1 || !c.dynamicImports || len(c.Imports) != 1 || c.Imports[0] != "env.target" || len(c.importFuncSigs) != 1 || len(c.Funcs) != 2 || len(c.Types) != 4 || len(c.ValueTypes) != 0 || len(c.Exports) != 2 || c.Exports["call"] != 2 || c.Exports["identity"] != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || requirePlan && !c.hasTableExportMetadata || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 {
		return false
	}
	if c.HasTable || c.TableValueTypeIndex != 0 || c.TableHasValueType || c.TableSize != 0 || c.TableMax != 0 || c.HasTableInitFunc || c.TableHasMax || c.TableAddr64 || c.TableInitFunc != 0 || c.tableImport != "" || c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax {
		return false
	}
	if c.HasMemory || c.MemMinPages != 0 || c.MemMaxPages != 0 || c.MemHasMax || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || requirePlan && !d.exactExports || d.staged || d.stagedMemory64 || d.hasGCInitializers() || d.hasExceptionTagMetadata() {
		return false
	}
	imported, identity, caller := c.importFuncSigs[0], c.Funcs[0], c.Funcs[1]
	if !slices.Equal(imported.Params, rv64PublicDynamicHostReferenceTypes) || !slices.Equal(imported.Results, rv64PublicDynamicHostReferenceTypes) || !rv64PublicReferenceSameTypes(imported.Params, identity.Params) || !rv64PublicReferenceSameTypes(imported.Results, identity.Results) || len(caller.Params) != len(imported.Params)+1 || !rv64PublicReferenceSameTypes(caller.Params[:len(imported.Params)], imported.Params) || caller.Params[len(imported.Params)] != ValI32 || !rv64PublicReferenceSameTypes(caller.Results, imported.Results) {
		return false
	}
	if !imported.HasTypeIndex || imported.TypeIndex != 0 || imported.unsafeCrossTail || !identity.HasTypeIndex || identity.TypeIndex != 0 || identity.unsafeCrossTail || !caller.HasTypeIndex || caller.TypeIndex != 1 || caller.unsafeCrossTail {
		return false
	}
	for i := range c.Types {
		typ := c.Types[i]
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
			return false
		}
	}
	if !rv64PublicReferenceScalarDescriptors(c.Types[0].Params, imported.Params) || !rv64PublicReferenceScalarDescriptors(c.Types[0].Results, imported.Results) || !rv64PublicReferenceScalarDescriptors(c.Types[1].Params, caller.Params) || !rv64PublicReferenceScalarDescriptors(c.Types[1].Results, caller.Results) || !rv64PublicReferenceScalarDescriptors(c.Types[2].Params, imported.Params) || !rv64PublicReferenceScalarDescriptors(c.Types[2].Results, imported.Results) || len(c.Types[3].Params) != 0 || !rv64PublicReferenceScalarDescriptors(c.Types[3].Results, imported.Results) {
		return false
	}
	if requirePlan {
		if len(c.GCTypeDescs) != 4 {
			return false
		}
		for i, desc := range c.GCTypeDescs {
			if desc.ID != corergc.TypeID(i) || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
				return false
			}
		}
	}
	tail := c.requiredFeatures.IsEnabled(CoreFeatureTailCall)
	wantFeatures := CoreFeatureBulkMemoryOperations | CoreFeatureMultiValue | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
	if tail {
		wantFeatures |= CoreFeatureTailCall
	}
	wantCodeLen := 7344
	wantCode := [sha256.Size]byte{0x07, 0xa1, 0x5e, 0xd8, 0x19, 0xf1, 0xbe, 0x52, 0x83, 0xb5, 0xc7, 0xbc, 0x9b, 0xb4, 0xfc, 0xf9, 0x03, 0x77, 0x28, 0x0f, 0x48, 0xf6, 0x63, 0x19, 0x2c, 0x1c, 0xbe, 0x07, 0x2b, 0xce, 0x01, 0xab}
	if tail {
		wantCodeLen = 2736
		wantCode = [sha256.Size]byte{0x6a, 0x15, 0x65, 0xa6, 0xcd, 0xf7, 0x76, 0x44, 0x72, 0x04, 0xe6, 0x9c, 0x41, 0xd3, 0xcc, 0x5a, 0xde, 0xab, 0xfd, 0x84, 0xb7, 0xad, 0x32, 0xbd, 0xfd, 0x0e, 0x85, 0x8c, 0x61, 0xfa, 0x18, 0x6b}
	}
	if c.requiredFeatures != wantFeatures || c.requiredAMD64Features != 0 || !c.dynamicFuncrefEscape || !c.needsFuncRefDescs() || requirePlan && !c.needsFuncRefContextHeader || len(c.FuncTypeID) != 3 || c.FuncTypeID[0] == 0 || c.FuncTypeID[0] != c.FuncTypeID[1] || c.FuncTypeID[2] == 0 || c.FuncTypeID[2] == c.FuncTypeID[0] || len(c.Entry) != 2 || len(c.InternalEntry) != 2 || c.Entry[0] != 0 || c.Entry[1] != 720 || c.InternalEntry[0] != 0 || c.InternalEntry[1] != 720 || len(c.code) != wantCodeLen || sha256.Sum256(c.code) != wantCode {
		return false
	}
	if !requirePlan {
		return true
	}
	m := rv64NativeTailModule(c)
	if !tail {
		return m == nil
	}
	return m != nil && len(m.Plans) == 2 && rv64PublicDynamicHostReferenceTailPlan(c, m.Plans[0], 19) && rv64PublicDynamicHostReferenceTailPlan(c, m.Plans[1], 36)
}

func rv64PublicDynamicHostReferenceTailPlan(c *Compiled, p encoder.NativeTailPlan, sourcePC uint32) bool {
	return rv64DynamicHostReferenceBoundaryForImage && p.Kind == encoder.NativeTailReference && p.TypeIndex == 2 && p.FunctionIndex == 2 && p.ImportIndex == 0 && p.SourcePC == sourcePC && p.FrameBytes == 160 && p.SourceBase == 104 && p.SourceStride == 8 && p.ReferenceBase == 152 && rv64PublicDynamicHostReferenceBoundaryWithPlan(c, false)
}

// rv64PublicHostReferenceBoundary recognizes the retained form of the exact
// one-import/one-wrapper host reference bridge admitted by the RV64 frontend.
// Source bodies are not retained in artifacts, so every remaining structural,
// type, feature, resource and binding field is rechecked here before code can
// become executable.
func rv64PublicHostReferenceBoundaryWithPlan(c *Compiled, requirePlan bool) bool {
	if !rv64HostReferenceBoundaryForImage || c == nil || !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI {
		return false
	}
	if c.NumImports != 1 || !c.dynamicImports || len(c.Imports) != 1 || c.Imports[0] != "env.target" || len(c.importFuncSigs) != 1 || len(c.Funcs) != 1 || len(c.Types) != 1 || len(c.ValueTypes) != 0 || len(c.Exports) != 1 || c.Exports["call"] != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || requirePlan && !c.hasTableExportMetadata || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 {
		return false
	}
	if c.HasTable || c.TableValueTypeIndex != 0 || c.TableHasValueType || c.TableSize != 0 || c.TableMax != 0 || c.HasTableInitFunc || c.TableHasMax || c.TableAddr64 || c.TableInitFunc != 0 || c.tableImport != "" || c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax {
		return false
	}
	if c.HasMemory || c.MemMinPages != 0 || c.MemMaxPages != 0 || c.MemHasMax || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || requirePlan && !d.exactExports || d.staged || d.stagedMemory64 || d.hasGCInitializers() || d.hasExceptionTagMetadata() {
		return false
	}
	tail := c.requiredFeatures.IsEnabled(CoreFeatureTailCall)
	imported, wrapper := c.importFuncSigs[0], c.Funcs[0]
	if len(imported.Params) > coreruntime.MaxHostArity || len(imported.Results) > coreruntime.MaxHostArity || !rv64PublicReferenceCarrierSignature(imported) || !rv64PublicReferenceSameTypes(imported.Params, wrapper.Params) || !rv64PublicReferenceSameTypes(imported.Results, wrapper.Results) || !imported.HasTypeIndex || imported.TypeIndex != 0 || !wrapper.HasTypeIndex || wrapper.TypeIndex != 0 || imported.unsafeCrossTail && !tail || wrapper.unsafeCrossTail {
		return false
	}
	typ := c.Types[0]
	if typ.RecGroup != 0 || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) || !rv64PublicReferenceScalarDescriptors(typ.Params, imported.Params) || !rv64PublicReferenceScalarDescriptors(typ.Results, imported.Results) {
		return false
	}
	if requirePlan {
		if len(c.GCTypeDescs) != 1 {
			return false
		}
		desc := c.GCTypeDescs[0]
		if desc.ID != 0 || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
			return false
		}
	}
	wantFeatures := CoreFeatureReferenceTypes
	if len(imported.Results) > 1 {
		wantFeatures |= CoreFeatureMultiValue
	}
	if tail {
		wantFeatures |= CoreFeatureTailCall
	}
	if c.requiredFeatures != wantFeatures || c.requiredAMD64Features != 0 || c.dynamicFuncrefEscape || c.needsFuncRefDescs() || requirePlan && !c.needsFuncRefContextHeader || len(c.FuncTypeID) != 2 || c.FuncTypeID[0] == 0 || c.FuncTypeID[0] != c.FuncTypeID[1] || len(c.Entry) != 1 || len(c.InternalEntry) != 1 || c.Entry[0] != c.InternalEntry[0] || c.Entry[0] < 0 || c.Entry[0]&15 != 0 || len(c.code) == 0 {
		return false
	}
	m := rv64NativeTailModule(c)
	if tail {
		if !requirePlan {
			return true
		}
		return m != nil && len(m.Plans) == 1 && rv64PublicHostReferenceTailPlan(c, m.Plans[0])
	}
	return m == nil
}

func rv64PublicHostReferenceTailPlan(c *Compiled, p encoder.NativeTailPlan) bool {
	if !rv64HostReferenceBoundaryForImage || p.Kind != encoder.NativeTailDirectImport || p.TypeIndex != 0 || p.FunctionIndex != 1 || p.ImportIndex != 0 || p.ReferenceBase != 0 || !rv64PublicHostReferenceBoundaryWithPlan(c, false) || !c.requiredFeatures.IsEnabled(CoreFeatureTailCall) {
		return false
	}
	wantPC := uint32(1)
	for i := range c.Funcs[0].Params {
		wantPC += 1 + rv64PublicReferenceU32Bytes(uint32(i))
	}
	n := len(c.Funcs[0].Params)
	return p.SourcePC == wantPC && p.FrameBytes == 48+16*n && p.SourceBase == 48+8*n && p.SourceStride == 8
}

// Artifact decoding validates declared reference carriers before it reaches
// the address-free native-tail appendix. The prefix mode therefore derives the
// expected tail shape from the exact feature set, while final admission still
// requires the canonical retained plan below.
func rv64PublicReferenceCallBoundaryWithPlan(c *Compiled, requirePlan bool) bool {
	if !rv64LocalReferenceCallBoundaryForImage || c == nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.Types) != 3 || len(c.ValueTypes) != 0 || len(c.Exports) != 3 || c.tableCount() != 0 || len(c.extraTables) != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tagImportCount() != 0 || c.HasStart {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.exceptionTags()) != 0 || !c.NeedsFuncRefDescs || len(c.FuncTypeID) != 3 {
		return false
	}
	wantFeatures := CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
	tail := rv64HasReferenceTail(c)
	if !requirePlan && c.requiredFeatures.IsEnabled(CoreFeatureTailCall) {
		tail = true
	}
	if tail {
		wantFeatures |= CoreFeatureTailCall
	}
	for _, sig := range c.Funcs {
		if len(sig.Results) > 1 {
			wantFeatures |= CoreFeatureMultiValue
			break
		}
	}
	if c.requiredFeatures != wantFeatures {
		return false
	}
	for i := range c.Funcs {
		if !c.Funcs[i].HasTypeIndex || c.Funcs[i].TypeIndex != uint32(i) || c.Types[i].RecGroup != uint32(i) {
			return false
		}
	}
	target, producer, caller := c.Funcs[0], c.Funcs[1], c.Funcs[2]
	referenceCarriers := rv64PublicReferenceCarrierSignature(target)
	if c.dynamicFuncrefEscape != referenceCarriers {
		return false
	}
	if !rv64PublicReferenceScalarSignature(target) && !referenceCarriers || len(producer.Params) != 0 || len(producer.Results) != 1 || producer.Results[0] != ValFuncRef || len(caller.Params) != len(target.Params)+1 || caller.Params[len(target.Params)] != ValFuncRef {
		return false
	}
	for i := range target.Params {
		if caller.Params[i] != target.Params[i] {
			return false
		}
	}
	numericResult := rv64PublicReferenceSameTypes(caller.Results, target.Results)
	referenceResult := len(caller.Results) == 1 && caller.Results[0] == ValFuncRef
	if !numericResult && !referenceResult || tail && !numericResult || referenceCarriers && !numericResult {
		return false
	}
	targetType, producerType, callerType := c.Types[0], c.Types[1], c.Types[2]
	for i := range c.Types {
		typ := c.Types[i]
		if !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.HasDescriptor || typ.Kind != CompositeTypeFunction || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
			return false
		}
	}
	if !rv64PublicReferenceScalarDescriptors(targetType.Params, target.Params) || !rv64PublicReferenceScalarDescriptors(targetType.Results, target.Results) || len(producerType.Params) != 0 || len(producerType.Results) != 1 || len(callerType.Params) != len(target.Params)+1 {
		return false
	}
	produced := producerType.Results[0]
	consumed := callerType.Params[len(target.Params)]
	if produced.Kind != ValueTypeReference || !produced.Ref.Nullable || produced.Ref.Exact || produced.Ref.Heap.Defined || produced.Ref.Heap.Abstract != AbstractHeapFunc || consumed.Kind != ValueTypeReference || !consumed.Ref.Nullable || consumed.Ref.Exact || !consumed.Ref.Heap.Defined || consumed.Ref.Heap.TypeIndex != 0 {
		return false
	}
	if !rv64PublicReferenceScalarDescriptors(callerType.Params[:len(target.Params)], target.Params) {
		return false
	}
	if numericResult {
		if !rv64PublicReferenceScalarDescriptors(callerType.Results, target.Results) {
			return false
		}
	} else {
		if len(callerType.Results) != 1 {
			return false
		}
		callerResult := callerType.Results[0]
		if callerResult.Kind != ValueTypeReference || !callerResult.Ref.Nullable || callerResult.Ref.Exact || callerResult.Ref.Heap.Defined || callerResult.Ref.Heap.Abstract != AbstractHeapFunc {
			return false
		}
	}
	for i, name := range []string{"target", "get", "call"} {
		index, ok := c.Exports[name]
		if !ok || index != i {
			return false
		}
	}
	if !requirePlan {
		return true
	}
	m := rv64NativeTailModule(c)
	if tail {
		return m != nil && len(m.Plans) == 1 && rv64PublicReferenceTailPlan(c, m.Plans[0])
	} else if m != nil {
		return false
	}
	return true
}
