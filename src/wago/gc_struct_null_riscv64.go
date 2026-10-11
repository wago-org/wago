//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCStructNullCodeDigest = [sha256.Size]byte{0xda, 0x20, 0xa4, 0xc8, 0x70, 0xac, 0xca, 0x39, 0x87, 0x51, 0xd5, 0x02, 0xd2, 0xf9, 0x33, 0x6b, 0x5b, 0x91, 0xd9, 0x8b, 0xdd, 0xf1, 0xd1, 0xd0, 0x57, 0xfc, 0x4d, 0xb3, 0x1d, 0x7a, 0xe8, 0x5f}

func rv64GCStructNullExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 1896 && sha256.Sum256(c.code) == rv64GCStructNullCodeDigest
}

// rv64GCStructNull pins the retained semantic directory because trusted
// artifacts do not contain the original Wasm bodies that prove both null traps.
func rv64GCStructNull(c *Compiled) bool {
	if c == nil || !rv64GCStructNullExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI ||
		!c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata ||
		c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 2 || len(c.Types) != 2 || len(c.Exports) != 2 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil ||
		len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 ||
		len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	if get, ok := c.Exports["struct.get-null"]; !ok || get != 0 {
		return false
	}
	if set, ok := c.Exports["struct.set-null"]; !ok || set != 1 {
		return false
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{
			{Storage: StorageTypeDescriptor{Value: i32}},
			{Mutable: true, Storage: StorageTypeDescriptor{Value: i32}},
		}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeFunction},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	wantFunc := FuncSig{TypeIndex: 1, HasTypeIndex: true}
	if len(c.FuncTypeID) != 2 {
		return false
	}
	for i := range c.Funcs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, wantFunc.Params) || !slices.Equal(got.Results, wantFunc.Results) || got.TypeIndex != wantFunc.TypeIndex || !got.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != 0xd60da539b2828050 {
			return false
		}
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}, {Kind: corergc.StorageI32, Offset: 4}}, Size: 8, Align: 4, Final: true},
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
	wantEntry := []int{0, 672}
	product := c.stagedGCStructProduct()
	return (product == stagedGCStructNullDereference || product == stagedGCStructGeneric) && c.stagedGCArrayProduct() == 0 && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		c.usesGCStructHelpers() && !c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0 &&
		slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry)
}
