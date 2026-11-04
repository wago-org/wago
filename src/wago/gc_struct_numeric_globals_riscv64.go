//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func rv64GCStructNumericGlobalsMetadata(c *Compiled, requireInitializers, allowUnrestored bool) bool {
	if c == nil || !c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		c.stagedGCArrayProduct() != 0 || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	product := c.stagedGCStructProduct()
	if product != stagedGCStructNumericGlobals && (!allowUnrestored || product != 0) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 0 || len(c.FuncTypeID) != 0 ||
		len(c.Types) != 1 || len(c.GCTypeDescs) != 1 || len(c.Exports) != 0 || len(c.GlobalExports) != 2 || len(c.Globals) != 2 || len(c.GlobalImports) != 0 ||
		len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 ||
		len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 ||
		c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if got, ok := c.GlobalExports["g0"]; !ok || got != 0 {
		return false
	}
	if got, ok := c.GlobalExports["g1"]; !ok || got != 1 {
		return false
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	wantType := DefinedTypeDescriptor{RecGroup: 0, Final: true, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{
		{Storage: StorageTypeDescriptor{Value: i32}},
		{Mutable: true, Storage: StorageTypeDescriptor{Value: i32}},
	}}
	gotType := c.Types[0]
	if gotType.RecGroup != wantType.RecGroup || gotType.Final != wantType.Final || !slices.Equal(gotType.Supers, wantType.Supers) || gotType.HasDescribes != wantType.HasDescribes || gotType.Describes != wantType.Describes ||
		gotType.HasDescriptor != wantType.HasDescriptor || gotType.Descriptor != wantType.Descriptor || gotType.Kind != wantType.Kind || !slices.Equal(gotType.Params, wantType.Params) ||
		!slices.Equal(gotType.Results, wantType.Results) || !slices.Equal(gotType.Fields, wantType.Fields) || gotType.Array != wantType.Array {
		return false
	}
	ref0 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	if c.ValueTypes[0] != ref0 {
		return false
	}
	for _, g := range c.Globals {
		if g.Type != ValAnyRef || !g.HasValueType || g.ValueTypeIndex != 0 || g.Mutable || g.Bits != 0 || g.V128 != (V128{}) || g.HasInitGlobal || g.InitGlobal != 0 || g.HasInitFunc || g.InitFunc != 0 || len(g.InitExpr) != 0 {
			return false
		}
	}
	wantDesc := nativegc.TypeDesc{ID: 0, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{{Kind: corergc.StorageI32}, {Kind: corergc.StorageI32, Offset: 4}}, Size: 8, Align: 4, Final: true}
	gotDesc := c.GCTypeDescs[0]
	if gotDesc.ID != wantDesc.ID || gotDesc.Kind != wantDesc.Kind || !slices.Equal(gotDesc.Fields, wantDesc.Fields) || gotDesc.Elem != wantDesc.Elem || gotDesc.Size != wantDesc.Size ||
		gotDesc.ElemSize != wantDesc.ElemSize || gotDesc.Align != wantDesc.Align || gotDesc.HasRefs != wantDesc.HasRefs || gotDesc.Final != wantDesc.Final || gotDesc.Super != wantDesc.Super || gotDesc.HasSuper != wantDesc.HasSuper {
		return false
	}
	staged := c.stagedFeatures()
	if (staged != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || staged != 0)) || c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC || c.requiredAMD64Features != 0 || c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	if requireInitializers {
		return len(d.gcStructGlobals) == 2 && d.gcStructGlobals[0].GlobalIndex == 0 && d.gcStructGlobals[0].TypeID == 0 && d.gcStructGlobals[0].FieldCount == 2 && !d.gcStructGlobals[0].Default && slices.Equal(d.gcStructGlobals[0].Bits, []uint64{10, 11}) &&
			d.gcStructGlobals[1].GlobalIndex == 1 && d.gcStructGlobals[1].TypeID == 0 && d.gcStructGlobals[1].FieldCount == 2 && !d.gcStructGlobals[1].Default && slices.Equal(d.gcStructGlobals[1].Bits, []uint64{20, 21})
	}
	return len(d.gcStructGlobals) == 0
}

func rv64GCStructNumericGlobals(c *Compiled) bool {
	return rv64GCStructNumericGlobalsMetadata(c, true, false)
}
