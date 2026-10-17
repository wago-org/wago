//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCRefTestTableCodeDigest = [sha256.Size]byte{0x6a, 0x0c, 0xce, 0xa7, 0xd2, 0x38, 0x18, 0x9e, 0xc4, 0x4a, 0x79, 0xe4, 0xca, 0x17, 0x65, 0x24, 0x73, 0x17, 0x77, 0xce, 0x8d, 0xd4, 0x7d, 0xf2, 0xcc, 0x8e, 0x5e, 0x05, 0x83, 0xdf, 0xbf, 0x96}

func rv64GCRefTestTableExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 4232 && sha256.Sum256(c.code) == rv64GCRefTestTableCodeDigest
}

// rv64GCRefTestTable pins the complete retained semantic directory because
// trusted artifacts no longer carry Wasm bodies.
func rv64GCRefTestTable(c *Compiled) bool {
	if c == nil || !rv64GCRefTestTableExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI ||
		!c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata ||
		c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 4 || len(c.Types) != 5 || len(c.Exports) != 4 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 1 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil ||
		len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	if !c.HasTable || c.TableType != ValAnyRef || !c.TableHasValueType || c.TableValueTypeIndex != 0 || c.TableSize != 2 || c.TableMax != 2 || c.TableHasMax || c.TableAddr64 ||
		c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.HasMemory || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 ||
		len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	wantExports := map[string]int{"init": 0, "base": 1, "left": 2, "right": 3}
	if len(c.Exports) != len(wantExports) {
		return false
	}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	i64 := ValueTypeDescriptor{Kind: ValueTypeI64}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Kind: CompositeTypeStruct},
		{RecGroup: 1, Kind: CompositeTypeStruct, Supers: []uint32{0}, Fields: []FieldTypeDescriptor{{Storage: StorageTypeDescriptor{Value: i32}}}},
		{RecGroup: 2, Kind: CompositeTypeStruct, Supers: []uint32{0}, Fields: []FieldTypeDescriptor{{Storage: StorageTypeDescriptor{Value: i64}}}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction},
		{RecGroup: 4, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}, Results: []ValueTypeDescriptor{i32}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	wantFuncs := []FuncSig{
		{TypeIndex: 3, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
	}
	wantTypeIDs := []uint64{
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}),
	}
	if !slices.Equal(c.FuncTypeID, wantTypeIDs) {
		return false
	}
	for i, want := range wantFuncs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || !got.HasTypeIndex || got.unsafeCrossTail {
			return false
		}
	}
	structRef := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapStruct}}}
	if c.ValueTypes[0] != structRef || len(c.GCTypeDescs) != 5 {
		return false
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindStruct, Align: 1},
		{ID: 1, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}}, Size: 4, Align: 4, Super: 0, HasSuper: true},
		{ID: 2, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI64}}, Size: 8, Align: 8, Super: 0, HasSuper: true},
		{ID: 3, Kind: corergc.KindFunc, Final: true},
		{ID: 4, Kind: corergc.KindFunc, Final: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	product := c.stagedGCStructProduct()
	productWitness := product == stagedGCStructRefTestTable || product == stagedGCStructGeneric
	wantEntry := []int{0, 864, 1728, 2592}
	return productWitness && c.stagedGCArrayProduct() == 0 && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		c.usesGCStructHelpers() && !c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0 &&
		slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry)
}

func railshotRestoreGCRefTestTableArtifact(c *Compiled) {
	if !rv64GCRefTestTable(c) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.gcStructProduct = stagedGCStructRefTestTable
}
