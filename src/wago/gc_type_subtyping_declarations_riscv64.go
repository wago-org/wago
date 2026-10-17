//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func rv64GCTypeSubtypingDeclarationsProfile(c *Compiled) uint8 {
	if c == nil {
		return 0
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	i64 := ValueTypeDescriptor{Kind: ValueTypeI64}
	anyref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	any := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	structref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapStruct}}}
	refType := func(index uint32, nullable bool) ValueTypeDescriptor {
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: nullable, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: index}}}
	}
	field := func(value ValueTypeDescriptor, mutable bool) FieldTypeDescriptor {
		return FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: value}, Mutable: mutable}
	}
	profiles := []struct {
		types []DefinedTypeDescriptor
		descs []nativegc.TypeDesc
	}{
		{
			types: []DefinedTypeDescriptor{
				{RecGroup: 0, Kind: CompositeTypeArray, Array: field(i32, false)},
				{RecGroup: 1, Supers: []uint32{0}, Kind: CompositeTypeArray, Array: field(i32, false)},
				{RecGroup: 2, Kind: CompositeTypeArray, Array: field(anyref, false)},
				{RecGroup: 3, Kind: CompositeTypeArray, Array: field(refType(0, true), false)},
				{RecGroup: 4, Kind: CompositeTypeArray, Array: field(refType(1, false), false)},
				{RecGroup: 5, Kind: CompositeTypeArray, Array: field(i32, true)},
				{RecGroup: 6, Supers: []uint32{5}, Kind: CompositeTypeArray, Array: field(i32, true)},
			},
			descs: []nativegc.TypeDesc{
				{ID: 0, Kind: corergc.KindArray, Elem: corergc.StorageI32, ElemSize: 4, Align: 4},
				{ID: 1, Kind: corergc.KindArray, Elem: corergc.StorageI32, ElemSize: 4, Align: 4, Super: 0, HasSuper: true},
				{ID: 2, Kind: corergc.KindArray, Elem: corergc.StorageRefNull, ElemSize: 4, Align: 4, HasRefs: true},
				{ID: 3, Kind: corergc.KindArray, Elem: corergc.StorageRefNull, ElemSize: 4, Align: 4, HasRefs: true},
				{ID: 4, Kind: corergc.KindArray, Elem: corergc.StorageRef, ElemSize: 4, Align: 4, HasRefs: true},
				{ID: 5, Kind: corergc.KindArray, Elem: corergc.StorageI32, ElemSize: 4, Align: 4},
				{ID: 6, Kind: corergc.KindArray, Elem: corergc.StorageI32, ElemSize: 4, Align: 4, Super: 5, HasSuper: true},
			},
		},
		{
			types: []DefinedTypeDescriptor{
				{RecGroup: 0, Kind: CompositeTypeStruct},
				{RecGroup: 1, Supers: []uint32{0}, Kind: CompositeTypeStruct},
				{RecGroup: 2, Supers: []uint32{1}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i32, false)}},
				{RecGroup: 3, Supers: []uint32{2}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i32, false), field(refType(0, true), false)}},
				{RecGroup: 4, Supers: []uint32{3}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i32, false), field(refType(0, false), false), field(i64, true)}},
				{RecGroup: 5, Supers: []uint32{4}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i32, false), field(refType(1, false), false), field(i64, true)}},
			},
			descs: []nativegc.TypeDesc{
				{ID: 0, Kind: corergc.KindStruct, Align: 1},
				{ID: 1, Kind: corergc.KindStruct, Align: 1, Super: 0, HasSuper: true},
				{ID: 2, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}}, Size: 4, Align: 4, Super: 1, HasSuper: true},
				{ID: 3, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}, {Kind: corergc.StorageRefNull, Offset: 4}}, Size: 8, Align: 4, HasRefs: true, Super: 2, HasSuper: true},
				{ID: 4, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}, {Kind: corergc.StorageRef, Offset: 4}, {Kind: corergc.StorageI64, Offset: 8}}, Size: 16, Align: 8, HasRefs: true, Super: 3, HasSuper: true},
				{ID: 5, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}, {Kind: corergc.StorageRef, Offset: 4}, {Kind: corergc.StorageI64, Offset: 8}}, Size: 16, Align: 8, HasRefs: true, Super: 4, HasSuper: true},
			},
		},
		{
			types: []DefinedTypeDescriptor{
				{RecGroup: 0, Kind: CompositeTypeStruct},
				{RecGroup: 1, Supers: []uint32{0}, Kind: CompositeTypeStruct},
				{RecGroup: 2, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{refType(1, false)}, Results: []ValueTypeDescriptor{anyref}},
				{RecGroup: 3, Supers: []uint32{2}, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{refType(0, false)}, Results: []ValueTypeDescriptor{any}},
				{RecGroup: 4, Supers: []uint32{3}, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{refType(0, true)}, Results: []ValueTypeDescriptor{refType(0, false)}},
				{RecGroup: 5, Supers: []uint32{4}, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{structref}, Results: []ValueTypeDescriptor{refType(1, false)}},
			},
			descs: []nativegc.TypeDesc{
				{ID: 0, Kind: corergc.KindStruct, Align: 1},
				{ID: 1, Kind: corergc.KindStruct, Align: 1, Super: 0, HasSuper: true},
				{ID: 2, Kind: corergc.KindFunc},
				{ID: 3, Kind: corergc.KindFunc, Super: 2, HasSuper: true},
				{ID: 4, Kind: corergc.KindFunc, Super: 3, HasSuper: true},
				{ID: 5, Kind: corergc.KindFunc, Super: 4, HasSuper: true},
			},
		},
		{
			types: []DefinedTypeDescriptor{
				{RecGroup: 0, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(anyref, false)}},
				{RecGroup: 1, Supers: []uint32{0}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(refType(1, false), false)}},
				{RecGroup: 2, Supers: []uint32{1}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(refType(1, false), false), field(i32, false)}},
			},
			descs: []nativegc.TypeDesc{
				{ID: 0, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageRefNull}}, Size: 4, Align: 4, HasRefs: true},
				{ID: 1, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageRef}}, Size: 4, Align: 4, HasRefs: true, Super: 0, HasSuper: true},
				{ID: 2, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageRef}, {Kind: corergc.StorageI32, Offset: 4}}, Size: 8, Align: 4, HasRefs: true, Super: 1, HasSuper: true},
			},
		},
		{
			types: []DefinedTypeDescriptor{
				{RecGroup: 0, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i32, false), field(refType(0, false), false)}},
				{RecGroup: 1, Supers: []uint32{0}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i32, false), field(refType(2, false), false)}},
				{RecGroup: 1, Supers: []uint32{0}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i32, false), field(refType(1, false), false)}},
			},
			descs: []nativegc.TypeDesc{
				{ID: 0, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}, {Kind: corergc.StorageRef, Offset: 4}}, Size: 8, Align: 4, HasRefs: true},
				{ID: 1, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}, {Kind: corergc.StorageRef, Offset: 4}}, Size: 8, Align: 4, HasRefs: true, Super: 0, HasSuper: true},
				{ID: 2, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}, {Kind: corergc.StorageRef, Offset: 4}}, Size: 8, Align: 4, HasRefs: true, Super: 0, HasSuper: true},
			},
		},
		{
			types: []DefinedTypeDescriptor{
				{RecGroup: 0, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i32, false), field(refType(1, false), false)}},
				{RecGroup: 0, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i64, false), field(refType(0, false), false)}},
				{RecGroup: 1, Supers: []uint32{1}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i64, false), field(refType(0, false), false), field(i32, false)}},
				{RecGroup: 1, Supers: []uint32{0}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i32, false), field(refType(1, false), false), field(i32, false)}},
				{RecGroup: 1, Supers: []uint32{1}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field(i64, false), field(refType(3, false), false), field(i32, false)}},
			},
			descs: []nativegc.TypeDesc{
				{ID: 0, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}, {Kind: corergc.StorageRef, Offset: 4}}, Size: 8, Align: 4, HasRefs: true},
				{ID: 1, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI64}, {Kind: corergc.StorageRef, Offset: 8}}, Size: 16, Align: 8, HasRefs: true},
				{ID: 2, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI64}, {Kind: corergc.StorageRef, Offset: 8}, {Kind: corergc.StorageI32, Offset: 12}}, Size: 16, Align: 8, HasRefs: true, Super: 1, HasSuper: true},
				{ID: 3, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}, {Kind: corergc.StorageRef, Offset: 4}, {Kind: corergc.StorageI32, Offset: 8}}, Size: 12, Align: 4, HasRefs: true, Super: 0, HasSuper: true},
				{ID: 4, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI64}, {Kind: corergc.StorageRef, Offset: 8}, {Kind: corergc.StorageI32, Offset: 12}}, Size: 16, Align: 8, HasRefs: true, Super: 1, HasSuper: true},
			},
		},
	}
	for profileIndex, profile := range profiles {
		if len(c.Types) != len(profile.types) || len(c.GCTypeDescs) != len(profile.descs) {
			continue
		}
		matched := true
		for i, want := range profile.types {
			got := c.Types[i]
			if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes != want.HasDescribes || got.Describes != want.Describes ||
				got.HasDescriptor != want.HasDescriptor || got.Descriptor != want.Descriptor || got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) ||
				!slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		for i, want := range profile.descs {
			got := c.GCTypeDescs[i]
			if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize ||
				got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
				matched = false
				break
			}
		}
		if matched {
			return uint8(profileIndex + 1)
		}
	}
	return 0
}

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
		rv64GCTypeSubtypingDeclarationsProfile(c) == 0 || len(c.ValueTypes) != 0 || len(c.Exports) != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 ||
		c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport ||
		c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 ||
		len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" ||
		c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 || c.stagedGCStructProduct() != 0 || c.stagedGCArrayProduct() != 0 ||
		c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 {
		return false
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
