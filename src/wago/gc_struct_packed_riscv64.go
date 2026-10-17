//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCStructPackedCodeDigest = [sha256.Size]byte{0xfc, 0x9d, 0x20, 0xbc, 0xd8, 0x67, 0xfe, 0x44, 0xd8, 0x88, 0x77, 0x70, 0x3f, 0x37, 0x97, 0xca, 0x0e, 0xd9, 0x53, 0xfa, 0xf5, 0x46, 0x39, 0x3c, 0xe1, 0x3b, 0xf9, 0xdd, 0x79, 0x17, 0x97, 0x9f}

func rv64GCStructPackedExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 8876 && sha256.Sum256(c.code) == rv64GCStructPackedCodeDigest
}

func rv64GCStructPackedMetadata(c *Compiled, requireInitializers bool) bool {
	if c == nil || !rv64GCStructPackedExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		(c.stagedGCStructProduct() != stagedGCStructPacked && c.stagedGCStructProduct() != stagedGCStructGeneric) || c.stagedGCArrayProduct() != 0 || c.NeedsFuncRefDescs ||
		c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	wantEntry := []int{0, 784, 1568, 2352, 3136, 3920, 4704, 5488, 6272, 7168}
	if !slices.Equal(c.Entry, wantEntry) || !slices.Equal(c.InternalEntry, wantEntry) || c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 ||
		len(c.Funcs) != 10 || len(c.FuncTypeID) != 10 || len(c.Types) != 3 || len(c.GCTypeDescs) != 3 || len(c.Exports) != 10 || len(c.GlobalExports) != 2 || len(c.Globals) != 2 || len(c.GlobalImports) != 0 ||
		len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 ||
		len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 ||
		c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	for name, index := range map[string]int{"get_packed_g0_0": 0, "get_packed_g1_0": 1, "get_packed_g0_1": 2, "get_packed_g1_1": 3, "get_packed_g0_2": 4, "get_packed_g1_2": 5, "get_packed_g0_3": 6, "get_packed_g1_3": 7, "set_get_packed_g0_1": 8, "set_get_packed_g0_3": 9} {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	if got, ok := c.GlobalExports["g0"]; !ok || got != 0 {
		return false
	}
	if got, ok := c.GlobalExports["g1"]; !ok || got != 1 {
		return false
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{
			{Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8}}, {Mutable: true, Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8}},
			{Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI16}}, {Mutable: true, Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI16}},
		}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{i32, i32}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}, Results: []ValueTypeDescriptor{i32, i32}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes != want.HasDescribes || got.Describes != want.Describes ||
			got.HasDescriptor != want.HasDescriptor || got.Descriptor != want.Descriptor || got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) ||
			!slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	ref0 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	if c.ValueTypes[0] != ref0 {
		return false
	}
	for _, g := range c.Globals {
		if g.Type != ValAnyRef || !g.HasValueType || g.ValueTypeIndex != 0 || g.Mutable || g.Bits != 0 || g.V128 != (V128{}) ||
			g.HasInitGlobal || g.InitGlobal != 0 || g.HasInitFunc || g.InitFunc != 0 || len(g.InitExpr) != 0 {
			return false
		}
	}
	type1 := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32, wasm.I32}}
	type2 := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32, wasm.I32}}
	for i, f := range c.Funcs {
		wantType, wantKey, wantParams := uint32(1), wasm.StructuralFuncTypeKey(type1), []ValType(nil)
		if i >= 8 {
			wantType, wantKey, wantParams = 2, wasm.StructuralFuncTypeKey(type2), []ValType{ValI32}
		}
		if !slices.Equal(f.Params, wantParams) || !slices.Equal(f.Results, []ValType{ValI32, ValI32}) || f.TypeIndex != wantType || !f.HasTypeIndex || f.unsafeCrossTail || c.FuncTypeID[i] != wantKey {
			return false
		}
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI8}, {Kind: corergc.StorageI8, Offset: 1}, {Kind: corergc.StorageI16, Offset: 2}, {Kind: corergc.StorageI16, Offset: 4}}, Size: 6, Align: 2, Final: true},
		{ID: 1, Kind: corergc.KindFunc, Final: true}, {ID: 2, Kind: corergc.KindFunc, Final: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size ||
			got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	if c.stagedFeatures() != CoreFeatureTypedFunctionReferences|CoreFeatureGC || c.requiredFeatures != CoreFeatureMultiValue|CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC || c.requiredAMD64Features != 0 || c.genericGCFrameRoots() != nil {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil ||
		d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	if requireInitializers {
		return len(d.gcStructGlobals) == 2 && d.gcStructGlobals[0].GlobalIndex == 0 && d.gcStructGlobals[0].TypeID == 0 && d.gcStructGlobals[0].FieldCount == 4 && !d.gcStructGlobals[0].Default &&
			slices.Equal(d.gcStructGlobals[0].Bits, []uint64{0, 1, 2, 3}) && d.gcStructGlobals[1].GlobalIndex == 1 && d.gcStructGlobals[1].TypeID == 0 && d.gcStructGlobals[1].FieldCount == 4 && !d.gcStructGlobals[1].Default && slices.Equal(d.gcStructGlobals[1].Bits, []uint64{254, 255, 65534, 65535})
	}
	return len(d.gcStructGlobals) == 0
}

func rv64GCStructPacked(c *Compiled) bool { return rv64GCStructPackedMetadata(c, true) }

func railshotRestoreGCStructPackedArtifact(c *Compiled) {
	if !rv64GCStructPackedMetadata(c, false) {
		return
	}
	c.memoryDir.gcStructGlobals = []gcStructGlobalInit{
		{GlobalIndex: 0, TypeID: 0, FieldCount: 4, Bits: []uint64{0, 1, 2, 3}},
		{GlobalIndex: 1, TypeID: 0, FieldCount: 4, Bits: []uint64{254, 255, 65534, 65535}},
	}
}
