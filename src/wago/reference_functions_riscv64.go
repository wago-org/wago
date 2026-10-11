//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"fmt"
	"slices"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

// This permission belongs to the destination instance, never to artifact bytes
// or a source Compiled's cache. Trusted decoding retains unpublished RW staging.
func railshotValidateReferenceFunctionInstance(c *Compiled, opts InstantiateOptions) error {
	c = c.executionView()
	if c.nativeReferenceFunctionABI && !opts.railshotTailPolicy.referenceFunctions {
		return fmt.Errorf("%w: closed RV64 reference functions require private Runtime permission", railshot.ErrUnsupported)
	}
	if c.nativeReferenceFunctionABI {
		return c.validateCached()
	}
	return nil
}

// The closed profile preserves physical function indices, but internal
// reference-valued signatures are never canonical descriptor identities.
// Callers use frozen metadata; admission validates exact type bindings first.
func railshotNonCallableFunction(c *Compiled, function int) bool {
	if c == nil || !c.nativeReferenceFunctionABI {
		return false
	}
	if (function == 0 && len(c.Funcs) != 0 && rv64PublicReferenceCallBoundary(c) && rv64PublicReferenceCarrierSignature(c.Funcs[0])) ||
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

func rv64PublicReferenceCallBoundary(c *Compiled) bool {
	return rv64PublicReferenceCallBoundaryWithPlan(c, true)
}

func rv64PublicHostReferenceBoundary(c *Compiled) bool {
	return rv64PublicHostReferenceBoundaryWithPlan(c, true)
}

func rv64PublicDynamicHostReferenceBoundary(c *Compiled) bool {
	return rv64PublicDynamicHostReferenceBoundaryWithPlan(c, true)
}

var rv64PublicDynamicHostReferenceTypes = []ValType{ValI32, ValFuncRef, ValExternRef, ValI64, ValF32, ValF64}

// rv64PublicDynamicHostReferenceBoundary is the retained counterpart of the
// fixed wire-level call_ref/return_call_ref host bridge. Artifacts do not retain Wasm
// bodies, so this independently binds every surviving type, index, feature and
// resource directory before ordinary code or the two canonical tail plans can
// become callable.
func rv64PublicDynamicHostReferenceBoundaryWithPlan(c *Compiled, requirePlan bool) bool {
	if c == nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI {
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
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || requirePlan && !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
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
	wantCode := [sha256.Size]byte{0xe9, 0x2f, 0xfd, 0x31, 0x95, 0x0a, 0x11, 0xc4, 0x28, 0x97, 0x36, 0x4c, 0xf7, 0x30, 0x3f, 0x13, 0x0d, 0xd5, 0xcc, 0x30, 0x6b, 0x78, 0xa5, 0x83, 0x74, 0x1f, 0x93, 0x1e, 0x48, 0xa0, 0xe5, 0x11}
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
	return p.Kind == encoder.NativeTailReference && p.TypeIndex == 2 && p.FunctionIndex == 2 && p.ImportIndex == 0 && p.SourcePC == sourcePC && p.FrameBytes == 160 && p.SourceBase == 104 && p.SourceStride == 8 && p.ReferenceBase == 152 && rv64PublicDynamicHostReferenceBoundaryWithPlan(c, false)
}

// rv64PublicHostReferenceBoundary recognizes the retained form of the exact
// one-import/one-wrapper host reference bridge admitted by the RV64 frontend.
// Source bodies are not retained in artifacts, so every remaining structural,
// type, feature, resource and binding field is rechecked here before code can
// become executable.
func rv64PublicHostReferenceBoundaryWithPlan(c *Compiled, requirePlan bool) bool {
	if c == nil || !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI {
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
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || requirePlan && !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
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
	if p.Kind != encoder.NativeTailDirectImport || p.TypeIndex != 0 || p.FunctionIndex != 1 || p.ImportIndex != 0 || p.ReferenceBase != 0 || !rv64PublicHostReferenceBoundaryWithPlan(c, false) || !c.requiredFeatures.IsEnabled(CoreFeatureTailCall) {
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
	if c == nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.Types) != 3 || len(c.ValueTypes) != 0 || len(c.Exports) != 3 || c.tableCount() != 0 || len(c.extraTables) != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tagImportCount() != 0 || c.HasStart {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 || !c.NeedsFuncRefDescs || len(c.FuncTypeID) != 3 {
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
	if d := c.memoryDir; d != nil && (len(d.defs) != 0 || len(d.exports) != 0 || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0) {
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
	if c == nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceControlABI || c.nativeReferenceCallABI || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || rv64HasReferenceTail(c) {
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
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
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
	if d := c.memoryDir; d != nil && (len(d.defs) != 0 || len(d.exports) != 0 || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0) {
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
	if d := c.memoryDir; d != nil && (len(d.defs) != 0 || len(d.exports) != 0 || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0) {
		return false
	}
	return true
}

// Source escape confinement is attested by the compiler-origin witness; Wasm
// bodies and dropped declarative targets are not retained in native artifacts.
// The retained shape and every binding still require independent admission.
func rv64ValidateReferenceFunctions(c *Compiled) error {
	if !c.nativeReferenceFunctionABI {
		return nil
	}
	if rv64Core3RefIsNullExecution(c) {
		return nil
	}
	reject := func(detail string) error { return fmt.Errorf("RV64 closed reference functions: %s", detail) }
	publicHostReferenceBoundary := rv64PublicHostReferenceBoundary(c)
	publicDynamicHostReferenceBoundary := rv64PublicDynamicHostReferenceBoundary(c)
	if c.NumImports < 0 || c.NumImports > railshot.MaxHostFunctionImports || len(c.Imports) != c.NumImports || len(c.importFuncSigs) != c.NumImports || c.dynamicImports != (c.NumImports > 0) {
		return reject("bounded numeric import directory required")
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || len(c.Funcs) == 0 || len(c.Funcs) > railshot.MaxFunctions-c.NumImports || len(c.Types) > railshot.MaxFunctions || len(c.Exports) > railshot.MaxFunctions || len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return reject("bounded framed function ABI required")
	}
	if c.dynamicFuncrefEscape && !rv64PublicReferenceCallBoundary(c) && !publicHostReferenceBoundary && !publicDynamicHostReferenceBoundary || !c.HasStart && c.StartIsImport || (!c.HasStart || c.StartIsImport) && c.StartLocalFunc != 0 || (!c.HasStart || !c.StartIsImport) && c.StartImportIdx != 0 || c.tableImport != "" || c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax || len(c.tableExports) != 0 || len(c.customInstructions) != 0 {
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
	if d := c.memoryDir; d != nil && (len(d.ehTags) != 0 || len(d.ehTagExports) != 0 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil) {
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
	if err := rv64ValidateDeclaredControlTypes(c); err != nil {
		return err
	}
	hasReferences := false
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
		if _, err := rv64NativeTailSignatureValues(sig, publicHostReferenceBoundary || publicDynamicHostReferenceBoundary); err != nil {
			return err
		}
	}
	// Exact declared/actual signatures are bounded before selected plans can
	// consult sparse source rows or compare their numeric result contracts.
	if m := rv64NativeTailModule(c); m != nil {
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
	if err := rv64ValidateReferenceFunctionTable(c, &remaining); err != nil {
		return err
	}
	if err := rv64ValidateReferenceFunctionState(c, &remaining); err != nil {
		return err
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
