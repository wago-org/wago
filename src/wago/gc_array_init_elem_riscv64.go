//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCArrayInitElemCodeDigest = [sha256.Size]byte{0x4c, 0xf6, 0xda, 0x07, 0x79, 0x85, 0x04, 0x63, 0xe4, 0x66, 0x81, 0x00, 0xa5, 0xf4, 0x44, 0xdc, 0xeb, 0x59, 0x3a, 0xec, 0x2e, 0x93, 0x2d, 0x28, 0x88, 0x27, 0xce, 0xd3, 0xb4, 0xc7, 0x36, 0x07}

func rv64GCArrayInitElemExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 6920 && sha256.Sum256(c.code) == rv64GCArrayInitElemCodeDigest
}

func rv64GCArrayInitElemMetadata(c *Compiled, requireInitializers bool) bool {
	wantEntry := []int{0, 576, 4016, 4736, 5488}
	if c == nil || !rv64GCArrayInitElemExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || !slices.Equal(c.Entry, wantEntry) || !slices.Equal(c.InternalEntry, wantEntry) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 5 || len(c.Types) != 5 || len(c.GCTypeDescs) != 5 || len(c.Exports) != 4 ||
		len(c.Globals) != 2 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 3 || c.tableCount() != 1 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 ||
		c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 1 || !c.HasTable || c.TableType != ValFuncRef || !c.TableHasValueType ||
		c.TableValueTypeIndex != 2 || c.TableSize != 1 || c.TableMax != 1 || c.TableHasMax || c.TableAddr64 || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" ||
		c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax || c.HasMemory || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	wantExports := map[string]int{"array_call_nth": 1, "array_init_elem-null": 2, "array_init_elem": 3, "drop_segs": 4}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	ref := func(index uint32, nullable bool) ValueTypeDescriptor {
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: nullable, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: index}}}
	}
	funcref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeFunction},
		{RecGroup: 1, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: ref(0, false)}}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Value: funcref}}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}},
		{RecGroup: 4, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, i32, i32}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	wantFuncs := []FuncSig{
		{TypeIndex: 0, HasTypeIndex: true},
		{Params: []ValType{ValI32}, TypeIndex: 3, HasTypeIndex: true},
		{TypeIndex: 0, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32, ValI32}, TypeIndex: 4, HasTypeIndex: true},
		{TypeIndex: 0, HasTypeIndex: true},
	}
	wantKeys := []uint64{0xd60da539b2828050, 0x7b5f290f8dfcecf1, 0xd60da539b2828050, 0x0c9bb65950c31eab, 0xd60da539b2828050}
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
		{ID: 0, Kind: corergc.KindFunc, Final: true},
		{ID: 1, Kind: corergc.KindArray, Elem: corergc.StorageFuncRef, ElemSize: 8, Align: 8, Final: true},
		{ID: 2, Kind: corergc.KindArray, Elem: corergc.StorageFuncRefNull, ElemSize: 8, Align: 8, Final: true},
		{ID: 3, Kind: corergc.KindFunc, Final: true},
		{ID: 4, Kind: corergc.KindFunc, Final: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	wantValues := []ValueTypeDescriptor{ref(1, false), ref(2, false), funcref}
	if !slices.Equal(c.ValueTypes, wantValues) {
		return false
	}
	for i := range c.Globals {
		g := c.Globals[i]
		if g.Type != ValAnyRef || !g.HasValueType || g.ValueTypeIndex != uint32(i) || g.Mutable || g.Bits != 0 || g.V128 != (V128{}) || g.HasInitGlobal || g.InitGlobal != 0 || g.HasInitFunc || g.InitFunc != 0 || len(g.InitExpr) != 0 {
			return false
		}
	}
	e := c.passiveElems[0]
	if e.TableIndex != 0 || e.RefType != ValFuncRef || e.HasValueType || e.ValueTypeIndex != 0 || e.Mode != ElemModePassive || e.Offset.Base != 0 || e.Offset.HasGlobal || e.Offset.Global != 0 || len(e.Offset.Expr) != 0 || len(e.Values) != 12 {
		return false
	}
	for _, value := range e.Values {
		if value.FuncIndex != 0 || value.GlobalIndex != 0 || len(value.Expr) != 0 || value.Null || value.HasGlobal || value.I31Wrap || value.RepeatPrevious {
			return false
		}
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	if requireInitializers {
		if len(d.gcArrayGlobals) != 2 || d.gcArrayGlobals[0].GlobalIndex != 0 || d.gcArrayGlobals[0].TypeID != 1 || d.gcArrayGlobals[0].Length != 12 || d.gcArrayGlobals[0].Mode != gcArrayGlobalInitFuncUniform || !slices.Equal(d.gcArrayGlobals[0].Bits, []uint64{0}) ||
			d.gcArrayGlobals[1].GlobalIndex != 1 || d.gcArrayGlobals[1].TypeID != 2 || d.gcArrayGlobals[1].Length != 12 || d.gcArrayGlobals[1].Mode != gcArrayGlobalInitDefault || len(d.gcArrayGlobals[1].Bits) != 0 {
			return false
		}
	} else if len(d.gcArrayGlobals) != 0 {
		return false
	}
	product := c.stagedGCArrayProduct()
	return c.stagedGCStructProduct() == 0 && (product == stagedGCArrayProductInitElem || product == stagedGCArrayProductGeneric) && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		!c.usesGCStructHelpers() && c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureBulkMemoryOperations|CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0
}

func rv64GCArrayInitElem(c *Compiled) bool { return rv64GCArrayInitElemMetadata(c, true) }
