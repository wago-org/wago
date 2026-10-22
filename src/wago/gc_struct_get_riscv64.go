//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCStructGetCodeDigest = [sha256.Size]byte{0x54, 0xa4, 0xd5, 0x21, 0x52, 0xa3, 0x42, 0x28, 0xf5, 0x08, 0x8d, 0xc4, 0x62, 0xea, 0x05, 0x47, 0x60, 0xfc, 0x3b, 0xd5, 0x92, 0x4c, 0x0a, 0xda, 0x6c, 0x43, 0xb7, 0x62, 0xc0, 0xd5, 0x11, 0x6d}

func rv64GCStructGetExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 1244 && sha256.Sum256(c.code) == rv64GCStructGetCodeDigest
}

// rv64GCStructGet pins the complete retained semantic directory because Wasm
// bodies are absent from trusted artifacts.
func rv64GCStructGet(c *Compiled) bool {
	if c == nil || !rv64GCStructGetExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI ||
		!c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata ||
		c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.Types) != 2 || len(c.Exports) != 1 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil ||
		len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 ||
		len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	if get, ok := c.Exports["get"]; !ok || get != 0 {
		return false
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{{Mutable: true, Storage: StorageTypeDescriptor{Value: i32}}}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{i32}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	wantFunc := FuncSig{Results: []ValType{ValI32}, TypeIndex: 1, HasTypeIndex: true}
	gotFunc := c.Funcs[0]
	funcType := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}
	if !slices.Equal(gotFunc.Params, wantFunc.Params) || !slices.Equal(gotFunc.Results, wantFunc.Results) || gotFunc.TypeIndex != wantFunc.TypeIndex || !gotFunc.HasTypeIndex || gotFunc.unsafeCrossTail ||
		len(c.FuncTypeID) != 1 || c.FuncTypeID[0] != wasm.StructuralFuncTypeKey(funcType) {
		return false
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}}, Size: 4, Align: 4, Final: true},
		{ID: 1, Kind: corergc.KindFunc, Final: true},
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
	wantEntry := []int{0}
	product := c.stagedGCStructProduct()
	return (product == stagedGCStructNumericLocal || product == stagedGCStructGeneric) && c.stagedGCArrayProduct() == 0 && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		c.usesGCStructHelpers() && !c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0 &&
		slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry)
}
