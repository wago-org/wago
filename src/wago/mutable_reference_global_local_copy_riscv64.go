//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

// Filled from the finalized public image and checked again for compiler-free
// artifacts. The source compiler has its own pre-wrapper image fingerprint.
var rv64MutableFuncrefLocalCopyCodeDigest = [sha256.Size]byte{0x17, 0x99, 0xa7, 0xc0, 0xec, 0xd2, 0x44, 0x10, 0xa5, 0xc3, 0x6e, 0x78, 0xa8, 0xd2, 0x9f, 0x50, 0x13, 0x25, 0xf2, 0x57, 0x8d, 0x9b, 0xa6, 0xb7, 0xf8, 0x39, 0xa6, 0x75, 0xcc, 0x28, 0x8e, 0xf2}

func rv64MutableFuncrefGlobalLocalCopyExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 876 && sha256.Sum256(c.code) == rv64MutableFuncrefLocalCopyCodeDigest
}

// rv64MutableFuncrefGlobalLocalCopy authenticates the first mixed local/imported
// mutable-reference product. The sole private cell is initialized to function
// zero and can only be copied into imported mutable target zero through the
// parked ownership transaction.
func rv64MutableFuncrefGlobalLocalCopy(c *Compiled) bool {
	if c == nil || c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || len(c.Types) != 1 || len(c.GCTypeDescs) != 1 {
		return false
	}
	if index, ok := c.Exports["copy_local"]; !ok || index != 0 {
		return false
	}
	if len(c.GlobalImports) != 1 || len(c.Globals) != 2 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 || c.Names != nil || c.HasTable || c.HasMemory || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	sig := c.Funcs[0]
	funcType := &wasm.CompType{Kind: wasm.CompFunc}
	if len(sig.Params) != 0 || len(sig.Results) != 0 || !sig.HasTypeIndex || sig.TypeIndex != 0 || sig.unsafeCrossTail || c.FuncTypeID[0] != wasm.StructuralFuncTypeKey(funcType) {
		return false
	}
	typ := c.Types[0]
	if typ.RecGroup != 0 || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Params) != 0 || len(typ.Results) != 0 || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
		return false
	}
	desc := c.GCTypeDescs[0]
	if desc.ID != 0 || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imp, imported, local := c.GlobalImports[0], c.Globals[0], c.Globals[1]
	exactImport, err := c.globalExactType(0)
	if err != nil || exactImport != plain || c.ValueTypes[0] != plain || imp.Module != "env" || imp.Name != "target" || !imp.HasValueType || imp.ValueTypeIndex != 0 || imp.Type != ValFuncRef || !imp.Mutable || imported.Type != ValFuncRef || !imported.Mutable || !imported.HasValueType || imported.ValueTypeIndex != 0 {
		return false
	}
	if imported.HasInitFunc || imported.InitFunc != 0 || imported.HasInitGlobal || imported.InitGlobal != 0 || len(imported.InitExpr) != 0 || imported.Bits != 0 || imported.V128 != (V128{}) {
		return false
	}
	exactLocal, err := c.globalExactType(1)
	if err != nil || exactLocal != plain || local.Type != ValFuncRef || !local.Mutable || !local.HasValueType || local.ValueTypeIndex != 0 || !local.HasInitFunc || local.InitFunc != 0 || local.HasInitGlobal || local.InitGlobal != 0 || len(local.InitExpr) != 0 || local.Bits != 0 || local.V128 != (V128{}) {
		return false
	}
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal
	return c.requiredFeatures == required && c.requiredAMD64Features == 0 && compiledStructuralRequiredFeatures(c) == required && slices.Equal(c.Entry, []int{0}) && slices.Equal(c.InternalEntry, []int{0}) && rv64MutableFuncrefGlobalLocalCopyExecutionCode(c)
}
