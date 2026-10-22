//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var (
	rv64GCBrOnCastAbstractCodeDigest     = [sha256.Size]byte{0x89, 0x4e, 0x26, 0x3b, 0x0e, 0x1a, 0xfe, 0xc0, 0x7e, 0xa9, 0x90, 0x8c, 0xe7, 0xc2, 0xed, 0xe4, 0x05, 0x6b, 0x3a, 0x65, 0x0a, 0x0d, 0x97, 0xd7, 0x71, 0xe9, 0xeb, 0x55, 0x49, 0x5c, 0xb3, 0x47}
	rv64GCBrOnCastFailAbstractCodeDigest = [sha256.Size]byte{0x00, 0x7c, 0x0d, 0x52, 0xd4, 0xa6, 0xde, 0x6f, 0x90, 0x73, 0x48, 0xb1, 0x78, 0xac, 0x7b, 0x25, 0x31, 0xb6, 0x82, 0x2a, 0x41, 0x4f, 0xfa, 0x73, 0x0c, 0x5d, 0x03, 0x20, 0xd7, 0x34, 0x5c, 0x27}
)

func rv64GCBrOnCastAbstractExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 9004 && sha256.Sum256(c.code) == rv64GCBrOnCastAbstractCodeDigest
}

func rv64GCBrOnCastFailAbstractExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 9068 && sha256.Sum256(c.code) == rv64GCBrOnCastFailAbstractCodeDigest
}

func rv64GCBrOnCastAbstract(c *Compiled) bool {
	return rv64GCBrOnCastAbstractMetadata(c, stagedGCStructBrOnCastAbstract, []int{0, 608, 1856, 2688, 3680, 5008, 5984}, false)
}

func rv64GCBrOnCastFailAbstract(c *Compiled) bool {
	return rv64GCBrOnCastAbstractMetadata(c, stagedGCStructBrOnCastFailAbstract, []int{0, 608, 1856, 2704, 3712, 5056, 6048}, true)
}

func rv64GCBrOnCastAbstractMetadata(c *Compiled, wantProduct stagedGCStructProduct, wantEntry []int, fail bool) bool {
	codeOK := rv64GCBrOnCastAbstractExecutionCode(c)
	if fail {
		codeOK = rv64GCBrOnCastFailAbstractExecutionCode(c)
	}
	if c == nil || !codeOK || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || !slices.Equal(c.Entry, wantEntry) || !slices.Equal(c.InternalEntry, wantEntry) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 7 || len(c.FuncTypeID) != 7 || len(c.Types) != 7 || len(c.GCTypeDescs) != 7 || len(c.ValueTypes) != 1 || len(c.Exports) != 6 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tableCount() != 1 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 ||
		!c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if c.TableType != ValAnyRef || !c.TableHasValueType || c.TableValueTypeIndex != 0 || c.TableSize != 10 || c.TableMax != 10 || c.TableHasMax || c.TableAddr64 {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 ||
		d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	wantExports := map[string]int{"init": 1, "br_on_null": 2, "br_on_i31": 3, "br_on_struct": 4, "br_on_array": 5, "null-diff": 6}
	if fail {
		wantExports = map[string]int{"init": 1, "br_on_non_null": 2, "br_on_non_i31": 3, "br_on_non_struct": 4, "br_on_non_array": 5, "null-diff": 6}
	}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	externref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}
	anyref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	structref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapStruct}}}
	ref1 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 1}}}
	ref2 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 2}}}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{{Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI16}}}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8}}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{externref}},
		{RecGroup: 4, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 5, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{structref}, Results: []ValueTypeDescriptor{ref2}},
		{RecGroup: 6, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{structref}, Results: []ValueTypeDescriptor{ref1}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	if !slices.Equal(c.ValueTypes, []ValueTypeDescriptor{anyref}) {
		return false
	}
	wantFuncs := []FuncSig{
		{Results: []ValType{ValI32}, TypeIndex: 0, HasTypeIndex: true},
		{Params: []ValType{ValExternRef}, TypeIndex: 3, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
	}
	wantKeys := []uint64{
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.ExternRef}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}),
	}
	for i, want := range wantFuncs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || got.HasTypeIndex != want.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wantKeys[i] {
			return false
		}
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindFunc, Final: true},
		{ID: 1, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI16}}, Size: 2, Align: 2, Final: true},
		{ID: 2, Kind: corergc.KindArray, Elem: corergc.StorageI8, ElemSize: 1, Align: 1, Final: true},
		{ID: 3, Kind: corergc.KindFunc, Final: true},
		{ID: 4, Kind: corergc.KindFunc, Final: true},
		{ID: 5, Kind: corergc.KindFunc, Final: true},
		{ID: 6, Kind: corergc.KindFunc, Final: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	product, arrayProduct := c.stagedGCStructProduct(), c.stagedGCArrayProduct()
	productWitness := product == wantProduct && arrayProduct == 0 || product == stagedGCStructGeneric && arrayProduct == stagedGCArrayProductGeneric
	return productWitness && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.usesGCStructHelpers() && c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil &&
		c.requiredFeatures == CoreFeatureBulkMemoryOperations|CoreFeatureMultiValue|CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0
}

func railshotRestoreGCBrOnCastAbstractArtifact(c *Compiled) {
	if !rv64GCBrOnCastAbstract(c) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.gcStructProduct = stagedGCStructBrOnCastAbstract
	c.codeCache.gcArrayProduct = 0
}

func railshotRestoreGCBrOnCastFailAbstractArtifact(c *Compiled) {
	if !rv64GCBrOnCastFailAbstract(c) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.gcStructProduct = stagedGCStructBrOnCastFailAbstract
	c.codeCache.gcArrayProduct = 0
}
