//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64OfficialGCRefEqCodeDigest = [sha256.Size]byte{0xc2, 0xd0, 0x8e, 0x0a, 0xf5, 0x42, 0xb4, 0xe6, 0x9d, 0x68, 0xce, 0x4f, 0x1d, 0x73, 0x65, 0x19, 0xa9, 0x99, 0x9c, 0xcb, 0xd0, 0x69, 0xfa, 0x27, 0x80, 0x40, 0x1e, 0x8b, 0xb0, 0x18, 0x34, 0xc8}

func rv64OfficialGCRefEqExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 3852 && sha256.Sum256(c.code) == rv64OfficialGCRefEqCodeDigest
}

// rv64OfficialGCRefEq is the retained counterpart of the exact allocating
// official gc/ref_eq fixture. Wasm bodies are not serialized, so publication
// is bound to its complete semantic directories and exact RV64G code image.
func rv64OfficialGCRefEq(c *Compiled) bool {
	if c == nil || !rv64OfficialGCRefEqExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI ||
		!c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata ||
		c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 2 || len(c.Types) != 9 || len(c.Exports) != 2 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 1 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil ||
		len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	if !c.HasTable || c.TableType != ValAnyRef || !c.TableHasValueType || c.TableValueTypeIndex != 0 || c.TableSize != 20 || c.TableMax != 20 || c.TableHasMax || c.TableAddr64 ||
		c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.HasMemory || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 ||
		len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	if init, ok := c.Exports["init"]; !ok || init != 0 {
		return false
	}
	if eq, ok := c.Exports["eq"]; !ok || eq != 1 {
		return false
	}

	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Kind: CompositeTypeStruct},
		{RecGroup: 1, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{{Storage: StorageTypeDescriptor{Value: i32}}}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8}}},
		{RecGroup: 3, Kind: CompositeTypeStruct, Supers: []uint32{0}},
		{RecGroup: 4, Kind: CompositeTypeStruct, Supers: []uint32{0}},
		{RecGroup: 5, Kind: CompositeTypeStruct, Supers: []uint32{1}, Fields: []FieldTypeDescriptor{{Storage: StorageTypeDescriptor{Value: i32}}}},
		{RecGroup: 6, Kind: CompositeTypeStruct, Supers: []uint32{1}, Fields: []FieldTypeDescriptor{{Storage: StorageTypeDescriptor{Value: i32}}}},
		{RecGroup: 7, Final: true, Kind: CompositeTypeFunction},
		{RecGroup: 8, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, i32}, Results: []ValueTypeDescriptor{i32}},
	}
	for i := range wantTypes {
		got, want := c.Types[i], wantTypes[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	wantFuncs := []FuncSig{
		{TypeIndex: 7, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32}, Results: []ValType{ValI32}, TypeIndex: 8, HasTypeIndex: true},
	}
	funcTypes := []*wasm.CompType{
		{Kind: wasm.CompFunc},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}},
	}
	if len(c.FuncTypeID) != 2 {
		return false
	}
	for i, want := range wantFuncs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || !got.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wasm.StructuralFuncTypeKey(funcTypes[i]) {
			return false
		}
	}
	eqref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapEq}}}
	if c.ValueTypes[0] != eqref || len(c.GCTypeDescs) != 9 {
		return false
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindStruct, Align: 1},
		{ID: 1, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}}, Size: 4, Align: 4},
		{ID: 2, Kind: corergc.KindArray, Elem: corergc.StorageI8, ElemSize: 1, Align: 1, Final: true},
		{ID: 3, Kind: corergc.KindStruct, Align: 1, Super: 0, HasSuper: true},
		{ID: 4, Kind: corergc.KindStruct, Align: 1, Super: 0, HasSuper: true},
		{ID: 5, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}}, Size: 4, Align: 4, Super: 1, HasSuper: true},
		{ID: 6, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}}, Size: 4, Align: 4, Super: 1, HasSuper: true},
		{ID: 7, Kind: corergc.KindFunc, Final: true},
		{ID: 8, Kind: corergc.KindFunc, Final: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	product, staged := c.stagedGCStructProduct(), c.stagedFeatures()
	arrayProduct := c.stagedGCArrayProduct()
	productWitness := product == stagedGCStructRefEq && arrayProduct == 0 || product == stagedGCStructGeneric && arrayProduct == stagedGCArrayProductGeneric
	wantEntry := []int{0, 2400}
	return productWitness && staged == CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.usesGCStructHelpers() && c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil &&
		c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0 && slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry)
}
