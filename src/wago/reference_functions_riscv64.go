//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
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
	if c == nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || c.dynamicFuncrefEscape || c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.Types) != 3 || len(c.ValueTypes) != 0 || len(c.Exports) != 3 || c.tableCount() != 0 || len(c.extraTables) != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tagImportCount() != 0 || c.HasStart {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 || !c.NeedsFuncRefDescs || len(c.FuncTypeID) != 3 {
		return false
	}
	wantFeatures := CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
	if c.requiredFeatures != wantFeatures || rv64HasReferenceTail(c) {
		return false
	}
	for i := range c.Funcs {
		if !c.Funcs[i].HasTypeIndex || c.Funcs[i].TypeIndex != uint32(i) || c.Types[i].RecGroup != uint32(i) {
			return false
		}
	}
	target, producer, caller := c.Funcs[0], c.Funcs[1], c.Funcs[2]
	if len(target.Params) != 0 || len(target.Results) != 1 || target.Results[0] != ValI32 || len(producer.Params) != 0 || len(producer.Results) != 1 || producer.Results[0] != ValFuncRef || len(caller.Params) != 1 || caller.Params[0] != ValFuncRef || len(caller.Results) != 1 || (caller.Results[0] != ValI32 && caller.Results[0] != ValFuncRef) {
		return false
	}
	targetType, producerType, callerType := c.Types[0], c.Types[1], c.Types[2]
	if !targetType.Final || len(targetType.Params) != 0 || len(targetType.Results) != 1 || targetType.Results[0].Kind != ValueTypeI32 || !producerType.Final || len(producerType.Params) != 0 || len(producerType.Results) != 1 || !callerType.Final || len(callerType.Params) != 1 || len(callerType.Results) != 1 {
		return false
	}
	produced := producerType.Results[0]
	consumed := callerType.Params[0]
	if produced.Kind != ValueTypeReference || !produced.Ref.Nullable || produced.Ref.Exact || produced.Ref.Heap.Defined || produced.Ref.Heap.Abstract != AbstractHeapFunc || consumed.Kind != ValueTypeReference || !consumed.Ref.Nullable || consumed.Ref.Exact || !consumed.Ref.Heap.Defined || consumed.Ref.Heap.TypeIndex != 0 {
		return false
	}
	callerResult := callerType.Results[0]
	if caller.Results[0] == ValI32 {
		if callerResult.Kind != ValueTypeI32 {
			return false
		}
	} else if callerResult.Kind != ValueTypeReference || !callerResult.Ref.Nullable || callerResult.Ref.Exact || callerResult.Ref.Heap.Defined || callerResult.Ref.Heap.Abstract != AbstractHeapFunc {
		return false
	}
	for i, name := range []string{"target", "get", "call"} {
		index, ok := c.Exports[name]
		if !ok || index != i {
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

// rv64Core3RefAsNonNullDeclarationMetadata is the exact retained metadata
// witness for pinned ref_as_non_null.2.wasm. It is deliberately separate from
// rv64Core3ReferenceDeclarationMetadata: all three reference parameters are
// non-null here, including abstract externref.
func rv64Core3RefAsNonNullDeclarationMetadata(c *Compiled) bool {
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
	wantEntries := [...]int{0, 656, 1312}
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

// Source escape confinement is attested by the compiler-origin witness; Wasm
// bodies and dropped declarative targets are not retained in native artifacts.
// The retained shape and every binding still require independent admission.
func rv64ValidateReferenceFunctions(c *Compiled) error {
	if !c.nativeReferenceFunctionABI {
		return nil
	}
	reject := func(detail string) error { return fmt.Errorf("RV64 closed reference functions: %s", detail) }
	if c.NumImports < 0 || c.NumImports > railshot.MaxHostFunctionImports || len(c.Imports) != c.NumImports || len(c.importFuncSigs) != c.NumImports || c.dynamicImports != (c.NumImports > 0) {
		return reject("bounded numeric import directory required")
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || len(c.Funcs) == 0 || len(c.Funcs) > railshot.MaxFunctions-c.NumImports || len(c.Types) > railshot.MaxFunctions || len(c.Exports) > railshot.MaxFunctions || len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return reject("bounded framed function ABI required")
	}
	if c.dynamicFuncrefEscape || !c.HasStart && c.StartIsImport || (!c.HasStart || c.StartIsImport) && c.StartLocalFunc != 0 || (!c.HasStart || !c.StartIsImport) && c.StartImportIdx != 0 || c.tableImport != "" || c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax || len(c.tableExports) != 0 || len(c.customInstructions) != 0 {
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
		if _, err := rv64NativeTailSignatureValues(sig); err != nil {
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
		if c.tableCount() != 0 || (c.nativeReferenceCallABI && !rv64PublicReferenceCallBoundary(c)) || rv64HasReferenceTail(c) {
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
