//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"crypto/sha256"
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCArrayCopyCodeDigest = [sha256.Size]byte{0x46, 0x4e, 0x41, 0xc2, 0x69, 0x52, 0x87, 0x0f, 0xfc, 0x53, 0x83, 0x2a, 0x2b, 0x5d, 0x13, 0xb2, 0xfb, 0x40, 0xb8, 0xb1, 0xc6, 0xdd, 0x35, 0x61, 0xc2, 0xb1, 0xb9, 0xac, 0x70, 0xca, 0x89, 0x52}

func rv64GCArrayCopyExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 5564 && sha256.Sum256(c.code) == rv64GCArrayCopyCodeDigest
}

func rv64GCArrayCopyMetadata(c *Compiled, requireInitializers bool) bool {
	wantEntry := []int{0, 720, 1456, 2192, 2960, 3824}
	if c == nil || !rv64GCArrayCopyExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || !slices.Equal(c.Entry, wantEntry) || !slices.Equal(c.InternalEntry, wantEntry) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 6 || len(c.Types) != 5 || len(c.GCTypeDescs) != 5 || len(c.Exports) != 6 ||
		len(c.Globals) != 2 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 2 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 ||
		c.activeDataCount() != 0 || len(c.PassiveData) != 1 || !bytes.Equal(c.PassiveData[0].Bytes, []byte("abcdefghijkl")) || len(c.Elems) != 0 || len(c.passiveElems) != 0 ||
		c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	if requireInitializers {
		if len(d.gcArrayGlobals) != 2 || d.gcArrayGlobals[0].GlobalIndex != 0 || d.gcArrayGlobals[0].TypeID != 0 || d.gcArrayGlobals[0].Length != 12 || d.gcArrayGlobals[0].Mode != gcArrayGlobalInitUniform || !slices.Equal(d.gcArrayGlobals[0].Bits, []uint64{10}) ||
			d.gcArrayGlobals[1].GlobalIndex != 1 || d.gcArrayGlobals[1].TypeID != 1 || d.gcArrayGlobals[1].Length != 12 || d.gcArrayGlobals[1].Mode != gcArrayGlobalInitDefault || len(d.gcArrayGlobals[1].Bits) != 0 {
			return false
		}
	} else if len(d.gcArrayGlobals) != 0 {
		return false
	}
	wantExports := map[string]int{"array_get_nth": 0, "array_copy-null-left": 1, "array_copy-null-right": 2, "array_copy": 3, "array_copy_overlap_test-1": 4, "array_copy_overlap_test-2": 5}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	ref := func(index uint32) ValueTypeDescriptor {
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: index}}}
	}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8}}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8}}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction},
		{RecGroup: 4, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, i32, i32}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 || got.Kind != want.Kind ||
			!slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	if !slices.Equal(c.ValueTypes, []ValueTypeDescriptor{ref(0), ref(1)}) {
		return false
	}
	for i, g := range c.Globals {
		if g.Type != ValAnyRef || !g.HasValueType || g.ValueTypeIndex != uint32(i) || g.Mutable != (i == 1) || g.Bits != 0 || g.V128 != (V128{}) || g.HasInitGlobal || g.InitGlobal != 0 || g.HasInitFunc || g.InitFunc != 0 || len(g.InitExpr) != 0 {
			return false
		}
	}
	wantFuncs := []FuncSig{
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 2, HasTypeIndex: true},
		{TypeIndex: 3, HasTypeIndex: true},
		{TypeIndex: 3, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32, ValI32}, TypeIndex: 4, HasTypeIndex: true},
		{TypeIndex: 3, HasTypeIndex: true},
		{TypeIndex: 3, HasTypeIndex: true},
	}
	wantKeys := []uint64{0x57cc9e275fbbb6a5, 0xd60da539b2828050, 0xd60da539b2828050, 0x0c9bb65950c31eab, 0xd60da539b2828050, 0xd60da539b2828050}
	if len(c.FuncTypeID) != len(wantFuncs) {
		return false
	}
	for i, want := range wantFuncs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || !got.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wantKeys[i] {
			return false
		}
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindArray, Elem: corergc.StorageI8, ElemSize: 1, Align: 1, Final: true},
		{ID: 1, Kind: corergc.KindArray, Elem: corergc.StorageI8, ElemSize: 1, Align: 1, Final: true},
		{ID: 2, Kind: corergc.KindFunc, Final: true}, {ID: 3, Kind: corergc.KindFunc, Final: true}, {ID: 4, Kind: corergc.KindFunc, Final: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	product := c.stagedGCArrayProduct()
	return c.stagedGCStructProduct() == 0 && (product == stagedGCArrayProductBulkCopy || product == stagedGCArrayProductGeneric) && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		!c.usesGCStructHelpers() && c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureBulkMemoryOperations|CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0
}

func rv64GCArrayCopy(c *Compiled) bool { return rv64GCArrayCopyMetadata(c, true) }

func railshotRestoreGCArrayCopyArtifact(c *Compiled) {
	if !rv64GCArrayCopyMetadata(c, false) {
		return
	}
	c.memoryDir.gcArrayGlobals = []gcArrayGlobalInit{
		{GlobalIndex: 0, TypeID: 0, Length: 12, Mode: gcArrayGlobalInitUniform, Bits: []uint64{10}},
		{GlobalIndex: 1, TypeID: 1, Length: 12, Mode: gcArrayGlobalInitDefault},
	}
}
