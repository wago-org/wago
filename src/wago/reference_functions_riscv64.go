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
	"fmt"
	"slices"
	"unsafe"

	"github.com/wago-org/wago/internal/buildfeatures"
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

// This permission belongs to the destination instance, never to artifact bytes
// or a source Compiled's cache. Trusted decoding retains unpublished RW staging.
func railshotValidateReferenceFunctionInstance(c *Compiled, opts InstantiateOptions) error {
	c = c.executionView()
	if !referenceSignaturesBuilt {
		if c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI {
			return fmt.Errorf("%w: public RV64 reference ABI is disabled in this build", railshot.ErrUnsupported)
		}
		return nil
	}
	closedReferences := c.nativeReferenceFunctionABI || rv64LegacyGCProductsForImage && rv64LegacyGCReferenceLink(c)
	if closedReferences && !opts.railshotTailPolicy.referenceFunctions && !rv64PrimitiveReferenceContract(c) && !rv64RootedInternalFunctionContract(c) {
		return fmt.Errorf("%w: closed RV64 reference functions require private Runtime permission", railshot.ErrUnsupported)
	}
	if closedReferences {
		return c.validateCached()
	}
	return nil
}

// The closed profile preserves physical function indices. Internal reference
// signatures need exact product admission or fresh rooted closed-owner proof
// before they can have canonical descriptor identities.
// Callers use frozen metadata; admission validates exact type bindings first.
func railshotNonCallableFunction(c *Compiled, function int) bool {
	return referenceSignaturesBuilt && c != nil && c.nativeReferenceFunctionABI && rv64NonCallableReferenceFunction(c, function)
}

func rv64NonCallableReferenceFunction(c *Compiled, function int) bool {
	if rv64PrimitiveReferenceContract(c) {
		return false
	}
	if rv64RootedClosedCallableFunction(c, function) {
		return false
	}
	if rv64LegacyGCProductsForImage {
		if function >= 0 && function < 3 && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingRuntimeCallCast && rv64GCTypeSubtypingRuntimeCallCast(c) {
			// The exact runtime call/cast product keeps these reference-valued
			// functions private, but its immutable table must be able to call them.
			return false
		}
		if function >= 0 && function < 2 && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingRuntimeFinalityCallCast && rv64GCTypeSubtypingRuntimeFinalityCallCast(c) {
			return false
		}
		if function >= 0 && function < 2 && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingRuntimeTypedTableCall && rv64GCTypeSubtypingRuntimeTypedTableCall(c) {
			return false
		}
		if function >= 0 && function < 3 && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingLinkProvider && rv64GCTypeSubtypingLink(c) {
			return false
		}
		if function >= 0 && function < 2 && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingDuplicateRecursiveLinkProvider && rv64GCTypeSubtypingDuplicateRecursiveLink(c) {
			return false
		}
		if function >= 0 && function < 2 && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingExtendedRecursiveLinkProvider && rv64GCTypeSubtypingExtendedRecursiveLink(c) {
			return false
		}
	}
	if (function == 0 && len(c.Funcs) != 0 && rv64PublicReferenceCallBoundary(c) && rv64PublicReferenceCarrierSignature(c.Funcs[0])) ||
		rv64MutableReferenceCarrierTarget(c, function) ||
		(function == 1 && len(c.Funcs) == 1 && rv64PublicHostReferenceBoundary(c)) ||
		(function <= 1 && rv64PublicDynamicHostReferenceBoundary(c)) {
		return false
	}
	local := function - c.NumImports
	if local < 0 || local >= len(c.Funcs) {
		return false
	}
	for _, list := range [][]ValType{c.Funcs[local].Params, c.Funcs[local].Results} {
		for _, v := range list {
			if v != ValI32 && v != ValI64 && v != ValF32 && v != ValF64 && v != ValV128 {
				return true
			}
		}
	}
	return false
}

// Only fresh source proof can publish collector-reference targets in this
// closed owner. The receipt binds exact signatures and signature keys. Imports
// and exported reference parameters cannot supply foreign callable targets.
func rv64RootedClosedCallableFunction(c *Compiled, function int) bool {
	if !nativeCollectorFrameRootsBuilt || c == nil || function < 0 || function >= len(c.Funcs) {
		return false
	}
	if memo := c.loadValidateMemo(); memo != nil && memo.executionView() == c {
		indexes := c.loadCompileIndexes()
		if indexes == nil {
			return false
		}
		mask := indexes.railshotTail.rootedClosedCallables
		return function/64 < len(mask) && mask[function/64]&(uint64(1)<<uint(function%64)) != 0
	}
	return rv64RootedCollectorSignature(c.Funcs[function]) && rv64RootedClosedCallableOwner(c) && rv64FreshRootedGC(c)
}

func rv64RootedCollectorSignature(sig FuncSig) bool {
	collectorSignature := false
	for _, list := range [][]ValType{sig.Params, sig.Results} {
		for _, typ := range list {
			collectorSignature = collectorSignature || isGCRefValType(typ)
		}
	}
	return collectorSignature
}

func rv64RootedClosedCallableOwner(c *Compiled) bool {
	if c == nil || (!c.nativeReferenceCallABI && !(rv64RootedReceiptsForImage && rv64CollectorTailMetadata(c))) || c.NumImports != 0 || c.tableImportCount() != 0 || len(c.tableExports) != 0 {
		return false
	}
	for _, global := range c.GlobalImports {
		if global.Type == ValFuncRef || global.Type == ValExternRef || isGCRefValType(global.Type) {
			return false
		}
	}
	for _, index := range c.GlobalExports {
		if index < 0 || index >= len(c.Globals) {
			return false
		}
		global := c.Globals[index]
		if global.Mutable && (global.Type == ValFuncRef || global.Type == ValExternRef || isGCRefValType(global.Type)) {
			return false
		}
	}
	for _, index := range c.Exports {
		if index < 0 || index >= len(c.Funcs) {
			return false
		}
		for _, typ := range c.Funcs[index].Params {
			if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 && typ != ValV128 {
				return false
			}
		}
	}
	return true
}

// freezeExecution calls this with a deeply owned unpublished snapshot under
// the code-cache lock. Publish the completed mask before the snapshot pointer.
// Public views and metadata copies never use it: lookup requires pointer identity.
func railshotInitializeExecutionSnapshot(c *Compiled) error {
	if !functionCodeForImage {
		return nil
	}
	if memo := c.loadValidateMemo(); memo == nil || memo.executionView() != nil {
		return nil
	}
	if rv64PrimitiveReferenceContract(c) {
		if err := c.validate(); err != nil {
			return err
		}
		// The owner already exists because a canonical execution contract is
		// held there. No snapshot allocation is required for this bounded flag.
		c.rv64ExecutionContract().verified.Store(c)
		c.ensureCompileIndexes().railshotTail.primitiveReferences = true
		if dynamicNativeTailForImage && rv64HasDynamicTail(rv64NativeTailModule(c)) {
			var keys []uint64
			if err := rv64ValidatePrimitiveFunctionKeys(c, &keys); err != nil {
				return err
			}
			c.ensureCompileIndexes().railshotTail.tailCode.get().declaredTypeKeys = keys
		}
	}
	if dynamicNativeTailForImage && nativeCollectorFrameRootsBuilt && rv64HasDynamicTail(rv64NativeTailModule(c)) && rv64FreshRootedGC(c) {
		graph, err := rv64DeclaredGraph(c, true)
		if err != nil {
			return err
		}
		keys := make([]uint64, len(c.Types))
		for index, typ := range c.Types {
			if typ.Kind != CompositeTypeFunction {
				continue
			}
			key, ok := graph.StructuralTypeKeyChecked(uint32(index))
			if !ok {
				return rv64TailGenerationError("rooted snapshot declared key")
			}
			keys[index] = key
		}
		c.ensureCompileIndexes().railshotTail.tailCode.get().declaredTypeKeys = keys
	}
	if !nativeCollectorFrameRootsBuilt || !rv64RootedClosedCallableOwner(c) || len(c.Funcs) > railshot.MaxModuleFunctions {
		return nil
	}
	hasCollectorSignature := false
	for _, sig := range c.Funcs {
		hasCollectorSignature = hasCollectorSignature || rv64RootedCollectorSignature(sig)
	}
	if !hasCollectorSignature || !rv64FreshRootedGC(c) {
		return nil
	}
	if err := c.validate(); err != nil {
		return err
	}
	mask := make([]uint64, (len(c.Funcs)+63)/64)
	for function, sig := range c.Funcs {
		if rv64RootedCollectorSignature(sig) {
			mask[function/64] |= uint64(1) << uint(function%64)
		}
	}
	c.ensureCompileIndexes().railshotTail.rootedClosedCallables = mask
	return nil
}

// Quota preflight reserves the new cache without rebuilding the proof graph.
// An invalid receipt may leave this reservation unused; it never creates trust.
func railshotExecutionSnapshotCacheBytes(c *Compiled) uintptr {
	if c == nil {
		return 0
	}
	var primitiveBytes uintptr
	if c.rv64ExecutionContract() != nil {
		// One alignment word added to the RV64 cold owner for the immutable
		// primitive classification. Reserve without running a source graph proof.
		primitiveBytes = unsafe.Sizeof(uint64(0))
		if dynamicNativeTailForImage && rv64HasDynamicTail(rv64NativeTailModule(c)) {
			primitiveBytes += unsafe.Sizeof([]uint64(nil)) + uintptr(len(c.Types))*unsafe.Sizeof(uint64(0))
		}
	}
	if !nativeCollectorFrameRootsBuilt || len(c.Funcs) > railshot.MaxModuleFunctions || !rv64RootedClosedCallableOwner(c) {
		return primitiveBytes
	}
	if memo := c.loadValidateMemo(); memo == nil || memo.rootedReceipt() == nil && c.rv64ExecutionContract() == nil {
		return primitiveBytes
	}
	for _, sig := range c.Funcs {
		if rv64RootedCollectorSignature(sig) {
			bytes := uintptr((len(c.Funcs)+63)/64) * unsafe.Sizeof(uint64(0))
			if c.loadCompileIndexes() == nil {
				bytes += unsafe.Sizeof(compiledCacheIndexes{})
			}
			return bytes + primitiveBytes
		}
	}
	return primitiveBytes
}

func rv64PublicReferenceCarrierSignature(sig FuncSig) bool {
	if len(sig.Params) > 256 || len(sig.Results) > 256 {
		return false
	}
	reference := false
	for _, values := range [][]ValType{sig.Params, sig.Results} {
		for _, typ := range values {
			switch typ {
			case ValI32, ValI64, ValF32, ValF64:
			case ValFuncRef, ValExternRef:
				reference = true
			default:
				return false
			}
		}
	}
	return reference
}

func rv64PublicReferenceCarrierTarget(c *Compiled, function int) bool {
	if c == nil || function != 0 || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || len(c.Funcs) != 3 || len(c.Types) != 3 || !rv64PublicReferenceCarrierSignature(c.Funcs[0]) || !c.Funcs[0].HasTypeIndex || c.Funcs[0].TypeIndex != 0 {
		return false
	}
	typ := c.Types[0]
	return typ.Kind == CompositeTypeFunction && rv64PublicReferenceScalarDescriptors(typ.Params, c.Funcs[0].Params) && rv64PublicReferenceScalarDescriptors(typ.Results, c.Funcs[0].Results)
}

func rv64MutableReferenceCarrierTarget(c *Compiled, function int) bool {
	variant := rv64MutableFuncrefGlobalLocalCallCodeVariant(c)
	if c == nil || variant < 19 || variant > 28 || function < 0 || function > 1 || len(c.Funcs) != 6 || len(c.Types) != 2 || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI {
		return false
	}
	sig := c.Funcs[function]
	carrierSignature := rv64PublicReferenceCarrierSignature(sig)
	if variant >= 23 {
		carrierSignature = rv64ReferenceVectorCarrierSignature(sig)
	}
	if !carrierSignature || !sig.HasTypeIndex || sig.TypeIndex != 0 {
		return false
	}
	typ := c.Types[0]
	return typ.Kind == CompositeTypeFunction && rv64PublicReferenceScalarDescriptors(typ.Params, sig.Params) && rv64PublicReferenceScalarDescriptors(typ.Results, sig.Results)
}

func rv64ReferenceVectorCarrierSignature(sig FuncSig) bool {
	if len(sig.Params) > 128 || len(sig.Results) > 128 {
		return false
	}
	reference, vector := false, false
	for _, values := range [][]ValType{sig.Params, sig.Results} {
		for _, typ := range values {
			switch typ {
			case ValI32, ValI64, ValF32, ValF64:
			case ValV128:
				vector = true
			case ValFuncRef, ValExternRef:
				reference = true
			default:
				return false
			}
		}
	}
	return reference && vector
}

func rv64PublicReferenceU32Bytes(value uint32) uint32 {
	n := uint32(1)
	for value >= 0x80 {
		n++
		value >>= 7
	}
	return n
}

func rv64PublicReferenceScalarSignature(sig FuncSig) bool {
	if len(sig.Params) > 256 || len(sig.Results) > 256 {
		return false
	}
	for _, values := range [][]ValType{sig.Params, sig.Results} {
		for _, typ := range values {
			if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 {
				return false
			}
		}
	}
	return true
}

func rv64PublicReferenceSameTypes(a, b []ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func rv64PublicReferenceScalarDescriptors(exact []ValueTypeDescriptor, abi []ValType) bool {
	if len(exact) != len(abi) {
		return false
	}
	for i := range exact {
		want, ok := valueTypeDescriptorFromValType(abi[i])
		if !ok || exact[i] != want {
			return false
		}
	}
	return true
}

const rv64Core3TypedReferencesBuilt = buildfeatures.ReferenceTypes != 0 && buildfeatures.TypedFunctionReferences != 0
const rv64Core3ReferenceWitnessesForImage = rv64Core3TypedReferencesBuilt && rv64ReferenceFunctionsForImage && rv64ReferenceCallsForImage
const rv64ReferenceElementsForImage = tablesForImage || activeElementsForImage || passiveElementsForImage
const rv64ReferenceStorageForImage = rv64ReferenceElementsForImage || memoriesForImage || globalsForImage || activeDataForImage || passiveDataForImage

func rv64Core3ReferenceDeclarationWitness(c *Compiled) bool {
	return rv64Core3RefDeclarations(c) || rv64Core3ReferenceDeclarations(c) || rv64Core3RefAsNonNullDeclarations(c) || rv64Core3RefIsNullDeclarations(c)
}

// rv64Core3ReferenceDeclarationMetadata recognizes the canonical fields that
// precede GC descriptors in the artifact codec. It is intentionally usable by
// the early typed-control consistency check while decoding; complete public
// admission additionally requires the trailing directories below. The pinned
// br_on_null and br_on_non_null declaration modules retain identical metadata;
// their distinct opcode/body contracts are proven before compilation.
func rv64Core3ReferenceDeclarationMetadata(c *Compiled) bool {
	if c == nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceControlABI || c.nativeReferenceCallABI || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences || c.requiredAMD64Features != 0 || c.dynamicFuncrefEscape || c.dynamicImports || c.needsFuncRefDescs() || c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.Types) != 4 || len(c.ValueTypes) != 0 || len(c.FuncTypeID) != 3 || len(c.Entry) != 3 || len(c.InternalEntry) != 3 || len(c.code) == 0 || len(c.Exports) != 0 || c.Names != nil {
		return false
	}
	if c.tableCount() != 0 || len(c.extraTables) != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.tableExports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 {
		return false
	}
	for i := range c.Funcs {
		sig := c.Funcs[i]
		if !sig.HasTypeIndex || sig.TypeIndex != uint32(i+1) || len(sig.Params) != 1 || len(sig.Results) != 0 || sig.unsafeCrossTail || c.Entry[i] != c.InternalEntry[i] || c.Entry[i]&15 != 0 || c.FuncTypeID[i] == 0 {
			return false
		}
		want := ValFuncRef
		if i == 2 {
			want = ValExternRef
		}
		if sig.Params[0] != want {
			return false
		}
	}
	for i := range c.Types {
		typ := &c.Types[i]
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Results) != 0 || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
			return false
		}
		if i == 0 {
			if len(typ.Params) != 0 {
				return false
			}
			continue
		}
		if len(typ.Params) != 1 {
			return false
		}
		param := typ.Params[0]
		if param.Kind != ValueTypeReference || !param.Ref.Nullable || param.Ref.Exact {
			return false
		}
		switch i {
		case 1:
			if !param.Ref.Heap.Defined || param.Ref.Heap.Abstract != 0 || param.Ref.Heap.TypeIndex != 0 {
				return false
			}
		case 2:
			if param.Ref.Heap.Defined || param.Ref.Heap.Abstract != AbstractHeapFunc || param.Ref.Heap.TypeIndex != 0 {
				return false
			}
		case 3:
			if param.Ref.Heap.Defined || param.Ref.Heap.Abstract != AbstractHeapExtern || param.Ref.Heap.TypeIndex != 0 {
				return false
			}
		}
	}
	return true
}

// rv64Core3ReferenceDeclarations is the complete retained artifact witness for
// the declaration-only modules from pinned Release 3 br_on_null.wast and
// br_on_non_null.wast. The retained shape proves that their externref carrier
// has no callable, descriptor, import, start, initializer, state or resource
// path.
func rv64Core3ReferenceDeclarations(c *Compiled) bool {
	if !rv64Core3ReferenceDeclarationMetadata(c) || !rv64Core3ReferenceDeclarationCode(c) || !c.needsFuncRefContextHeader || len(c.GCTypeDescs) != 4 {
		return false
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != corergc.TypeID(i) || desc.Kind != corergc.KindFunc || !desc.Final || desc.HasSuper || desc.Super != 0 || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs {
			return false
		}
	}
	if d := c.memoryDir; d != nil && (len(d.defs) != 0 || len(d.exports) != 0 || d.staged || d.stagedMemory64 || d.hasGCInitializers() || d.hasExceptionTagMetadata()) {
		return false
	}
	return true
}

func rv64Core3ReferenceDeclarationCode(c *Compiled) bool {
	if c == nil {
		return false
	}
	switch fmt.Sprintf("%x", sha256.Sum256(c.code)) {
	case "495375f625e59894be58278d507ee66834182d853bf8873f633d78915e46e035", // br_on_null.1
		"d05c044f01f47560b97b18aab0140831aba2c4b885edf85eeacfac5d610073c7": // br_on_non_null.1
		return true
	default:
		return false
	}
}

// rv64Core3RefDeclarationMetadata is the exact retained prefix for the pinned
// ref.0.wasm declaration matrix. The single private function exercises both
// nullable and non-null abstract/indexed reference types, but has no callable
// or state surface.
func rv64Core3RefDeclarationMetadata(c *Compiled) bool {
	if c == nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceControlABI || c.nativeReferenceCallABI || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences || c.requiredAMD64Features != 0 || c.dynamicFuncrefEscape || c.dynamicImports || c.needsFuncRefDescs() || c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.Types) != 2 || len(c.ValueTypes) != 0 || len(c.FuncTypeID) != 1 || c.FuncTypeID[0] != 0x452bd5aba9b3044d || len(c.Entry) != 1 || len(c.InternalEntry) != 1 || c.Entry[0] != 0 || c.InternalEntry[0] != 0 || len(c.code) == 0 || len(c.Exports) != 0 || c.Names != nil {
		return false
	}
	if c.tableCount() != 0 || len(c.extraTables) != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.tableExports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 {
		return false
	}
	sig := c.Funcs[0]
	wantABI := []ValType{ValFuncRef, ValExternRef, ValFuncRef, ValExternRef, ValFuncRef, ValFuncRef, ValFuncRef, ValFuncRef, ValFuncRef, ValExternRef, ValFuncRef, ValFuncRef}
	if !sig.HasTypeIndex || sig.TypeIndex != 1 || !slices.Equal(sig.Params, wantABI) || len(sig.Results) != 0 || sig.unsafeCrossTail {
		return false
	}
	if typ := &c.Types[0]; typ.RecGroup != 0 || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Params) != 0 || len(typ.Results) != 0 || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
		return false
	}
	ref := func(nullable, defined bool, heap AbstractHeapType) ValueTypeDescriptor {
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: nullable, Heap: HeapTypeDescriptor{Defined: defined, Abstract: heap}}}
	}
	nonNullIndexed := ref(false, true, 0)
	nullableIndexed := ref(true, true, 0)
	wantParams := []ValueTypeDescriptor{
		ref(true, false, AbstractHeapFunc),
		ref(true, false, AbstractHeapExtern),
		ref(false, false, AbstractHeapFunc),
		ref(false, false, AbstractHeapExtern),
		nonNullIndexed, nonNullIndexed, nonNullIndexed, nonNullIndexed,
		ref(true, false, AbstractHeapFunc),
		ref(true, false, AbstractHeapExtern),
		nullableIndexed, nullableIndexed,
	}
	typ := &c.Types[1]
	return typ.RecGroup == 1 && typ.Final && len(typ.Supers) == 0 && !typ.HasDescribes && typ.Describes == 0 && !typ.HasDescriptor && typ.Descriptor == 0 && typ.Kind == CompositeTypeFunction && slices.Equal(typ.Params, wantParams) && len(typ.Results) == 0 && len(typ.Fields) == 0 && typ.Array == (FieldTypeDescriptor{})
}

func rv64Core3RefDeclarationCode(c *Compiled) bool {
	return c != nil && len(c.code) == 672 && sha256.Sum256(c.code) == ([32]byte{0x85, 0x5d, 0xab, 0x7b, 0x1c, 0xec, 0x04, 0x4d, 0x22, 0x68, 0xf2, 0x85, 0xb3, 0x78, 0x54, 0x79, 0x12, 0x21, 0x26, 0xf4, 0x69, 0x28, 0xa7, 0x1c, 0xac, 0xfe, 0x37, 0xfc, 0x56, 0x41, 0x97, 0x58})
}

func rv64Core3RefDeclarations(c *Compiled) bool {
	if !rv64Core3RefDeclarationMetadata(c) || !rv64Core3RefDeclarationCode(c) || !c.needsFuncRefContextHeader || len(c.GCTypeDescs) != 2 {
		return false
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != corergc.TypeID(i) || desc.Kind != corergc.KindFunc || !desc.Final || desc.HasSuper || desc.Super != 0 || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs {
			return false
		}
	}
	if d := c.memoryDir; d != nil && (len(d.defs) != 0 || len(d.exports) != 0 || d.staged || d.stagedMemory64 || d.hasGCInitializers() || d.hasExceptionTagMetadata()) {
		return false
	}
	return true
}

func rv64Core3RefAsNonNullDeclarationCode(c *Compiled) bool {
	if c == nil {
		return false
	}
	return fmt.Sprintf("%x", sha256.Sum256(c.code)) == "4ab02e20f9f4f69df8ef671c3683db5940325b8687bd2a9d556192453508b177"
}

func rv64Core3RefIsNullDeclarationCode(c *Compiled) bool {
	if c == nil {
		return false
	}
	return fmt.Sprintf("%x", sha256.Sum256(c.code)) == "d2b9e9c8c334de03404b77c7044ee894fc4585a122c601f8c7c25180765d5478"
}

func rv64Core3RefIsNullExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 8872 && sha256.Sum256(c.code) == ([32]byte{0xb7, 0x0c, 0x68, 0x57, 0x59, 0xc1, 0xfe, 0x72, 0x23, 0xd8, 0x07, 0x16, 0x7d, 0x4b, 0xe8, 0xc8, 0x33, 0xf8, 0x8d, 0x2b, 0x1a, 0x5a, 0xb8, 0x29, 0xfc, 0xbf, 0xff, 0x35, 0x08, 0x09, 0x58, 0x01})
}

// rv64Core3RefIsNullExecutionCodecPrefix binds every field decoded before the
// artifact codec performs its early typed-control check. The final witness
// below additionally requires the later GC suffix and normalized directories.
func rv64Core3RefIsNullExecutionCodecPrefix(c *Compiled) bool {
	if !activeElementsForImage || c == nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceControlABI || c.nativeReferenceCallABI || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences || c.requiredAMD64Features != 0 || c.dynamicFuncrefEscape || c.dynamicImports || !c.NeedsFuncRefDescs || c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 10 || len(c.Types) != 7 || len(c.ValueTypes) != 4 || len(c.FuncTypeID) != 10 || len(c.Entry) != 10 || len(c.InternalEntry) != 10 || len(c.Exports) != 8 || c.Names != nil {
		return false
	}
	if c.memoryImport != "" || c.HasMemory || c.MemMinPages != 0 || c.MemMaxPages != 0 || c.MemHasMax || c.tableImport != "" || c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax || c.syncHostSlots != 0 && c.syncHostSlots != uint16(coreruntime.MaxHostArity) {
		return false
	}
	if !rv64Core3RefIsNullExecutionCode(c) {
		return false
	}
	wantEntries := [...]int{0, 576, 1200, 1824, 2448, 3120, 4000, 6128, 6960, 7920}
	wantBindings := [...]uint32{0, 1, 2, 3, 4, 5, 0, 6, 6, 6}
	wantTypeIDs := [...]uint64{0xd60da539b2828050, 0x457208c8efc22dab, 0x159570dc55660444, 0x43a91b81cda1964c, 0x87259896b4921f6c, 0x009df426b447def3, 0xd60da539b2828050, 0x57cc9e275fbbb6a5, 0x57cc9e275fbbb6a5, 0x57cc9e275fbbb6a5}
	wantParams := [][]ValType{nil, {ValFuncRef}, {ValExternRef}, {ValFuncRef}, nil, {ValExternRef}, nil, {ValI32}, {ValI32}, {ValI32}}
	wantResults := [][]ValType{nil, {ValI32}, {ValI32}, {ValI32}, {ValI32}, nil, nil, {ValI32}, {ValI32}, {ValI32}}
	for i := range c.Funcs {
		sig := &c.Funcs[i]
		if c.Entry[i] != wantEntries[i] || c.InternalEntry[i] != wantEntries[i] || !sig.HasTypeIndex || sig.TypeIndex != wantBindings[i] || sig.unsafeCrossTail || c.FuncTypeID[i] != wantTypeIDs[i] || !rv64Core3ABIValues(sig.Params, wantParams[i]) || !rv64Core3ABIValues(sig.Results, wantResults[i]) {
			return false
		}
	}
	if !rv64Core3RefIsNullExecutionTypes(c) || !rv64Core3RefIsNullExecutionTables(c) || !rv64Core3RefIsNullExecutionElements(c) {
		return false
	}
	wantExports := map[string]int{
		"funcref": 1, "externref": 2, "ref-null": 4, "init": 5,
		"deinit": 6, "funcref-elem": 7, "externref-elem": 8, "ref-elem": 9,
	}
	for name, index := range wantExports {
		if actual, ok := c.Exports[name]; !ok || actual != index {
			return false
		}
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.tableExports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.compactData != nil || len(c.Data) != 0 || len(c.PassiveData) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 {
		return false
	}
	return true
}

// rv64Core3RefIsNullExecution is the retained compiler/artifact witness for
// pinned Core 3 ref_is_null.0.wasm. It binds every executable directory and
// the mixed 32/8/32-byte table layout. Recognition alone does not open source
// compilation or instantiation; those boundaries require their own lifetime,
// publication and externref-store proofs.
func rv64Core3RefIsNullExecution(c *Compiled) bool {
	if !rv64Core3RefIsNullExecutionCodecPrefix(c) || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata {
		return false
	}
	if len(c.GCTypeDescs) != 7 {
		return false
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != corergc.TypeID(i) || desc.Kind != corergc.KindFunc || !desc.Final || desc.HasSuper || desc.Super != 0 || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs {
			return false
		}
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || d.hasGCInitializers() || d.hasExceptionTagMetadata() {
		return false
	}
	return true
}

func rv64Core3ABIValues(got, want []ValType) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func rv64Core3RefIsNullExecutionTypes(c *Compiled) bool {
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	nullFunc := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}
	nullExtern := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}
	nullIndexed := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Defined: true}}}
	nonNullIndexed := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true}}}
	wantParams := [][]ValueTypeDescriptor{nil, {nullFunc}, {nullExtern}, {nullIndexed}, nil, {nullExtern}, {i32}}
	wantResults := [][]ValueTypeDescriptor{nil, {i32}, {i32}, {i32}, {i32}, nil, {i32}}
	for i := range c.Types {
		typ := &c.Types[i]
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) || !rv64Core3ValueTypes(typ.Params, wantParams[i]) || !rv64Core3ValueTypes(typ.Results, wantResults[i]) {
			return false
		}
	}
	wantValues := [...]ValueTypeDescriptor{nullFunc, nullExtern, nullIndexed, nonNullIndexed}
	for i := range wantValues {
		if c.ValueTypes[i] != wantValues[i] {
			return false
		}
	}
	return true
}

func rv64Core3ValueTypes(got, want []ValueTypeDescriptor) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func rv64Core3RefIsNullExecutionTables(c *Compiled) bool {
	if c.tableCount() != 3 || c.tableImportCount() != 0 || !c.HasTable || c.TableType != ValFuncRef || c.TableValueTypeIndex != 0 || !c.TableHasValueType || c.TableSize != 2 || c.TableMax != 2 || c.HasTableInitFunc || c.TableHasMax || c.TableAddr64 || c.TableInitFunc != 0 || len(c.extraTables) != 2 {
		return false
	}
	want := [...]tableDef{
		{Size: 2, Max: 2, Type: ValExternRef, ValueTypeIndex: 1, HasValueType: true},
		{Size: 2, Max: 2, Type: ValFuncRef, ValueTypeIndex: 2, HasValueType: true},
	}
	for i := range want {
		if c.extraTables[i] != want[i] {
			return false
		}
	}
	return c.tableEntryBytes(0) == coreruntime.TableEntryBytes && c.tableEntryBytes(1) == 8 && c.tableEntryBytes(2) == coreruntime.TableEntryBytes
}

func rv64Core3RefIsNullExecutionElements(c *Compiled) bool {
	if len(c.Elems) != 2 {
		return false
	}
	for i := range c.Elems {
		e := &c.Elems[i]
		if e.Mode != ElemModeActive || e.Offset.Base != 1 || e.Offset.HasGlobal || e.Offset.Global != 0 || len(e.Offset.Expr) != 0 || len(e.Values) != 1 {
			return false
		}
		v := e.Values[0]
		if v.FuncIndex != 0 || v.GlobalIndex != 0 || len(v.Expr) != 0 || v.Null || v.HasGlobal || v.I31Wrap || v.RepeatPrevious {
			return false
		}
	}
	first, second := c.Elems[0], c.Elems[1]
	return first.TableIndex == 0 && first.RefType == ValFuncRef && first.ValueTypeIndex == 0 && !first.HasValueType && second.TableIndex == 2 && second.RefType == ValFuncRef && second.ValueTypeIndex == 3 && second.HasValueType
}

// rv64Core3RefAsNonNullDeclarationMetadata is the exact retained metadata
// witness for pinned ref_as_non_null.2.wasm. It is deliberately separate from
// rv64Core3ReferenceDeclarationMetadata: all three reference parameters are
// non-null here, including abstract externref.
func rv64Core3RefAsNonNullDeclarationMetadata(c *Compiled) bool {
	return rv64Core3NonNullDeclarationMetadata(c, [...]int{0, 656, 1312})
}

// rv64Core3RefIsNullDeclarationMetadata differs only in the exact framed entry
// extents emitted by ref.is_null before the numeric result is dropped.
func rv64Core3RefIsNullDeclarationMetadata(c *Compiled) bool {
	return rv64Core3NonNullDeclarationMetadata(c, [...]int{0, 608, 1216})
}

func rv64Core3NonNullDeclarationMetadata(c *Compiled, wantEntries [3]int) bool {
	if c == nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceControlABI || c.nativeReferenceCallABI || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences || c.requiredAMD64Features != 0 || c.dynamicFuncrefEscape || c.dynamicImports || c.needsFuncRefDescs() || c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.Types) != 4 || len(c.ValueTypes) != 0 || len(c.FuncTypeID) != 3 || len(c.Entry) != 3 || len(c.InternalEntry) != 3 || len(c.code) == 0 || len(c.Exports) != 0 || c.Names != nil {
		return false
	}
	if c.tableCount() != 0 || len(c.extraTables) != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.tableExports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 {
		return false
	}
	wantTypeIDs := [...]uint64{0x2e23aa23f0da9ca1, 0xc164fecadb664ff3, 0x60e6cc415c67ca8d}
	for i := range c.Funcs {
		sig := c.Funcs[i]
		if !sig.HasTypeIndex || sig.TypeIndex != uint32(i+1) || len(sig.Params) != 1 || len(sig.Results) != 0 || sig.unsafeCrossTail || c.Entry[i] != wantEntries[i] || c.InternalEntry[i] != wantEntries[i] || c.FuncTypeID[i] != wantTypeIDs[i] {
			return false
		}
		want := ValFuncRef
		if i == 2 {
			want = ValExternRef
		}
		if sig.Params[0] != want {
			return false
		}
	}
	for i := range c.Types {
		typ := &c.Types[i]
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Results) != 0 || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
			return false
		}
		if i == 0 {
			if len(typ.Params) != 0 {
				return false
			}
			continue
		}
		if len(typ.Params) != 1 {
			return false
		}
		param := typ.Params[0]
		if param.Kind != ValueTypeReference || param.Ref.Nullable || param.Ref.Exact {
			return false
		}
		switch i {
		case 1:
			if !param.Ref.Heap.Defined || param.Ref.Heap.Abstract != 0 || param.Ref.Heap.TypeIndex != 0 {
				return false
			}
		case 2:
			if param.Ref.Heap.Defined || param.Ref.Heap.Abstract != AbstractHeapFunc || param.Ref.Heap.TypeIndex != 0 {
				return false
			}
		case 3:
			if param.Ref.Heap.Defined || param.Ref.Heap.Abstract != AbstractHeapExtern || param.Ref.Heap.TypeIndex != 0 {
				return false
			}
		}
	}
	return true
}

// rv64Core3RefAsNonNullDeclarations completes the retained artifact witness.
// The module has no callable, descriptor, import, start, initializer, state or
// resource path, so its non-null externref parameter remains declaration-only.
func rv64Core3RefAsNonNullDeclarations(c *Compiled) bool {
	if !rv64Core3RefAsNonNullDeclarationMetadata(c) || !rv64Core3RefAsNonNullDeclarationCode(c) || !c.needsFuncRefContextHeader || len(c.GCTypeDescs) != 4 {
		return false
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != corergc.TypeID(i) || desc.Kind != corergc.KindFunc || !desc.Final || desc.HasSuper || desc.Super != 0 || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs {
			return false
		}
	}
	if d := c.memoryDir; d != nil && (len(d.defs) != 0 || len(d.exports) != 0 || d.staged || d.stagedMemory64 || d.hasGCInitializers() || d.hasExceptionTagMetadata()) {
		return false
	}
	return true
}

// rv64Core3RefIsNullDeclarations completes the retained artifact witness for
// pinned ref_is_null.1.wasm. Its externref parameter is private and non-null,
// and the module has no callable or resource surface.
func rv64Core3RefIsNullDeclarations(c *Compiled) bool {
	if !rv64Core3RefIsNullDeclarationMetadata(c) || !rv64Core3RefIsNullDeclarationCode(c) || !c.needsFuncRefContextHeader || len(c.GCTypeDescs) != 4 {
		return false
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != corergc.TypeID(i) || desc.Kind != corergc.KindFunc || !desc.Final || desc.HasSuper || desc.Super != 0 || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs {
			return false
		}
	}
	if d := c.memoryDir; d != nil && (len(d.defs) != 0 || len(d.exports) != 0 || d.staged || d.stagedMemory64 || d.hasGCInitializers() || d.hasExceptionTagMetadata()) {
		return false
	}
	return true
}

// Source escape confinement is attested by the compiler-origin witness; Wasm
// bodies and dropped declarative targets are not retained in native artifacts.
// The retained shape and every binding still require independent admission.
func rv64ValidateReferenceFunctions(c *Compiled) error {
	if c.nativeReferenceFunctionABI && rv64PrimitiveReferenceContract(c) {
		for _, sig := range c.Funcs {
			if err := validateFuncSignature(sig, c.Types); err != nil {
				return err
			}
		}
		return nil
	}
	if !c.nativeReferenceFunctionABI {
		return nil
	}
	if rv64MutableFuncrefGlobalLocalCall(c) {
		return nil // Exact code and retained metadata bind the sole typed-cell call_ref product.
	}
	if rv64Core3ReferenceWitnessesForImage && rv64Core3RefIsNullExecution(c) {
		return nil
	}
	reject := func(detail string) error { return fmt.Errorf("RV64 closed reference functions: %s", detail) }
	publicHostReferenceBoundary := rv64PublicHostReferenceBoundary(c)
	publicDynamicHostReferenceBoundary := rv64PublicDynamicHostReferenceBoundary(c)
	publicReferenceCallBoundary := rv64PublicReferenceCallBoundary(c)
	privateRuntimeCallCast := rv64LegacyGCProductsForImage && (rv64GCTypeSubtypingRuntimeCallCast(c) || rv64GCTypeSubtypingRuntimeFinalityCallCast(c) || rv64GCTypeSubtypingRuntimeTypedTableCall(c) || rv64GCTypeSubtypingRefTestSingle(c))
	if c.NumImports < 0 || c.NumImports > railshot.MaxFunctionImports || len(c.Imports) != c.NumImports || len(c.importFuncSigs) != c.NumImports || c.dynamicImports != (c.NumImports > 0) {
		return reject("bounded numeric import directory required")
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || len(c.Funcs) == 0 || len(c.Funcs) > railshot.MaxModuleFunctions-c.NumImports || len(c.Types) > railshot.MaxFunctions || len(c.Exports) > railshot.MaxModuleExports || len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return reject("bounded framed function ABI required")
	}
	if c.dynamicFuncrefEscape && !privateRuntimeCallCast && !publicReferenceCallBoundary && !publicHostReferenceBoundary && !publicDynamicHostReferenceBoundary || !c.HasStart && c.StartIsImport || (!c.HasStart || c.StartIsImport) && c.StartLocalFunc != 0 || (!c.HasStart || !c.StartIsImport) && c.StartImportIdx != 0 || c.tableImport != "" || c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax || len(c.tableExports) != 0 || len(c.customInstructions) != 0 {
		return reject("external state or callable escape")
	}
	if c.HasStart {
		if _, err := compiledStartIndex(c); err != nil {
			return err
		}
	}
	if c.HasStart && !c.StartIsImport {
		local := c.StartLocalFunc
		entry, end := c.Entry[local], len(c.code)
		if local+1 < len(c.Entry) {
			end = c.Entry[local+1]
		}
		if local >= len(c.InternalEntry) || c.InternalEntry[local] != entry || entry < 0 || entry&15 != 0 || end <= entry || end > len(c.code) {
			return reject("defined start framed entry extent")
		}
		if _, err := railshot.NativeFrameBytes(c.code[:end], entry); err != nil {
			return reject("defined start actual frame packet")
		}
	}
	if d := c.memoryDir; d != nil && (d.hasExceptionTagMetadata() || d.hasGCInitializers()) {
		return reject("resource initializer metadata")
	}
	// Bound resource directories before exact-name helpers scan their prefixes.
	if c.memoryCount() > railshot.MaxProductionMemories || len(c.Globals) > 256 || len(c.GlobalImports) > len(c.Globals) || len(c.ValueTypes) > 6 {
		return reject("bounded numeric resource directories required")
	}
	functions, _, memories, _, exact := c.importModuleEndSections()
	if !exact {
		return reject("exact import names required")
	}
	for _, ends := range [][]uint64{functions, memories} {
		for _, end := range ends {
			if end == 0 {
				return reject("exact import separator required")
			}
		}
	}
	if err := c.validateImportModuleEnds(); err != nil {
		return err
	}
	if !privateRuntimeCallCast && !(rv64LegacyGCProductsForImage && (rv64GCTypeSubtypingRecursiveFunctions(c) || rv64GCTypeSubtypingRefFuncGlobals(c))) {
		if err := rv64ValidateDeclaredControlTypes(c); err != nil {
			return err
		}
	}
	hasReferences := privateRuntimeCallCast || publicReferenceCallBoundary || publicHostReferenceBoundary || publicDynamicHostReferenceBoundary
	for i, sig := range c.Funcs {
		if !sig.HasTypeIndex || uint64(sig.TypeIndex) >= uint64(len(c.Types)) {
			return reject("exact actual function type binding required")
		}
		if err := validateFuncSignature(sig, c.Types); err != nil {
			return err
		}
		hasReferences = hasReferences || railshotNonCallableFunction(c, c.NumImports+i)
		if publicHostReferenceBoundary || publicDynamicHostReferenceBoundary {
			for _, values := range [][]ValType{sig.Params, sig.Results} {
				for _, value := range values {
					hasReferences = hasReferences || value == ValFuncRef || value == ValExternRef
				}
			}
		}
	}
	if !hasReferences {
		return reject("witness without a reference-valued function")
	}
	for _, sig := range c.importFuncSigs {
		if !sig.HasTypeIndex || uint64(sig.TypeIndex) >= uint64(len(c.Types)) {
			return reject("exact imported function type binding required")
		}
		if err := validateFuncSignature(sig, c.Types); err != nil {
			return err
		}
		if _, err := rv64NativeCallSignatureValues(sig, publicHostReferenceBoundary || publicDynamicHostReferenceBoundary, false); err != nil {
			return err
		}
	}
	// Exact declared/actual signatures are bounded before selected plans can
	// consult sparse source rows or compare their numeric result contracts.
	if m := rv64NativeTailModule(c); buildfeatures.TailCall != 0 && m != nil {
		for _, p := range m.Plans {
			if p.Kind == 0 {
				if p.TypeIndex != 0 || p.ReferenceBase != 0 || uint64(p.ImportIndex) >= uint64(c.NumImports) {
					return reject("canonical numeric direct imported-tail plan required")
				}
			} else if (p.Kind != 1 && p.Kind != 2) || p.ImportIndex != 0 {
				return reject("only canonical numeric proper-tail plans admitted")
			}
		}
		if err := rv64ValidateNativeTailPlans(c, m.Plans); err != nil {
			return err
		}
	}
	remaining := railshot.MaxModuleBytes
	if c.HasStart {
		remaining -= 5
	}
	if rv64ReferenceElementsForImage {
		if err := rv64ValidateReferenceFunctionTable(c, &remaining); err != nil {
			return err
		}
	} else if c.TableHasValueType || c.TableValueTypeIndex != 0 {
		return fmt.Errorf("RV64 closed reference table: unused table metadata")
	}
	if rv64ReferenceStorageForImage {
		if err := rv64ValidateReferenceFunctionState(c, &remaining); err != nil {
			return err
		}
	} else {
		if len(c.GlobalImports) != 0 {
			return fmt.Errorf("RV64 closed reference state: directory bound")
		}
		if c.MemMinPages != 0 || c.MemMaxPages != 0 || c.MemHasMax || c.memoryImport != "" {
			return fmt.Errorf("RV64 closed reference state: inactive memory metadata")
		}
		if len(c.ValueTypes) != 0 {
			return fmt.Errorf("RV64 closed reference state: unused exact value types")
		}
	}
	if err := rv64ValidateReferenceFunctionExports(c, &remaining); err != nil {
		return err
	}
	for i := range c.importFuncSigs {
		if remaining < 5 || len(c.Imports[i]) > remaining-5 {
			return reject("import binding/name byte bound")
		}
		remaining -= 5 + len(c.Imports[i])
	}
	for _, typ := range c.Types {
		if typ.Describes != 0 || typ.Descriptor != 0 {
			return reject("inactive type metadata")
		}
		for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
			for _, v := range values {
				remaining--
				if v.Kind == ValueTypeReference {
					remaining -= 5
				}
				if remaining < 0 {
					return reject("aggregate declared carrier byte bound")
				}
			}
		}
	}
	referenceExports := false
	for _, index := range c.Exports {
		if index < 0 || index >= c.NumImports+len(c.Funcs) {
			return reject("function export index")
		}
		referenceExports = referenceExports || railshotNonCallableFunction(c, index)
	}
	if referenceExports {
		if c.tableCount() != 0 || (c.nativeReferenceCallABI || rv64HasReferenceTail(c)) && !rv64PublicReferenceCallBoundary(c) && !publicHostReferenceBoundary && !publicDynamicHostReferenceBoundary {
			return reject("public reference wrapper requires ephemeral owner-scoped transport")
		}
		for _, global := range c.Globals {
			if global.Type == ValFuncRef {
				return reject("public reference wrapper requires reference-global-free transport")
			}
		}
	}
	if c.tableCount() != 0 {
		// Full signatures are validated before the common numeric key checker
		// uses its fixed carrier arrays. Private ordinals remain sparse holes.
		return rv64ValidateOwnedTable(c)
	}
	return nil
}

// Internal collector signatures are safe without a separate Runtime pilot flag
// when the authenticated rooted frame contract exposes only numeric callable
// parameters/results. Reference metadata remains private to the compiled owner.
func rv64RootedInternalFunctionContract(c *Compiled) bool {
	if !nativeCollectorFrameRootsBuilt || c == nil || !rv64SourceContractValid(c) || !rv64FreshRootedGC(c) {
		return false
	}
	numeric := func(sig FuncSig) bool {
		if !sig.HasTypeIndex || uint64(sig.TypeIndex) >= uint64(len(c.Types)) || validateFuncSignature(sig, c.Types) != nil {
			return false
		}
		for _, values := range [][]ValueTypeDescriptor{c.Types[sig.TypeIndex].Params, c.Types[sig.TypeIndex].Results} {
			for _, value := range values {
				if value.Kind == ValueTypeReference {
					return false
				}
			}
		}
		for _, values := range [][]ValType{sig.Params, sig.Results} {
			for _, value := range values {
				if value != ValI32 && value != ValI64 && value != ValF32 && value != ValF64 && value != ValV128 {
					return false
				}
			}
		}
		return true
	}
	for _, sig := range c.importFuncSigs {
		if !numeric(sig) {
			return false
		}
	}
	for _, index := range c.Exports {
		if index < 0 || index >= c.NumImports+len(c.Funcs) {
			return false
		}
		if index >= c.NumImports && !numeric(c.Funcs[index-c.NumImports]) {
			return false
		}
	}
	return true
}
