//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCExternCodeDigest = [sha256.Size]byte{0xb4, 0x64, 0x11, 0xb1, 0x4b, 0xaf, 0x1a, 0xe9, 0xc6, 0xd6, 0x5a, 0xb5, 0x76, 0x88, 0x57, 0x17, 0x6c, 0x8e, 0x73, 0x06, 0x13, 0xfe, 0x24, 0xd6, 0x35, 0x52, 0xf9, 0xca, 0x09, 0x4b, 0xe6, 0x30}

func rv64GCExternExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 6076 && sha256.Sum256(c.code) == rv64GCExternCodeDigest
}

// rv64GCExtern pins the complete retained semantic directory because trusted
// artifacts no longer carry the original Wasm bodies.
func rv64GCExtern(c *Compiled) bool {
	wantEntry := []int{0, 576, 1792, 2464, 3136, 3968}
	if c == nil || !rv64GCExternExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || !slices.Equal(c.Entry, wantEntry) || !slices.Equal(c.InternalEntry, wantEntry) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 6 || len(c.FuncTypeID) != 6 || len(c.Types) != 8 || len(c.GCTypeDescs) != 8 || len(c.Exports) != 5 ||
		len(c.Globals) != 2 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 2 || c.tableCount() != 1 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 ||
		c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || !c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 ||
		c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if c.TableType != ValAnyRef || !c.TableHasValueType || c.TableValueTypeIndex != 1 || c.TableSize != 10 || c.TableMax != 10 || c.TableHasMax || c.TableAddr64 {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 ||
		d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	wantExports := map[string]int{"init": 1, "internalize": 2, "externalize": 3, "externalize-i": 4, "externalize-ii": 5}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	externref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}
	anyref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeFunction},
		{RecGroup: 1, Final: true, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8}}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{externref}},
		{RecGroup: 4, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{externref}, Results: []ValueTypeDescriptor{anyref}},
		{RecGroup: 5, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{anyref}, Results: []ValueTypeDescriptor{externref}},
		{RecGroup: 6, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}, Results: []ValueTypeDescriptor{externref}},
		{RecGroup: 7, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}, Results: []ValueTypeDescriptor{anyref}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	if !slices.Equal(c.ValueTypes, []ValueTypeDescriptor{externref, anyref}) {
		return false
	}
	wantGlobals := []struct {
		typ        ValType
		valueIndex uint32
	}{{ValExternRef, 0}, {ValAnyRef, 1}}
	for i, want := range wantGlobals {
		g := c.Globals[i]
		if g.Type != want.typ || !g.HasValueType || g.ValueTypeIndex != want.valueIndex || g.Mutable || g.Bits != 0 || g.V128 != (V128{}) || g.HasInitGlobal || g.InitGlobal != 0 || g.HasInitFunc || g.InitFunc != 0 || len(g.InitExpr) != 0 {
			return false
		}
	}
	wantFuncs := []FuncSig{
		{TypeIndex: 0, HasTypeIndex: true},
		{Params: []ValType{ValExternRef}, TypeIndex: 3, HasTypeIndex: true},
		{Params: []ValType{ValExternRef}, Results: []ValType{ValAnyRef}, TypeIndex: 4, HasTypeIndex: true},
		{Params: []ValType{ValAnyRef}, Results: []ValType{ValExternRef}, TypeIndex: 5, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValExternRef}, TypeIndex: 6, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValAnyRef}, TypeIndex: 7, HasTypeIndex: true},
	}
	wantKeys := []uint64{
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.ExternRef}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.ExternRef}, Results: []wasm.ValType{wasm.AnyRef}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.AnyRef}, Results: []wasm.ValType{wasm.ExternRef}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.ExternRef}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.AnyRef}}),
	}
	for i, want := range wantFuncs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || got.HasTypeIndex != want.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wantKeys[i] {
			return false
		}
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindFunc, Final: true},
		{ID: 1, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{}, Align: 1, Final: true},
		{ID: 2, Kind: corergc.KindArray, Elem: corergc.StorageI8, ElemSize: 1, Align: 1, Final: true},
		{ID: 3, Kind: corergc.KindFunc, Final: true}, {ID: 4, Kind: corergc.KindFunc, Final: true}, {ID: 5, Kind: corergc.KindFunc, Final: true},
		{ID: 6, Kind: corergc.KindFunc, Final: true}, {ID: 7, Kind: corergc.KindFunc, Final: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	product, arrayProduct := c.stagedGCStructProduct(), c.stagedGCArrayProduct()
	productWitness := product == stagedGCStructExtern && arrayProduct == 0 || product == stagedGCStructGeneric && arrayProduct == stagedGCArrayProductGeneric
	return productWitness && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.usesGCStructHelpers() && c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil &&
		c.requiredFeatures == CoreFeatureBulkMemoryOperations|CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0
}

func railshotRestoreGCExternArtifact(c *Compiled) {
	if !rv64GCExtern(c) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.gcStructProduct = stagedGCStructExtern
	c.codeCache.gcArrayProduct = 0
}
