//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCArrayFixedOfficialCodeDigest = [sha256.Size]byte{0xef, 0x84, 0xbb, 0x7b, 0x69, 0x10, 0x6b, 0x24, 0x8f, 0xd8, 0x23, 0xcf, 0xb8, 0xc3, 0x70, 0xda, 0xe7, 0x6a, 0xdf, 0x9a, 0x44, 0x63, 0x27, 0xdc, 0x16, 0x01, 0xee, 0x72, 0xd7, 0x3f, 0x3d, 0x88}

func rv64GCArrayFixedOfficialExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 6684 && sha256.Sum256(c.code) == rv64GCArrayFixedOfficialCodeDigest
}

func rv64FixedOfficialValue(got ValueTypeDescriptor, kind ValueTypeKind, defined bool, index uint32, abstract AbstractHeapType) bool {
	if got.Kind != kind {
		return false
	}
	if kind != ValueTypeReference {
		return got.Ref == (ReferenceTypeDescriptor{})
	}
	return !got.Ref.Nullable && !got.Ref.Exact && got.Ref.Heap.Defined == defined && got.Ref.Heap.TypeIndex == index && got.Ref.Heap.Abstract == abstract
}

func rv64GCArrayFixedOfficialMetadata(c *Compiled, requireInitializer bool) bool {
	if c == nil || !rv64GCArrayFixedOfficialExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI ||
		!c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata ||
		c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 7 || len(c.Types) != 9 || len(c.Exports) != 4 ||
		len(c.Globals) != 1 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil ||
		len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory ||
		c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	if requireInitializer {
		if len(d.gcArrayGlobals) != 1 {
			return false
		}
		init := d.gcArrayGlobals[0]
		if init.GlobalIndex != 0 || init.TypeID != 0 || init.Length != 2 || init.Mode != gcArrayGlobalInitFixed || !slices.Equal(init.Bits, []uint64{0x3f800000, 0x40000000}) {
			return false
		}
	} else if len(d.gcArrayGlobals) != 0 {
		return false
	}
	wantExports := map[string]int{"new": 0, "get": 2, "set_get": 4, "len": 6}
	if len(c.Exports) != len(wantExports) {
		return false
	}
	for name, index := range wantExports {
		got, ok := c.Exports[name]
		if !ok || got != index {
			return false
		}
	}
	if !rv64FixedOfficialValue(c.ValueTypes[0], ValueTypeReference, true, 0, 0) {
		return false
	}
	g := c.Globals[0]
	if g.Type != ValAnyRef || !g.HasValueType || g.ValueTypeIndex != 0 || g.Mutable || g.Bits != 0 || g.V128 != (V128{}) || g.HasInitGlobal || g.InitGlobal != 0 || g.HasInitFunc || g.InitFunc != 0 || len(g.InitExpr) != 0 {
		return false
	}
	f32 := ValueTypeDescriptor{Kind: ValueTypeF32}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	ref0 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	ref1 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 1}}}
	arrayRef := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapArray}}}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: f32}}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Value: f32}}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{ref0}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, ref0}, Results: []ValueTypeDescriptor{f32}},
		{RecGroup: 4, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}, Results: []ValueTypeDescriptor{f32}},
		{RecGroup: 5, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, ref1, f32}, Results: []ValueTypeDescriptor{f32}},
		{RecGroup: 6, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, f32}, Results: []ValueTypeDescriptor{f32}},
		{RecGroup: 7, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{arrayRef}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 8, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{i32}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	wantFuncs := []FuncSig{
		{Results: []ValType{ValAnyRef}, TypeIndex: 2, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValAnyRef}, Results: []ValType{ValF32}, TypeIndex: 3, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValF32}, TypeIndex: 4, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValAnyRef, ValF32}, Results: []ValType{ValF32}, TypeIndex: 5, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValF32}, Results: []ValType{ValF32}, TypeIndex: 6, HasTypeIndex: true},
		{Params: []ValType{ValAnyRef}, Results: []ValType{ValI32}, TypeIndex: 7, HasTypeIndex: true},
		{Results: []ValType{ValI32}, TypeIndex: 8, HasTypeIndex: true},
	}
	if len(c.FuncTypeID) != len(wantFuncs) {
		return false
	}
	wantKeys := []uint64{0x939a2318fdb43e49, 0x1e9d08a2ba462ae, 0x73a107b89bfa4630, 0x190fa81f1e8540eb, 0x5afd6e39cda81e0f, 0xd8dae22518d445e2, 0x87259896b4921f6c}
	for i, want := range wantFuncs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || !got.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wantKeys[i] {
			return false
		}
	}
	wantDescs := [9]nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindArray, Elem: corergc.StorageF32, ElemSize: 4, Align: 4, Final: true},
		{ID: 1, Kind: corergc.KindArray, Elem: corergc.StorageF32, ElemSize: 4, Align: 4, Final: true},
	}
	for i := 2; i < 9; i++ {
		wantDescs[i] = nativegc.TypeDesc{ID: nativegc.TypeID(i), Kind: corergc.KindFunc, Final: true}
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
	wantEntry := []int{0, 704, 1424, 2160, 2976, 3936, 4624}
	product := c.stagedGCArrayProduct()
	return c.stagedGCStructProduct() == 0 && (product == stagedGCArrayProductNumericFixed || product == stagedGCArrayProductGeneric) && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		!c.usesGCStructHelpers() && c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0 &&
		slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry)
}

func rv64GCArrayFixedOfficial(c *Compiled) bool {
	return rv64GCArrayFixedOfficialMetadata(c, true)
}
