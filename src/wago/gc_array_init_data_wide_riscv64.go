//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"crypto/sha256"
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCArrayInitDataWideCodeDigest = [sha256.Size]byte{0x43, 0x94, 0xfb, 0x40, 0xfd, 0x19, 0x9a, 0x68, 0x7a, 0xcb, 0xc0, 0xef, 0x67, 0xc1, 0x39, 0x0a, 0x8b, 0x5b, 0xea, 0x36, 0x44, 0x0a, 0xea, 0xe5, 0x47, 0x96, 0x0b, 0x23, 0xa9, 0x5d, 0x84, 0x9b}

func rv64GCArrayInitDataWideExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 9960 && sha256.Sum256(c.code) == rv64GCArrayInitDataWideCodeDigest
}

func rv64GCArrayInitDataWideMetadata(c *Compiled) bool {
	wantEntry := []int{0, 784, 1568, 2352, 3136, 3920, 4704, 5488, 6272, 7056, 7840, 8624}
	if c == nil || !rv64GCArrayInitDataWideExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || !slices.Equal(c.Entry, wantEntry) || !slices.Equal(c.InternalEntry, wantEntry) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 12 || len(c.Types) != 3 || len(c.GCTypeDescs) != 3 || len(c.Exports) != 12 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 ||
		c.activeDataCount() != 0 || len(c.PassiveData) != 7 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 ||
		c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	wantData := [][]byte{nil, []byte("1"), []byte("12"), []byte("123"), []byte("1234"), []byte("1234567"), []byte("123456789")}
	for i, want := range wantData {
		if !bytes.Equal(c.PassiveData[i].Bytes, want) {
			return false
		}
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	wantExports := []string{"f0", "f1", "f2", "f3", "f4", "f9", "g0", "g1", "g4", "g7", "g8", "g9"}
	for i, name := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != i {
			return false
		}
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	i64 := ValueTypeDescriptor{Kind: ValueTypeI64}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Value: i32}}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Value: i64}}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeFunction},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 || got.Kind != want.Kind ||
			!slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	if len(c.FuncTypeID) != 12 {
		return false
	}
	for i, got := range c.Funcs {
		if len(got.Params) != 0 || len(got.Results) != 0 || got.TypeIndex != 2 || !got.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != 0xd60da539b2828050 {
			return false
		}
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindArray, Elem: corergc.StorageI32, ElemSize: 4, Align: 4, Final: true},
		{ID: 1, Kind: corergc.KindArray, Elem: corergc.StorageI64, ElemSize: 8, Align: 8, Final: true},
		{ID: 2, Kind: corergc.KindFunc, Final: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	product := c.stagedGCArrayProduct()
	return c.stagedGCStructProduct() == 0 && (product == stagedGCArrayProductInitData || product == stagedGCArrayProductGeneric) && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		!c.usesGCStructHelpers() && c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureBulkMemoryOperations|CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0
}
