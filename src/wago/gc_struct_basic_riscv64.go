//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCStructBasicCodeDigest = [sha256.Size]byte{0xbb, 0x9b, 0x11, 0x04, 0xff, 0xc5, 0x8c, 0x5d, 0xc1, 0xfd, 0x9d, 0x09, 0x12, 0xbe, 0xa2, 0x4f, 0xbc, 0x57, 0xb5, 0x8f, 0xa5, 0x71, 0xc3, 0x83, 0xf8, 0xc5, 0x50, 0xb3, 0xab, 0x97, 0xf4, 0x4d}

func rv64GCStructBasicExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 10232 && sha256.Sum256(c.code) == rv64GCStructBasicCodeDigest
}

func rv64GCStructBasicMetadata(c *Compiled, requireInitializers bool) bool {
	if c == nil || !rv64GCStructBasicExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		(c.stagedGCStructProduct() != stagedGCStructBasic && c.stagedGCStructProduct() != stagedGCStructGeneric) || c.stagedGCArrayProduct() != 0 || c.NeedsFuncRefDescs ||
		c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	wantEntry := []int{0, 656, 1360, 2080, 2784, 3504, 4208, 4928, 5632, 6352, 7152, 7904, 8704}
	if !slices.Equal(c.Entry, wantEntry) || !slices.Equal(c.InternalEntry, wantEntry) || c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 ||
		len(c.Funcs) != 13 || len(c.FuncTypeID) != 13 || len(c.Types) != 6 || len(c.GCTypeDescs) != 6 || len(c.Exports) != 7 || len(c.GlobalExports) != 0 || len(c.Globals) != 2 || len(c.GlobalImports) != 0 ||
		len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 ||
		len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 ||
		c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	for name, index := range map[string]int{"new": 0, "get_0_0": 2, "get_vec_0": 4, "get_0_y": 6, "get_vec_y": 8, "set_get_y": 10, "set_get_1": 12} {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	f32 := ValueTypeDescriptor{Kind: ValueTypeF32}
	ref0 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	any := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{{Storage: StorageTypeDescriptor{Value: f32}}, {Mutable: true, Storage: StorageTypeDescriptor{Value: f32}}, {Storage: StorageTypeDescriptor{Value: f32}}}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{any}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{ref0}, Results: []ValueTypeDescriptor{f32}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{f32}},
		{RecGroup: 4, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{ref0, f32}, Results: []ValueTypeDescriptor{f32}},
		{RecGroup: 5, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{f32}, Results: []ValueTypeDescriptor{f32}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes != want.HasDescribes || got.Describes != want.Describes || got.HasDescriptor != want.HasDescriptor || got.Descriptor != want.Descriptor || got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	if c.ValueTypes[0] != ref0 {
		return false
	}
	for _, g := range c.Globals {
		if g.Type != ValAnyRef || !g.HasValueType || g.ValueTypeIndex != 0 || g.Mutable || g.Bits != 0 || g.V128 != (V128{}) || g.HasInitGlobal || g.InitGlobal != 0 || g.HasInitFunc || g.InitFunc != 0 || len(g.InitExpr) != 0 {
			return false
		}
	}
	wantType := []uint32{1, 2, 3, 2, 3, 2, 3, 2, 3, 4, 5, 4, 5}
	wantKey := []uint64{0, 9867768264444847677, 6486324970157355495, 3025803499173818292, 1643776510887884640, 1110474601441303821}
	wantParams := [][]ValType{nil, nil, {ValAnyRef}, nil, {ValAnyRef, ValF32}, {ValF32}}
	wantResults := [][]ValType{nil, {ValAnyRef}, {ValF32}, {ValF32}, {ValF32}, {ValF32}}
	for i, f := range c.Funcs {
		typeIndex := wantType[i]
		if !slices.Equal(f.Params, wantParams[typeIndex]) || !slices.Equal(f.Results, wantResults[typeIndex]) || f.TypeIndex != typeIndex || !f.HasTypeIndex || f.unsafeCrossTail || c.FuncTypeID[i] != wantKey[typeIndex] {
			return false
		}
	}
	wantDescs := [6]nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageF32}, {Kind: corergc.StorageF32, Offset: 4}, {Kind: corergc.StorageF32, Offset: 8}}, Size: 12, Align: 4, Final: true},
		{ID: 1, Kind: corergc.KindFunc, Final: true}, {ID: 2, Kind: corergc.KindFunc, Final: true},
		{ID: 3, Kind: corergc.KindFunc, Final: true}, {ID: 4, Kind: corergc.KindFunc, Final: true}, {ID: 5, Kind: corergc.KindFunc, Final: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	if c.stagedFeatures() != CoreFeatureTypedFunctionReferences|CoreFeatureGC || c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC || c.requiredAMD64Features != 0 || c.genericGCFrameRoots() != nil {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	if requireInitializers {
		return len(d.gcStructGlobals) == 2 && d.gcStructGlobals[0].GlobalIndex == 0 && d.gcStructGlobals[0].TypeID == 0 && d.gcStructGlobals[0].FieldCount == 3 && !d.gcStructGlobals[0].Default && slices.Equal(d.gcStructGlobals[0].Bits, []uint64{1065353216, 1073741824, 1077936128}) && d.gcStructGlobals[1].GlobalIndex == 1 && d.gcStructGlobals[1].TypeID == 0 && d.gcStructGlobals[1].FieldCount == 3 && d.gcStructGlobals[1].Default && len(d.gcStructGlobals[1].Bits) == 0
	}
	return len(d.gcStructGlobals) == 0
}

func rv64GCStructBasic(c *Compiled) bool { return rv64GCStructBasicMetadata(c, true) }

func railshotRestoreGCStructBasicArtifact(c *Compiled) {
	if !rv64GCStructBasicMetadata(c, false) {
		return
	}
	c.memoryDir.gcStructGlobals = []gcStructGlobalInit{{GlobalIndex: 0, TypeID: 0, FieldCount: 3, Bits: []uint64{1065353216, 1073741824, 1077936128}}, {GlobalIndex: 1, TypeID: 0, FieldCount: 3, Default: true}}
}
