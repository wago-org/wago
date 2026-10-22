//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCArrayLocalCodeDigest = [sha256.Size]byte{0x88, 0xe5, 0x7a, 0x7f, 0x97, 0x1e, 0x9d, 0x4a, 0xf1, 0x5e, 0x5f, 0x79, 0xd1, 0xa4, 0x6b, 0xc6, 0x44, 0x8e, 0xaa, 0xa1, 0xae, 0x2d, 0x02, 0x61, 0xd3, 0x18, 0xa6, 0xa8, 0xba, 0xe6, 0xac, 0x79}

func rv64GCArrayLocalExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 3468 && sha256.Sum256(c.code) == rv64GCArrayLocalCodeDigest
}

// rv64GCArrayLocal pins the complete retained semantic directory because Wasm
// bodies and local declarations are absent from trusted artifacts.
func rv64GCArrayLocal(c *Compiled) bool {
	if c == nil || !rv64GCArrayLocalExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI ||
		!c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata ||
		c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.Types) != 4 || len(c.Exports) != 3 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil ||
		len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 ||
		len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	get, getOK := c.Exports["get"]
	setGet, setGetOK := c.Exports["set_get"]
	length, lengthOK := c.Exports["len"]
	if !getOK || get != 0 || !setGetOK || setGet != 1 || !lengthOK || length != 2 {
		return false
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Value: i32}}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, i32}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, i32, i32}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}, Results: []ValueTypeDescriptor{i32}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	wantFuncs := []FuncSig{
		{Params: []ValType{ValI32, ValI32}, Results: []ValType{ValI32}, TypeIndex: 1, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32, ValI32}, Results: []ValType{ValI32}, TypeIndex: 2, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 3, HasTypeIndex: true},
	}
	funcTypes := []*wasm.CompType{
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}},
	}
	if len(c.FuncTypeID) != len(wantFuncs) {
		return false
	}
	for i, want := range wantFuncs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || !got.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wasm.StructuralFuncTypeKey(funcTypes[i]) {
			return false
		}
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindArray, Elem: corergc.StorageI32, ElemSize: 4, Align: 4, Final: true},
		{ID: 1, Kind: corergc.KindFunc, Final: true},
		{ID: 2, Kind: corergc.KindFunc, Final: true},
		{ID: 3, Kind: corergc.KindFunc, Final: true},
	}
	if len(c.GCTypeDescs) != len(wantDescs) {
		return false
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	wantEntry := []int{0, 784, 1680}
	product := c.stagedGCArrayProduct()
	return c.stagedGCStructProduct() == 0 && (product == stagedGCArrayProductNumericLocal || product == stagedGCArrayProductGeneric) && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		!c.usesGCStructHelpers() && c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0 &&
		slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry)
}
