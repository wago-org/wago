//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCRefTestAbstractCodeDigest = [sha256.Size]byte{0x46, 0x80, 0x00, 0x62, 0x2e, 0x09, 0xec, 0x52, 0xd3, 0xde, 0xaa, 0x35, 0xee, 0xff, 0xae, 0x93, 0xb8, 0x4e, 0x68, 0x15, 0x94, 0x3c, 0x51, 0x9d, 0xdc, 0x0d, 0x6c, 0x06, 0x22, 0x3d, 0xe3, 0x60}

func rv64GCRefTestAbstractExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 18172 && sha256.Sum256(c.code) == rv64GCRefTestAbstractCodeDigest
}

// rv64GCRefTestAbstract pins the complete retained semantic directory because
// trusted artifacts no longer carry Wasm bodies.
func rv64GCRefTestAbstract(c *Compiled) bool {
	if c == nil || !rv64GCRefTestAbstractExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI ||
		!c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata ||
		!c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 12 || len(c.Types) != 5 || len(c.Exports) != 11 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 3 || c.tableCount() != 3 || len(c.extraTables) != 2 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil ||
		len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	if !c.HasTable || c.TableType != ValAnyRef || !c.TableHasValueType || c.TableValueTypeIndex != 0 || c.TableSize != 10 || c.TableMax != 10 || c.TableHasMax || c.TableAddr64 ||
		c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.HasMemory || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	wantExtra := []tableDef{
		{Size: 10, Max: 10, Type: ValFuncRef, ValueTypeIndex: 1, HasValueType: true},
		{Size: 10, Max: 10, Type: ValExternRef, ValueTypeIndex: 2, HasValueType: true},
	}
	if !slices.Equal(c.extraTables, wantExtra) {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 ||
		len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	wantExports := map[string]int{
		"init": 1, "ref_test_null_data": 2, "ref_test_any": 3, "ref_test_eq": 4, "ref_test_i31": 5, "ref_test_struct": 6,
		"ref_test_array": 7, "ref_test_null_func": 8, "ref_test_func": 9, "ref_test_null_extern": 10, "ref_test_extern": 11,
	}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	anyref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	funcref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}
	externref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeFunction},
		{RecGroup: 1, Final: true, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8}}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{externref}},
		{RecGroup: 4, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}, Results: []ValueTypeDescriptor{i32}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	wantSigs := []FuncSig{
		{TypeIndex: 0, HasTypeIndex: true},
		{Params: []ValType{ValExternRef}, TypeIndex: 3, HasTypeIndex: true},
	}
	for range 10 {
		wantSigs = append(wantSigs, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true})
	}
	wantTypeIDs := []uint64{
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc}),
		wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.ExternRef}}),
	}
	i32Key := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}})
	for range 10 {
		wantTypeIDs = append(wantTypeIDs, i32Key)
	}
	if len(c.FuncTypeID) != len(wantTypeIDs) {
		return false
	}
	for i, want := range wantSigs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || got.HasTypeIndex != want.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wantTypeIDs[i] {
			return false
		}
	}
	if !slices.Equal(c.ValueTypes, []ValueTypeDescriptor{anyref, funcref, externref}) || len(c.GCTypeDescs) != 5 {
		return false
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindFunc, Final: true},
		{ID: 1, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{}, Align: 1, Final: true},
		{ID: 2, Kind: corergc.KindArray, Elem: corergc.StorageI8, ElemSize: 1, Align: 1, Final: true},
		{ID: 3, Kind: corergc.KindFunc, Final: true},
		{ID: 4, Kind: corergc.KindFunc, Final: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	product, arrayProduct := c.stagedGCStructProduct(), c.stagedGCArrayProduct()
	productWitness := product == stagedGCStructRefTestAbstract && arrayProduct == 0 || product == stagedGCStructGeneric && arrayProduct == stagedGCArrayProductGeneric
	wantEntry := []int{0, 576, 4976, 6048, 7184, 8320, 9456, 10592, 11728, 12960, 14192, 15424}
	return productWitness && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		c.usesGCStructHelpers() && c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureBulkMemoryOperations|CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0 &&
		slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry)
}

func railshotRestoreGCRefTestAbstractArtifact(c *Compiled) {
	if !rv64GCRefTestAbstract(c) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.gcStructProduct = stagedGCStructRefTestAbstract
	c.codeCache.gcArrayProduct = 0
}
