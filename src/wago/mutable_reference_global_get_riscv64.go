//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

var rv64MutableFuncrefGetCodeDigest = [sha256.Size]byte{0xf2, 0x8e, 0x26, 0x87, 0xa5, 0x9c, 0x8d, 0x50, 0xce, 0x32, 0x3b, 0x5f, 0x53, 0x90, 0x88, 0x08, 0x77, 0xa2, 0xf9, 0x1a, 0x58, 0x73, 0xc0, 0x10, 0x9e, 0x63, 0x30, 0x11, 0x73, 0xb4, 0x6f, 0xd6}

func rv64MutableFuncrefGlobalGetExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 620 && sha256.Sum256(c.code) == rv64MutableFuncrefGetCodeDigest
}

// rv64MutableFuncrefGlobalGet authenticates the first executable mutable
// funcref-import product. The sole function loads the imported cell and reduces
// it to i32 through ref.is_null; no descriptor can escape or be written by guest
// code. The code digest keeps trusted artifacts on that exact lowering.
func rv64MutableFuncrefGlobalGet(c *Compiled) bool {
	if c == nil || c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || len(c.Types) != 1 || len(c.GCTypeDescs) != 1 {
		return false
	}
	if index, ok := c.Exports["is_null"]; !ok || index != 0 {
		return false
	}
	if len(c.GlobalImports) != 1 || len(c.Globals) != 1 || len(c.GlobalExports) != 1 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
		return false
	}
	if index, ok := c.GlobalExports["cell"]; !ok || index != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 || c.Names != nil || c.HasTable || c.HasMemory || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	sig := c.Funcs[0]
	funcType := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}
	if len(sig.Params) != 0 || len(sig.Results) != 1 || sig.Results[0] != ValI32 || !sig.HasTypeIndex || sig.TypeIndex != 0 || sig.unsafeCrossTail || c.FuncTypeID[0] != wasm.StructuralFuncTypeKey(funcType) {
		return false
	}
	typ := c.Types[0]
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	if typ.RecGroup != 0 || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Params) != 0 || !slices.Equal(typ.Results, []ValueTypeDescriptor{i32}) || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
		return false
	}
	desc := c.GCTypeDescs[0]
	if desc.ID != 0 || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imp, global := c.GlobalImports[0], c.Globals[0]
	exact, err := c.globalExactType(0)
	if err != nil || exact != plain || c.ValueTypes[0] != plain || !imp.HasValueType || imp.ValueTypeIndex != 0 || imp.Type != ValFuncRef || !imp.Mutable || global.Type != ValFuncRef || !global.Mutable || !global.HasValueType || global.ValueTypeIndex != 0 {
		return false
	}
	if global.HasInitFunc || global.InitFunc != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) {
		return false
	}
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal
	return c.requiredFeatures == required && c.requiredAMD64Features == 0 && compiledStructuralRequiredFeatures(c) == required && len(c.Entry) == 1 && c.Entry[0] == 0 && len(c.InternalEntry) == 1 && c.InternalEntry[0] == 0 && rv64MutableFuncrefGlobalGetExecutionCode(c)
}
