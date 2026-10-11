//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"crypto/sha256"
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCArrayPackedDataCodeDigest = [sha256.Size]byte{0x14, 0xcf, 0xa1, 0x40, 0x4f, 0xc3, 0x21, 0xc8, 0xac, 0x7e, 0x46, 0x35, 0x49, 0x1a, 0x89, 0x82, 0x07, 0xb8, 0x8a, 0x25, 0x89, 0xf9, 0xca, 0x93, 0x36, 0xf3, 0x65, 0x76, 0xb9, 0xe7, 0x3f, 0x2f}

func rv64GCArrayPackedDataExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 9308 && sha256.Sum256(c.code) == rv64GCArrayPackedDataCodeDigest
}

func rv64GCArrayPackedData(c *Compiled) bool {
	wantEntry := []int{0, 704, 1424, 2144, 2880, 3600, 4336, 5152, 5968, 6656, 7376}
	if c == nil || !rv64GCArrayPackedDataExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || !slices.Equal(c.Entry, wantEntry) || !slices.Equal(c.InternalEntry, wantEntry) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 11 || len(c.Types) != 10 || len(c.Exports) != 7 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 ||
		c.activeDataCount() != 0 || len(c.PassiveData) != 1 || !bytes.Equal(c.PassiveData[0].Bytes, []byte{0, 1, 2, 0xff, 4}) || len(c.Elems) != 0 || len(c.passiveElems) != 0 ||
		c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	wantExports := map[string]int{"new": 0, "new-overflow": 1, "get_u": 3, "get_s": 5, "set_get": 7, "len": 9, "drop_segs": 10}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	ref0 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	ref1 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 1}}}
	arrayRef := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapArray}}}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8}}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8}}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{ref0}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, ref0}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 4, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 5, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, ref1, i32}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 6, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, i32}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 7, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{arrayRef}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 8, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 9, Final: true, Kind: CompositeTypeFunction},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 || got.Kind != want.Kind ||
			!slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	wantFuncs := []FuncSig{
		{Results: []ValType{ValAnyRef}, TypeIndex: 2, HasTypeIndex: true}, {Results: []ValType{ValAnyRef}, TypeIndex: 2, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValAnyRef}, Results: []ValType{ValI32}, TypeIndex: 3, HasTypeIndex: true}, {Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValAnyRef}, Results: []ValType{ValI32}, TypeIndex: 3, HasTypeIndex: true}, {Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValAnyRef, ValI32}, Results: []ValType{ValI32}, TypeIndex: 5, HasTypeIndex: true}, {Params: []ValType{ValI32, ValI32}, Results: []ValType{ValI32}, TypeIndex: 6, HasTypeIndex: true},
		{Params: []ValType{ValAnyRef}, Results: []ValType{ValI32}, TypeIndex: 7, HasTypeIndex: true}, {Results: []ValType{ValI32}, TypeIndex: 8, HasTypeIndex: true}, {TypeIndex: 9, HasTypeIndex: true},
	}
	wantKeys := []uint64{0x9d924130d291ce1b, 0x9d924130d291ce1b, 0x33ecf62bddd36dcc, 0x57cc9e275fbbb6a5, 0x33ecf62bddd36dcc, 0x57cc9e275fbbb6a5, 0xebcf22e40854897b, 0xbe46e6e8b421c6ad, 0xd8dae22518d445e2, 0x87259896b4921f6c, 0xd60da539b2828050}
	if len(c.FuncTypeID) != len(wantFuncs) {
		return false
	}
	for i, want := range wantFuncs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || !got.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wantKeys[i] {
			return false
		}
	}
	wantDescs := []nativegc.TypeDesc{{ID: 0, Kind: corergc.KindArray, Elem: corergc.StorageI8, ElemSize: 1, Align: 1, Final: true}, {ID: 1, Kind: corergc.KindArray, Elem: corergc.StorageI8, ElemSize: 1, Align: 1, Final: true}}
	for i := 2; i < 10; i++ {
		wantDescs = append(wantDescs, nativegc.TypeDesc{ID: nativegc.TypeID(i), Kind: corergc.KindFunc, Final: true})
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
	product := c.stagedGCArrayProduct()
	return c.stagedGCStructProduct() == 0 && (product == stagedGCArrayProductPackedData || product == stagedGCArrayProductGeneric) && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		!c.usesGCStructHelpers() && c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureBulkMemoryOperations|CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0
}

func railshotRestoreGCArrayPackedDataArtifact(*Compiled) {}
