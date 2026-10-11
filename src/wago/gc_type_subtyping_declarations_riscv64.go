//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func rv64GCTypeSubtypingDeclarationsMetadata(c *Compiled, allowUnrestored bool) bool {
	if c == nil || !c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || c.usesGCStructHelpers() || c.usesGCArrayHelpers() {
		return false
	}
	product := c.stagedGCTypeSubtypingProduct()
	if product != stagedGCTypeSubtypingDeclarations && (!allowUnrestored || product != 0) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 0 || len(c.FuncTypeID) != 0 ||
		len(c.Types) != 7 || len(c.GCTypeDescs) != 7 || len(c.ValueTypes) != 0 || len(c.Exports) != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 ||
		c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport ||
		c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 ||
		len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" ||
		c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 || c.stagedGCStructProduct() != 0 || c.stagedGCArrayProduct() != 0 ||
		c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 {
		return false
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	anyref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	ref0 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	ref1 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 1}}}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: i32}}},
		{RecGroup: 1, Supers: []uint32{0}, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: i32}}},
		{RecGroup: 2, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: anyref}}},
		{RecGroup: 3, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: ref0}}},
		{RecGroup: 4, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: ref1}}},
		{RecGroup: 5, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Value: i32}}},
		{RecGroup: 6, Supers: []uint32{5}, Kind: CompositeTypeArray, Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Value: i32}}},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || len(got.Params) != 0 || len(got.Results) != 0 || len(got.Fields) != 0 || got.Array != want.Array {
			return false
		}
	}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindArray, Elem: corergc.StorageI32, ElemSize: 4, Align: 4},
		{ID: 1, Kind: corergc.KindArray, Elem: corergc.StorageI32, ElemSize: 4, Align: 4, Super: 0, HasSuper: true},
		{ID: 2, Kind: corergc.KindArray, Elem: corergc.StorageRefNull, ElemSize: 4, Align: 4, HasRefs: true},
		{ID: 3, Kind: corergc.KindArray, Elem: corergc.StorageRefNull, ElemSize: 4, Align: 4, HasRefs: true},
		{ID: 4, Kind: corergc.KindArray, Elem: corergc.StorageRef, ElemSize: 4, Align: 4, HasRefs: true},
		{ID: 5, Kind: corergc.KindArray, Elem: corergc.StorageI32, ElemSize: 4, Align: 4},
		{ID: 6, Kind: corergc.KindArray, Elem: corergc.StorageI32, ElemSize: 4, Align: 4, Super: 5, HasSuper: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || len(got.Fields) != 0 || got.Elem != want.Elem || got.Size != 0 || got.ElemSize != want.ElemSize || got.Align != want.Align ||
			got.HasRefs != want.HasRefs || got.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	staged := c.stagedFeatures()
	if staged != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || staged != 0 && staged != CoreFeatureTypedFunctionReferences) || c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC || c.requiredAMD64Features != 0 {
		return false
	}
	d := c.memoryDir
	return d != nil && len(d.defs) == 0 && len(d.exports) == 0 && d.exactExports && !d.staged && !d.stagedMemory64 && len(d.gcStructGlobals) == 0 &&
		len(d.gcArrayGlobals) == 0 && d.gcArrayElement == nil && d.gcI31TableInit == nil && len(d.ehTags) == 0 && len(d.ehTagExports) == 0
}

func rv64GCTypeSubtypingDeclarations(c *Compiled) bool {
	return rv64GCTypeSubtypingDeclarationsMetadata(c, false)
}

func railshotRestoreGCTypeSubtypingDeclarationsArtifact(c *Compiled) {
	if !rv64GCTypeSubtypingDeclarationsMetadata(c, true) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= c.requiredFeatures & (CoreFeatureTypedFunctionReferences | CoreFeatureGC)
	c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingDeclarations
}
