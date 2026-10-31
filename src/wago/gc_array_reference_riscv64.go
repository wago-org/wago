//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCArrayReferenceCodeDigest = [sha256.Size]byte{0x6d, 0x16, 0x29, 0x30, 0x5d, 0x94, 0xb7, 0x54, 0x3e, 0xf3, 0xee, 0x2a, 0xd3, 0x63, 0x7b, 0xd7, 0xb1, 0x97, 0x9b, 0x67, 0xc0, 0x7a, 0x63, 0x65, 0x8b, 0xa3, 0x76, 0xd1, 0xee, 0x55, 0xcc, 0x18}

func rv64GCArrayReferenceExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 9836 && sha256.Sum256(c.code) == rv64GCArrayReferenceCodeDigest
}

func rv64GCArrayReferenceInitializer(init *gcArrayElementInit) bool {
	return init != nil && init.SegmentIndex == 0 && init.TypeID == 0 && init.Count == 2 && len(init.Values) == 2 &&
		init.Values[0].Mode == gcArrayGlobalInitUniform && init.Values[0].Length == 3 && slices.Equal(init.Values[0].Bits, []uint64{7}) &&
		init.Values[1].Mode == gcArrayGlobalInitFixed && init.Values[1].Length == 2 && slices.Equal(init.Values[1].Bits, []uint64{1, 2})
}

func rv64GCArrayReferenceMetadata(c *Compiled, requireInitializer bool) bool {
	wantEntry := []int{0, 704, 1424, 2128, 2832, 3632, 4400, 5392, 6224, 6912, 7632}
	if c == nil || !rv64GCArrayReferenceExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || !slices.Equal(c.Entry, wantEntry) || !slices.Equal(c.InternalEntry, wantEntry) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 11 || len(c.Types) != 15 || len(c.Exports) != 6 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 ||
		c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 1 || c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 ||
		c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	anyref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	segmentRef := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	if c.ValueTypes[0] != segmentRef {
		return false
	}
	e := c.passiveElems[0]
	if e.TableIndex != 0 || e.RefType != ValAnyRef || !e.HasValueType || e.ValueTypeIndex != 0 || e.Mode != ElemModePassive ||
		e.Offset.Base != 0 || e.Offset.HasGlobal || e.Offset.Global != 0 || len(e.Offset.Expr) != 0 || len(e.Values) != 0 {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 || requireInitializer && !rv64GCArrayReferenceInitializer(d.gcArrayElement) || !requireInitializer && d.gcArrayElement != nil {
		return false
	}
	wantExports := map[string]int{"new": 0, "new-overflow": 1, "get": 5, "set_get": 7, "len": 9, "drop_segs": 10}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	ref := func(index uint32, nullable bool) ValueTypeDescriptor {
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: nullable, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: index}}}
	}
	arrayRef := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapArray}}}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8}}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: ref(0, false)}}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Value: ref(0, false)}}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: ref(0, true)}}},
		{RecGroup: 4, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Value: anyref}}},
		{RecGroup: 5, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{ref(1, false)}},
		{RecGroup: 6, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{ref(3, false)}},
		{RecGroup: 7, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{ref(4, false)}},
		{RecGroup: 8, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, i32, ref(1, false)}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 9, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, i32}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 10, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, i32, ref(2, false), i32}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 11, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, i32, i32}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 12, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{arrayRef}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 13, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 14, Final: true, Kind: CompositeTypeFunction},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 || got.Kind != want.Kind ||
			!slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	wantFuncs := []FuncSig{
		{Results: []ValType{ValAnyRef}, TypeIndex: 5, HasTypeIndex: true}, {Results: []ValType{ValAnyRef}, TypeIndex: 5, HasTypeIndex: true},
		{Results: []ValType{ValAnyRef}, TypeIndex: 6, HasTypeIndex: true}, {Results: []ValType{ValAnyRef}, TypeIndex: 7, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32, ValAnyRef}, Results: []ValType{ValI32}, TypeIndex: 8, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32}, Results: []ValType{ValI32}, TypeIndex: 9, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32, ValAnyRef, ValI32}, Results: []ValType{ValI32}, TypeIndex: 10, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32, ValI32}, Results: []ValType{ValI32}, TypeIndex: 11, HasTypeIndex: true},
		{Params: []ValType{ValAnyRef}, Results: []ValType{ValI32}, TypeIndex: 12, HasTypeIndex: true},
		{Results: []ValType{ValI32}, TypeIndex: 13, HasTypeIndex: true}, {TypeIndex: 14, HasTypeIndex: true},
	}
	wantKeys := []uint64{0xef44958924c77f16, 0xef44958924c77f16, 0x07f2c2f43452c7a5, 0xd936ae3de377ee15, 0xf7d6b3d87f275c93, 0xbe46e6e8b421c6ad, 0x2d29bd92a1fa58ed, 0x1de96d7414d5b5f8, 0xd8dae22518d445e2, 0x87259896b4921f6c, 0xd60da539b2828050}
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
		{ID: 1, Kind: corergc.KindArray, Elem: corergc.StorageRef, ElemSize: 4, Align: 4, HasRefs: true, Final: true},
		{ID: 2, Kind: corergc.KindArray, Elem: corergc.StorageRef, ElemSize: 4, Align: 4, HasRefs: true, Final: true},
		{ID: 3, Kind: corergc.KindArray, Elem: corergc.StorageRefNull, ElemSize: 4, Align: 4, HasRefs: true, Final: true},
		{ID: 4, Kind: corergc.KindArray, Elem: corergc.StorageRefNull, ElemSize: 4, Align: 4, HasRefs: true, Final: true},
	}
	for i := 5; i < 15; i++ {
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
	return c.stagedGCStructProduct() == 0 && (product == stagedGCArrayProductReferenceElements || product == stagedGCArrayProductGeneric) && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		!c.usesGCStructHelpers() && c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureBulkMemoryOperations|CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0
}

func rv64GCArrayReference(c *Compiled) bool { return rv64GCArrayReferenceMetadata(c, true) }
